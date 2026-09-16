package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveDefaults(t *testing.T) {
	t.Setenv(EnvServer, "")
	t.Setenv(EnvTokenPath, "")
	t.Setenv(EnvCachePath, "")
	t.Setenv(EnvCAPath, "")

	cfg, err := Resolve("", "", "", "")
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

	cfg, err := Resolve("", "", "", "")
	require.NoError(t, err)
	require.Equal(t, "env-server:6000", cfg.Server)
	require.Equal(t, "/tmp/env-token", cfg.TokenPath)
}

func TestResolveFlagOverridesEnv(t *testing.T) {
	t.Setenv(EnvServer, "env-server:6000")
	t.Setenv(EnvTokenPath, "/tmp/env-token")

	cfg, err := Resolve("flag-server:7000", "/tmp/flag-token", "", "")
	require.NoError(t, err)
	require.Equal(t, "flag-server:7000", cfg.Server)
	require.Equal(t, "/tmp/flag-token", cfg.TokenPath)
}

func TestResolvePartialOverrides(t *testing.T) {
	t.Setenv(EnvServer, "env-server:6000")
	t.Setenv(EnvTokenPath, "")

	cfg, err := Resolve("", "", "", "")
	require.NoError(t, err)
	require.Equal(t, "env-server:6000", cfg.Server)
	require.Contains(t, cfg.TokenPath, ".gophkeeper")
}

func TestResolveCAPath(t *testing.T) {
	t.Run("flag wins over env", func(t *testing.T) {
		t.Setenv(EnvCAPath, "/tmp/env-ca.crt")
		cfg, err := Resolve("", "", "", "/tmp/flag-ca.crt")
		require.NoError(t, err)
		require.Equal(t, "/tmp/flag-ca.crt", cfg.CAPath)
	})

	t.Run("env fallback", func(t *testing.T) {
		t.Setenv(EnvCAPath, "/tmp/env-ca.crt")
		cfg, err := Resolve("", "", "", "")
		require.NoError(t, err)
		require.Equal(t, "/tmp/env-ca.crt", cfg.CAPath)
	})

	t.Run("empty by default", func(t *testing.T) {
		t.Setenv(EnvCAPath, "")
		cfg, err := Resolve("", "", "", "")
		require.NoError(t, err)
		require.Equal(t, "", cfg.CAPath)
	})
}

func TestResolveDefaultTokenPathError(t *testing.T) {
	// An empty HOME makes os.UserHomeDir fail, so the default token
	// path cannot be resolved.
	t.Setenv("HOME", "")

	_, err := Resolve("", "", "", "")
	require.Error(t, err)
	// The chain is config wrap -> token wrap -> the os.UserHomeDir
	// failure ("$HOME is not defined"); assert the actual cause is
	// preserved through both layers.
	require.ErrorContains(t, err, "home directory")
	require.ErrorContains(t, err, "not defined")
	require.Contains(t, err.Error(), "default token path")
}
