package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactDSN(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "userinfo password is masked",
			dsn:  "postgres://user:secret@localhost:5432/db?sslmode=disable",
			want: "postgres://user:xxxxx@localhost:5432/db?sslmode=disable",
		},
		{
			name: "password query parameter is masked",
			dsn:  "postgres://user@localhost:5432/db?password=secret&sslmode=disable",
			want: "postgres://user@localhost:5432/db?password=xxxxx&sslmode=disable",
		},
		{
			name: "both passwords are masked",
			dsn:  "postgres://user:secret@localhost:5432/db?password=secret2",
			want: "postgres://user:xxxxx@localhost:5432/db?password=xxxxx",
		},
		{
			name: "no secrets is unchanged",
			dsn:  "postgres://user@localhost:5432/db?sslmode=disable",
			want: "postgres://user@localhost:5432/db?sslmode=disable",
		},
		{
			name: "unparsable dsn is fully redacted",
			dsn:  ":\x00:bad",
			want: "***",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, redactDSN(tt.dsn))
		})
	}
}
