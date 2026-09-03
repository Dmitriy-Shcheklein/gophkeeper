package auth

import "context"

// claimsContextKey is the unexported type used to store Claims in a
// context.Context.
type claimsContextKey struct{}

// ContextWithClaims returns a context carrying the given claims.
func ContextWithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsContextKey{}, claims)
}

// ClaimsFromContext extracts claims previously stored by
// ContextWithClaims. The boolean result reports whether claims were
// present.
func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey{}).(*Claims)
	if !ok || claims == nil {
		return nil, false
	}
	return claims, true
}
