package config_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitriy/gophkeeper/internal/server/config"
)

// validArgs are flags that satisfy the required settings, so table
// cases can focus on the single aspect they exercise.
var validArgs = []string{
	"--dsn", "postgres://u:p@localhost:5432/gk",
	"--jwt-secret", "secret",
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load(validArgs)
	require.NoError(t, err)

	assert.Equal(t, ":50051", cfg.Address)
	assert.Equal(t, "postgres://u:p@localhost:5432/gk", cfg.DSN)
	assert.Equal(t, "secret", cfg.JWTSecret)
	assert.Equal(t, 24*time.Hour, cfg.JWTTTL)
	assert.Equal(t, slog.LevelInfo, cfg.LogLevel)
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		args    []string
		want    *config.Config
		wantErr string
	}{
		{
			name: "env overrides defaults",
			env: map[string]string{
				"GRPC_ADDRESS": ":9999",
				"JWT_TTL":      "1h30m",
				"LOG_LEVEL":    "debug",
				"DATABASE_DSN": "postgres://e:p@db:5432/e",
				"JWT_SECRET":   "envsecret",
			},
			want: &config.Config{
				Address:   ":9999",
				DSN:       "postgres://e:p@db:5432/e",
				JWTSecret: "envsecret",
				JWTTTL:    90 * time.Minute,
				LogLevel:  slog.LevelDebug,
			},
		},
		{
			name: "flags override env",
			env: map[string]string{
				"GRPC_ADDRESS": ":9999",
				"JWT_TTL":      "1h30m",
				"LOG_LEVEL":    "debug",
				"DATABASE_DSN": "postgres://e:p@db:5432/e",
				"JWT_SECRET":   "envsecret",
			},
			args: []string{
				"--address", ":7777",
				"--dsn", "postgres://f:p@db:5432/f",
				"--jwt-secret", "flagsecret",
				"--jwt-ttl", "48h",
				"--log-level", "error",
			},
			want: &config.Config{
				Address:   ":7777",
				DSN:       "postgres://f:p@db:5432/f",
				JWTSecret: "flagsecret",
				JWTTTL:    48 * time.Hour,
				LogLevel:  slog.LevelError,
			},
		},
		{
			name:    "missing dsn",
			args:    []string{"--jwt-secret", "secret"},
			env:     map[string]string{"JWT_SECRET": "secret"},
			wantErr: "required",
		},
		{
			name:    "missing jwt secret",
			args:    []string{"--dsn", "postgres://u:p@localhost:5432/gk"},
			env:     map[string]string{"DATABASE_DSN": "postgres://u:p@localhost:5432/gk"},
			wantErr: "required",
		},
		{
			name:    "bad jwt ttl",
			args:    append(validArgs, "--jwt-ttl", "24hours"),
			wantErr: "parse --jwt-ttl",
		},
		{
			name:    "zero jwt ttl",
			args:    append(validArgs, "--jwt-ttl", "0s"),
			wantErr: "must be positive",
		},
		{
			name:    "bad log level",
			args:    append(validArgs, "--log-level", "verbose"),
			wantErr: "parse --log-level",
		},
		{
			name:    "unknown flag",
			args:    append(validArgs, "--unknown"),
			wantErr: "parse flags",
		},
		{
			name: "empty env values fall back to defaults",
			env: map[string]string{
				"GRPC_ADDRESS": "",
				"JWT_TTL":      "",
				"LOG_LEVEL":    "",
			},
			args: validArgs,
			want: &config.Config{
				Address:   ":50051",
				DSN:       "postgres://u:p@localhost:5432/gk",
				JWTSecret: "secret",
				JWTTTL:    24 * time.Hour,
				LogLevel:  slog.LevelInfo,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for name, value := range tt.env {
				t.Setenv(name, value)
			}

			got, err := config.Load(tt.args)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadLogLevelCaseInsensitive(t *testing.T) {
	t.Setenv("LOG_LEVEL", "WARN")
	cfg, err := config.Load(validArgs)
	require.NoError(t, err)
	assert.Equal(t, slog.LevelWarn, cfg.LogLevel)
}
