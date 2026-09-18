package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/dmitriy/gophkeeper/internal/client/cache"
	"github.com/dmitriy/gophkeeper/internal/client/gateway"
	"github.com/dmitriy/gophkeeper/internal/client/token"
)

// Authentication validation errors: returned by AuthService before
// any network activity so the user gets immediate feedback. The CLI
// maps them to friendly messages.
var (
	// ErrEmptyLogin is returned by Register and Login when the login
	// is empty.
	ErrEmptyLogin = errors.New("login must not be empty")
	// ErrEmptyPassword is returned by Register and Login when the
	// password is empty.
	ErrEmptyPassword = errors.New("password must not be empty")
)

// authGateway is the gateway API AuthService depends on: the
// authentication RPCs plus the token state access needed to restore
// a persisted token and to reset it on logout. The concrete
// *gateway.Gateway satisfies it.
type authGateway interface {
	gateway.AuthGateway

	// SetToken stores the access token in memory (restore, logout).
	SetToken(token string)
	// HasToken reports whether an access token is set in memory.
	HasToken() bool
}

// compile-time assertion: *gateway.Gateway implements authGateway.
var _ authGateway = (*gateway.Gateway)(nil)

// AuthService is the client-side authentication business logic. It
// validates credentials before touching the network and owns the
// token lifecycle across CLI invocations: restore on start, persist
// on login (delegated to the gateway), clear on logout.
type AuthService struct {
	gw authGateway
	// store is the persistent token store, accessed directly: the
	// gateway itself hands its store out for exactly this purpose
	// (see gateway.Gateway.TokenStore).
	store *token.Store
	// cache is the optional offline snapshot store; it is cleared on
	// every auth lifecycle change (login, register, logout) so a
	// cache can never outlive the account that produced it. nil
	// disables the offline cache.
	cache *cache.Store
}

// NewAuthService returns an AuthService working through gw and
// persisting/restoring the token via store, without the offline
// cache. Both arguments must not be nil. The constructor does not
// load a persisted token: call Restore explicitly (see the package
// comment for the lifecycle).
func NewAuthService(gw authGateway, store *token.Store) *AuthService {
	return &AuthService{gw: gw, store: store}
}

// NewAuthServiceWithCache returns an AuthService that additionally
// manages the offline cache lifecycle: the snapshot is cleared on
// login, register and logout so cached data always belongs to the
// currently authenticated account.
func NewAuthServiceWithCache(gw authGateway, store *token.Store, c *cache.Store) *AuthService {
	return &AuthService{gw: gw, store: store, cache: c}
}

// clearCache drops the offline snapshot (best-effort: a cache
// failure must not fail the auth operation).
func (s *AuthService) clearCache() {
	if s.cache != nil {
		_ = s.cache.Clear()
	}
}

// Register creates a new account. The login and password must be
// non-empty (client-side fast-fail with ErrEmptyLogin or
// ErrEmptyPassword); otherwise the request is delegated to the
// gateway, which on success stores the token in memory and persists
// it for the next CLI invocation.
func (s *AuthService) Register(ctx context.Context, login, password string) error {
	if err := validateCredentials(login, password); err != nil {
		return err
	}
	if _, err := s.gw.Register(ctx, login, password); err != nil {
		return fmt.Errorf("service: register: %w", err)
	}
	s.clearCache()
	return nil
}

// Login authenticates an existing account. The validation and token
// handling contract is the same as for Register.
func (s *AuthService) Login(ctx context.Context, login, password string) error {
	if err := validateCredentials(login, password); err != nil {
		return err
	}
	if _, err := s.gw.Login(ctx, login, password); err != nil {
		return fmt.Errorf("service: login: %w", err)
	}
	s.clearCache()
	return nil
}

// Logout clears the persisted token and the in-memory one. It is
// idempotent: logging out with no token anywhere is a no-op.
func (s *AuthService) Logout() error {
	if err := s.store.Clear(); err != nil {
		return fmt.Errorf("service: logout: %w", err)
	}
	s.gw.SetToken("")
	s.clearCache()
	return nil
}

// Restore loads a token persisted by a previous CLI invocation into
// the gateway. Not having a saved token is a normal state for a user
// who has not logged in yet, so it is not an error: Restore returns
// nil and the service stays unauthenticated. Any other store read
// failure is propagated to the caller.
func (s *AuthService) Restore() error {
	stored, err := s.store.Load()
	if err != nil {
		if errors.Is(err, token.ErrNoToken) {
			return nil
		}
		return fmt.Errorf("service: restore token: %w", err)
	}
	if stored == "" {
		return nil
	}
	s.gw.SetToken(stored)
	return nil
}

// IsAuthenticated reports whether an access token is set in the
// gateway, i.e. whether authenticated calls may proceed.
func (s *AuthService) IsAuthenticated() bool {
	return s.gw.HasToken()
}

// validateCredentials enforces the minimal client-side credential
// rules: both login and password must be non-empty.
func validateCredentials(login, password string) error {
	if login == "" {
		return ErrEmptyLogin
	}
	if password == "" {
		return ErrEmptyPassword
	}
	return nil
}
