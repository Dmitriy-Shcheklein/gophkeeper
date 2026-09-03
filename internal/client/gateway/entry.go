package gateway

import (
	"context"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	v1 "github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
)

// EntryGateway is the client-side entry management API. The service
// layer depends on this interface (mockable in tests), not on the
// concrete Gateway.
type EntryGateway interface {
	// Create stores a new entry and returns it with server-assigned
	// id, version and timestamps.
	Create(ctx context.Context, entry *model.Entry) (*model.Entry, error)
	// Get returns a single entry by id.
	Get(ctx context.Context, id string) (*model.Entry, error)
	// List returns all entries of the authenticated user.
	List(ctx context.Context) ([]*model.Entry, error)
	// Update replaces an entry's content; it must carry the Version
	// the update is based on (optimistic locking).
	Update(ctx context.Context, entry *model.Entry) (*model.Entry, error)
	// Delete removes an entry by id.
	Delete(ctx context.Context, id string) error
	// Sync returns the full current set of the user's entries.
	Sync(ctx context.Context) ([]*model.Entry, error)
}

// compile-time assertion: Gateway implements EntryGateway.
var _ EntryGateway = (*Gateway)(nil)

// Create stores a new entry; id, version and timestamps of the
// argument are ignored by the server.
func (g *Gateway) Create(ctx context.Context, entry *model.Entry) (*model.Entry, error) {
	resp, err := g.entries.Create(ctx, &v1.CreateEntryRequest{
		Entry: entryToProto(entry),
	})
	if err != nil {
		return nil, translateError(err)
	}
	return entryFromProto(resp.GetEntry()), nil
}

// Get returns a single entry by id.
func (g *Gateway) Get(ctx context.Context, id string) (*model.Entry, error) {
	resp, err := g.entries.Get(ctx, &v1.GetEntryRequest{Id: id})
	if err != nil {
		return nil, translateError(err)
	}
	return entryFromProto(resp.GetEntry()), nil
}

// List returns all entries of the authenticated user.
func (g *Gateway) List(ctx context.Context) ([]*model.Entry, error) {
	resp, err := g.entries.List(ctx, &v1.ListEntriesRequest{})
	if err != nil {
		return nil, translateError(err)
	}
	return entriesFromProto(resp.GetEntries()), nil
}

// Update replaces an entry's content. The entry's Version field is
// passed to the server for optimistic locking; on a stale version the
// server fails the call and ErrConflict is returned.
func (g *Gateway) Update(ctx context.Context, entry *model.Entry) (*model.Entry, error) {
	resp, err := g.entries.Update(ctx, &v1.UpdateEntryRequest{
		Entry: entryToProto(entry),
	})
	if err != nil {
		return nil, translateError(err)
	}
	return entryFromProto(resp.GetEntry()), nil
}

// Delete removes an entry by id.
func (g *Gateway) Delete(ctx context.Context, id string) error {
	_, err := g.entries.Delete(ctx, &v1.DeleteEntryRequest{Id: id})
	if err != nil {
		return translateError(err)
	}
	return nil
}

// Sync returns the full current set of the user's entries.
func (g *Gateway) Sync(ctx context.Context) ([]*model.Entry, error) {
	resp, err := g.entries.Sync(ctx, &v1.SyncRequest{})
	if err != nil {
		return nil, translateError(err)
	}
	return entriesFromProto(resp.GetEntries()), nil
}
