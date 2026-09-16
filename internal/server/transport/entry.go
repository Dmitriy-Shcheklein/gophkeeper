package transport

import (
	"context"
	"errors"
	"io"

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
	// Payloads are only loaded when includeData is set.
	List(ctx context.Context, userID string, entryType *model.EntryType, includeData bool) ([]*model.Entry, error)
	// Update applies changes to an entry guarded by optimistic locking.
	Update(ctx context.Context, userID string, entry *model.Entry) (*model.Entry, error)
	// Delete removes an entry of the user by id.
	Delete(ctx context.Context, userID, entryID string) error
	// Sync returns the full current entry state of the user.
	Sync(ctx context.Context, userID string, includeData bool) ([]*model.Entry, error)
	// BeginUpload starts a chunked upload (see service.UploadSession).
	BeginUpload(ctx context.Context, userID string, header *model.Entry, expectedVersion int64) (service.UploadSession, error)
	// EntryDataInfo describes the payload layout for download streaming.
	EntryDataInfo(ctx context.Context, userID, entryID string) (chunkCount int, size int64, err error)
	// DownloadChunk returns one stored payload chunk.
	DownloadChunk(ctx context.Context, userID, entryID string, seq int) ([]byte, error)
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
	return gophkeeperv1.CreateEntryResponse_builder{Entry: entryToProto(entry)}.Build(), nil
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
	return gophkeeperv1.GetEntryResponse_builder{Entry: entryToProto(entry)}.Build(), nil
}

// List returns all entries of the authenticated user. Payloads are
// carried only when the request sets include_data; otherwise the
// entries come with data_size so clients can fetch content on demand.
func (h *EntryHandler) List(ctx context.Context, req *gophkeeperv1.ListEntriesRequest) (*gophkeeperv1.ListEntriesResponse, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := h.entries.List(ctx, userID, nil, req.GetIncludeData())
	if err != nil {
		return nil, toStatusError(err)
	}
	return gophkeeperv1.ListEntriesResponse_builder{Entries: entriesToProto(entries)}.Build(), nil
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
	return gophkeeperv1.UpdateEntryResponse_builder{Entry: entryToProto(entry)}.Build(), nil
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
// the same server state. Payloads are carried only when the request
// sets include_data.
func (h *EntryHandler) Sync(ctx context.Context, req *gophkeeperv1.SyncRequest) (*gophkeeperv1.SyncResponse, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := h.entries.Sync(ctx, userID, req.GetIncludeData())
	if err != nil {
		return nil, toStatusError(err)
	}
	return gophkeeperv1.SyncResponse_builder{Entries: entriesToProto(entries)}.Build(), nil
}

// Upload implements the client-streaming RPC: the first message must
// carry the entry header, the last one the payload digest, everything
// in between are payload chunks. The entry becomes visible only after
// the stream completes successfully; a failed or aborted stream rolls
// the upload back.
func (h *EntryHandler) Upload(stream gophkeeperv1.EntryService_UploadServer) error {
	userID, err := userIDFromContext(stream.Context())
	if err != nil {
		return err
	}

	first, err := stream.Recv()
	if err != nil {
		return status.Error(codes.InvalidArgument, "read upload header: "+err.Error())
	}
	header := first.GetHeader()
	if header == nil {
		return status.Error(codes.InvalidArgument, "upload must start with a header")
	}

	session, err := h.entries.BeginUpload(stream.Context(), userID, &model.Entry{
		Type:     entryTypeFromProto(header.GetEntry().GetType()),
		Label:    header.GetEntry().GetLabel(),
		Metadata: header.GetEntry().GetMetadata(),
		ID:       header.GetEntry().GetId(),
	}, header.GetExpectedVersion())
	if err != nil {
		return toStatusError(err)
	}
	// Roll back whatever was persisted if the stream does not end in a
	// successful commit.
	committed := false
	defer func() {
		if !committed {
			session.Abort(stream.Context())
		}
	}()

	var digest string
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return status.Error(codes.Internal, "read upload message: "+err.Error())
		}
		switch req.WhichPayload() {
		case gophkeeperv1.UploadEntryRequest_Chunk_case:
			if digest != "" {
				return status.Error(codes.InvalidArgument, "chunk after footer")
			}
			if err := session.AddChunk(req.GetChunk().GetData()); err != nil {
				return toStatusError(err)
			}
		case gophkeeperv1.UploadEntryRequest_Footer_case:
			digest = req.GetFooter().GetSha256()
		default:
			return status.Error(codes.InvalidArgument, "unexpected message type in upload stream")
		}
	}
	if digest == "" {
		return status.Error(codes.InvalidArgument, "upload must end with a footer")
	}

	entry, err := session.Commit(stream.Context(), digest)
	if err != nil {
		return toStatusError(err)
	}
	committed = true

	return stream.SendAndClose(gophkeeperv1.UploadEntryResponse_builder{Entry: entryToProto(entry)}.Build())
}

// DownloadEntryData implements the server-streaming RPC: it sends a
// header with the total payload size followed by the payload chunks.
func (h *EntryHandler) DownloadEntryData(req *gophkeeperv1.DownloadEntryDataRequest, stream gophkeeperv1.EntryService_DownloadEntryDataServer) error {
	userID, err := userIDFromContext(stream.Context())
	if err != nil {
		return err
	}

	chunkCount, size, err := h.entries.EntryDataInfo(stream.Context(), userID, req.GetId())
	if err != nil {
		return toStatusError(err)
	}
	if err := stream.Send(gophkeeperv1.DownloadEntryDataResponse_builder{
		Header: gophkeeperv1.DownloadEntryDataHeader_builder{Size: size}.Build(),
	}.Build()); err != nil {
		return status.Error(codes.Internal, "send download header: "+err.Error())
	}

	if chunkCount == 0 {
		// Inline payload: fetch it whole and stream it as one chunk.
		entry, err := h.entries.Get(stream.Context(), userID, req.GetId())
		if err != nil {
			return toStatusError(err)
		}
		if err := sendChunk(stream, entry.Data); err != nil {
			return err
		}
		return nil
	}

	for seq := 0; seq < chunkCount; seq++ {
		data, err := h.entries.DownloadChunk(stream.Context(), userID, req.GetId(), seq)
		if err != nil {
			return toStatusError(err)
		}
		if err := sendChunk(stream, data); err != nil {
			return err
		}
	}
	return nil
}

// sendChunk streams one payload chunk.
func sendChunk(stream gophkeeperv1.EntryService_DownloadEntryDataServer, data []byte) error {
	if err := stream.Send(gophkeeperv1.DownloadEntryDataResponse_builder{
		Chunk: gophkeeperv1.DataChunk_builder{Data: data}.Build(),
	}.Build()); err != nil {
		return status.Error(codes.Internal, "send download chunk: "+err.Error())
	}
	return nil
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
