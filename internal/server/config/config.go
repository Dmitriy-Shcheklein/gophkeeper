// Package config loads and validates the GophKeeper server
// configuration from command-line flags and environment variables.
//
// Every setting is read from an environment variable first and can be
// overridden by the corresponding command-line flag: flags take
// precedence over the environment. The DSN and JWT secret are required
// and have no defaults; Load fails fast when they are missing.
//
// In Docker deployments the environment variables (DATABASE_DSN,
// JWT_SECRET, ...) are the primary configuration source because the
// image entrypoint runs the binary without arguments.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment variable names read by Load.
const (
	// EnvGRPCAddress holds the gRPC listen address.
	EnvGRPCAddress = "GRPC_ADDRESS"
	// EnvDSN holds the PostgreSQL connection string.
	EnvDSN = "DATABASE_DSN"
	// EnvJWTSecret holds the JWT signing secret.
	EnvJWTSecret = "JWT_SECRET"
	// EnvJWTTTL holds the JWT time-to-live duration.
	EnvJWTTTL = "JWT_TTL"
	// EnvLogLevel holds the slog level name.
	EnvLogLevel = "LOG_LEVEL"
	// EnvMaxDataSize holds the maximum entry payload size in bytes.
	EnvMaxDataSize = "MAX_DATA_SIZE"
	// EnvTLSCertFile holds the path to the TLS certificate (PEM).
	EnvTLSCertFile = "TLS_CERT"
	// EnvTLSKeyFile holds the path to the TLS private key (PEM).
	EnvTLSKeyFile = "TLS_KEY"
	// EnvAutocertDomain holds the public domain for automatic ACME
	// certificate management (Let's Encrypt).
	EnvAutocertDomain = "AUTOCERT_DOMAIN"
	// EnvAutocertCacheDir holds the directory for ACME account and
	// certificate cache.
	EnvAutocertCacheDir = "AUTOCERT_CACHE_DIR"
)

// Default flag values.
const (
	// DefaultAddress is the gRPC listen address used when neither the
	// GRPC_ADDRESS environment variable nor the --address flag is set.
	// The ":host:port" form makes the server reachable from outside
	// the container, which the Docker image relies on.
	DefaultAddress = ":50051"
	// DefaultJWTTTL is the token lifetime used when neither the
	// JWT_TTL environment variable nor the --jwt-ttl flag is set.
	DefaultJWTTTL = 24 * time.Hour
	// DefaultLogLevel is the slog level used when neither the
	// LOG_LEVEL environment variable nor the --log-level flag is set.
	DefaultLogLevel = slog.LevelInfo
	// DefaultMaxDataSize is the maximum accepted entry payload size
	// (1 GiB) used when neither the MAX_DATA_SIZE environment variable
	// nor the --max-data-size flag is set.
	DefaultMaxDataSize = int64(1 << 30)
	// DefaultAutocertCacheDir is the certificate cache directory used
	// when neither the AUTOCERT_CACHE_DIR environment variable nor the
	// --autocert-cache-dir flag is set.
	DefaultAutocertCacheDir = "./certs-cache"
)

// Config is the fully resolved server configuration. Use Load to
// construct it; a zero Config is not valid.
type Config struct {
	// Address is the gRPC listen address, e.g. ":50051".
	Address string
	// DSN is the PostgreSQL connection string in URL form, e.g.
	// "postgres://user:pass@host:5432/db?sslmode=disable". It is
	// required: server startup fails without it. The URL form is
	// required as well because the migrations runner and DSN
	// redaction parse it as a URL; keyword-value DSNs are rejected.
	DSN string
	// JWTSecret is the HMAC secret used to sign access tokens. It is
	// required: server startup fails without it.
	JWTSecret string
	// JWTTTL is the lifetime of issued access tokens.
	JWTTTL time.Duration
	// LogLevel is the minimum slog level for server logs.
	LogLevel slog.Level
	// MaxDataSize is the maximum accepted entry payload size in bytes,
	// enforced on both the unary and the streaming upload paths.
	MaxDataSize int64
	// TLSCertFile is the path to the PEM-encoded TLS certificate chain;
	// TLSKeyFile is the matching PEM-encoded private key. When both are
	// set the gRPC server serves TLS with this pair (the self-signed
	// scenario, e.g. certificates produced by `gophkeeper-server
	// gen-cert`). Exactly one TLS mode must be configured: this pair or
	// AutocertDomain — the server refuses to start in plaintext.
	TLSCertFile string
	// TLSKeyFile is the path to the PEM-encoded TLS private key; see
	// TLSCertFile.
	TLSKeyFile string
	// AutocertDomain is the public domain name for automatic
	// certificate issuance via ACME (Let's Encrypt) using
	// autocert.Manager. Mutually exclusive with TLSCertFile/TLSKeyFile.
	AutocertDomain string
	// AutocertCacheDir is the directory where autocert.Manager stores
	// the ACME account and issued certificates. Only used in the
	// autocert mode.
	AutocertCacheDir string
}

// ErrHelp is returned by Load when the -h or --help flag is requested.
// The flag usage text has already been printed to stdout by the time
// ErrHelp is returned, so the caller should exit successfully without
// printing anything else.
var ErrHelp = errors.New("config: help requested")

// Load resolves the configuration from the given command-line
// arguments and the process environment, with flags taking precedence
// over environment variables. It returns an error describing the first
// problem found: unknown flags, an unparsable --jwt-ttl (or $JWT_TTL),
// an unknown --log-level (or $LOG_LEVEL), a DSN that is not a
// postgres:// URL, or a missing required DSN / JWT secret. When the
// -h or --help flag is requested it returns ErrHelp after printing the
// usage to stdout.
func Load(args []string) (*Config, error) {
	fs := flag.NewFlagSet("gophkeeper-server", flag.ContinueOnError)
	// Flag errors are wrapped and reported by the caller; help output
	// goes to stdout via the custom Usage below.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(os.Stdout, "Usage of %s:\n", fs.Name())
		// PrintDefaults writes to the flag set output, which is
		// discarded for errors; point it at stdout for the usage.
		fs.SetOutput(os.Stdout)
		fs.PrintDefaults()
		fs.SetOutput(io.Discard)
	}

	var (
		address      = fs.String("address", envString(EnvGRPCAddress, DefaultAddress), "gRPC listen address ($"+EnvGRPCAddress+")")
		dsnVal, _    = os.LookupEnv(EnvDSN)
		secretVal, _ = os.LookupEnv(EnvJWTSecret)
		dsn          = fs.String("dsn", dsnVal, "PostgreSQL connection URL, required ($"+EnvDSN+")")
		secret       = fs.String("jwt-secret", secretVal, "JWT signing secret, required ($"+EnvJWTSecret+")")
		ttl          = fs.String("jwt-ttl", envString(EnvJWTTTL, DefaultJWTTTL.String()), "access token lifetime, Go duration ($"+EnvJWTTTL+")")
		logLevel     = fs.String("log-level", envLevelName(DefaultLogLevel), "log level: debug, info, warn or error ($"+EnvLogLevel+")")
		maxDataSize  = fs.Int64("max-data-size", envInt64(EnvMaxDataSize, DefaultMaxDataSize), "maximum entry payload size in bytes ($"+EnvMaxDataSize+")")
		tlsCert      = fs.String("tls-cert", envString(EnvTLSCertFile, ""), "PEM TLS certificate file ($"+EnvTLSCertFile+")")
		tlsKey       = fs.String("tls-key", envString(EnvTLSKeyFile, ""), "PEM TLS private key file ($"+EnvTLSKeyFile+")")
		acmeDomain   = fs.String("autocert-domain", envString(EnvAutocertDomain, ""), "public domain for ACME (Let's Encrypt) certificates ($"+EnvAutocertDomain+")")
		acmeCacheDir = fs.String("autocert-cache-dir", envString(EnvAutocertCacheDir, DefaultAutocertCacheDir), "ACME certificate cache directory ($"+EnvAutocertCacheDir+")")
	)

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, ErrHelp
		}
		return nil, fmt.Errorf("config: parse flags: %w", err)
	}

	tokenTTL, err := time.ParseDuration(*ttl)
	if err != nil {
		return nil, fmt.Errorf("config: parse %s %q: %w", valueSource(fs, "jwt-ttl", EnvJWTTTL), *ttl, err)
	}
	if tokenTTL <= 0 {
		return nil, fmt.Errorf("config: %s %q must be positive", valueSource(fs, "jwt-ttl", EnvJWTTTL), *ttl)
	}

	level, err := parseLevel(valueSource(fs, "log-level", EnvLogLevel), *logLevel)
	if err != nil {
		return nil, err
	}

	if *dsn == "" {
		return nil, fmt.Errorf("config: --dsn (or $%s) is required", EnvDSN)
	}
	if err := validateDSN(*dsn); err != nil {
		return nil, fmt.Errorf("config: invalid --dsn (or $%s): %w", EnvDSN, err)
	}
	if *secret == "" {
		return nil, fmt.Errorf("config: --jwt-secret (or $%s) is required", EnvJWTSecret)
	}
	if *maxDataSize <= 0 {
		return nil, fmt.Errorf("config: --max-data-size (or $%s) must be positive", EnvMaxDataSize)
	}

	switch {
	case *tlsCert != "" && *tlsKey == "":
		return nil, fmt.Errorf("config: --tls-cert (or $%s) is set but --tls-key (or $%s) is missing", EnvTLSCertFile, EnvTLSKeyFile)
	case *tlsCert == "" && *tlsKey != "":
		return nil, fmt.Errorf("config: --tls-key (or $%s) is set but --tls-cert (or $%s) is missing", EnvTLSKeyFile, EnvTLSCertFile)
	case *tlsCert != "" && *acmeDomain != "":
		return nil, fmt.Errorf("config: --tls-cert/--tls-key and --autocert-domain are mutually exclusive TLS modes")
	case *acmeDomain != "" && *acmeCacheDir == "":
		return nil, fmt.Errorf("config: --autocert-cache-dir (or $%s) must not be empty", EnvAutocertCacheDir)
	case *tlsCert == "" && *acmeDomain == "":
		return nil, fmt.Errorf("config: TLS is required: set --tls-cert and --tls-key (e.g. via 'gophkeeper-server gen-cert') or --autocert-domain (or $%s/$%s)", EnvTLSCertFile, EnvAutocertDomain)
	}

	return &Config{
		Address:          *address,
		DSN:              *dsn,
		JWTSecret:        *secret,
		JWTTTL:           tokenTTL,
		LogLevel:         level,
		MaxDataSize:      *maxDataSize,
		TLSCertFile:      *tlsCert,
		TLSKeyFile:       *tlsKey,
		AutocertDomain:   *acmeDomain,
		AutocertCacheDir: acmeCacheDirValue(*acmeDomain, *acmeCacheDir),
	}, nil
}

// acmeCacheDirValue returns the cache dir only in autocert mode so the
// resolved Config does not carry a default cache dir in cert-pair mode.
func acmeCacheDirValue(domain, cacheDir string) string {
	if domain == "" {
		return ""
	}
	return cacheDir
}

// valueSource describes where the value of the named setting came
// from: the command-line flag if it was explicitly set, otherwise the
// environment variable if it is non-empty, otherwise the flag name
// (the default value). It is used to make validation errors point at
// the actual source of an invalid value.
func valueSource(fs *flag.FlagSet, flagName, envName string) string {
	fromFlag := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == flagName {
			fromFlag = true
		}
	})
	if fromFlag {
		return "--" + flagName
	}
	if _, ok := os.LookupEnv(envName); ok {
		return "$" + envName
	}
	return "--" + flagName
}

// envString returns the value of the environment variable name, or
// fallback when it is unset or empty.
func envString(name, fallback string) string {
	return envGet(name, fallback)
}

// envInt64 returns the integer value of the environment variable name,
// or fallback when it is unset, empty or unparsable.
func envInt64(name string, fallback int64) int64 {
	if value, ok := os.LookupEnv(name); ok && value != "" {
		if n, err := strconv.ParseInt(value, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}

// envLevelName returns the canonical slog name of the level given in
// the LOG_LEVEL environment variable, or fallback when it is unset.
func envLevelName(fallback slog.Level) string {
	if value, ok := os.LookupEnv(EnvLogLevel); ok && value != "" {
		return value
	}
	return fallback.String()
}

// envGet returns the value of the named environment variable when it
// is set and non-empty, or fallback otherwise. Use os.LookupEnv
// directly when the caller needs to distinguish between an unset
// variable and an explicitly empty one.
func envGet(name, fallback string) string {
	if value, ok := os.LookupEnv(name); ok && value != "" {
		return value
	}
	return fallback
}

// parseLevel converts a level name (case-insensitive: debug, info,
// warn or error) into a slog.Level.
func parseLevel(source, name string) (slog.Level, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.ToLower(name))); err != nil {
		return 0, fmt.Errorf("config: parse %s %q: %w", source, name, err)
	}
	return level, nil
}

// validateDSN checks that the DSN is a URL with a PostgreSQL scheme.
// The URL form is required because both the migrations runner (which
// swaps the scheme to golang-migrate's pgx5 driver) and DSN redaction
// parse the DSN as a URL.
func validateDSN(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("parse DSN: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return fmt.Errorf("unsupported DSN scheme %q: use a postgres:// URL, keyword-value DSNs are not supported", u.Scheme)
	}
	return nil
}
