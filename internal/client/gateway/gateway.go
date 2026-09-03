// Package gateway implements the GophKeeper gRPC client: a Gateway
// holding the connection and the access token, with token injection
// into outgoing requests, plus AuthGateway and EntryGateway
// implementations used by the service layer (and, through it, the CLI
// and TUI).
//
// Security note: the transport is deliberately insecure (no TLS) for
// now — the technical specification leaves transport security at the
// implementer's discretion, and the chosen deployment model is
// transport isolation (the client talks to the server over a trusted
// network segment). TLS can be enabled later by replacing
// insecure.NewCredentials with proper transport credentials; nothing
// else in this package depends on that choice.
package gateway

import (
	"context"
	"fmt"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/dmitriy/gophkeeper/internal/client/token"
	v1 "github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
)

const (
	// authorizationHeader is the outgoing metadata key carrying the
	// bearer token; it matches the server-side auth middleware.
	authorizationHeader = "authorization"

	// bearerPrefix is the token scheme expected by the server,
	// followed by a space.
	bearerPrefix = "Bearer "
)

// Gateway is the client's connection to the GophKeeper server.
//
// It owns the gRPC ClientConn, the current access token (in memory)
// and the token store for persistence. It implements both the
// AuthGateway and EntryGateway interfaces. All methods are safe for
// concurrent use.
//
// The connection is established lazily by grpc.NewClient: no network
// activity happens until the first RPC.
type Gateway struct {
	conn       *grpc.ClientConn
	tokenStore *token.Store

	auth    v1.AuthServiceClient
	entries v1.EntryServiceClient

	mu    sync.RWMutex
	token string
}

// New creates a Gateway dialing target (lazily) with insecure
// transport credentials — see the package comment for the security
// rationale.
//
// tokenStore must not be nil: Register and Login persist the obtained
// token through it so the next CLI invocation (a separate process)
// can pick the token up from disk. The constructor does NOT load a
// persisted token automatically — the service layer decides whether
// and when to do it (see SetToken).
func New(target string, tokenStore *token.Store) (*Gateway, error) {
	if tokenStore == nil {
		return nil, fmt.Errorf("gateway: token store must not be nil")
	}
	g := &Gateway{tokenStore: tokenStore}
	conn, err := grpc.NewClient(target, g.dialOptions()...)
	if err != nil {
		return nil, fmt.Errorf("gateway: create client: %w", err)
	}
	g.init(conn)
	return g, nil
}

// dialOptions returns the connection options, including the token
// injection interceptor bound to this Gateway.
func (g *Gateway) dialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(g.injectToken),
	}
}

// init finishes Gateway construction on an established connection.
func (g *Gateway) init(conn *grpc.ClientConn) {
	g.conn = conn
	g.auth = v1.NewAuthServiceClient(conn)
	g.entries = v1.NewEntryServiceClient(conn)
}

// injectToken is a unary client interceptor attaching the
// "authorization: Bearer <token>" metadata to every outgoing request
// when a token is set.
func (g *Gateway) injectToken(
	ctx context.Context,
	method string,
	req, reply any,
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption,
) error {
	if tok := g.Token(); tok != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, authorizationHeader, bearerPrefix+tok)
	}
	return invoker(ctx, method, req, reply, cc, opts...)
}

// SetToken stores the access token in memory. It is called after a
// successful Register/Login and may also be used by the service layer
// to restore a token loaded from the token store.
func (g *Gateway) SetToken(token string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.token = token
}

// Token returns the current in-memory access token, empty if none is
// set.
func (g *Gateway) Token() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.token
}

// HasToken reports whether an access token is set in memory.
func (g *Gateway) HasToken() bool {
	return g.Token() != ""
}

// Close closes the underlying connection. The Gateway must not be
// used afterwards.
func (g *Gateway) Close() error {
	if err := g.conn.Close(); err != nil {
		return fmt.Errorf("gateway: close connection: %w", err)
	}
	return nil
}

// TokenStore exposes the token store so the service layer can load a
// persisted token at startup and clear it on logout without the
// gateway growing a method for every such concern.
func (g *Gateway) TokenStore() *token.Store {
	return g.tokenStore
}
