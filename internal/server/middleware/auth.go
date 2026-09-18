// Package middleware provides gRPC server interceptors of the
// GophKeeper server.
package middleware

import (
	"context"
	"errors"
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

// unauthenticatedMessage is the generic gRPC error message returned for
// every authentication failure, deliberately free of details.
const unauthenticatedMessage = "authentication required"

// authenticate failure causes.
var (
	errMissingMetadata   = errors.New("no incoming metadata")
	errMissingAuthHeader = errors.New("missing authorization header")
	errInvalidAuthHeader = errors.New("invalid authorization header")
	errInvalidToken      = errors.New("invalid token")
)

// NewAuthInterceptor returns a gRPC unary server interceptor that
// authenticates requests using a bearer JWT from the "authorization"
// metadata key ("Bearer <token>" format). On success the verified
// claims are stored in the request context (see
// auth.ContextWithClaims); on failure the interceptor aborts the call
// with codes.Unauthenticated and a generic message, without exposing
// JWT error details.
//
// The exemptMethods variadic lists full method names (e.g.
// "/gophkeeperv1.AuthService/Register") that skip authentication
// entirely; such calls proceed without claims in the context. Pass no
// arguments to protect every method.
func NewAuthInterceptor(manager *auth.JWTManager, exemptMethods ...string) grpc.UnaryServerInterceptor {
	exempt := make(map[string]struct{}, len(exemptMethods))
	for _, method := range exemptMethods {
		exempt[method] = struct{}{}
	}

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if _, ok := exempt[info.FullMethod]; ok {
			return handler(ctx, req)
		}
		claims, err := authenticate(ctx, manager)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, unauthenticatedMessage)
		}
		return handler(auth.ContextWithClaims(ctx, claims), req)
	}
}

// NewAuthStreamInterceptor returns a gRPC stream server interceptor
// with the same semantics as NewAuthInterceptor: it authenticates the
// stream using the bearer JWT from the "authorization" metadata key and
// stores the verified claims in the stream context, which the wrapped
// handler sees through ServerStream.Context. Exempt method names skip
// authentication entirely.
func NewAuthStreamInterceptor(manager *auth.JWTManager, exemptMethods ...string) grpc.StreamServerInterceptor {
	exempt := make(map[string]struct{}, len(exemptMethods))
	for _, method := range exemptMethods {
		exempt[method] = struct{}{}
	}

	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if _, ok := exempt[info.FullMethod]; ok {
			return handler(srv, ss)
		}
		claims, err := authenticate(ss.Context(), manager)
		if err != nil {
			return status.Error(codes.Unauthenticated, unauthenticatedMessage)
		}
		return handler(srv, &claimsServerStream{ServerStream: ss, claims: claims})
	}
}

// claimsServerStream wraps a grpc.ServerStream, replacing its context
// with one carrying the verified claims of the authenticated caller.
type claimsServerStream struct {
	grpc.ServerStream
	claims *auth.Claims
}

// Context returns the stream context enriched with the claims.
func (s *claimsServerStream) Context() context.Context {
	return auth.ContextWithClaims(s.ServerStream.Context(), s.claims)
}

// authenticate extracts and verifies the bearer token from the incoming
// metadata, returning the verified claims or one of the err* sentinels
// describing the failure cause (mapped to a generic gRPC error by the
// caller).
func authenticate(ctx context.Context, manager *auth.JWTManager) (*auth.Claims, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, errMissingMetadata
	}
	values := md.Get(authorizationHeader)
	if len(values) == 0 {
		return nil, errMissingAuthHeader
	}
	// Multiple authorization values are ambiguous; use the first one.
	token, ok := strings.CutPrefix(values[0], bearerPrefix)
	if !ok || token == "" {
		return nil, errInvalidAuthHeader
	}
	claims, err := manager.Verify(token)
	if err != nil {
		return nil, errInvalidToken
	}
	return claims, nil
}
