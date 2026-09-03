package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitriy/gophkeeper/internal/client/token"
)

// fakeAuthGateway is a hand-written gateway.AuthGateway recording the
// received credentials and returning canned tokens or errors.
type fakeAuthGateway struct {
	// registerToken / loginToken are returned on success.
	registerToken string
	loginToken    string
	// registerErr / loginErr are returned when non-nil.
	registerErr error
	loginErr    error

	// registerCalls / loginCalls count the gateway invocations.
	registerCalls int
	loginCalls    int
	// lastRegister / lastLogin record the credentials of the last call.
	lastRegister [2]string
	lastLogin    [2]string

	// token mimics the gateway's in-memory token state.
	token string
}

func (f *fakeAuthGateway) Register(_ context.Context, login, password string) (string, error) {
	f.registerCalls++
	f.lastRegister = [2]string{login, password}
	if f.registerErr != nil {
		return "", f.registerErr
	}
	f.token = f.registerToken
	return f.registerToken, nil
}

func (f *fakeAuthGateway) Login(_ context.Context, login, password string) (string, error) {
	f.loginCalls++
	f.lastLogin = [2]string{login, password}
	if f.loginErr != nil {
		return "", f.loginErr
	}
	f.token = f.loginToken
	return f.loginToken, nil
}

func (f *fakeAuthGateway) HasToken() bool {
	return f.token != ""
}

func (f *fakeAuthGateway) SetToken(value string) {
	f.token = value
}

// newTestAuthService builds an AuthService with a temp-dir token store
// and returns the fake gateway for assertions.
func newTestAuthService(t *testing.T) (*AuthService, *fakeAuthGateway) {
	t.Helper()
	store, err := token.New(t.TempDir() + "/token")
	require.NoError(t, err)
	gw := &fakeAuthGateway{}
	return NewAuthService(gw, store), gw
}

func TestAuthServiceRegisterSuccess(t *testing.T) {
	svc, gw := newTestAuthService(t)
	gw.registerToken = "token-1"

	err := svc.Register(context.Background(), "alice", "secret")

	require.NoError(t, err)
	assert.Equal(t, 1, gw.registerCalls)
	assert.Equal(t, [2]string{"alice", "secret"}, gw.lastRegister)
	assert.True(t, gw.HasToken())
	assert.True(t, svc.IsAuthenticated())
}

func TestAuthServiceLoginSuccess(t *testing.T) {
	svc, gw := newTestAuthService(t)
	gw.loginToken = "token-2"

	err := svc.Login(context.Background(), "bob", "hunter2")

	require.NoError(t, err)
	assert.Equal(t, 1, gw.loginCalls)
	assert.Equal(t, [2]string{"bob", "hunter2"}, gw.lastLogin)
	assert.True(t, svc.IsAuthenticated())
}

func TestAuthServiceRegisterValidation(t *testing.T) {
	tests := []struct {
		name     string
		login    string
		password string
		wantErr  error
	}{
		{"empty login", "", "secret", ErrEmptyLogin},
		{"empty password", "alice", "", ErrEmptyPassword},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, gw := newTestAuthService(t)

			err := svc.Register(context.Background(), tt.login, tt.password)

			assert.ErrorIs(t, err, tt.wantErr)
			assert.Zero(t, gw.registerCalls, "gateway must not be called on validation failure")
			assert.False(t, svc.IsAuthenticated())
		})
	}
}

func TestAuthServiceLoginValidation(t *testing.T) {
	tests := []struct {
		name     string
		login    string
		password string
		wantErr  error
	}{
		{"empty login", "", "secret", ErrEmptyLogin},
		{"empty password", "bob", "", ErrEmptyPassword},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, gw := newTestAuthService(t)

			err := svc.Login(context.Background(), tt.login, tt.password)

			assert.ErrorIs(t, err, tt.wantErr)
			assert.Zero(t, gw.loginCalls, "gateway must not be called on validation failure")
		})
	}
}

func TestAuthServiceRegisterPropagatesGatewayError(t *testing.T) {
	svc, gw := newTestAuthService(t)
	gw.registerErr = errors.New("boom")

	err := svc.Register(context.Background(), "alice", "secret")

	assert.ErrorIs(t, err, gw.registerErr)
	assert.False(t, svc.IsAuthenticated())
}

func TestAuthServiceLogoutClearsStoreAndMemory(t *testing.T) {
	svc, gw := newTestAuthService(t)
	gw.token = "token-3"
	require.NoError(t, svc.store.Save("token-3"))

	err := svc.Logout()

	require.NoError(t, err)
	assert.False(t, svc.IsAuthenticated())
	_, loadErr := svc.store.Load()
	assert.ErrorIs(t, loadErr, token.ErrNoToken, "persisted token must be removed")
}

func TestAuthServiceLogoutIsIdempotent(t *testing.T) {
	svc, _ := newTestAuthService(t)

	assert.NoError(t, svc.Logout())
	assert.NoError(t, svc.Logout())
	assert.False(t, svc.IsAuthenticated())
}

func TestAuthServiceIsAuthenticatedInitiallyFalse(t *testing.T) {
	svc, _ := newTestAuthService(t)

	assert.False(t, svc.IsAuthenticated())
}

func TestAuthServiceRestoreLoadsPersistedToken(t *testing.T) {
	store, err := token.New(t.TempDir() + "/token")
	require.NoError(t, err)
	require.NoError(t, store.Save("saved-token"))
	gw := &fakeAuthGateway{}
	svc := NewAuthService(gw, store)

	err = svc.Restore()

	require.NoError(t, err)
	assert.True(t, gw.HasToken())
	assert.True(t, svc.IsAuthenticated())
}

func TestAuthServiceRestoreWithoutSavedTokenStaysUnauthenticated(t *testing.T) {
	svc, _ := newTestAuthService(t)

	err := svc.Restore()

	require.NoError(t, err, "no saved token is a normal state, not an error")
	assert.False(t, svc.IsAuthenticated())
}
