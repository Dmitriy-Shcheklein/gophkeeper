// Package transport implements the gRPC transport layer of the
// GophKeeper server: protobuf request/response handlers for the
// AuthService and EntryService, conversions between protobuf and
// domain models, extraction of the authenticated user identity from
// request contexts and mapping of domain errors to gRPC status codes.
package transport

import (
	"context"

	"github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
	"github.com/dmitriy/gophkeeper/internal/server/service"
)

// authService is the part of service.AuthService needed by AuthHandler.
// It is defined here (consumer side) so handlers can be unit tested
// with mocks; *service.AuthService satisfies it.
type authService interface {
	// Register validates the credentials and creates a user account,
	// returning an access token.
	Register(ctx context.Context, login, password string) (string, error)
	// Login verifies the credentials, returning an access token.
	Login(ctx context.Context, login, password string) (string, error)
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

// Register creates a new user account and returns an access token.
// The user field of the response is left unset: the service layer
// returns only the token, and the client already knows the login it
// registered.
func (h *AuthHandler) Register(ctx context.Context, req *gophkeeperv1.RegisterRequest) (*gophkeeperv1.RegisterResponse, error) {
	token, err := h.auth.Register(ctx, req.GetLogin(), req.GetPassword())
	if err != nil {
		return nil, toStatusError(err)
	}
	return &gophkeeperv1.RegisterResponse{AccessToken: token}, nil
}

// Login verifies the credentials and returns an access token.
// The user field of the response is left unset: the service layer
// returns only the token, and the client already knows the login it
// authenticated with.
func (h *AuthHandler) Login(ctx context.Context, req *gophkeeperv1.LoginRequest) (*gophkeeperv1.LoginResponse, error) {
	token, err := h.auth.Login(ctx, req.GetLogin(), req.GetPassword())
	if err != nil {
		return nil, toStatusError(err)
	}
	return &gophkeeperv1.LoginResponse{AccessToken: token}, nil
}
