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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
	"github.com/dmitriy/gophkeeper/internal/server/model"
	"github.com/dmitriy/gophkeeper/internal/server/service"
)

// fakeAuthService is a hand-written mock of the transport.authService
// interface, recording invocations and returning preset results.
type fakeAuthService struct {
	registerFn func(ctx context.Context, login, password string) (*model.User, string, error)
	loginFn    func(ctx context.Context, login, password string) (*model.User, string, error)
}

func (f *fakeAuthService) Register(ctx context.Context, login, password string) (*model.User, string, error) {
	return f.registerFn(ctx, login, password)
}

func (f *fakeAuthService) Login(ctx context.Context, login, password string) (*model.User, string, error) {
	return f.loginFn(ctx, login, password)
}

// sampleUser returns a fully populated domain user for mapping tests.
func sampleUser() *model.User {
	return &model.User{
		ID:        "user-1",
		Login:     "alice",
		PassHash:  "$2a$10$hash",
		CreatedAt: time.Unix(1700000000, 0),
	}
}

func TestAuthHandler_Register_Success(t *testing.T) {
	var gotLogin, gotPassword string
	fake := &fakeAuthService{
		registerFn: func(_ context.Context, login, password string) (*model.User, string, error) {
			gotLogin, gotPassword = login, password
			return sampleUser(), "issued-token", nil
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
	require.NotNil(t, resp.GetUser())
	assert.Equal(t, "user-1", resp.GetUser().GetId())
	assert.Equal(t, "alice", resp.GetUser().GetLogin())
	assert.Equal(t, int64(1700000000), resp.GetUser().GetCreatedAt())
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
				registerFn: func(context.Context, string, string) (*model.User, string, error) {
					return nil, "", tt.serviceErr
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
		loginFn: func(_ context.Context, login, password string) (*model.User, string, error) {
			gotLogin, gotPassword = login, password
			return sampleUser(), "issued-token", nil
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
	require.NotNil(t, resp.GetUser())
	assert.Equal(t, "user-1", resp.GetUser().GetId())
	assert.Equal(t, "alice", resp.GetUser().GetLogin())
	assert.Equal(t, int64(1700000000), resp.GetUser().GetCreatedAt())
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
				loginFn: func(context.Context, string, string) (*model.User, string, error) {
					return nil, "", tt.serviceErr
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
