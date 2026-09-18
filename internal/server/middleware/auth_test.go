package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/dmitriy/gophkeeper/internal/server/auth"
)

// handler captures the context it is invoked with, mimicking a real
// gRPC handler.
func handler(ctx context.Context, captured *context.Context) (any, error) {
	*captured = ctx
	return "ok", nil
}

// invoke runs the interceptor with the given metadata (nil means no
// metadata) for the given full method name and returns the context seen
// by the handler.
func invoke(t *testing.T, interceptor grpc.UnaryServerInterceptor, method string, md metadata.MD) (context.Context, error) {
	t.Helper()
	ctx := context.Background()
	if md != nil {
		ctx = metadata.NewIncomingContext(ctx, md)
	}
	var captured context.Context
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, func(ctx context.Context, _ any) (any, error) {
		return handler(ctx, &captured)
	})
	return captured, err
}

// expiredToken crafts an already-expired token signed with the secret.
func expiredToken(t *testing.T, secret string) string {
	t.Helper()
	claims := auth.Claims{
		UserID: "user-1",
		Login:  "alice",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Minute)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	require.NoError(t, err)
	return token
}

func TestInterceptorValidToken(t *testing.T) {
	mgr, err := auth.New("test-secret", time.Minute)
	require.NoError(t, err)

	token, err := mgr.Generate("user-1", "alice")
	require.NoError(t, err)

	captured, err := invoke(t, NewAuthInterceptor(mgr), "/test/method", metadata.Pairs("authorization", "Bearer "+token))
	require.NoError(t, err)

	claims, ok := auth.ClaimsFromContext(captured)
	require.True(t, ok)
	assert.Equal(t, "user-1", claims.UserID)
	assert.Equal(t, "alice", claims.Login)
}

func TestInterceptorUnauthenticated(t *testing.T) {
	mgr, err := auth.New("test-secret", time.Minute)
	require.NoError(t, err)
	interceptor := NewAuthInterceptor(mgr)

	validToken, err := mgr.Generate("user-1", "alice")
	require.NoError(t, err)

	tests := []struct {
		name string
		md   metadata.MD
	}{
		{"no metadata", nil},
		{"no authorization header", metadata.Pairs("other", "value")},
		{"malformed header", metadata.Pairs("authorization", "not-a-bearer-token")},
		{"wrong scheme", metadata.Pairs("authorization", "Basic "+validToken)},
		{"empty token", metadata.Pairs("authorization", "Bearer ")},
		{"bad token", metadata.Pairs("authorization", "Bearer garbage")},
		{"expired token", metadata.Pairs("authorization", "Bearer "+expiredToken(t, "test-secret"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := invoke(t, interceptor, "/test/method", tt.md)
			require.Error(t, err)
			assert.Equal(t, codes.Unauthenticated, status.Code(err))
			// The message must be generic: no JWT internals leaked.
			assert.Equal(t, "authentication required", status.Convert(err).Message())
		})
	}
}

func TestInterceptorExemptMethods(t *testing.T) {
	mgr, err := auth.New("test-secret", time.Minute)
	require.NoError(t, err)
	interceptor := NewAuthInterceptor(mgr, "/test/public", "/test/other/public")

	t.Run("exempt method without token passes through", func(t *testing.T) {
		captured, err := invoke(t, interceptor, "/test/public", nil)
		require.NoError(t, err)
		// No claims in the context for exempt methods.
		_, ok := auth.ClaimsFromContext(captured)
		assert.False(t, ok)
	})

	t.Run("non-exempt method without token still rejected", func(t *testing.T) {
		_, err := invoke(t, interceptor, "/test/private", nil)
		require.Error(t, err)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("exempt method with valid token passes through", func(t *testing.T) {
		token, err := mgr.Generate("user-1", "alice")
		require.NoError(t, err)
		captured, err := invoke(t, interceptor, "/test/public", metadata.Pairs("authorization", "Bearer "+token))
		require.NoError(t, err)
		// Still no claims injected for exempt methods.
		_, ok := auth.ClaimsFromContext(captured)
		assert.False(t, ok)
	})
}
