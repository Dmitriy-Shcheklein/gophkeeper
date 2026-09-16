package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitriy/gophkeeper/internal/server/model"
)

// mockEntryRepo is a minimal in-memory EntryRepository for tests,
// mirroring the semantics of the postgres implementation.
type mockEntryRepo struct {
	entries map[string]*model.Entry
	// chunks holds the stored chunked payloads: entry id -> seq -> data.
	chunks map[string]map[int][]byte
	// nextID is incremented on every Create to generate unique IDs.
	nextID int64
	// createErr, if set, is returned by Create.
	createErr error
	// listErr, if set, is returned by List.
	listErr error
	// updateErr, if set, is returned by Update.
	updateErr error
	// deleteErr, if set, is returned by Delete.
	deleteErr error
	// lastListFilter records the filter passed to the last List call.
	lastListFilter *model.EntryType
	// lastListIncludeData records the includeData flag of the last List.
	lastListIncludeData bool
}

func newMockEntryRepo() *mockEntryRepo {
	return &mockEntryRepo{
		entries: make(map[string]*model.Entry),
		chunks:  make(map[string]map[int][]byte),
	}
}

func (m *mockEntryRepo) Create(_ context.Context, entry *model.Entry) error {
	if m.createErr != nil {
		return m.createErr
	}
	id := atomic.AddInt64(&m.nextID, 1)
	entry.ID = fmt.Sprintf("%08x-0000-4000-8000-%012x", id, id)
	entry.Version = 1
	entry.CreatedAt = time.Now()
	entry.UpdatedAt = entry.CreatedAt
	entry.DataSize = int64(len(entry.Data))
	entry.State = model.EntryStateReady
	m.entries[entry.ID] = entry
	return nil
}

func (m *mockEntryRepo) CreatePending(_ context.Context, entry *model.Entry) error {
	if m.createErr != nil {
		return m.createErr
	}
	id := atomic.AddInt64(&m.nextID, 1)
	entry.ID = fmt.Sprintf("%08x-0000-4000-8000-%012x", id, id)
	entry.Version = 1
	entry.CreatedAt = time.Now()
	entry.UpdatedAt = entry.CreatedAt
	entry.Data = nil
	entry.DataSize = 0
	entry.State = model.EntryStatePending
	m.entries[entry.ID] = entry
	return nil
}

func (m *mockEntryRepo) AppendChunk(_ context.Context, entryID string, seq int, data []byte) error {
	if _, ok := m.entries[entryID]; !ok {
		return model.ErrNotFound
	}
	if m.chunks[entryID] == nil {
		m.chunks[entryID] = make(map[int][]byte)
	}
	// Mirror the (entry_id, seq) primary key of the real storage.
	if _, dup := m.chunks[entryID][seq]; dup {
		return fmt.Errorf("duplicate chunk seq %d", seq)
	}
	m.chunks[entryID][seq] = data
	return nil
}

func (m *mockEntryRepo) FinalizeCreate(_ context.Context, entry *model.Entry) error {
	stored, ok := m.entries[entry.ID]
	if !ok || stored.State != model.EntryStatePending {
		return model.ErrNotFound
	}
	stored.State = model.EntryStateReady
	stored.DataSize = m.chunkedSize(stored.ID)
	stored.UpdatedAt = time.Now()
	*entry = *stored
	return nil
}

func (m *mockEntryRepo) chunkedSize(entryID string) int64 {
	var total int64
	for _, data := range m.chunks[entryID] {
		total += int64(len(data))
	}
	return total
}

func (m *mockEntryRepo) PrepareUpdate(_ context.Context, entry *model.Entry) (int, error) {
	stored, ok := m.entries[entry.ID]
	if !ok || stored.UserID != entry.UserID || stored.State != model.EntryStateReady {
		return 0, model.ErrNotFound
	}
	if stored.Version != entry.Version {
		return 0, model.ErrConflict
	}
	return len(m.chunks[entry.ID]), nil
}

func (m *mockEntryRepo) FinalizeUpdate(_ context.Context, entry *model.Entry, chunkOffset int) error {
	stored, ok := m.entries[entry.ID]
	if !ok || stored.UserID != entry.UserID || stored.State != model.EntryStateReady {
		return model.ErrNotFound
	}
	if stored.Version != entry.Version {
		return model.ErrConflict
	}
	chunks := m.chunks[entry.ID]
	for seq := range chunks {
		if seq < chunkOffset {
			delete(chunks, seq)
		}
	}
	// Mirror the storage invariant: after the finalize the chunks are
	// renumbered to start at 0.
	for seq := chunkOffset; seq < chunkOffset+len(chunks); seq++ {
		if data, ok := chunks[seq]; ok {
			delete(chunks, seq)
			chunks[seq-chunkOffset] = data
		}
	}
	stored.Label = entry.Label
	stored.Metadata = entry.Metadata
	stored.Data = nil
	stored.DataSize = m.chunkedSize(stored.ID)
	stored.Version++
	stored.UpdatedAt = time.Now()
	*entry = *stored
	return nil
}

func (m *mockEntryRepo) AbortUpdate(_ context.Context, entryID string, chunkOffset int) error {
	for seq := range m.chunks[entryID] {
		if seq >= chunkOffset {
			delete(m.chunks[entryID], seq)
		}
	}
	return nil
}

func (m *mockEntryRepo) DeleteIfPending(_ context.Context, entryID string) error {
	if entry, ok := m.entries[entryID]; ok && entry.State == model.EntryStatePending {
		delete(m.entries, entryID)
	}
	return nil
}

func (m *mockEntryRepo) ChunkCount(_ context.Context, _, entryID string) (int, error) {
	return len(m.chunks[entryID]), nil
}

func (m *mockEntryRepo) Chunk(_ context.Context, userID, entryID string, seq int) ([]byte, error) {
	entry, ok := m.entries[entryID]
	if !ok || entry.UserID != userID || entry.State != model.EntryStateReady {
		return nil, model.ErrNotFound
	}
	data, ok := m.chunks[entryID][seq]
	if !ok {
		return nil, model.ErrNotFound
	}
	return data, nil
}

// chunked strips the payload of a chunked entry the way the postgres
// implementation does: Data is empty, DataSize carries the size.
func (m *mockEntryRepo) chunked(entry *model.Entry) *model.Entry {
	if len(m.chunks[entry.ID]) == 0 {
		return entry
	}
	copied := *entry
	copied.Data = nil
	return &copied
}

func (m *mockEntryRepo) GetByID(_ context.Context, userID, entryID string) (*model.Entry, error) {
	entry, ok := m.entries[entryID]
	if !ok || entry.UserID != userID || entry.State == model.EntryStatePending {
		return nil, model.ErrNotFound
	}
	return m.chunked(entry), nil
}

func (m *mockEntryRepo) List(_ context.Context, userID string, entryType *model.EntryType, includeData bool) ([]*model.Entry, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	m.lastListFilter = entryType
	m.lastListIncludeData = includeData
	var result []*model.Entry
	for _, entry := range m.entries {
		if entry.UserID != userID || entry.State == model.EntryStatePending {
			continue
		}
		if entryType != nil && entry.Type != *entryType {
			continue
		}
		if !includeData {
			result = append(result, m.chunked(entry))
			continue
		}
		result = append(result, entry)
	}
	return result, nil
}

func (m *mockEntryRepo) Update(_ context.Context, entry *model.Entry) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	stored, ok := m.entries[entry.ID]
	if !ok || stored.UserID != entry.UserID {
		return model.ErrNotFound
	}
	if stored.Version != entry.Version {
		return model.ErrConflict
	}
	stored.Label = entry.Label
	stored.Metadata = entry.Metadata
	stored.Data = entry.Data
	stored.Version++
	stored.UpdatedAt = time.Now()
	*entry = *stored
	return nil
}

func (m *mockEntryRepo) Delete(_ context.Context, userID, entryID string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	entry, ok := m.entries[entryID]
	if !ok || entry.UserID != userID {
		return model.ErrNotFound
	}
	delete(m.entries, entryID)
	return nil
}

// Test UUIDs standing in for the database-generated identifiers the
// service validates before hitting the repository.
const (
	testUUID1       = "11111111-1111-4111-8111-111111111111"
	testUUID2       = "22222222-2222-4222-8222-222222222222"
	testUUID3       = "33333333-3333-4333-8333-333333333333"
	testUUIDMissing = "99999999-9999-4999-8999-999999999999"
)

func newTestEntryService() (*EntryService, *mockEntryRepo) {
	repo := newMockEntryRepo()
	return NewEntryService(repo), repo
}

func TestEntryCreateSuccess(t *testing.T) {
	svc, repo := newTestEntryService()

	entry := &model.Entry{
		UserID:   "user-1",
		Type:     model.EntryTypeLoginPassword,
		Label:    "github",
		Metadata: "work account",
		Data:     []byte(`{"login":"a","password":"b"}`),
	}
	got, err := svc.Create(context.Background(), "user-1", entry)
	require.NoError(t, err)

	// The entry passed to the repo must keep its payload fields.
	require.Len(t, repo.entries, 1)
	var stored *model.Entry
	for _, e := range repo.entries {
		stored = e
	}
	assert.Equal(t, "user-1", stored.UserID)
	assert.Equal(t, model.EntryTypeLoginPassword, stored.Type)
	assert.Equal(t, "github", stored.Label)
	assert.Equal(t, "work account", stored.Metadata)
	assert.Equal(t, []byte(`{"login":"a","password":"b"}`), stored.Data)

	// The returned entry is filled by the repo.
	assert.Same(t, entry, got)
	assert.Equal(t, stored.ID, got.ID)
	assert.Equal(t, int64(1), got.Version)
	assert.False(t, got.CreatedAt.IsZero())
	assert.False(t, got.UpdatedAt.IsZero())
}

func TestEntryCreateForcesUserID(t *testing.T) {
	// The entry's own UserID field must not override the authenticated
	// user: the service always writes the argument into the entry.
	svc, repo := newTestEntryService()

	entry := &model.Entry{
		UserID: "attacker",
		Type:   model.EntryTypeText,
		Label:  "note",
		Data:   []byte("data"),
	}
	_, err := svc.Create(context.Background(), "user-1", entry)
	require.NoError(t, err)
	stored, ok := repo.entries[entry.ID]
	require.True(t, ok)
	assert.Equal(t, "user-1", stored.UserID)
}

func TestEntryCreateValidation(t *testing.T) {
	tests := []struct {
		name   string
		userID string
		entry  *model.Entry
		want   error
	}{
		{
			name:  "empty user id",
			entry: &model.Entry{Type: model.EntryTypeText, Label: "l", Data: []byte("d")},
			want:  ErrEmptyUserID,
		},
		{
			name:   "invalid type",
			userID: "user-1",
			entry:  &model.Entry{Type: model.EntryType(99), Label: "l", Data: []byte("d")},
			want:   ErrInvalidEntryType,
		},
		{
			name:   "zero type",
			userID: "user-1",
			entry:  &model.Entry{Label: "l", Data: []byte("d")},
			want:   ErrInvalidEntryType,
		},
		{
			name:   "empty label",
			userID: "user-1",
			entry:  &model.Entry{Type: model.EntryTypeText, Data: []byte("d")},
			want:   ErrEmptyLabel,
		},
		{
			name:   "label too long",
			userID: "user-1",
			entry:  &model.Entry{Type: model.EntryTypeText, Label: strings.Repeat("a", 256), Data: []byte("d")},
			want:   ErrLabelTooLong,
		},
		{
			name:   "empty data",
			userID: "user-1",
			entry:  &model.Entry{Type: model.EntryTypeBinary, Label: "l"},
			want:   ErrEmptyData,
		},
		{
			name:   "nil data",
			userID: "user-1",
			entry:  &model.Entry{Type: model.EntryTypeCard, Label: "l"},
			want:   ErrEmptyData,
		},
		{
			name:   "metadata too long",
			userID: "user-1",
			entry:  &model.Entry{Type: model.EntryTypeText, Label: "l", Data: []byte("d"), Metadata: strings.Repeat("m", 10001)},
			want:   ErrMetadataTooLong,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newTestEntryService()
			_, err := svc.Create(context.Background(), tt.userID, tt.entry)
			assert.ErrorIs(t, err, tt.want)
			assert.Empty(t, repo.entries)
		})
	}

	// Boundary values must be accepted.
	svc, _ := newTestEntryService()
	_, err := svc.Create(context.Background(), "user-1", &model.Entry{
		Type:     model.EntryTypeText,
		Label:    strings.Repeat("a", 255),
		Metadata: strings.Repeat("m", 10000),
		Data:     []byte("d"),
	})
	assert.NoError(t, err)
}

func TestEntryUpdateAcceptedAtBoundary(t *testing.T) {
	svc, repo := newTestEntryService()
	repo.entries[testUUID1] = &model.Entry{
		ID: testUUID1, UserID: "user-1", Type: model.EntryTypeText,
		Label: "old", Data: []byte("old"), Version: 1,
	}

	updated, err := svc.Update(context.Background(), "user-1", &model.Entry{
		ID:       testUUID1,
		UserID:   "user-1",
		Type:     model.EntryTypeText,
		Label:    strings.Repeat("a", 255),
		Metadata: strings.Repeat("m", 10000),
		Data:     []byte("new"),
		Version:  1,
	})
	require.NoError(t, err)
	assert.Len(t, updated.Label, 255)
	assert.Len(t, updated.Metadata, 10000)
}

func TestEntryCreateAllTypesRequireData(t *testing.T) {
	for _, entryType := range []model.EntryType{
		model.EntryTypeLoginPassword, model.EntryTypeText,
		model.EntryTypeBinary, model.EntryTypeCard,
	} {
		svc, _ := newTestEntryService()
		_, err := svc.Create(context.Background(), "user-1", &model.Entry{
			Type: entryType, Label: "l",
		})
		assert.ErrorIs(t, err, ErrEmptyData, "type %d", entryType)
	}
}

func TestEntryCreateRepoErrorPropagates(t *testing.T) {
	repo := newMockEntryRepo()
	repo.createErr = errors.New("boom")
	svc := NewEntryService(repo)

	_, err := svc.Create(context.Background(), "user-1", &model.Entry{
		Type: model.EntryTypeText, Label: "l", Data: []byte("d"),
	})
	assert.ErrorContains(t, err, "boom")
}

func TestEntryGetSuccess(t *testing.T) {
	svc, repo := newTestEntryService()
	repo.entries[testUUID1] = &model.Entry{ID: testUUID1, UserID: "user-1", Type: model.EntryTypeText, Label: "l", Data: []byte("d")}

	got, err := svc.Get(context.Background(), "user-1", testUUID1)
	require.NoError(t, err)
	assert.Same(t, repo.entries[testUUID1], got)
}

func TestEntryGetNotFound(t *testing.T) {
	svc, _ := newTestEntryService()

	_, err := svc.Get(context.Background(), "user-1", testUUIDMissing)
	assert.ErrorIs(t, err, model.ErrNotFound)
}

func TestEntryGetEmptyUserID(t *testing.T) {
	svc, _ := newTestEntryService()

	_, err := svc.Get(context.Background(), "", testUUID1)
	assert.ErrorIs(t, err, ErrEmptyUserID)
}

func TestEntryGetEmptyEntryID(t *testing.T) {
	svc, _ := newTestEntryService()

	_, err := svc.Get(context.Background(), "user-1", "")
	assert.ErrorIs(t, err, ErrEmptyEntryID)
}

func TestEntryList(t *testing.T) {
	textType := model.EntryTypeText
	invalidType := model.EntryType(99)

	tests := []struct {
		name    string
		filter  *model.EntryType
		wantErr error
	}{
		{name: "nil filter", filter: nil},
		{name: "valid filter", filter: &textType},
		{name: "invalid filter", filter: &invalidType, wantErr: ErrInvalidEntryType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newTestEntryService()
			repo.entries[testUUID1] = &model.Entry{ID: testUUID1, UserID: "user-1", Type: model.EntryTypeText, Data: []byte("d")}

			got, err := svc.List(context.Background(), "user-1", tt.filter, true)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Same(t, repo.entries[testUUID1], got[0])

			if tt.filter == nil {
				assert.Nil(t, repo.lastListFilter)
			} else {
				assert.Equal(t, *tt.filter, *repo.lastListFilter)
			}
		})
	}
}

func TestEntryListRepoErrorPropagates(t *testing.T) {
	repo := newMockEntryRepo()
	repo.listErr = errors.New("boom")
	svc := NewEntryService(repo)

	_, err := svc.List(context.Background(), "user-1", nil, true)
	assert.ErrorContains(t, err, "boom")
}

func TestEntryUpdateSuccess(t *testing.T) {
	svc, repo := newTestEntryService()
	repo.entries[testUUID1] = &model.Entry{
		ID: testUUID1, UserID: "user-1", Type: model.EntryTypeText,
		Label: "old", Data: []byte("old"), Version: 3,
	}

	updated, err := svc.Update(context.Background(), "user-1", &model.Entry{
		ID: testUUID1, UserID: "user-1", Type: model.EntryTypeText,
		Label: "new", Metadata: "meta", Data: []byte("new"), Version: 3,
	})
	require.NoError(t, err)
	assert.Equal(t, "new", updated.Label)
	assert.Equal(t, "meta", updated.Metadata)
	assert.Equal(t, []byte("new"), updated.Data)
	assert.Equal(t, int64(4), updated.Version)
	assert.Equal(t, "new", repo.entries[testUUID1].Label)
}

func TestEntryUpdateValidation(t *testing.T) {
	tests := []struct {
		name  string
		entry *model.Entry
		want  error
	}{
		{
			name:  "empty id",
			entry: &model.Entry{UserID: "user-1", Type: model.EntryTypeText, Label: "l", Data: []byte("d"), Version: 1},
			want:  ErrEmptyEntryID,
		},
		{
			name:  "version below one",
			entry: &model.Entry{ID: testUUID1, UserID: "user-1", Type: model.EntryTypeText, Label: "l", Data: []byte("d"), Version: 0},
			want:  ErrInvalidVersion,
		},
		{
			name:  "bad label",
			entry: &model.Entry{ID: testUUID1, UserID: "user-1", Type: model.EntryTypeText, Data: []byte("d"), Version: 1},
			want:  ErrEmptyLabel,
		},
		{
			name:  "empty data",
			entry: &model.Entry{ID: testUUID1, UserID: "user-1", Type: model.EntryTypeText, Label: "l", Version: 1},
			want:  ErrEmptyData,
		},
		{
			name:  "invalid type",
			entry: &model.Entry{ID: testUUID1, UserID: "user-1", Type: model.EntryType(42), Label: "l", Data: []byte("d"), Version: 1},
			want:  ErrInvalidEntryType,
		},
		{
			name:  "metadata too long",
			entry: &model.Entry{ID: testUUID1, UserID: "user-1", Type: model.EntryTypeText, Label: "l", Data: []byte("d"), Metadata: strings.Repeat("m", 10001), Version: 1},
			want:  ErrMetadataTooLong,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newTestEntryService()
			_, err := svc.Update(context.Background(), "user-1", tt.entry)
			assert.ErrorIs(t, err, tt.want)
		})
	}
}

func TestEntryUpdateConflict(t *testing.T) {
	svc, repo := newTestEntryService()
	repo.entries[testUUID1] = &model.Entry{ID: testUUID1, UserID: "user-1", Type: model.EntryTypeText, Label: "l", Data: []byte("d"), Version: 5}

	_, err := svc.Update(context.Background(), "user-1", &model.Entry{
		ID: testUUID1, UserID: "user-1", Type: model.EntryTypeText,
		Label: "l", Data: []byte("d"), Version: 4,
	})
	assert.ErrorIs(t, err, model.ErrConflict)
}

func TestEntryUpdateNotFound(t *testing.T) {
	svc, _ := newTestEntryService()

	_, err := svc.Update(context.Background(), "user-1", &model.Entry{
		ID: testUUIDMissing, UserID: "user-1", Type: model.EntryTypeText,
		Label: "l", Data: []byte("d"), Version: 1,
	})
	assert.ErrorIs(t, err, model.ErrNotFound)
}

func TestEntryUpdateForcesUserID(t *testing.T) {
	// The authenticated user wins over any UserID carried in the entry.
	svc, repo := newTestEntryService()
	repo.entries[testUUID1] = &model.Entry{ID: testUUID1, UserID: "user-1", Type: model.EntryTypeText, Label: "l", Data: []byte("d"), Version: 1}

	_, err := svc.Update(context.Background(), "user-1", &model.Entry{
		ID: testUUID1, UserID: "someone-else", Type: model.EntryTypeText,
		Label: "new", Data: []byte("new"), Version: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, "new", repo.entries[testUUID1].Label)
}

func TestEntryDeleteSuccess(t *testing.T) {
	svc, repo := newTestEntryService()
	repo.entries[testUUID1] = &model.Entry{ID: testUUID1, UserID: "user-1", Type: model.EntryTypeText, Label: "l", Data: []byte("d")}

	err := svc.Delete(context.Background(), "user-1", testUUID1)
	require.NoError(t, err)
	assert.NotContains(t, repo.entries, testUUID1)
}

func TestEntryDeleteNotFound(t *testing.T) {
	svc, _ := newTestEntryService()

	err := svc.Delete(context.Background(), "user-1", testUUIDMissing)
	assert.ErrorIs(t, err, model.ErrNotFound)
}

func TestEntryDeleteEmptyUserID(t *testing.T) {
	svc, _ := newTestEntryService()

	err := svc.Delete(context.Background(), "", testUUID1)
	assert.ErrorIs(t, err, ErrEmptyUserID)
}

func TestEntryDeleteEmptyEntryID(t *testing.T) {
	svc, _ := newTestEntryService()

	err := svc.Delete(context.Background(), "user-1", "")
	assert.ErrorIs(t, err, ErrEmptyEntryID)
}

func TestEntrySyncReturnsFullList(t *testing.T) {
	svc, repo := newTestEntryService()
	repo.entries[testUUID1] = &model.Entry{ID: testUUID1, UserID: "user-1", Type: model.EntryTypeText, Data: []byte("d1")}
	repo.entries[testUUID2] = &model.Entry{ID: testUUID2, UserID: "user-1", Type: model.EntryTypeCard, Data: []byte("d2")}
	repo.entries["foreign"] = &model.Entry{ID: "foreign", UserID: "user-2", Type: model.EntryTypeText, Data: []byte("d3")}

	got, err := svc.Sync(context.Background(), "user-1", true)
	require.NoError(t, err)
	require.Len(t, got, 2)

	// Sync must request the full state, i.e. pass a nil type filter.
	assert.Nil(t, repo.lastListFilter)
}

func TestEntrySyncEmptyUserID(t *testing.T) {
	svc, _ := newTestEntryService()

	_, err := svc.Sync(context.Background(), "", true)
	assert.ErrorIs(t, err, ErrEmptyUserID)
}

// TestEntryIDMustBeUUID verifies the service rejects malformed entry
// ids at the boundary: they can never match a stored entry (ids are
// database-generated UUIDs) and would only produce driver-level
// errors if they reached the repository.
func TestEntryIDMustBeUUID(t *testing.T) {
	svc, _ := newTestEntryService()
	ctx := context.Background()

	for _, id := range []string{"zzzzzzzz", "e1", "missing-id", "../../etc"} {
		_, err := svc.Get(ctx, "user-1", id)
		assert.ErrorIs(t, err, ErrInvalidEntryID, "Get(%q)", id)

		err = svc.Delete(ctx, "user-1", id)
		assert.ErrorIs(t, err, ErrInvalidEntryID, "Delete(%q)", id)

		_, _, err = svc.EntryDataInfo(ctx, "user-1", id)
		assert.ErrorIs(t, err, ErrInvalidEntryID, "EntryDataInfo(%q)", id)

		_, err = svc.DownloadChunk(ctx, "user-1", id, 0)
		assert.ErrorIs(t, err, ErrInvalidEntryID, "DownloadChunk(%q)", id)

		_, err = svc.Update(ctx, "user-1", &model.Entry{ID: id, Version: 1, Type: model.EntryTypeText, Label: "l", Data: []byte("d")})
		assert.ErrorIs(t, err, ErrInvalidEntryID, "Update(%q)", id)
	}

	// Well-formed but non-existent id keeps the not-found semantics.
	_, err := svc.Get(ctx, "user-1", testUUIDMissing)
	assert.ErrorIs(t, err, model.ErrNotFound)
}
