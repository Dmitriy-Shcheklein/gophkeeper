// Package main is the entry point of the GophKeeper gRPC server. It
// loads the configuration, prepares logging, applies database
// migrations, wires the storage, service and transport layers together
// and serves until SIGINT/SIGTERM, then shuts down gracefully.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"golang.org/x/crypto/acme/autocert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
	"github.com/dmitriy/gophkeeper/internal/server/auth"
	"github.com/dmitriy/gophkeeper/internal/server/certgen"
	"github.com/dmitriy/gophkeeper/internal/server/config"
	"github.com/dmitriy/gophkeeper/internal/server/middleware"
	"github.com/dmitriy/gophkeeper/internal/server/repository/postgres"
	"github.com/dmitriy/gophkeeper/internal/server/service"
	"github.com/dmitriy/gophkeeper/internal/server/transport"
	"github.com/dmitriy/gophkeeper/migrations"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

// buildDate is set at build time via -ldflags "-X main.buildDate=...".
var buildDate = "unknown"

// gracefulShutdownTimeout bounds GracefulStop before the server is
// stopped forcibly.
const gracefulShutdownTimeout = 10 * time.Second

// redacted is logged in place of secret values.
const redacted = "***"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "gen-cert" {
		if err := runGenCert(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "gophkeeper-server: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		// -h/--help already printed the usage to stdout; exit
		// successfully.
		if errors.Is(err, config.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "gophkeeper-server: %v\n", err)
		os.Exit(1)
	}
}

// run executes the server lifecycle and returns a fatal error, if any.
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return err
	}

	logger := newLogger(cfg.LogLevel)
	logStartupConfig(logger, cfg)

	// A signal during startup (before Serve) must abort the startup
	// instead of being swallowed; blocking steps such as connecting or
	// migrating are checked at each stage boundary below.
	if ctx.Err() != nil {
		logger.Info("shutdown signal received during startup")
		return nil
	}

	storage, err := postgres.New(ctx, cfg.DSN)
	if err != nil {
		if ctx.Err() != nil {
			logger.Info("shutdown signal received during startup")
			return nil
		}
		return fmt.Errorf("connect to database: %w", err)
	}
	defer storage.Close()

	if err := runMigrations(cfg.DSN, logger); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	if signalledDuringStartup(ctx, logger) {
		return nil
	}

	jwtManager, err := auth.New(cfg.JWTSecret, cfg.JWTTTL)
	if err != nil {
		return fmt.Errorf("create JWT manager: %w", err)
	}

	authService := service.NewAuthService(storage.Users(), jwtManager)
	entryService := service.NewEntryServiceWithLimits(storage.Entries(), cfg.MaxDataSize)

	// messageSizeHeadroom covers the protobuf overhead around a
	// maximum-size payload.
	const messageSizeHeadroom = 1 << 20
	msgSize := int(cfg.MaxDataSize) + messageSizeHeadroom

	transportCreds, err := transportCredentials(cfg)
	if err != nil {
		return err
	}

	grpcServer := grpc.NewServer(
		grpc.Creds(transportCreds),
		grpc.MaxRecvMsgSize(msgSize),
		grpc.MaxSendMsgSize(msgSize),
		grpc.ChainUnaryInterceptor(
			middleware.NewAuthInterceptor(jwtManager,
				gophkeeperv1.AuthService_Register_FullMethodName,
				gophkeeperv1.AuthService_Login_FullMethodName,
			),
		),
		grpc.ChainStreamInterceptor(
			middleware.NewAuthStreamInterceptor(jwtManager),
		),
	)
	gophkeeperv1.RegisterAuthServiceServer(grpcServer, transport.NewAuthHandler(authService))
	gophkeeperv1.RegisterEntryServiceServer(grpcServer, transport.NewEntryHandler(entryService))

	listener, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Address, err)
	}
	if signalledDuringStartup(ctx, logger) {
		return nil
	}
	logger.Info("serving gRPC", "address", cfg.Address)

	serveErr := make(chan error, 1)
	go func() { serveErr <- grpcServer.Serve(listener) }()

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	// GracefulStop stops accepting new calls and waits for the
	// in-flight ones; fall back to an immediate Stop when it takes
	// too long.
	done := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
		logger.Info("gRPC server drained")
	case <-time.After(gracefulShutdownTimeout):
		logger.Warn("graceful shutdown timed out, forcing stop")
		grpcServer.Stop()
	}

	storage.Close()
	logger.Info("shutdown complete")
	return nil
}

// transportCredentials builds the server TLS credentials from the
// resolved config: either a static certificate pair or an
// autocert.Manager for automatic ACME issuance. The config loader
// guarantees that exactly one TLS mode is configured; plaintext
// serving is not supported.
func transportCredentials(cfg *config.Config) (credentials.TransportCredentials, error) {
	if cfg.AutocertDomain != "" {
		manager := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(cfg.AutocertDomain),
			Cache:      autocert.DirCache(cfg.AutocertCacheDir),
		}
		return credentials.NewTLS(&tls.Config{
			GetCertificate: manager.GetCertificate,
			MinVersion:     tls.VersionTLS12,
		}), nil
	}
	creds, err := credentials.NewServerTLSFromFile(cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS certificate pair (%s, %s): %w", cfg.TLSCertFile, cfg.TLSKeyFile, err)
	}
	return creds, nil
}

// runGenCert parses gen-cert flags and writes a self-signed
// certificate pair. It runs before the regular server config loading
// so it never requires DATABASE_DSN or JWT_SECRET.
func runGenCert(args []string) error {
	fs := flag.NewFlagSet("gen-cert", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	outDir := fs.String("out-dir", "./certs", "output directory for server.crt and server.key")
	cn := fs.String("cn", "", "certificate CommonName; defaults to the first DNS name")
	years := fs.Int("years", 1, "certificate validity in years")
	var dnsFlags, ipFlags certFlagValues
	fs.Var(&dnsFlags, "dns", "DNS SAN entry, repeatable (e.g. --dns localhost)")
	fs.Var(&ipFlags, "ip", "IP SAN entry, repeatable (e.g. --ip 127.0.0.1)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("gen-cert: parse flags: %w", err)
	}
	if len(dnsFlags) == 0 && len(ipFlags) == 0 {
		return fmt.Errorf("gen-cert: at least one --dns or --ip SAN is required")
	}
	ips := make([]net.IP, 0, len(ipFlags))
	for _, s := range ipFlags {
		ip := net.ParseIP(s)
		if ip == nil {
			return fmt.Errorf("gen-cert: invalid IP SAN %q", s)
		}
		ips = append(ips, ip)
	}
	certPath, keyPath, err := certgen.Generate(certgen.Options{
		OutDir:     *outDir,
		DNSNames:   dnsFlags,
		IPs:        ips,
		Years:      *years,
		CommonName: *cn,
	})
	if err != nil {
		return err
	}
	fmt.Printf("Self-signed TLS certificate pair written:\n  certificate: %s\n  private key: %s\n\n"+
		"Start the server with:\n  --tls-cert %s --tls-key %s\n\n"+
		"Point clients at the certificate:\n  gophkeeper-client --tls-ca %s ...\n",
		certPath, keyPath, certPath, keyPath, certPath)
	return nil
}

// certFlagValues collects repeated flag occurrences (--dns/--ip).
type certFlagValues []string

func (v *certFlagValues) String() string { return fmt.Sprint(*v) }

func (v *certFlagValues) Set(value string) error {
	*v = append(*v, value)
	return nil
}

// newLogger creates the process logger with the given minimum level,
// writing structured text to stderr.
func newLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// logStartupConfig logs the resolved configuration, redacting secrets.
func logStartupConfig(logger *slog.Logger, cfg *config.Config) {
	logger.Info("starting gophkeeper-server",
		"version", version,
		"buildDate", buildDate,
		"address", cfg.Address,
		"dsn", redactDSN(cfg.DSN),
		"jwtSecret", redacted,
		"jwtTTL", cfg.JWTTTL.String(),
		"logLevel", cfg.LogLevel.String(),
	)
}

// redactDSN hides secret components of a DSN URL for logging: the
// userinfo password (via url.URL.Redacted) and a password query
// parameter, which pgx also accepts. A DSN that fails to parse (which
// config validation should make impossible) is replaced entirely.
func redactDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return redacted
	}
	query := u.Query()
	if query.Has("password") {
		// "xxxxx" matches the url.URL.Redacted convention; "***"
		// would be percent-encoded in the query string.
		query.Set("password", "xxxxx")
		u.RawQuery = query.Encode()
	}
	return u.Redacted()
}

// signalledDuringStartup reports whether a termination signal arrived
// before the server began serving, logging the aborted stage when it
// did. The deferred storage close releases the pool.
func signalledDuringStartup(ctx context.Context, logger *slog.Logger) bool {
	if ctx.Err() == nil {
		return false
	}
	logger.Info("shutdown signal received during startup")
	return true
}

// runMigrations applies all pending database migrations embedded in
// the migrations package. Applying an already up-to-date schema is not
// an error.
func runMigrations(dsn string, logger *slog.Logger) error {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("open embedded migrations: %w", err)
	}

	// golang-migrate's pgx/v5 driver is registered under the pgx5
	// scheme. Load guarantees the DSN is a postgres:// URL, so the
	// scheme can simply be swapped.
	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("parse DSN: %w", err)
	}
	u.Scheme = "pgx5"

	m, err := migrate.NewWithSourceInstance("iofs", source, u.String())
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	defer func() {
		srcErr, dbErr := m.Close()
		if srcErr != nil || dbErr != nil {
			logger.Warn("close migrator", "sourceErr", srcErr, "dbErr", dbErr)
		}
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run migrations: %w", err)
	}

	dbVersion, dirty, err := m.Version()
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	logger.Info("database migrations applied", "version", dbVersion, "dirty", dirty)
	return nil
}
