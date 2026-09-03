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

	"github.com/dmitriy/gophkeeper/internal/client/token"
)

// Environment variable names read by Resolve.
const (
	// EnvServer holds the GophKeeper server address (host:port).
	EnvServer = "SERVER_ADDRESS"
	// EnvTokenPath holds the path of the token file.
	EnvTokenPath = "GOPHKEEPER_TOKEN_PATH"
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
}

// Resolve builds the configuration from the given flag values
// (empty when the flag was not set), applying the environment
// variables and the built-in defaults: an empty server falls back to
// $SERVER_ADDRESS and then DefaultServer; an empty token path falls
// back to $GOPHKEEPER_TOKEN_PATH and then token.DefaultPath(). It
// fails only when the default token path cannot be determined (no
// home directory).
func Resolve(serverFlag, tokenFlag string) (*Config, error) {
	server := firstNonEmpty(serverFlag, os.Getenv(EnvServer), DefaultServer)

	tokenPath := firstNonEmpty(tokenFlag, os.Getenv(EnvTokenPath))
	if tokenPath == "" {
		var err error
		tokenPath, err = token.DefaultPath()
		if err != nil {
			return nil, fmt.Errorf("config: resolve default token path: %w", err)
		}
	}

	return &Config{Server: server, TokenPath: tokenPath}, nil
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
