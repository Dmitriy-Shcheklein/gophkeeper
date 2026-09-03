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
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
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
)

// Config is the fully resolved server configuration. Use Load to
// construct it; a zero Config is not valid.
type Config struct {
	// Address is the gRPC listen address, e.g. ":50051".
	Address string
	// DSN is the PostgreSQL connection string in URL form, e.g.
	// "postgres://user:pass@host:5432/db?sslmode=disable". It is
	// required: server startup fails without it. The URL form is
	// required as well because the migrations runner reuses it.
	DSN string
	// JWTSecret is the HMAC secret used to sign access tokens. It is
	// required: server startup fails without it.
	JWTSecret string
	// JWTTTL is the lifetime of issued access tokens.
	JWTTTL time.Duration
	// LogLevel is the minimum slog level for server logs.
	LogLevel slog.Level
}

// Load resolves the configuration from the given command-line
// arguments and the process environment, with flags taking precedence
// over environment variables. It returns an error describing the first
// problem found: unknown flags, an unparsable --jwt-ttl, an unknown
// --log-level, or a missing required DSN / JWT secret.
func Load(args []string) (*Config, error) {
	fs := flag.NewFlagSet("gophkeeper-server", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var (
		address  = fs.String("address", envString(EnvGRPCAddress, DefaultAddress), "gRPC listen address ($"+EnvGRPCAddress+")")
		dsn      = fs.String("dsn", os.Getenv(EnvDSN), "PostgreSQL connection URL, required ($"+EnvDSN+")")
		secret   = fs.String("jwt-secret", os.Getenv(EnvJWTSecret), "JWT signing secret, required ($"+EnvJWTSecret+")")
		ttl      = fs.String("jwt-ttl", envString(EnvJWTTTL, DefaultJWTTTL.String()), "access token lifetime, Go duration ($"+EnvJWTTTL+")")
		logLevel = fs.String("log-level", envLevelName(DefaultLogLevel), "log level: debug, info, warn or error ($"+EnvLogLevel+")")
	)

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("config: parse flags: %w", err)
	}

	tokenTTL, err := time.ParseDuration(*ttl)
	if err != nil {
		return nil, fmt.Errorf("config: parse --jwt-ttl %q: %w", *ttl, err)
	}
	if tokenTTL <= 0 {
		return nil, fmt.Errorf("config: --jwt-ttl %q must be positive", *ttl)
	}

	level, err := parseLevel(*logLevel)
	if err != nil {
		return nil, err
	}

	if *dsn == "" {
		return nil, fmt.Errorf("config: --dsn (or $%s) is required", EnvDSN)
	}
	if *secret == "" {
		return nil, fmt.Errorf("config: --jwt-secret (or $%s) is required", EnvJWTSecret)
	}

	return &Config{
		Address:   *address,
		DSN:       *dsn,
		JWTSecret: *secret,
		JWTTTL:    tokenTTL,
		LogLevel:  level,
	}, nil
}

// envString returns the value of the environment variable name, or
// fallback when it is unset or empty.
func envString(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// envLevelName returns the canonical slog name of the level given in
// the LOG_LEVEL environment variable, or fallback when it is unset.
func envLevelName(fallback slog.Level) string {
	if value := os.Getenv(EnvLogLevel); value != "" {
		return value
	}
	return fallback.String()
}

// parseLevel converts a level name (case-insensitive: debug, info,
// warn or error) into a slog.Level.
func parseLevel(name string) (slog.Level, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.ToLower(name))); err != nil {
		return 0, fmt.Errorf("config: parse --log-level %q: %w", name, err)
	}
	return level, nil
}
