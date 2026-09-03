// Package main is the entry point of the GophKeeper gRPC server. It
// loads the configuration, prepares logging, applies database
// migrations, wires the storage, service and transport layers together
// and serves until SIGINT/SIGTERM, then shuts down gracefully.
package main

import (
	"context"
	"errors"
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
	"google.golang.org/grpc"

	"github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
	"github.com/dmitriy/gophkeeper/internal/server/auth"
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
	if err := run(); err != nil {
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

	storage, err := postgres.New(ctx, cfg.DSN)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer storage.Close()

	if err := runMigrations(cfg.DSN, logger); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	jwtManager, err := auth.New(cfg.JWTSecret, cfg.JWTTTL)
	if err != nil {
		return fmt.Errorf("create JWT manager: %w", err)
	}

	authService := service.NewAuthService(storage.Users(), jwtManager)
	entryService := service.NewEntryService(storage.Entries())

	grpcServer := grpc.NewServer(grpc.ChainUnaryInterceptor(
		middleware.NewAuthInterceptor(jwtManager,
			gophkeeperv1.AuthService_Register_FullMethodName,
			gophkeeperv1.AuthService_Login_FullMethodName,
		),
	))
	gophkeeperv1.RegisterAuthServiceServer(grpcServer, transport.NewAuthHandler(authService))
	gophkeeperv1.RegisterEntryServiceServer(grpcServer, transport.NewEntryHandler(entryService))

	listener, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Address, err)
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

// redactDSN hides the password component of a DSN URL, returning a
// generic placeholder when the DSN is not a parseable URL.
func redactDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return redacted
	}
	return u.Redacted()
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
	// scheme; the config requires the DSN to be a URL, so the scheme
	// can simply be swapped.
	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("parse DSN: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return fmt.Errorf("unsupported DSN scheme %q: use a postgres:// URL (required by the migrations runner)", u.Scheme)
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
