package gateway

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/dmitriy/gophkeeper/internal/client/token"
	v1 "github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
)

// authzFromContext extracts the raw "authorization" metadata value
// seen by the server side of the bufconn connection.
func authzFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	if values := md.Get("authorization"); len(values) > 0 {
		return values[0]
	}
	return ""
}

// mustStore returns a token.Store pointed at a per-test temp file.
func mustStore(t *testing.T) *token.Store {
	t.Helper()
	store, err := token.New(filepath.Join(t.TempDir(), "token"))
	if err != nil {
		t.Fatalf("token.New: %v", err)
	}
	return store
}

// newBufnetGateway starts an in-process gRPC server on a bufconn
// listener, registers services with reg, and returns a Gateway wired
// to it with a fresh token store. Everything is cleaned up on test
// completion.
func newBufnetGateway(t *testing.T, reg func(s *grpc.Server)) *Gateway {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	reg(srv)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	g := &Gateway{tokenStore: mustStore(t)}
	opts := append(g.dialOptions(), grpc.WithContextDialer(
		func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		},
	))
	conn, err := grpc.NewClient("passthrough:///bufnet", opts...)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	g.init(conn)
	return g
}

// fakeAuthService is a hand-written in-process implementation of the
// AuthService API capturing requests and the authorization metadata,
// with canned tokens and errors.
type fakeAuthService struct {
	v1.UnimplementedAuthServiceServer

	mu            sync.Mutex
	authz         string
	lastRegister  *v1.RegisterRequest
	lastLogin     *v1.LoginRequest
	registerToken string
	loginToken    string
	registerErr   error
	loginErr      error
}

func (f *fakeAuthService) Register(ctx context.Context, req *v1.RegisterRequest) (*v1.RegisterResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authz = authzFromContext(ctx)
	f.lastRegister = req
	if f.registerErr != nil {
		return nil, f.registerErr
	}
	return &v1.RegisterResponse{AccessToken: f.registerToken}, nil
}

func (f *fakeAuthService) Login(ctx context.Context, req *v1.LoginRequest) (*v1.LoginResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authz = authzFromContext(ctx)
	f.lastLogin = req
	if f.loginErr != nil {
		return nil, f.loginErr
	}
	return &v1.LoginResponse{AccessToken: f.loginToken}, nil
}

func (f *fakeAuthService) sawAuthz() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.authz
}

func TestNewRejectsNilTokenStore(t *testing.T) {
	if _, err := New("localhost:1", nil); err == nil {
		t.Fatal("New with nil token store returned nil error, want error")
	}
}

func TestGatewayTokenAccessors(t *testing.T) {
	g, err := New("localhost:1", mustStore(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = g.Close() }()

	if g.HasToken() {
		t.Fatal("HasToken = true on fresh gateway, want false")
	}
	if got := g.Token(); got != "" {
		t.Fatalf("Token = %q, want empty", got)
	}

	g.SetToken("tok-1")
	if !g.HasToken() {
		t.Fatal("HasToken = false after SetToken, want true")
	}
	if got := g.Token(); got != "tok-1" {
		t.Fatalf("Token = %q, want %q", got, "tok-1")
	}
}

func TestGatewayClose(t *testing.T) {
	g, err := New("localhost:1", mustStore(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := g.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestGatewayTokenStoreAccessor(t *testing.T) {
	g, err := New("localhost:1", mustStore(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = g.Close() }()

	if g.TokenStore() == nil {
		t.Fatal("TokenStore = nil, want the store passed to New")
	}
}

func TestGatewayInjectsBearerToken(t *testing.T) {
	auth := &fakeAuthService{loginToken: "tok"}
	g := newBufnetGateway(t, func(s *grpc.Server) {
		v1.RegisterAuthServiceServer(s, auth)
	})

	g.SetToken("tok-123")
	if _, err := g.Login(context.Background(), "user", "pass"); err != nil {
		t.Fatalf("Login: %v", err)
	}

	if got := auth.sawAuthz(); got != "Bearer tok-123" {
		t.Fatalf("server saw authorization %q, want %q", got, "Bearer tok-123")
	}
}

func TestGatewayOmitsHeaderWithoutToken(t *testing.T) {
	auth := &fakeAuthService{loginToken: "tok"}
	g := newBufnetGateway(t, func(s *grpc.Server) {
		v1.RegisterAuthServiceServer(s, auth)
	})

	if _, err := g.Login(context.Background(), "user", "pass"); err != nil {
		t.Fatalf("Login: %v", err)
	}

	if got := auth.sawAuthz(); got != "" {
		t.Fatalf("server saw authorization %q, want no header", got)
	}
}

func TestRegisterPersistsTokenFailure(t *testing.T) {
	// Make Save fail: the store path sits under a regular file, so
	// creating the token directory fails.
	base := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(base, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	store, err := token.New(filepath.Join(base, "token"))
	if err != nil {
		t.Fatalf("token.New: %v", err)
	}

	g := newBufnetGateway(t, func(s *grpc.Server) {
		v1.RegisterAuthServiceServer(s, &fakeAuthService{registerToken: "tok"})
	})
	g.tokenStore = store

	if _, err := g.Register(context.Background(), "user", "pass"); err == nil {
		t.Fatal("Register returned nil error, want persistence error")
	}
}

// errorTranslationTests drive every mapped gRPC code through a real
// RPC against the bufconn fake to verify the translation table.
var errorTranslationTests = []struct {
	name    string
	code    codes.Code
	want    error
	wantMsg string
}{
	{
		name:    "not found",
		code:    codes.NotFound,
		want:    ErrNotFound,
		wantMsg: "entry not found",
	},
	{
		name:    "already exists",
		code:    codes.AlreadyExists,
		want:    ErrAlreadyExists,
		wantMsg: "entry already exists",
	},
	{
		name:    "failed precondition",
		code:    codes.FailedPrecondition,
		want:    ErrConflict,
		wantMsg: "entry was modified, re-fetch and retry",
	},
	{
		name:    "unauthenticated",
		code:    codes.Unauthenticated,
		want:    ErrUnauthenticated,
		wantMsg: "not logged in or session expired, please log in",
	},
	{
		name:    "invalid argument wrapped with server message",
		code:    codes.InvalidArgument,
		want:    nil,
		wantMsg: "gateway: InvalidArgument: label must not be empty",
	},
	{
		name:    "unavailable wrapped with code and message",
		code:    codes.Unavailable,
		want:    nil,
		wantMsg: "gateway: Unavailable: connection refused",
	},
}

func TestEntryErrorTranslation(t *testing.T) {
	for _, tt := range errorTranslationTests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeEntryServer{deleteErr: status.Error(tt.code, serverMessageFor(tt.code))}
			g := newBufnetGateway(t, func(s *grpc.Server) {
				v1.RegisterEntryServiceServer(s, fake)
			})

			err := g.Delete(context.Background(), "id-1")

			if tt.want != nil {
				if !errors.Is(err, tt.want) {
					t.Fatalf("error = %v, want %v", err, tt.want)
				}
				if err.Error() != tt.wantMsg {
					t.Fatalf("message = %q, want %q", err.Error(), tt.wantMsg)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantMsg)
			}
		})
	}
}

func TestTranslateNonStatusError(t *testing.T) {
	sentinel := errors.New("context deadline exceeded")
	err := translateError(sentinel)
	if !errors.Is(err, sentinel) {
		t.Fatalf("translateError dropped the original error: %v", err)
	}
	if !strings.HasPrefix(err.Error(), "gateway:") {
		t.Fatalf("error = %q, want gateway-prefixed", err.Error())
	}
}

func TestTranslateNilError(t *testing.T) {
	if err := translateError(nil); err != nil {
		t.Fatalf("translateError(nil) = %v, want nil", err)
	}
}

// serverMessageFor returns a distinctive status message for a code,
// used to verify the message survives translation.
func serverMessageFor(code codes.Code) string {
	switch code {
	case codes.InvalidArgument:
		return "label must not be empty"
	case codes.Unavailable:
		return "connection refused"
	default:
		return "server said no"
	}
}
