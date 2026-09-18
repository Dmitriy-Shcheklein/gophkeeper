package gateway

import (
	"context"
	"errors"
	"io"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	v1 "github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
)

// ChunkSize is the payload chunk size used by the streaming upload
// (1 MiB). It must not exceed the server-side MaxChunkSize.
const ChunkSize = 1 << 20

// EntryGateway is the client-side entry management API. The service
// layer depends on this interface (mockable in tests), not on the
// concrete Gateway.
type EntryGateway interface {
	// Create stores a new entry and returns it with server-assigned
	// id, version and timestamps.
	Create(ctx context.Context, entry *model.Entry) (*model.Entry, error)
	// Get returns a single entry by id. For chunked entries the payload
	// is not carried: Data is empty and DataSize reports the content
	// size; use DownloadEntryData to fetch it.
	Get(ctx context.Context, id string) (*model.Entry, error)
	// List returns all entries of the authenticated user. Payloads are
	// carried only when includeData is set.
	List(ctx context.Context, includeData bool) ([]*model.Entry, error)
	// Update replaces an entry's content; it must carry the Version
	// the update is based on (optimistic locking).
	Update(ctx context.Context, entry *model.Entry) (*model.Entry, error)
	// Delete removes an entry by id.
	Delete(ctx context.Context, id string) error
	// Sync returns the full current set of the user's entries. Payloads
	// are carried only when includeData is set.
	Sync(ctx context.Context, includeData bool) ([]*model.Entry, error)
	// Upload starts a chunked upload stream. The returned stream must
	// be driven: SendHeader first, then any number of SendChunk calls,
	// then CloseAndCommit with the hex SHA-256 digest of the payload.
	Upload(ctx context.Context) (UploadStream, error)
	// DownloadEntryData streams the payload of a single entry: RecvSize
	// reports the total size, RecvChunk yields the pieces until io.EOF.
	DownloadEntryData(ctx context.Context, id string) (DownloadStream, error)
}

// UploadStream is one chunked upload in progress.
type UploadStream interface {
	// SendHeader describes the entry being uploaded: expectedVersion 0
	// creates a new entry, a positive value updates the existing one.
	SendHeader(entry *model.Entry, expectedVersion int64) error
	// SendChunk sends one payload piece.
	SendChunk(data []byte) error
	// CloseAndCommit ends the stream with the payload digest and
	// returns the stored entry.
	CloseAndCommit(sha256hex string) (*model.Entry, error)
}

// DownloadStream is one payload download in progress.
type DownloadStream interface {
	// RecvSize reports the total payload size; it must be called
	// before RecvChunk.
	RecvSize() (int64, error)
	// RecvChunk returns the next payload piece; io.EOF marks the end.
	RecvChunk() ([]byte, error)
	// Close releases the stream.
	Close() error
}

// compile-time assertion: Gateway implements EntryGateway.
var _ EntryGateway = (*Gateway)(nil)

// Create stores a new entry; id, version and timestamps of the
// argument are ignored by the server.
func (g *Gateway) Create(ctx context.Context, entry *model.Entry) (*model.Entry, error) {
	resp, err := g.entries.Create(ctx, v1.CreateEntryRequest_builder{
		Entry: entryToProto(entry),
	}.Build())
	if err != nil {
		return nil, translateError(err)
	}
	return entryFromProto(resp.GetEntry()), nil
}

// Get returns a single entry by id.
func (g *Gateway) Get(ctx context.Context, id string) (*model.Entry, error) {
	resp, err := g.entries.Get(ctx, v1.GetEntryRequest_builder{Id: id}.Build())
	if err != nil {
		return nil, translateError(err)
	}
	return entryFromProto(resp.GetEntry()), nil
}

// List returns all entries of the authenticated user. Payloads are
// carried only when includeData is set; otherwise entries come with
// DataSize only.
func (g *Gateway) List(ctx context.Context, includeData bool) ([]*model.Entry, error) {
	resp, err := g.entries.List(ctx, v1.ListEntriesRequest_builder{IncludeData: includeData}.Build())
	if err != nil {
		return nil, translateError(err)
	}
	return entriesFromProto(resp.GetEntries()), nil
}

// Update replaces an entry's content. The entry's Version field is
// passed to the server for optimistic locking; on a stale version the
// server fails the call and ErrConflict is returned.
func (g *Gateway) Update(ctx context.Context, entry *model.Entry) (*model.Entry, error) {
	resp, err := g.entries.Update(ctx, v1.UpdateEntryRequest_builder{
		Entry: entryToProto(entry),
	}.Build())
	if err != nil {
		return nil, translateError(err)
	}
	return entryFromProto(resp.GetEntry()), nil
}

// Delete removes an entry by id.
func (g *Gateway) Delete(ctx context.Context, id string) error {
	_, err := g.entries.Delete(ctx, v1.DeleteEntryRequest_builder{Id: id}.Build())
	if err != nil {
		return translateError(err)
	}
	return nil
}

// Sync returns the full current set of the user's entries. Payloads
// are carried only when includeData is set.
func (g *Gateway) Sync(ctx context.Context, includeData bool) ([]*model.Entry, error) {
	resp, err := g.entries.Sync(ctx, v1.SyncRequest_builder{IncludeData: includeData}.Build())
	if err != nil {
		return nil, translateError(err)
	}
	return entriesFromProto(resp.GetEntries()), nil
}

// Upload starts a chunked upload stream.
func (g *Gateway) Upload(ctx context.Context) (UploadStream, error) {
	s, err := g.entries.Upload(ctx)
	if err != nil {
		return nil, translateError(err)
	}
	return &uploadStream{stream: s}, nil
}

// DownloadEntryData streams the payload of a single entry.
func (g *Gateway) DownloadEntryData(ctx context.Context, id string) (DownloadStream, error) {
	s, err := g.entries.DownloadEntryData(ctx, v1.DownloadEntryDataRequest_builder{Id: id}.Build())
	if err != nil {
		return nil, translateError(err)
	}
	return &downloadStream{stream: s}, nil
}

// uploadStream adapts the generated client stream to UploadStream.
type uploadStream struct {
	stream v1.EntryService_UploadClient
	header bool
}

// SendHeader sends the first message of the stream.
func (u *uploadStream) SendHeader(entry *model.Entry, expectedVersion int64) error {
	if u.header {
		return errors.New("gateway: upload header already sent")
	}
	u.header = true
	return u.stream.Send(v1.UploadEntryRequest_builder{
		Header: v1.UploadEntryHeader_builder{
			Entry:           entryToProto(entry),
			ExpectedVersion: expectedVersion,
		}.Build(),
	}.Build())
}

// SendChunk sends one payload chunk.
func (u *uploadStream) SendChunk(data []byte) error {
	if !u.header {
		return errors.New("gateway: upload header not sent")
	}
	return u.stream.Send(v1.UploadEntryRequest_builder{
		Chunk: v1.UploadEntryChunk_builder{Data: data}.Build(),
	}.Build())
}

// CloseAndCommit closes the stream with the footer and returns the
// stored entry.
func (u *uploadStream) CloseAndCommit(sha256hex string) (*model.Entry, error) {
	if !u.header {
		return nil, errors.New("gateway: upload header not sent")
	}
	if err := u.stream.Send(v1.UploadEntryRequest_builder{
		Footer: v1.UploadEntryFooter_builder{Sha256: sha256hex}.Build(),
	}.Build()); err != nil {
		return nil, translateError(err)
	}
	resp, err := u.stream.CloseAndRecv()
	if err != nil {
		return nil, translateError(err)
	}
	return entryFromProto(resp.GetEntry()), nil
}

// downloadStream adapts the generated client stream to DownloadStream.
type downloadStream struct {
	stream   v1.EntryService_DownloadEntryDataClient
	gotSize  bool
	recvDone bool
}

// RecvSize receives the header message and reports the payload size.
func (d *downloadStream) RecvSize() (int64, error) {
	if d.gotSize {
		return 0, errors.New("gateway: download size already received")
	}
	msg, err := d.stream.Recv()
	if err != nil {
		return 0, translateError(err)
	}
	header := msg.GetHeader()
	if header == nil {
		return 0, errors.New("gateway: download must start with a size header")
	}
	d.gotSize = true
	return header.GetSize(), nil
}

// RecvChunk receives the next payload chunk; io.EOF marks the end.
func (d *downloadStream) RecvChunk() ([]byte, error) {
	if !d.gotSize {
		return nil, errors.New("gateway: download size not received")
	}
	if d.recvDone {
		return nil, io.EOF
	}
	msg, err := d.stream.Recv()
	if errors.Is(err, io.EOF) {
		d.recvDone = true
		return nil, io.EOF
	}
	if err != nil {
		return nil, translateError(err)
	}
	chunk := msg.GetChunk()
	if chunk == nil {
		return nil, errors.New("gateway: unexpected message in download stream")
	}
	return chunk.GetData(), nil
}

// Close releases the download stream. Server-streaming RPCs need no
// explicit close on the client side: the stream ends when the server
// finishes or the context is cancelled.
func (d *downloadStream) Close() error {
	return nil
}
