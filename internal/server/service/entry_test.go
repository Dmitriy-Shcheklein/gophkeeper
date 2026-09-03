package service

import (
	"context"
	"errors"
	"strings"
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
}

func newMockEntryRepo() *mockEntryRepo {
	return &mockEntryRepo{entries: make(map[string]*model.Entry)}
}

func (m *mockEntryRepo) Create(_ context.Context, entry *model.Entry) error {
	if m.createErr != nil {
		return m.createErr
	}
	entry.ID = "generated-" + entry.Label
	entry.Version = 1
	entry.CreatedAt = time.Now()
	entry.UpdatedAt = entry.CreatedAt
	m.entries[entry.ID] = entry
	return nil
}

func (m *mockEntryRepo) GetByID(_ context.Context, userID, entryID string) (*model.Entry, error) {
	entry, ok := m.entries[entryID]
	if !ok || entry.UserID != userID {
		return nil, model.ErrNotFound
	}
	return entry, nil
}

func (m *mockEntryRepo) List(_ context.Context, userID string, entryType *model.EntryType) ([]*model.Entry, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	m.lastListFilter = entryType
	var result []*model.Entry
	for _, entry := range m.entries {
		if entry.UserID != userID {
			continue
		}
		if entryType != nil && entry.Type != *entryType {
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
	stored := repo.entries["generated-github"]
	require.NotNil(t, stored)
	assert.Equal(t, "user-1", stored.UserID)
	assert.Equal(t, model.EntryTypeLoginPassword, stored.Type)
	assert.Equal(t, "github", stored.Label)
	assert.Equal(t, "work account", stored.Metadata)
	assert.Equal(t, []byte(`{"login":"a","password":"b"}`), stored.Data)

	// The returned entry is filled by the repo.
	assert.Same(t, entry, got)
	assert.Equal(t, "generated-github", got.ID)
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
	assert.Equal(t, "user-1", repo.entries["generated-note"].UserID)
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
	repo.entries["e1"] = &model.Entry{ID: "e1", UserID: "user-1", Type: model.EntryTypeText, Label: "l", Data: []byte("d")}

	got, err := svc.Get(context.Background(), "user-1", "e1")
	require.NoError(t, err)
	assert.Same(t, repo.entries["e1"], got)
}

func TestEntryGetNotFound(t *testing.T) {
	svc, _ := newTestEntryService()

	_, err := svc.Get(context.Background(), "user-1", "missing")
	assert.ErrorIs(t, err, model.ErrNotFound)
}

func TestEntryGetEmptyUserID(t *testing.T) {
	svc, _ := newTestEntryService()

	_, err := svc.Get(context.Background(), "", "e1")
	assert.ErrorIs(t, err, ErrEmptyUserID)
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
			repo.entries["e1"] = &model.Entry{ID: "e1", UserID: "user-1", Type: model.EntryTypeText, Data: []byte("d")}

			got, err := svc.List(context.Background(), "user-1", tt.filter)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Same(t, repo.entries["e1"], got[0])

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

	_, err := svc.List(context.Background(), "user-1", nil)
	assert.ErrorContains(t, err, "boom")
}

func TestEntryUpdateSuccess(t *testing.T) {
	svc, repo := newTestEntryService()
	repo.entries["e1"] = &model.Entry{
		ID: "e1", UserID: "user-1", Type: model.EntryTypeText,
		Label: "old", Data: []byte("old"), Version: 3,
	}

	updated, err := svc.Update(context.Background(), "user-1", &model.Entry{
		ID: "e1", UserID: "user-1", Type: model.EntryTypeText,
		Label: "new", Metadata: "meta", Data: []byte("new"), Version: 3,
	})
	require.NoError(t, err)
	assert.Equal(t, "new", updated.Label)
	assert.Equal(t, "meta", updated.Metadata)
	assert.Equal(t, []byte("new"), updated.Data)
	assert.Equal(t, int64(4), updated.Version)
	assert.Equal(t, "new", repo.entries["e1"].Label)
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
			entry: &model.Entry{ID: "e1", UserID: "user-1", Type: model.EntryTypeText, Label: "l", Data: []byte("d"), Version: 0},
			want:  ErrInvalidVersion,
		},
		{
			name:  "bad label",
			entry: &model.Entry{ID: "e1", UserID: "user-1", Type: model.EntryTypeText, Data: []byte("d"), Version: 1},
			want:  ErrEmptyLabel,
		},
		{
			name:  "empty data",
			entry: &model.Entry{ID: "e1", UserID: "user-1", Type: model.EntryTypeText, Label: "l", Version: 1},
			want:  ErrEmptyData,
		},
		{
			name:  "invalid type",
			entry: &model.Entry{ID: "e1", UserID: "user-1", Type: model.EntryType(42), Label: "l", Data: []byte("d"), Version: 1},
			want:  ErrInvalidEntryType,
		},
		{
			name:  "metadata too long",
			entry: &model.Entry{ID: "e1", UserID: "user-1", Type: model.EntryTypeText, Label: "l", Data: []byte("d"), Metadata: strings.Repeat("m", 10001), Version: 1},
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
	repo.entries["e1"] = &model.Entry{ID: "e1", UserID: "user-1", Type: model.EntryTypeText, Label: "l", Data: []byte("d"), Version: 5}

	_, err := svc.Update(context.Background(), "user-1", &model.Entry{
		ID: "e1", UserID: "user-1", Type: model.EntryTypeText,
		Label: "l", Data: []byte("d"), Version: 4,
	})
	assert.ErrorIs(t, err, model.ErrConflict)
}

func TestEntryUpdateNotFound(t *testing.T) {
	svc, _ := newTestEntryService()

	_, err := svc.Update(context.Background(), "user-1", &model.Entry{
		ID: "missing", UserID: "user-1", Type: model.EntryTypeText,
		Label: "l", Data: []byte("d"), Version: 1,
	})
	assert.ErrorIs(t, err, model.ErrNotFound)
}

func TestEntryUpdateForcesUserID(t *testing.T) {
	// The authenticated user wins over any UserID carried in the entry.
	svc, repo := newTestEntryService()
	repo.entries["e1"] = &model.Entry{ID: "e1", UserID: "user-1", Type: model.EntryTypeText, Label: "l", Data: []byte("d"), Version: 1}

	_, err := svc.Update(context.Background(), "user-1", &model.Entry{
		ID: "e1", UserID: "someone-else", Type: model.EntryTypeText,
		Label: "new", Data: []byte("new"), Version: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, "new", repo.entries["e1"].Label)
}

func TestEntryDeleteSuccess(t *testing.T) {
	svc, repo := newTestEntryService()
	repo.entries["e1"] = &model.Entry{ID: "e1", UserID: "user-1", Type: model.EntryTypeText, Label: "l", Data: []byte("d")}

	err := svc.Delete(context.Background(), "user-1", "e1")
	require.NoError(t, err)
	assert.NotContains(t, repo.entries, "e1")
}

func TestEntryDeleteNotFound(t *testing.T) {
	svc, _ := newTestEntryService()

	err := svc.Delete(context.Background(), "user-1", "missing")
	assert.ErrorIs(t, err, model.ErrNotFound)
}

func TestEntryDeleteEmptyUserID(t *testing.T) {
	svc, _ := newTestEntryService()

	err := svc.Delete(context.Background(), "", "e1")
	assert.ErrorIs(t, err, ErrEmptyUserID)
}

func TestEntrySyncReturnsFullList(t *testing.T) {
	svc, repo := newTestEntryService()
	repo.entries["e1"] = &model.Entry{ID: "e1", UserID: "user-1", Type: model.EntryTypeText, Data: []byte("d1")}
	repo.entries["e2"] = &model.Entry{ID: "e2", UserID: "user-1", Type: model.EntryTypeCard, Data: []byte("d2")}
	repo.entries["foreign"] = &model.Entry{ID: "foreign", UserID: "user-2", Type: model.EntryTypeText, Data: []byte("d3")}

	got, err := svc.Sync(context.Background(), "user-1")
	require.NoError(t, err)
	require.Len(t, got, 2)

	// Sync must request the full state, i.e. pass a nil type filter.
	assert.Nil(t, repo.lastListFilter)
}

func TestEntrySyncEmptyUserID(t *testing.T) {
	svc, _ := newTestEntryService()

	_, err := svc.Sync(context.Background(), "")
	assert.ErrorIs(t, err, ErrEmptyUserID)
}
