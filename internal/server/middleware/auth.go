// Package middleware provides gRPC server interceptors of the
// GophKeeper server.
package middleware

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/dmitriy/gophkeeper/internal/server/auth"
)

// authorizationHeader is the metadata key holding the bearer token.
const authorizationHeader = "authorization"

// bearerPrefix is the expected token scheme, followed by a space.
const bearerPrefix = "Bearer "

// NewAuthInterceptor returns a gRPC unary server interceptor that
// authenticates requests using a bearer JWT from the "authorization"
// metadata key ("Bearer <token>" format). On success the verified
// claims are stored in the request context (see
// auth.ContextWithClaims); on failure the interceptor aborts the call
// with codes.Unauthenticated without exposing JWT error details.
func NewAuthInterceptor(jwt *auth.JWTManager) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		claims, err := authenticate(ctx, jwt)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "authentication required")
		}
		return handler(auth.ContextWithClaims(ctx, claims), req)
	}
}

// authenticate extracts and verifies the bearer token from the incoming
// metadata, returning the verified claims.
func authenticate(ctx context.Context, jwt *auth.JWTManager) (*auth.Claims, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}
	values := md.Get(authorizationHeader)
	if len(values) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing authorization header")
	}
	// Multiple authorization values are ambiguous; use the first one.
	token, ok := strings.CutPrefix(values[0], bearerPrefix)
	if !ok || token == "" {
		return nil, status.Error(codes.Unauthenticated, "invalid authorization header")
	}
	claims, err := jwt.Verify(token)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}
	return claims, nil
}
