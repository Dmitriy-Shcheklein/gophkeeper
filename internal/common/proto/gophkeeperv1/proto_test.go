// Contract tests for the generated gophkeeper.v1 protobuf API.
//
// These tests are independent of the .proto sources: they pin the wire
// behaviour of the generated code (field round-trips, RPC method names,
// handler wiring) so that accidental regeneration mistakes or manual
// edits to *_pb.go files are caught by `go test ./...`.

package gophkeeperv1_test

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
)

// sampleEntry returns a fully populated Entry covering every field.
func sampleEntry() *gophkeeperv1.Entry {
	return &gophkeeperv1.Entry{
		Id:        "entry-1",
		Type:      gophkeeperv1.EntryType_ENTRY_TYPE_CARD,
		Label:     "Bank card",
		Metadata:  "expires 2030",
		Data:      []byte{0x01, 0x02, 0xff},
		Version:   7,
		CreatedAt: 1000,
		UpdatedAt: 2000,
	}
}

func sampleUser() *gophkeeperv1.User {
	return &gophkeeperv1.User{Id: "user-1", Login: "alice", CreatedAt: 42}
}

// TestMessageRoundTrip marshals and unmarshals every request/response
// message with all fields set and asserts the values survive the trip.
func TestMessageRoundTrip(t *testing.T) {
	entry := sampleEntry()

	// Every generated message embeds protoreflect.ProtoMessage, which
	// carries the String() method, so the combined interface lets the
	// loop call both proto.Marshal and String directly.
	msgs := []interface {
		proto.Message
		String() string
	}{
		&gophkeeperv1.RegisterRequest{Login: "alice", Password: "secret"},
		&gophkeeperv1.RegisterResponse{User: sampleUser(), AccessToken: "tok"},
		&gophkeeperv1.LoginRequest{Login: "alice", Password: "secret"},
		&gophkeeperv1.LoginResponse{User: sampleUser(), AccessToken: "tok"},
		&gophkeeperv1.CreateEntryRequest{Entry: entry},
		&gophkeeperv1.CreateEntryResponse{Entry: entry},
		&gophkeeperv1.GetEntryRequest{Id: entry.GetId()},
		&gophkeeperv1.GetEntryResponse{Entry: entry},
		&gophkeeperv1.ListEntriesRequest{},
		&gophkeeperv1.ListEntriesResponse{Entries: []*gophkeeperv1.Entry{entry}},
		&gophkeeperv1.UpdateEntryRequest{Entry: entry},
		&gophkeeperv1.UpdateEntryResponse{Entry: entry},
		&gophkeeperv1.DeleteEntryRequest{Id: entry.GetId()},
		&gophkeeperv1.DeleteEntryResponse{},
		&gophkeeperv1.SyncRequest{},
		&gophkeeperv1.SyncResponse{Entries: []*gophkeeperv1.Entry{entry}},
	}

	for _, m := range msgs {
		data, err := proto.Marshal(m)
		if err != nil {
			t.Fatalf("marshal %T: %v", m, err)
		}
		fresh := m.ProtoReflect().New().Interface()
		if err := proto.Unmarshal(data, fresh); err != nil {
			t.Fatalf("unmarshal %T: %v", m, err)
		}
		if !proto.Equal(m, fresh) {
			t.Errorf("%T did not survive a marshal/unmarshal round trip", m)
		}
		_ = m.String() // smoke-check: String() must not panic, even for empty messages
	}
}

// TestEntryFieldGetters checks that every Entry field getter returns the
// stored value and that getters on a nil message return the zero value
// instead of panicking.
func TestEntryFieldGetters(t *testing.T) {
	e := sampleEntry()
	if e.GetId() != "entry-1" ||
		e.GetType() != gophkeeperv1.EntryType_ENTRY_TYPE_CARD ||
		e.GetLabel() != "Bank card" ||
		e.GetMetadata() != "expires 2030" ||
		string(e.GetData()) != string([]byte{0x01, 0x02, 0xff}) ||
		e.GetVersion() != 7 ||
		e.GetCreatedAt() != 1000 ||
		e.GetUpdatedAt() != 2000 {
		t.Errorf("Entry getters returned unexpected values: %+v", e)
	}

	u := sampleUser()
	if u.GetId() != "user-1" || u.GetLogin() != "alice" || u.GetCreatedAt() != 42 {
		t.Errorf("User getters returned unexpected values: %+v", u)
	}

	var nilEntry *gophkeeperv1.Entry
	if nilEntry.GetId() != "" || nilEntry.GetType() != gophkeeperv1.EntryType_ENTRY_TYPE_UNSPECIFIED ||
		nilEntry.GetData() != nil || nilEntry.GetVersion() != 0 {
		t.Error("nil Entry getters must return zero values")
	}

	var nilUser *gophkeeperv1.User
	if nilUser.GetId() != "" || nilUser.GetLogin() != "" || nilUser.GetCreatedAt() != 0 {
		t.Error("nil User getters must return zero values")
	}

	var nilResp *gophkeeperv1.LoginResponse
	if nilResp.GetAccessToken() != "" || nilResp.GetUser() != nil {
		t.Error("nil LoginResponse getters must return zero values")
	}
}

func TestEntryTypeString(t *testing.T) {
	tests := map[gophkeeperv1.EntryType]string{
		gophkeeperv1.EntryType_ENTRY_TYPE_UNSPECIFIED:    "ENTRY_TYPE_UNSPECIFIED",
		gophkeeperv1.EntryType_ENTRY_TYPE_LOGIN_PASSWORD: "ENTRY_TYPE_LOGIN_PASSWORD",
		gophkeeperv1.EntryType_ENTRY_TYPE_TEXT:           "ENTRY_TYPE_TEXT",
		gophkeeperv1.EntryType_ENTRY_TYPE_BINARY:         "ENTRY_TYPE_BINARY",
		gophkeeperv1.EntryType_ENTRY_TYPE_CARD:           "ENTRY_TYPE_CARD",
		gophkeeperv1.EntryType(99):                       "99",
	}
	for et, want := range tests {
		if got := et.String(); got != want {
			t.Errorf("EntryType(%d).String() = %q, want %q", et, got, want)
		}
	}
}

// --- bufconn fixtures -------------------------------------------------------

// stubEntryService answers every RPC with a canned but consistent response.
type stubEntryService struct {
	gophkeeperv1.UnimplementedEntryServiceServer
	entry *gophkeeperv1.Entry
}

func (s *stubEntryService) Create(context.Context, *gophkeeperv1.CreateEntryRequest) (*gophkeeperv1.CreateEntryResponse, error) {
	return &gophkeeperv1.CreateEntryResponse{Entry: s.entry}, nil
}

func (s *stubEntryService) Get(context.Context, *gophkeeperv1.GetEntryRequest) (*gophkeeperv1.GetEntryResponse, error) {
	return &gophkeeperv1.GetEntryResponse{Entry: s.entry}, nil
}

func (s *stubEntryService) List(context.Context, *gophkeeperv1.ListEntriesRequest) (*gophkeeperv1.ListEntriesResponse, error) {
	return &gophkeeperv1.ListEntriesResponse{Entries: []*gophkeeperv1.Entry{s.entry}}, nil
}

func (s *stubEntryService) Update(context.Context, *gophkeeperv1.UpdateEntryRequest) (*gophkeeperv1.UpdateEntryResponse, error) {
	return &gophkeeperv1.UpdateEntryResponse{Entry: s.entry}, nil
}

func (s *stubEntryService) Delete(context.Context, *gophkeeperv1.DeleteEntryRequest) (*gophkeeperv1.DeleteEntryResponse, error) {
	return &gophkeeperv1.DeleteEntryResponse{}, nil
}

func (s *stubEntryService) Sync(context.Context, *gophkeeperv1.SyncRequest) (*gophkeeperv1.SyncResponse, error) {
	return &gophkeeperv1.SyncResponse{Entries: []*gophkeeperv1.Entry{s.entry}}, nil
}

type stubAuthService struct {
	gophkeeperv1.UnimplementedAuthServiceServer
}

func (stubAuthService) Register(context.Context, *gophkeeperv1.RegisterRequest) (*gophkeeperv1.RegisterResponse, error) {
	return &gophkeeperv1.RegisterResponse{User: sampleUser(), AccessToken: "tok"}, nil
}

func (stubAuthService) Login(context.Context, *gophkeeperv1.LoginRequest) (*gophkeeperv1.LoginResponse, error) {
	return &gophkeeperv1.LoginResponse{User: sampleUser(), AccessToken: "tok"}, nil
}

// dialBufconn starts an in-process gRPC server serving the stub
// implementations and returns a connected client plus a cleanup func.
func dialBufconn(t *testing.T, srv *grpc.Server) *grpc.ClientConn {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// TestRPCOverWire runs every client method against a stub server over a
// bufconn, verifying the generated full-method names, client wrappers and
// server wiring agree with each other.
func TestRPCOverWire(t *testing.T) {
	srv := grpc.NewServer()
	gophkeeperv1.RegisterAuthServiceServer(srv, stubAuthService{})
	gophkeeperv1.RegisterEntryServiceServer(srv, &stubEntryService{entry: sampleEntry()})

	conn := dialBufconn(t, srv)
	ctx := context.Background()

	auth := gophkeeperv1.NewAuthServiceClient(conn)
	reg, err := auth.Register(ctx, &gophkeeperv1.RegisterRequest{Login: "alice", Password: "secret"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if reg.GetAccessToken() != "tok" || reg.GetUser().GetLogin() != "alice" {
		t.Errorf("Register response = %+v", reg)
	}
	lg, err := auth.Login(ctx, &gophkeeperv1.LoginRequest{Login: "alice", Password: "secret"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if lg.GetAccessToken() != "tok" {
		t.Errorf("Login response = %+v", lg)
	}

	entries := gophkeeperv1.NewEntryServiceClient(conn)
	entry := sampleEntry()
	if _, err := entries.Create(ctx, &gophkeeperv1.CreateEntryRequest{Entry: entry}); err != nil {
		t.Errorf("Create: %v", err)
	}
	got, err := entries.Get(ctx, &gophkeeperv1.GetEntryRequest{Id: entry.GetId()})
	if err != nil {
		t.Errorf("Get: %v", err)
	} else if !proto.Equal(got.GetEntry(), entry) {
		t.Errorf("Get response entry = %+v, want %+v", got.GetEntry(), entry)
	}
	if _, err := entries.List(ctx, &gophkeeperv1.ListEntriesRequest{}); err != nil {
		t.Errorf("List: %v", err)
	}
	if _, err := entries.Update(ctx, &gophkeeperv1.UpdateEntryRequest{Entry: entry}); err != nil {
		t.Errorf("Update: %v", err)
	}
	if _, err := entries.Delete(ctx, &gophkeeperv1.DeleteEntryRequest{Id: entry.GetId()}); err != nil {
		t.Errorf("Delete: %v", err)
	}
	if _, err := entries.Sync(ctx, &gophkeeperv1.SyncRequest{}); err != nil {
		t.Errorf("Sync: %v", err)
	}
}

// TestUnimplementedServer checks that a server embedding only
// UnimplementedAuthServiceServer/UnimplementedEntryServiceServer answers
// every RPC with codes.Unimplemented.
func TestUnimplementedServer(t *testing.T) {
	srv := grpc.NewServer()
	gophkeeperv1.RegisterAuthServiceServer(srv, gophkeeperv1.UnimplementedAuthServiceServer{})
	gophkeeperv1.RegisterEntryServiceServer(srv, gophkeeperv1.UnimplementedEntryServiceServer{})

	conn := dialBufconn(t, srv)
	ctx := context.Background()

	auth := gophkeeperv1.NewAuthServiceClient(conn)
	entries := gophkeeperv1.NewEntryServiceClient(conn)

	rpcs := []struct {
		name string
		call func() error
	}{
		{"Register", func() error {
			_, err := auth.Register(ctx, &gophkeeperv1.RegisterRequest{})
			return err
		}},
		{"Login", func() error {
			_, err := auth.Login(ctx, &gophkeeperv1.LoginRequest{})
			return err
		}},
		{"Create", func() error {
			_, err := entries.Create(ctx, &gophkeeperv1.CreateEntryRequest{})
			return err
		}},
		{"Get", func() error {
			_, err := entries.Get(ctx, &gophkeeperv1.GetEntryRequest{})
			return err
		}},
		{"List", func() error {
			_, err := entries.List(ctx, &gophkeeperv1.ListEntriesRequest{})
			return err
		}},
		{"Update", func() error {
			_, err := entries.Update(ctx, &gophkeeperv1.UpdateEntryRequest{})
			return err
		}},
		{"Delete", func() error {
			_, err := entries.Delete(ctx, &gophkeeperv1.DeleteEntryRequest{})
			return err
		}},
		{"Sync", func() error {
			_, err := entries.Sync(ctx, &gophkeeperv1.SyncRequest{})
			return err
		}},
	}

	for _, tc := range rpcs {
		if err := tc.call(); status.Code(err) != codes.Unimplemented {
			t.Errorf("%s: code = %v, want Unimplemented (err = %v)", tc.name, status.Code(err), err)
		}
	}
}
