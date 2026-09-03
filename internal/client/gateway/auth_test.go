package gateway

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v1 "github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
)

func TestRegisterSuccess(t *testing.T) {
	auth := &fakeAuthService{registerToken: "reg-token"}
	g := newBufnetGateway(t, func(s *grpc.Server) {
		v1.RegisterAuthServiceServer(s, auth)
	})

	got, err := g.Register(context.Background(), "alice", "secret")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if got != "reg-token" {
		t.Fatalf("Register token = %q, want %q", got, "reg-token")
	}
	if req := auth.lastRegister; req == nil {
		t.Fatal("server did not receive Register request")
	} else if req.GetLogin() != "alice" || req.GetPassword() != "secret" {
		t.Fatalf("credentials = (%q, %q), want (alice, secret)", req.GetLogin(), req.GetPassword())
	}
	if !g.HasToken() || g.Token() != "reg-token" {
		t.Fatalf("token not set in memory: HasToken=%v Token=%q", g.HasToken(), g.Token())
	}

	persisted, err := g.tokenStore.Load()
	if err != nil {
		t.Fatalf("tokenStore.Load: %v", err)
	}
	if persisted != "reg-token" {
		t.Fatalf("persisted token = %q, want %q", persisted, "reg-token")
	}
}

func TestLoginSuccess(t *testing.T) {
	auth := &fakeAuthService{loginToken: "login-token"}
	g := newBufnetGateway(t, func(s *grpc.Server) {
		v1.RegisterAuthServiceServer(s, auth)
	})

	got, err := g.Login(context.Background(), "alice", "secret")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if got != "login-token" {
		t.Fatalf("Login token = %q, want %q", got, "login-token")
	}
	if req := auth.lastLogin; req == nil {
		t.Fatal("server did not receive Login request")
	} else if req.GetLogin() != "alice" || req.GetPassword() != "secret" {
		t.Fatalf("credentials = (%q, %q), want (alice, secret)", req.GetLogin(), req.GetPassword())
	}
	if !g.HasToken() || g.Token() != "login-token" {
		t.Fatalf("token not set in memory: HasToken=%v Token=%q", g.HasToken(), g.Token())
	}

	persisted, err := g.tokenStore.Load()
	if err != nil {
		t.Fatalf("tokenStore.Load: %v", err)
	}
	if persisted != "login-token" {
		t.Fatalf("persisted token = %q, want %q", persisted, "login-token")
	}
}

func TestLoginInvalidCredentials(t *testing.T) {
	auth := &fakeAuthService{
		loginErr: status.Error(codes.Unauthenticated, "invalid credentials"),
	}
	g := newBufnetGateway(t, func(s *grpc.Server) {
		v1.RegisterAuthServiceServer(s, auth)
	})

	_, err := g.Login(context.Background(), "alice", "wrong")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("error = %v, want ErrUnauthenticated", err)
	}
	if g.HasToken() {
		t.Fatal("HasToken = true after failed Login, want false")
	}
}

func TestRegisterEmptyTokenStillAccepted(t *testing.T) {
	// The server decides what a success looks like; the gateway
	// accepts an empty token (HasToken stays false) without failing.
	auth := &fakeAuthService{registerToken: ""}
	g := newBufnetGateway(t, func(s *grpc.Server) {
		v1.RegisterAuthServiceServer(s, auth)
	})

	got, err := g.Register(context.Background(), "alice", "secret")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got != "" {
		t.Fatalf("token = %q, want empty", got)
	}
	if g.HasToken() {
		t.Fatal("HasToken = true, want false")
	}
}

func TestAuthInterfaceCompliance(_ *testing.T) {
	var _ AuthGateway = (*Gateway)(nil)
}
