package transport

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
	"github.com/dmitriy/gophkeeper/internal/server/auth"
	"github.com/dmitriy/gophkeeper/internal/server/model"
	"github.com/dmitriy/gophkeeper/internal/server/service"
)

// entryService is the part of service.EntryService needed by
// EntryHandler. It is defined here (consumer side) so handlers can be
// unit tested with mocks; *service.EntryService satisfies it.
type entryService interface {
	// Create stores a new entry for the given user.
	Create(ctx context.Context, userID string, entry *model.Entry) (*model.Entry, error)
	// Get returns a single entry of the user by id.
	Get(ctx context.Context, userID, entryID string) (*model.Entry, error)
	// List returns the entries of the user, optionally filtered by type.
	List(ctx context.Context, userID string, entryType *model.EntryType) ([]*model.Entry, error)
	// Update applies changes to an entry guarded by optimistic locking.
	Update(ctx context.Context, userID string, entry *model.Entry) (*model.Entry, error)
	// Delete removes an entry of the user by id.
	Delete(ctx context.Context, userID, entryID string) error
	// Sync returns the full current entry state of the user.
	Sync(ctx context.Context, userID string) ([]*model.Entry, error)
}

// EntryHandler implements gophkeeperv1.EntryServiceServer on top of the
// entry service, mapping domain errors to gRPC status codes. The user
// identity is taken exclusively from the request context claims placed
// by the auth interceptor; request-provided user identifiers are never
// trusted.
type EntryHandler struct {
	gophkeeperv1.UnimplementedEntryServiceServer

	entries entryService
}

// Compile-time interface satisfaction checks.
var (
	_ gophkeeperv1.EntryServiceServer = (*EntryHandler)(nil)
	_ entryService                    = (*service.EntryService)(nil)
)

// NewEntryHandler creates an EntryHandler backed by the given entry
// service.
func NewEntryHandler(entries entryService) *EntryHandler {
	return &EntryHandler{entries: entries}
}

// Create stores a new entry for the authenticated user. The entry id,
// version and timestamps in the request are ignored; the response
// carries the server-assigned values.
func (h *EntryHandler) Create(ctx context.Context, req *gophkeeperv1.CreateEntryRequest) (*gophkeeperv1.CreateEntryResponse, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	p := req.GetEntry()
	entry, err := h.entries.Create(ctx, userID, &model.Entry{
		Type:     entryTypeFromProto(p.GetType()),
		Label:    p.GetLabel(),
		Metadata: p.GetMetadata(),
		Data:     p.GetData(),
	})
	if err != nil {
		return nil, toStatusError(err)
	}
	return &gophkeeperv1.CreateEntryResponse{Entry: entryToProto(entry)}, nil
}

// Get returns a single entry of the authenticated user by id, or
// codes.NotFound if it does not exist.
func (h *EntryHandler) Get(ctx context.Context, req *gophkeeperv1.GetEntryRequest) (*gophkeeperv1.GetEntryResponse, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	entry, err := h.entries.Get(ctx, userID, req.GetId())
	if err != nil {
		return nil, toStatusError(err)
	}
	return &gophkeeperv1.GetEntryResponse{Entry: entryToProto(entry)}, nil
}

// List returns all entries of the authenticated user. The API has no
// type filter yet, so the service is asked for entries of all types.
func (h *EntryHandler) List(ctx context.Context, _ *gophkeeperv1.ListEntriesRequest) (*gophkeeperv1.ListEntriesResponse, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := h.entries.List(ctx, userID, nil)
	if err != nil {
		return nil, toStatusError(err)
	}
	return &gophkeeperv1.ListEntriesResponse{Entries: entriesToProto(entries)}, nil
}

// Update replaces the mutable content of an entry guarded by
// optimistic locking: the request must carry the version the update is
// based on, otherwise codes.FailedPrecondition is returned. The entry
// owner is always the authenticated user: the protobuf Entry message
// carries no user id and the handler passes the identity from the
// context claims to the service.
func (h *EntryHandler) Update(ctx context.Context, req *gophkeeperv1.UpdateEntryRequest) (*gophkeeperv1.UpdateEntryResponse, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	p := req.GetEntry()
	entry, err := h.entries.Update(ctx, userID, &model.Entry{
		ID:       p.GetId(),
		Type:     entryTypeFromProto(p.GetType()),
		Label:    p.GetLabel(),
		Metadata: p.GetMetadata(),
		Data:     p.GetData(),
		Version:  p.GetVersion(),
	})
	if err != nil {
		return nil, toStatusError(err)
	}
	return &gophkeeperv1.UpdateEntryResponse{Entry: entryToProto(entry)}, nil
}

// Delete removes an entry of the authenticated user by id, or returns
// codes.NotFound if it does not exist.
func (h *EntryHandler) Delete(ctx context.Context, req *gophkeeperv1.DeleteEntryRequest) (*gophkeeperv1.DeleteEntryResponse, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.entries.Delete(ctx, userID, req.GetId()); err != nil {
		return nil, toStatusError(err)
	}
	return &gophkeeperv1.DeleteEntryResponse{}, nil
}

// Sync returns the full current set of the authenticated user's
// entries so several authorized clients of the same owner converge on
// the same server state.
func (h *EntryHandler) Sync(ctx context.Context, _ *gophkeeperv1.SyncRequest) (*gophkeeperv1.SyncResponse, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := h.entries.Sync(ctx, userID)
	if err != nil {
		return nil, toStatusError(err)
	}
	return &gophkeeperv1.SyncResponse{Entries: entriesToProto(entries)}, nil
}

// userIDFromContext extracts the user id of the authenticated caller
// from the context claims placed by the auth interceptor. Missing
// claims are impossible behind the interceptor; the defensive
// codes.Internal response makes the misconfiguration visible instead
// of treating the request as belonging to an empty user.
func userIDFromContext(ctx context.Context) (string, error) {
	claims, ok := auth.ClaimsFromContext(ctx)
	if !ok {
		return "", status.Error(codes.Internal, "missing user identity")
	}
	return claims.UserID, nil
}
