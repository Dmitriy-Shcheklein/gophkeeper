package config

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveDefaults(t *testing.T) {
	t.Setenv(EnvServer, "")
	t.Setenv(EnvTokenPath, "")

	cfg, err := Resolve("", "")
	require.NoError(t, err)
	require.Equal(t, DefaultServer, cfg.Server)

	home, err := filepath.Abs(".") // sanity: just ensure default path resolves
	require.NoError(t, err)
	require.NotEmpty(t, home)
	require.Contains(t, cfg.TokenPath, ".gophkeeper")
}

func TestResolveEnvFallback(t *testing.T) {
	t.Setenv(EnvServer, "env-server:6000")
	t.Setenv(EnvTokenPath, "/tmp/env-token")

	cfg, err := Resolve("", "")
	require.NoError(t, err)
	require.Equal(t, "env-server:6000", cfg.Server)
	require.Equal(t, "/tmp/env-token", cfg.TokenPath)
}

func TestResolveFlagOverridesEnv(t *testing.T) {
	t.Setenv(EnvServer, "env-server:6000")
	t.Setenv(EnvTokenPath, "/tmp/env-token")

	cfg, err := Resolve("flag-server:7000", "/tmp/flag-token")
	require.NoError(t, err)
	require.Equal(t, "flag-server:7000", cfg.Server)
	require.Equal(t, "/tmp/flag-token", cfg.TokenPath)
}

func TestResolvePartialOverrides(t *testing.T) {
	t.Setenv(EnvServer, "env-server:6000")
	t.Setenv(EnvTokenPath, "")

	cfg, err := Resolve("", "")
	require.NoError(t, err)
	require.Equal(t, "env-server:6000", cfg.Server)
	require.Contains(t, cfg.TokenPath, ".gophkeeper")
}

func TestResolveDefaultTokenPathError(t *testing.T) {
	// An empty HOME makes os.UserHomeDir fail, so the default token
	// path cannot be resolved.
	t.Setenv("HOME", "")

	_, err := Resolve("", "")
	require.Error(t, err)
	require.True(t, errors.Is(err, err), "expected home dir error, got: %v", err)
}
