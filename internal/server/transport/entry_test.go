package transport

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
	"github.com/dmitriy/gophkeeper/internal/server/model"
	"github.com/dmitriy/gophkeeper/internal/server/service"
)

// fakeEntryService is a hand-written mock of the transport.entryService
// interface, recording invocations and returning preset results.
type fakeEntryService struct {
	createFn func(ctx context.Context, userID string, entry *model.Entry) (*model.Entry, error)
	getFn    func(ctx context.Context, userID, entryID string) (*model.Entry, error)
	listFn   func(ctx context.Context, userID string, entryType *model.EntryType) ([]*model.Entry, error)
	updateFn func(ctx context.Context, userID string, entry *model.Entry) (*model.Entry, error)
	deleteFn func(ctx context.Context, userID, entryID string) error
	syncFn   func(ctx context.Context, userID string) ([]*model.Entry, error)
}

func (f *fakeEntryService) Create(ctx context.Context, userID string, entry *model.Entry) (*model.Entry, error) {
	return f.createFn(ctx, userID, entry)
}

func (f *fakeEntryService) Get(ctx context.Context, userID, entryID string) (*model.Entry, error) {
	return f.getFn(ctx, userID, entryID)
}

func (f *fakeEntryService) List(ctx context.Context, userID string, entryType *model.EntryType) ([]*model.Entry, error) {
	return f.listFn(ctx, userID, entryType)
}

func (f *fakeEntryService) Update(ctx context.Context, userID string, entry *model.Entry) (*model.Entry, error) {
	return f.updateFn(ctx, userID, entry)
}

func (f *fakeEntryService) Delete(ctx context.Context, userID, entryID string) error {
	return f.deleteFn(ctx, userID, entryID)
}

func (f *fakeEntryService) Sync(ctx context.Context, userID string) ([]*model.Entry, error) {
	return f.syncFn(ctx, userID)
}

// sampleEntry returns a fully populated domain entry for mapping tests.
func sampleEntry() *model.Entry {
	return &model.Entry{
		ID:        "entry-1",
		UserID:    "user-1",
		Type:      model.EntryTypeLoginPassword,
		Label:     "GitHub",
		Metadata:  "https://github.com",
		Data:      []byte{0x01, 0x02, 0x03},
		Version:   7,
		CreatedAt: time.Unix(1700000000, 0),
		UpdatedAt: time.Unix(1700001234, 0),
	}
}

func TestEntryHandler_Create_Success(t *testing.T) {
	var gotUserID string
	var gotEntry *model.Entry
	created := sampleEntry()
	fake := &fakeEntryService{
		createFn: func(_ context.Context, userID string, entry *model.Entry) (*model.Entry, error) {
			gotUserID, gotEntry = userID, entry
			return created, nil
		},
	}
	handler := NewEntryHandler(fake)

	resp, err := handler.Create(claimsContext("user-1"), &gophkeeperv1.CreateEntryRequest{
		Entry: &gophkeeperv1.Entry{
			Type:     gophkeeperv1.EntryType_ENTRY_TYPE_TEXT,
			Label:    "note",
			Metadata: "meta",
			Data:     []byte("payload"),
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "user-1", gotUserID)
	require.NotNil(t, gotEntry)
	assert.Equal(t, model.EntryTypeText, gotEntry.Type)
	assert.Equal(t, "note", gotEntry.Label)
	assert.Equal(t, "meta", gotEntry.Metadata)
	assert.Equal(t, []byte("payload"), gotEntry.Data)

	require.NotNil(t, resp.GetEntry())
	assert.Equal(t, "entry-1", resp.GetEntry().GetId())
	assert.Equal(t, gophkeeperv1.EntryType_ENTRY_TYPE_LOGIN_PASSWORD, resp.GetEntry().GetType())
	assert.Equal(t, "GitHub", resp.GetEntry().GetLabel())
	assert.Equal(t, "https://github.com", resp.GetEntry().GetMetadata())
	assert.Equal(t, []byte{0x01, 0x02, 0x03}, resp.GetEntry().GetData())
	assert.Equal(t, int64(7), resp.GetEntry().GetVersion())
	assert.Equal(t, int64(1700000000), resp.GetEntry().GetCreatedAt())
	assert.Equal(t, int64(1700001234), resp.GetEntry().GetUpdatedAt())
}

func TestEntryHandler_Create_MissingClaims(t *testing.T) {
	handler := NewEntryHandler(&fakeEntryService{})

	resp, err := handler.Create(context.Background(), &gophkeeperv1.CreateEntryRequest{
		Entry: &gophkeeperv1.Entry{Label: "note", Data: []byte("x")},
	})

	require.Nil(t, resp)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Equal(t, "missing user identity", status.Convert(err).Message())
}

func TestEntryHandler_Create_ErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		serviceErr error
		wantCode   codes.Code
		wantMsg    string
	}{
		{
			name:       "empty label",
			serviceErr: service.ErrEmptyLabel,
			wantCode:   codes.InvalidArgument,
			wantMsg:    service.ErrEmptyLabel.Error(),
		},
		{
			name:       "label too long",
			serviceErr: service.ErrLabelTooLong,
			wantCode:   codes.InvalidArgument,
			wantMsg:    service.ErrLabelTooLong.Error(),
		},
		{
			name:       "internal error is generic",
			serviceErr: errors.New("db: connection refused"),
			wantCode:   codes.Internal,
			wantMsg:    "internal error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeEntryService{
				createFn: func(context.Context, string, *model.Entry) (*model.Entry, error) {
					return nil, tt.serviceErr
				},
			}
			handler := NewEntryHandler(fake)

			resp, err := handler.Create(claimsContext("user-1"), &gophkeeperv1.CreateEntryRequest{
				Entry: &gophkeeperv1.Entry{Label: "note", Data: []byte("x")},
			})

			require.Nil(t, resp)
			assert.Equal(t, tt.wantCode, status.Code(err))
			assert.Equal(t, tt.wantMsg, status.Convert(err).Message())
		})
	}
}

func TestEntryHandler_Get_Success(t *testing.T) {
	var gotUserID, gotEntryID string
	entry := sampleEntry()
	fake := &fakeEntryService{
		getFn: func(_ context.Context, userID, entryID string) (*model.Entry, error) {
			gotUserID, gotEntryID = userID, entryID
			return entry, nil
		},
	}
	handler := NewEntryHandler(fake)

	resp, err := handler.Get(claimsContext("user-1"), &gophkeeperv1.GetEntryRequest{Id: "entry-1"})

	require.NoError(t, err)
	assert.Equal(t, "user-1", gotUserID)
	assert.Equal(t, "entry-1", gotEntryID)
	require.NotNil(t, resp.GetEntry())
	assert.Equal(t, "entry-1", resp.GetEntry().GetId())
	assert.Equal(t, "GitHub", resp.GetEntry().GetLabel())
}

func TestEntryHandler_Get_NotFound(t *testing.T) {
	fake := &fakeEntryService{
		getFn: func(context.Context, string, string) (*model.Entry, error) {
			return nil, fmt.Errorf("service: get entry: %w", model.ErrNotFound)
		},
	}
	handler := NewEntryHandler(fake)

	resp, err := handler.Get(claimsContext("user-1"), &gophkeeperv1.GetEntryRequest{Id: "missing"})

	require.Nil(t, resp)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestEntryHandler_List_Success(t *testing.T) {
	var gotUserID string
	var gotType *model.EntryType
	entries := []*model.Entry{sampleEntry(), {
		ID:        "entry-2",
		UserID:    "user-1",
		Type:      model.EntryTypeCard,
		Label:     "Visa",
		Data:      []byte("card"),
		Version:   1,
		CreatedAt: time.Unix(1700000001, 0),
		UpdatedAt: time.Unix(1700000001, 0),
	}}
	fake := &fakeEntryService{
		listFn: func(_ context.Context, userID string, entryType *model.EntryType) ([]*model.Entry, error) {
			gotUserID, gotType = userID, entryType
			return entries, nil
		},
	}
	handler := NewEntryHandler(fake)

	resp, err := handler.List(claimsContext("user-1"), &gophkeeperv1.ListEntriesRequest{})

	require.NoError(t, err)
	assert.Equal(t, "user-1", gotUserID)
	assert.Nil(t, gotType, "List has no filters in the API, nil type expected")
	require.Len(t, resp.GetEntries(), 2)
	assert.Equal(t, "entry-1", resp.GetEntries()[0].GetId())
	assert.Equal(t, gophkeeperv1.EntryType_ENTRY_TYPE_LOGIN_PASSWORD, resp.GetEntries()[0].GetType())
	assert.Equal(t, "entry-2", resp.GetEntries()[1].GetId())
	assert.Equal(t, gophkeeperv1.EntryType_ENTRY_TYPE_CARD, resp.GetEntries()[1].GetType())
}

func TestEntryHandler_Update_Success(t *testing.T) {
	var gotUserID string
	var gotEntry *model.Entry
	updated := sampleEntry()
	updated.Version = 8
	fake := &fakeEntryService{
		updateFn: func(_ context.Context, userID string, entry *model.Entry) (*model.Entry, error) {
			gotUserID, gotEntry = userID, entry
			return updated, nil
		},
	}
	handler := NewEntryHandler(fake)

	resp, err := handler.Update(claimsContext("user-1"), &gophkeeperv1.UpdateEntryRequest{
		Entry: &gophkeeperv1.Entry{
			Id:       "entry-1",
			Type:     gophkeeperv1.EntryType_ENTRY_TYPE_TEXT,
			Label:    "renamed",
			Metadata: "new-meta",
			Data:     []byte("new-payload"),
			Version:  7,
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "user-1", gotUserID, "userID must come from claims, not the request")
	require.NotNil(t, gotEntry)
	assert.Equal(t, "entry-1", gotEntry.ID)
	assert.Equal(t, model.EntryTypeText, gotEntry.Type)
	assert.Equal(t, "renamed", gotEntry.Label)
	assert.Equal(t, "new-meta", gotEntry.Metadata)
	assert.Equal(t, []byte("new-payload"), gotEntry.Data)
	assert.Equal(t, int64(7), gotEntry.Version)

	require.NotNil(t, resp.GetEntry())
	assert.Equal(t, "entry-1", resp.GetEntry().GetId())
	assert.Equal(t, int64(8), resp.GetEntry().GetVersion())
}

func TestEntryHandler_Update_Conflict(t *testing.T) {
	fake := &fakeEntryService{
		updateFn: func(context.Context, string, *model.Entry) (*model.Entry, error) {
			return nil, fmt.Errorf("service: update entry: %w", model.ErrConflict)
		},
	}
	handler := NewEntryHandler(fake)

	resp, err := handler.Update(claimsContext("user-1"), &gophkeeperv1.UpdateEntryRequest{
		Entry: &gophkeeperv1.Entry{Id: "entry-1", Version: 6, Label: "l", Data: []byte("d")},
	})

	require.Nil(t, resp)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Contains(t, status.Convert(err).Message(), "conflict")
}

func TestEntryHandler_Delete_Success(t *testing.T) {
	var gotUserID, gotEntryID string
	fake := &fakeEntryService{
		deleteFn: func(_ context.Context, userID, entryID string) error {
			gotUserID, gotEntryID = userID, entryID
			return nil
		},
	}
	handler := NewEntryHandler(fake)

	resp, err := handler.Delete(claimsContext("user-1"), &gophkeeperv1.DeleteEntryRequest{Id: "entry-1"})

	require.NoError(t, err)
	assert.Equal(t, "user-1", gotUserID)
	assert.Equal(t, "entry-1", gotEntryID)
	require.NotNil(t, resp)
}

func TestEntryHandler_Delete_NotFound(t *testing.T) {
	fake := &fakeEntryService{
		deleteFn: func(context.Context, string, string) error {
			return fmt.Errorf("service: delete entry: %w", model.ErrNotFound)
		},
	}
	handler := NewEntryHandler(fake)

	resp, err := handler.Delete(claimsContext("user-1"), &gophkeeperv1.DeleteEntryRequest{Id: "missing"})

	require.Nil(t, resp)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestEntryHandler_Sync_Success(t *testing.T) {
	var gotUserID string
	entries := []*model.Entry{sampleEntry()}
	fake := &fakeEntryService{
		syncFn: func(_ context.Context, userID string) ([]*model.Entry, error) {
			gotUserID = userID
			return entries, nil
		},
	}
	handler := NewEntryHandler(fake)

	resp, err := handler.Sync(claimsContext("user-1"), &gophkeeperv1.SyncRequest{})

	require.NoError(t, err)
	assert.Equal(t, "user-1", gotUserID)
	require.Len(t, resp.GetEntries(), 1)
	assert.Equal(t, "entry-1", resp.GetEntries()[0].GetId())
	assert.Equal(t, int64(1700000000), resp.GetEntries()[0].GetCreatedAt())
}

func TestEntryHandler_Sync_MissingClaims(t *testing.T) {
	handler := NewEntryHandler(&fakeEntryService{})

	resp, err := handler.Sync(context.Background(), &gophkeeperv1.SyncRequest{})

	require.Nil(t, resp)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Equal(t, "missing user identity", status.Convert(err).Message())
}
