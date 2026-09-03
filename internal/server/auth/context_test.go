package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextWithClaims(t *testing.T) {
	mgr, err := New("test-secret", time.Minute)
	require.NoError(t, err)

	token, err := mgr.Generate("user-1", "alice")
	require.NoError(t, err)
	claims, err := mgr.Verify(token)
	require.NoError(t, err)

	ctx := ContextWithClaims(context.Background(), claims)
	got, ok := ClaimsFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, claims, got)

	got, ok = ClaimsFromContext(context.Background())
	assert.False(t, ok)
	assert.Nil(t, got)
}
