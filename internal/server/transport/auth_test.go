// Package transport implements the gRPC transport layer of the
// GophKeeper server: protobuf request/response handlers for the
// AuthService and EntryService, conversions between protobuf and
// domain models, extraction of the authenticated user identity from
// request contexts and mapping of domain errors to gRPC status codes.
package transport

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
	"github.com/dmitriy/gophkeeper/internal/server/auth"
	"github.com/dmitriy/gophkeeper/internal/server/model"
	"github.com/dmitriy/gophkeeper/internal/server/service"
)

// fakeAuthService is a hand-written mock of the transport.authService
// interface, recording invocations and returning preset results.
type fakeAuthService struct {
	registerFn func(ctx context.Context, login, password string) (string, error)
	loginFn    func(ctx context.Context, login, password string) (string, error)
}

func (f *fakeAuthService) Register(ctx context.Context, login, password string) (string, error) {
	return f.registerFn(ctx, login, password)
}

func (f *fakeAuthService) Login(ctx context.Context, login, password string) (string, error) {
	return f.loginFn(ctx, login, password)
}

func TestAuthHandler_Register_Success(t *testing.T) {
	var gotLogin, gotPassword string
	fake := &fakeAuthService{
		registerFn: func(_ context.Context, login, password string) (string, error) {
			gotLogin, gotPassword = login, password
			return "issued-token", nil
		},
	}
	handler := NewAuthHandler(fake)

	resp, err := handler.Register(context.Background(), &gophkeeperv1.RegisterRequest{
		Login:    "alice",
		Password: "strong-password",
	})

	require.NoError(t, err)
	assert.Equal(t, "alice", gotLogin)
	assert.Equal(t, "strong-password", gotPassword)
	require.NotNil(t, resp)
	assert.Equal(t, "issued-token", resp.GetAccessToken())
	assert.Nil(t, resp.GetUser())
}

func TestAuthHandler_Register_ErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		serviceErr error
		wantCode   codes.Code
		wantMsg    string
	}{
		{
			name:       "login taken",
			serviceErr: fmt.Errorf("service: create user: %w", model.ErrAlreadyExists),
			wantCode:   codes.AlreadyExists,
			wantMsg:    model.ErrAlreadyExists.Error(),
		},
		{
			name:       "empty login",
			serviceErr: service.ErrEmptyLogin,
			wantCode:   codes.InvalidArgument,
			wantMsg:    service.ErrEmptyLogin.Error(),
		},
		{
			name:       "password too short",
			serviceErr: service.ErrPasswordTooShort,
			wantCode:   codes.InvalidArgument,
			wantMsg:    service.ErrPasswordTooShort.Error(),
		},
		{
			name:       "internal error is generic",
			serviceErr: errors.New("db: connection refused"),
			wantCode:   codes.Internal,
			wantMsg:    "internal error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeAuthService{
				registerFn: func(context.Context, string, string) (string, error) {
					return "", tt.serviceErr
				},
			}
			handler := NewAuthHandler(fake)

			resp, err := handler.Register(context.Background(), &gophkeeperv1.RegisterRequest{
				Login:    "alice",
				Password: "strong-password",
			})

			require.Nil(t, resp)
			assert.Equal(t, tt.wantCode, status.Code(err))
			assert.Equal(t, tt.wantMsg, status.Convert(err).Message())
		})
	}
}

func TestAuthHandler_Login_Success(t *testing.T) {
	var gotLogin, gotPassword string
	fake := &fakeAuthService{
		loginFn: func(_ context.Context, login, password string) (string, error) {
			gotLogin, gotPassword = login, password
			return "issued-token", nil
		},
	}
	handler := NewAuthHandler(fake)

	resp, err := handler.Login(context.Background(), &gophkeeperv1.LoginRequest{
		Login:    "alice",
		Password: "strong-password",
	})

	require.NoError(t, err)
	assert.Equal(t, "alice", gotLogin)
	assert.Equal(t, "strong-password", gotPassword)
	require.NotNil(t, resp)
	assert.Equal(t, "issued-token", resp.GetAccessToken())
	assert.Nil(t, resp.GetUser())
}

func TestAuthHandler_Login_ErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		serviceErr error
		wantCode   codes.Code
		wantMsg    string
	}{
		{
			name:       "wrong credentials",
			serviceErr: fmt.Errorf("service: login: %w: %w", model.ErrUnauthorized, service.ErrInvalidCredentials),
			wantCode:   codes.Unauthenticated,
			wantMsg:    service.ErrInvalidCredentials.Error(),
		},
		{
			name:       "empty password",
			serviceErr: service.ErrEmptyPassword,
			wantCode:   codes.InvalidArgument,
			wantMsg:    service.ErrEmptyPassword.Error(),
		},
		{
			name:       "internal error is generic",
			serviceErr: errors.New("db: connection refused"),
			wantCode:   codes.Internal,
			wantMsg:    "internal error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeAuthService{
				loginFn: func(context.Context, string, string) (string, error) {
					return "", tt.serviceErr
				},
			}
			handler := NewAuthHandler(fake)

			resp, err := handler.Login(context.Background(), &gophkeeperv1.LoginRequest{
				Login:    "alice",
				Password: "strong-password",
			})

			require.Nil(t, resp)
			assert.Equal(t, tt.wantCode, status.Code(err))
			assert.Equal(t, tt.wantMsg, status.Convert(err).Message())
		})
	}
}

// claimsContext returns a context carrying claims of a user with the
// given id, as the auth interceptor would.
func claimsContext(userID string) context.Context {
	return auth.ContextWithClaims(context.Background(), &auth.Claims{UserID: userID, Login: "alice"})
}
