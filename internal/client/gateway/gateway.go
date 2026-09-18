// Package gateway implements the GophKeeper gRPC client: a Gateway
// holding the connection and the access token, with token injection
// into outgoing requests, plus AuthGateway and EntryGateway
// implementations used by the service layer (and, through it, the CLI
// and TUI).
//
// Security note: the transport is always TLS. The caller supplies the
// *tls.Config: pass a config with a pinned CA certificate (--tls-ca,
// self-signed server scenario) or nil to verify the server against
// the system root certificate store (public CA / autocert scenario).
// Plaintext connections are not supported.
package gateway

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
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

// New creates a Gateway dialing target (lazily) over TLS. The
// transport is always encrypted: pass tlsCfg with a pinned CA
// certificate for the self-signed server scenario, or nil to verify
// the server against the system root certificate store (public CA /
// autocert scenario). Plaintext connections are not supported.
//
// tokenStore must not be nil: Register and Login persist the obtained
// token through it so the next CLI invocation (a separate process)
// can pick the token up from disk. The constructor does NOT load a
// persisted token automatically — the service layer decides whether
// and when to do it (see SetToken).
func New(target string, tokenStore *token.Store, tlsCfg *tls.Config) (*Gateway, error) {
	resolved, err := resolveTLSConfig(tlsCfg)
	if err != nil {
		return nil, err
	}
	if tokenStore == nil {
		return nil, fmt.Errorf("gateway: token store must not be nil")
	}
	g := &Gateway{tokenStore: tokenStore}
	conn, err := grpc.NewClient(target, g.dialOptions(resolved)...)
	if err != nil {
		return nil, fmt.Errorf("gateway: create client: %w", err)
	}
	g.init(conn)
	return g, nil
}

// resolveTLSConfig fills in the defaults for a TLS config: nil means
// system root certificate store; a provided config keeps its RootCAs
// (pinned CA) and gets a safe minimum TLS version.
func resolveTLSConfig(tlsCfg *tls.Config) (*tls.Config, error) {
	if tlsCfg == nil {
		return &tls.Config{MinVersion: tls.VersionTLS12}, nil
	}
	resolved := tlsCfg.Clone()
	if resolved.MinVersion == 0 {
		resolved.MinVersion = tls.VersionTLS12
	}
	return resolved, nil
}

// LoadTLSConfigWithCA builds a TLS config that verifies the server
// certificate against the CA certificate in caFile (self-signed
// scenario). The file must contain a PEM certificate; it is added to
// the trust pool alongside the system roots. caFile may be empty to
// skip pinning and use only the system roots.
func LoadTLSConfigWithCA(caFile string) (*tls.Config, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile == "" {
		return tlsCfg, nil
	}
	pemBytes, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("gateway: read TLS CA file %s: %w", caFile, err)
	}
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(pemBytes); !ok {
		return nil, fmt.Errorf("gateway: no PEM certificate found in TLS CA file %s", caFile)
	}
	tlsCfg.RootCAs = pool
	return tlsCfg, nil
}

// dialOptions returns the connection options, including the token
// injection interceptors bound to this Gateway (both unary and
// streaming RPCs) and the raised message size limits matching the
// server's default maximum payload size.
func (g *Gateway) dialOptions(tlsCfg *tls.Config) []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithUnaryInterceptor(g.injectToken),
		grpc.WithStreamInterceptor(g.injectTokenStream),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(defaultMaxMsgSize),
			grpc.MaxCallRecvMsgSize(defaultMaxMsgSize),
		),
	}
}

// defaultMaxMsgSize is the client-side gRPC message size cap (1 GiB
// plus headroom): it matches the server's default maximum payload
// size so unary calls with large payloads are not silently rejected
// by the client transport before reaching the server.
const defaultMaxMsgSize = 1<<30 + 1<<20

// injectTokenStream is the streaming counterpart of injectToken: it
// attaches the "authorization: Bearer <token>" metadata to every
// outgoing streaming RPC when a token is set.
func (g *Gateway) injectTokenStream(
	ctx context.Context,
	desc *grpc.StreamDesc,
	cc *grpc.ClientConn,
	method string,
	streamer grpc.Streamer,
	opts ...grpc.CallOption,
) (grpc.ClientStream, error) {
	if tok := g.Token(); tok != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, authorizationHeader, bearerPrefix+tok)
	}
	return streamer(ctx, desc, cc, method, opts...)
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
