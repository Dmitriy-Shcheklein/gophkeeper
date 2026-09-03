package gateway

import (
	"context"
	"fmt"

	v1 "github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
)

// AuthGateway is the client-side authentication API. The service
// layer depends on this interface (mockable in tests), not on the
// concrete Gateway.
type AuthGateway interface {
	// Register creates a new account and returns the access token.
	Register(ctx context.Context, login, password string) (token string, err error)
	// Login authenticates an existing account and returns the access
	// token.
	Login(ctx context.Context, login, password string) (token string, err error)
}

// compile-time assertion: Gateway implements AuthGateway.
var _ AuthGateway = (*Gateway)(nil)

// Register creates a new account on the server.
//
// On success the returned token is stored in memory (SetToken) and
// persisted via the token store — the CLI is one-shot per command, so
// saving inside the gateway is the pragmatic way to make the token of
// this process available to the next one. A persistence failure is
// reported to the caller: silently losing the token would leave the
// user logged out on the next invocation with no explanation.
func (g *Gateway) Register(ctx context.Context, login, password string) (string, error) {
	resp, err := g.auth.Register(ctx, &v1.RegisterRequest{
		Login:    login,
		Password: password,
	})
	if err != nil {
		return "", translateError(err)
	}
	return g.acceptToken(resp.GetAccessToken())
}

// Login authenticates an existing account. See Register for the
// token handling contract.
func (g *Gateway) Login(ctx context.Context, login, password string) (string, error) {
	resp, err := g.auth.Login(ctx, &v1.LoginRequest{
		Login:    login,
		Password: password,
	})
	if err != nil {
		return "", translateError(err)
	}
	return g.acceptToken(resp.GetAccessToken())
}

// acceptToken stores the token in memory and persists it, returning
// it to the caller.
func (g *Gateway) acceptToken(token string) (string, error) {
	g.SetToken(token)
	if err := g.tokenStore.Save(token); err != nil {
		return "", fmt.Errorf("gateway: persist token: %w", err)
	}
	return token, nil
}
