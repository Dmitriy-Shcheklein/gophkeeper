package gateway

import (
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	v1 "github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
)

// Client-actionable sentinel errors returned by the gateway methods
// in place of raw gRPC status errors; use errors.Is to detect them.
var (
	// ErrNotFound means the requested entry does not exist.
	ErrNotFound = errors.New("entry not found")
	// ErrAlreadyExists means an entry with the same identity already
	// exists (e.g. duplicate create).
	ErrAlreadyExists = errors.New("entry already exists")
	// ErrConflict means the optimistic-locking precondition failed:
	// the entry was modified by someone else; re-fetch and retry.
	ErrConflict = errors.New("entry was modified, re-fetch and retry")
	// ErrUnauthenticated means the request carried no valid token:
	// the user is not logged in or the token has expired; the service
	// and CLI layers use it to prompt a login.
	ErrUnauthenticated = errors.New("not logged in or session expired, please log in")
)

// translateError maps a gRPC error to a client-friendly error:
// server-meaningful codes become the sentinel errors above, any other
// status error is wrapped preserving its chain (callers can
// errors.As the *status.Status; the wrapped message keeps the code
// and the client-actionable server message, e.g. the name of the
// offending field), and non-status errors are wrapped as-is.
func translateError(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("gateway: %w", err)
	}
	switch st.Code() {
	case codes.NotFound:
		return ErrNotFound
	case codes.AlreadyExists:
		return ErrAlreadyExists
	case codes.FailedPrecondition:
		return ErrConflict
	case codes.Unauthenticated:
		return ErrUnauthenticated
	default:
		// Wrap the original error (not just code+message) so callers
		// can errors.As the *status.Status out of the chain.
		return fmt.Errorf("gateway: %w", err)
	}
}

// entryTypeToProto maps a client EntryType to its proto counterpart.
// The numeric values coincide by design; unknown values are passed
// through so the server can reject them with a proper validation
// error instead of the client silently mangling them.
func entryTypeToProto(t model.EntryType) v1.EntryType {
	return v1.EntryType(t)
}

// entryTypeFromProto maps a proto EntryType back to the client type.
func entryTypeFromProto(t v1.EntryType) model.EntryType {
	return model.EntryType(t)
}

// entryToProto converts a client entry to the proto message. The
// payload bytes are shared (not copied) — the proto message is
// treated as read-only downstream.
func entryToProto(e *model.Entry) *v1.Entry {
	if e == nil {
		return nil
	}
	return v1.Entry_builder{
		Id:       e.ID,
		Type:     entryTypeToProto(e.Type),
		Label:    e.Label,
		Metadata: e.Metadata,
		Data:     e.Data,
		Version:  e.Version,
	}.Build()
}

// entryFromProto converts a proto entry to the client model,
// reconstructing the timestamps from Unix seconds (UTC). The payload
// bytes are shared (not copied).
func entryFromProto(e *v1.Entry) *model.Entry {
	if e == nil {
		return nil
	}
	return &model.Entry{
		ID:        e.GetId(),
		Type:      entryTypeFromProto(e.GetType()),
		Label:     e.GetLabel(),
		Metadata:  e.GetMetadata(),
		Data:      e.GetData(),
		DataSize:  e.GetDataSize(),
		Version:   e.GetVersion(),
		CreatedAt: time.Unix(e.GetCreatedAt(), 0).UTC(),
		UpdatedAt: time.Unix(e.GetUpdatedAt(), 0).UTC(),
	}
}

// entriesFromProto converts a list of proto entries.
func entriesFromProto(entries []*v1.Entry) []*model.Entry {
	out := make([]*model.Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, entryFromProto(e))
	}
	return out
}
