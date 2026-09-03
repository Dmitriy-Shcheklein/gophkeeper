// Package transport implements the gRPC transport layer of the
// GophKeeper server: protobuf request/response handlers for the
// AuthService and EntryService, conversions between protobuf and
// domain models, extraction of the authenticated user identity from
// request contexts and mapping of domain errors to gRPC status codes.
package transport

import (
	"context"

	"github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
	"github.com/dmitriy/gophkeeper/internal/server/model"
	"github.com/dmitriy/gophkeeper/internal/server/service"
)

// authService is the part of service.AuthService needed by AuthHandler.
// It is defined here (consumer side) so handlers can be unit tested
// with mocks; *service.AuthService satisfies it.
type authService interface {
	// Register validates the credentials and creates a user account,
	// returning the created user and an access token.
	Register(ctx context.Context, login, password string) (*model.User, string, error)
	// Login verifies the credentials, returning the user and an
	// access token.
	Login(ctx context.Context, login, password string) (*model.User, string, error)
}

// AuthHandler implements gophkeeperv1.AuthServiceServer on top of the
// auth service, mapping domain errors to gRPC status codes.
type AuthHandler struct {
	gophkeeperv1.UnimplementedAuthServiceServer

	auth authService
}

// Compile-time interface satisfaction checks.
var (
	_ gophkeeperv1.AuthServiceServer = (*AuthHandler)(nil)
	_ authService                    = (*service.AuthService)(nil)
)

// NewAuthHandler creates an AuthHandler backed by the given auth
// service.
func NewAuthHandler(auth authService) *AuthHandler {
	return &AuthHandler{auth: auth}
}

// Register creates a new user account and returns the created user
// with an access token.
func (h *AuthHandler) Register(ctx context.Context, req *gophkeeperv1.RegisterRequest) (*gophkeeperv1.RegisterResponse, error) {
	user, token, err := h.auth.Register(ctx, req.GetLogin(), req.GetPassword())
	if err != nil {
		return nil, toStatusError(err)
	}
	return &gophkeeperv1.RegisterResponse{User: userToProto(user), AccessToken: token}, nil
}

// Login verifies the credentials and returns the authenticated user
// with an access token.
func (h *AuthHandler) Login(ctx context.Context, req *gophkeeperv1.LoginRequest) (*gophkeeperv1.LoginResponse, error) {
	user, token, err := h.auth.Login(ctx, req.GetLogin(), req.GetPassword())
	if err != nil {
		return nil, toStatusError(err)
	}
	return &gophkeeperv1.LoginResponse{User: userToProto(user), AccessToken: token}, nil
}
