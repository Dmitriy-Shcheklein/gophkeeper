// Package config loads and resolves the GophKeeper client
// configuration.
//
// Every setting is resolved with the precedence flag > environment
// variable > built-in default: the caller passes the flag values it
// collected (empty when the flag was not set), Resolve fills the
// gaps from the environment and falls back to defaults. The client
// keeps its configuration minimal on purpose: the server address and
// the token file location are all a one-shot CLI needs.
package config

import (
	"fmt"
	"os"

	"github.com/dmitriy/gophkeeper/internal/client/cache"
	"github.com/dmitriy/gophkeeper/internal/client/token"
)

// Environment variable names read by Resolve.
const (
	// EnvServer holds the GophKeeper server address (host:port).
	EnvServer = "SERVER_ADDRESS"
	// EnvTokenPath holds the path of the token file.
	EnvTokenPath = "GOPHKEEPER_TOKEN_PATH"
	// EnvCachePath holds the path of the offline cache file.
	EnvCachePath = "GOPHKEEPER_CACHE_PATH"
	// EnvCAPath holds the path of the PEM CA certificate used to
	// verify the TLS server certificate (self-signed scenario).
	EnvCAPath = "GOPHKEEPER_CA_PATH"
)

// DefaultServer is the server address used when neither the
// SERVER_ADDRESS environment variable nor the --server flag is set:
// the local development server.
const DefaultServer = "localhost:50051"

// Config is the fully resolved client configuration. Use Resolve to
// construct it; a zero Config is not valid.
type Config struct {
	// Server is the GophKeeper server address in host:port form,
	// e.g. "localhost:50051".
	Server string
	// TokenPath is the file path the access token is persisted to.
	TokenPath string
	// CachePath is the file path of the encrypted offline cache
	// (the entry snapshot read when the server is unreachable).
	CachePath string
	// CAPath is the optional path to a PEM CA certificate used to
	// verify the server TLS certificate (self-signed scenario,
	// e.g. the server.crt produced by 'gophkeeper-server gen-cert').
	// When empty the server is verified against the system root
	// certificate store. The transport is always TLS.
	CAPath string
}

// Resolve builds the configuration from the given flag values
// (empty when the flag was not set), applying the environment
// variables and the built-in defaults: an empty server falls back to
// $SERVER_ADDRESS and then DefaultServer; an empty token path falls
// back to $GOPHKEEPER_TOKEN_PATH and then token.DefaultPath(); an
// empty cache path falls back to $GOPHKEEPER_CACHE_PATH and then
// cache.DefaultPath(). It fails only when one of the default paths
// cannot be determined (no home directory).
func Resolve(serverFlag, tokenFlag, cacheFlag, caFlag string) (*Config, error) {
	server := firstNonEmpty(serverFlag, os.Getenv(EnvServer), DefaultServer)

	tokenPath, err := resolvePath(tokenFlag, os.Getenv(EnvTokenPath), token.DefaultPath)
	if err != nil {
		return nil, fmt.Errorf("config: resolve default token path: %w", err)
	}
	cachePath, err := resolvePath(cacheFlag, os.Getenv(EnvCachePath), cache.DefaultPath)
	if err != nil {
		return nil, fmt.Errorf("config: resolve default cache path: %w", err)
	}
	caPath := firstNonEmpty(caFlag, os.Getenv(EnvCAPath))

	return &Config{Server: server, TokenPath: tokenPath, CachePath: cachePath, CAPath: caPath}, nil
}

// resolvePath applies the flag > environment > default fallback for
// a file path setting; def produces the built-in default.
func resolvePath(flagValue, envValue string, def func() (string, error)) (string, error) {
	if path := firstNonEmpty(flagValue, envValue); path != "" {
		return path, nil
	}
	return def()
}

// firstNonEmpty returns the first non-empty argument, or "" when all
// of them are empty.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
