package gateway

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	v1 "github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
)

// fakeEntryServer is a hand-written in-process implementation of the
// EntryService API: it captures every request and the authorization
// metadata, and returns canned responses and errors.
type fakeEntryServer struct {
	v1.UnimplementedEntryServiceServer

	mu         sync.Mutex
	authz      string
	lastCreate *v1.CreateEntryRequest
	lastGet    *v1.GetEntryRequest
	lastUpdate *v1.UpdateEntryRequest
	lastDelete *v1.DeleteEntryRequest

	createResp *v1.CreateEntryResponse
	getResp    *v1.GetEntryResponse
	listResp   *v1.ListEntriesResponse
	updateResp *v1.UpdateEntryResponse

	createErr, getErr, listErr, updateErr, deleteErr, syncErr error
}

func (f *fakeEntryServer) Create(ctx context.Context, req *v1.CreateEntryRequest) (*v1.CreateEntryResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authz = authzFromContext(ctx)
	f.lastCreate = req
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.createResp, nil
}

func (f *fakeEntryServer) Get(ctx context.Context, req *v1.GetEntryRequest) (*v1.GetEntryResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authz = authzFromContext(ctx)
	f.lastGet = req
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.getResp, nil
}

func (f *fakeEntryServer) List(ctx context.Context, _ *v1.ListEntriesRequest) (*v1.ListEntriesResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authz = authzFromContext(ctx)
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResp, nil
}

func (f *fakeEntryServer) Update(ctx context.Context, req *v1.UpdateEntryRequest) (*v1.UpdateEntryResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authz = authzFromContext(ctx)
	f.lastUpdate = req
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return f.updateResp, nil
}

func (f *fakeEntryServer) Delete(ctx context.Context, req *v1.DeleteEntryRequest) (*v1.DeleteEntryResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authz = authzFromContext(ctx)
	f.lastDelete = req
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	return &v1.DeleteEntryResponse{}, nil
}

func (f *fakeEntryServer) Sync(ctx context.Context, _ *v1.SyncRequest) (*v1.SyncResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authz = authzFromContext(ctx)
	if f.syncErr != nil {
		return nil, f.syncErr
	}
	return &v1.SyncResponse{Entries: f.listResp.GetEntries()}, nil
}

func (f *fakeEntryServer) sawAuthz() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.authz
}

// cannedEntry is a fully-populated proto entry with deterministic
// field values, used to verify conversion completeness.
func cannedProtoEntry() *v1.Entry {
	return &v1.Entry{
		Id:        "entry-42",
		Type:      v1.EntryType_ENTRY_TYPE_CARD,
		Label:     "Visa",
		Metadata:  "issued 2020",
		Data:      []byte{0xde, 0xad, 0xbe, 0xef},
		Version:   7,
		CreatedAt: 1700000000,
		UpdatedAt: 1700001234,
	}
}

func TestCreate(t *testing.T) {
	fake := &fakeEntryServer{createResp: &v1.CreateEntryResponse{Entry: cannedProtoEntry()}}
	g := newBufnetGateway(t, func(s *grpc.Server) {
		v1.RegisterEntryServiceServer(s, fake)
	})
	g.SetToken("tok-1")

	in := &model.Entry{
		Type:     model.EntryTypeCard,
		Label:    "Visa",
		Metadata: "issued 2020",
		Data:     []byte{0xde, 0xad, 0xbe, 0xef},
	}
	got, err := g.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got.ID != "entry-42" || got.Type != model.EntryTypeCard || got.Label != "Visa" ||
		got.Metadata != "issued 2020" || string(got.Data) != string([]byte{0xde, 0xad, 0xbe, 0xef}) ||
		got.Version != 7 || got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("Create returned %+v, want server-populated entry", got)
	}

	req := fake.lastCreate
	if req == nil {
		t.Fatal("server did not receive Create request")
	}
	if req.GetEntry().GetLabel() != "Visa" || req.GetEntry().GetMetadata() != "issued 2020" {
		t.Fatalf("Create payload = %+v, want label/metadata/data passed through", req.GetEntry())
	}
	if got := fake.sawAuthz(); got != "Bearer tok-1" {
		t.Fatalf("server saw authorization %q, want %q", got, "Bearer tok-1")
	}
}

func TestGet(t *testing.T) {
	fake := &fakeEntryServer{getResp: &v1.GetEntryResponse{Entry: cannedProtoEntry()}}
	g := newBufnetGateway(t, func(s *grpc.Server) {
		v1.RegisterEntryServiceServer(s, fake)
	})

	got, err := g.Get(context.Background(), "entry-42")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "entry-42" {
		t.Fatalf("Get ID = %q, want entry-42", got.ID)
	}
	if req := fake.lastGet; req == nil || req.GetId() != "entry-42" {
		t.Fatalf("Get request id = %+v, want entry-42", req)
	}
}

func TestList(t *testing.T) {
	fake := &fakeEntryServer{listResp: &v1.ListEntriesResponse{Entries: []*v1.Entry{
		cannedProtoEntry(),
		{Id: "entry-43", Type: v1.EntryType_ENTRY_TYPE_TEXT, Label: "Note", CreatedAt: 1, UpdatedAt: 2},
	}}}
	g := newBufnetGateway(t, func(s *grpc.Server) {
		v1.RegisterEntryServiceServer(s, fake)
	})

	got, err := g.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List len = %d, want 2", len(got))
	}
	if got[0].ID != "entry-42" || got[0].Type != model.EntryTypeCard {
		t.Fatalf("entry[0] = %+v, want card entry-42", got[0])
	}
	if got[1].ID != "entry-43" || got[1].Type != model.EntryTypeText {
		t.Fatalf("entry[1] = %+v, want text entry-43", got[1])
	}
}

func TestListEmpty(t *testing.T) {
	fake := &fakeEntryServer{listResp: &v1.ListEntriesResponse{}}
	g := newBufnetGateway(t, func(s *grpc.Server) {
		v1.RegisterEntryServiceServer(s, fake)
	})

	got, err := g.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List len = %d, want 0", len(got))
	}
}

func TestUpdatePassesVersion(t *testing.T) {
	fake := &fakeEntryServer{updateResp: &v1.UpdateEntryResponse{Entry: cannedProtoEntry()}}
	g := newBufnetGateway(t, func(s *grpc.Server) {
		v1.RegisterEntryServiceServer(s, fake)
	})

	in := cannedClientEntry()
	in.Version = 7
	in.Label = "Visa Renamed"
	got, err := g.Update(context.Background(), in)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	req := fake.lastUpdate
	if req == nil {
		t.Fatal("server did not receive Update request")
	}
	if req.GetEntry().GetVersion() != 7 {
		t.Fatalf("Update version = %d, want 7 (optimistic lock)", req.GetEntry().GetVersion())
	}
	if req.GetEntry().GetLabel() != "Visa Renamed" {
		t.Fatalf("Update label = %q, want Visa Renamed", req.GetEntry().GetLabel())
	}
	if got.Version != 7 {
		t.Fatalf("updated entry version = %d, want 7", got.Version)
	}
}

func TestDelete(t *testing.T) {
	fake := &fakeEntryServer{}
	g := newBufnetGateway(t, func(s *grpc.Server) {
		v1.RegisterEntryServiceServer(s, fake)
	})

	if err := g.Delete(context.Background(), "entry-42"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if req := fake.lastDelete; req == nil || req.GetId() != "entry-42" {
		t.Fatalf("Delete request = %+v, want id entry-42", req)
	}
}

func TestSync(t *testing.T) {
	fake := &fakeEntryServer{listResp: &v1.ListEntriesResponse{Entries: []*v1.Entry{
		cannedProtoEntry(),
	}}}
	g := newBufnetGateway(t, func(s *grpc.Server) {
		v1.RegisterEntryServiceServer(s, fake)
	})
	g.SetToken("tok-9")

	got, err := g.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(got) != 1 || got[0].ID != "entry-42" {
		t.Fatalf("Sync = %+v, want one entry-42", got)
	}
	if got := fake.sawAuthz(); got != "Bearer tok-9" {
		t.Fatalf("server saw authorization %q, want %q", got, "Bearer tok-9")
	}
}

// cannedClientEntry is the client-model mirror of cannedProtoEntry.
func cannedClientEntry() *model.Entry {
	return &model.Entry{
		ID:        "entry-42",
		Type:      model.EntryTypeCard,
		Label:     "Visa",
		Metadata:  "issued 2020",
		Data:      []byte{0xde, 0xad, 0xbe, 0xef},
		Version:   7,
		CreatedAt: time.Unix(1700000000, 0).UTC(),
		UpdatedAt: time.Unix(1700001234, 0).UTC(),
	}
}
