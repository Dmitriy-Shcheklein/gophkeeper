package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitriy/gophkeeper/internal/server/model"
)

// digestOf returns the hex-encoded SHA-256 of the concatenated chunks.
func digestOf(chunks ...[]byte) string {
	h := sha256.New()
	for _, c := range chunks {
		h.Write(c)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestBeginUploadValidation(t *testing.T) {
	svc, _ := newTestEntryService()
	ctx := context.Background()

	tests := []struct {
		name    string
		userID  string
		header  *model.Entry
		version int64
		wantErr error
	}{
		{name: "empty user", userID: "", header: &model.Entry{Type: model.EntryTypeText, Label: "l"}, wantErr: ErrEmptyUserID},
		{name: "nil header", userID: "u", header: nil, wantErr: ErrEmptyData},
		{name: "invalid type", userID: "u", header: &model.Entry{Type: 99, Label: "l"}, wantErr: ErrInvalidEntryType},
		{name: "empty label", userID: "u", header: &model.Entry{Type: model.EntryTypeText}, wantErr: ErrEmptyLabel},
		{name: "label too long", userID: "u", header: &model.Entry{Type: model.EntryTypeText, Label: makeString(maxLabelLen + 1)}, wantErr: ErrLabelTooLong},
		{name: "metadata too long", userID: "u", header: &model.Entry{Type: model.EntryTypeText, Label: "l", Metadata: makeString(maxMetadataLen + 1)}, wantErr: ErrMetadataTooLong},
		{name: "negative version", userID: "u", header: &model.Entry{Type: model.EntryTypeText, Label: "l"}, version: -1, wantErr: ErrInvalidVersion},
		{name: "update without id", userID: "u", header: &model.Entry{Type: model.EntryTypeText, Label: "l"}, version: 3, wantErr: ErrEmptyEntryID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, err := svc.BeginUpload(ctx, tt.userID, tt.header, tt.version)
			require.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, session)
		})
	}
}

// makeString returns a string of the given length.
func makeString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}

func TestUploadSessionCreate(t *testing.T) {
	svc, repo := newTestEntryService()
	ctx := context.Background()

	chunks := [][]byte{[]byte("first-half-"), []byte("second-half")}
	session, err := svc.BeginUpload(ctx, "user-1", &model.Entry{
		Type:     model.EntryTypeBinary,
		Label:    "movie.bin",
		Metadata: "meta",
	}, 0)
	require.NoError(t, err)

	for _, c := range chunks {
		require.NoError(t, session.AddChunk(c))
	}

	stored, err := session.Commit(ctx, digestOf(chunks...))
	require.NoError(t, err)
	assert.NotEmpty(t, stored.ID)
	assert.Equal(t, int64(1), stored.Version)
	assert.Equal(t, int64(len("first-half-second-half")), stored.DataSize)

	// The stored entry is visible and reports the chunked size.
	got, err := svc.Get(ctx, "user-1", stored.ID)
	require.NoError(t, err)
	assert.Empty(t, got.Data)
	assert.Equal(t, int64(len("first-half-second-half")), got.DataSize)
	assert.Equal(t, model.EntryTypeBinary, got.Type)

	// The session is spent: further chunks are not accepted into the
	// committed entry (extra chunk gets stored but the entry is done).
	require.NoError(t, session.AddChunk([]byte("late")))
	listed, err := svc.List(ctx, "user-1", nil, false)
	require.NoError(t, err)
	require.Len(t, listed, 1)

	_ = repo
}

func TestUploadSessionCreateEmpty(t *testing.T) {
	svc, repo := newTestEntryService()
	ctx := context.Background()

	session, err := svc.BeginUpload(ctx, "user-1", &model.Entry{
		Type: model.EntryTypeBinary, Label: "empty",
	}, 0)
	require.NoError(t, err)

	_, err = session.Commit(ctx, digestOf())
	assert.ErrorIs(t, err, ErrEmptyChunkedData)

	// The failed upload is rolled back.
	session.Abort(ctx)
	listed, err := svc.List(ctx, "user-1", nil, false)
	require.NoError(t, err)
	assert.Empty(t, listed)
	_ = repo
}

func TestUploadSessionChecksumMismatch(t *testing.T) {
	svc, _ := newTestEntryService()
	ctx := context.Background()

	session, err := svc.BeginUpload(ctx, "user-1", &model.Entry{
		Type: model.EntryTypeBinary, Label: "corrupt",
	}, 0)
	require.NoError(t, err)
	require.NoError(t, session.AddChunk([]byte("payload")))

	_, err = session.Commit(ctx, digestOf([]byte("other-payload")))
	assert.ErrorIs(t, err, ErrChecksumMismatch)
}

func TestUploadSessionChunkLimits(t *testing.T) {
	svc, _ := newTestEntryService()
	ctx := context.Background()

	session, err := svc.BeginUpload(ctx, "user-1", &model.Entry{
		Type: model.EntryTypeBinary, Label: "big",
	}, 0)
	require.NoError(t, err)

	err = session.AddChunk(make([]byte, MaxChunkSize+1))
	assert.ErrorIs(t, err, ErrChunkTooLarge)
}

func TestUploadSessionDataTooLarge(t *testing.T) {
	svc := NewEntryServiceWithLimits(newMockEntryRepo(), 100)
	ctx := context.Background()

	session, err := svc.BeginUpload(ctx, "user-1", &model.Entry{
		Type: model.EntryTypeBinary, Label: "over",
	}, 0)
	require.NoError(t, err)

	require.NoError(t, session.AddChunk(make([]byte, 60)))
	err = session.AddChunk(make([]byte, 60))
	assert.ErrorIs(t, err, ErrDataTooLarge)
}

func TestUploadSessionUpdate(t *testing.T) {
	svc, repo := newTestEntryService()
	ctx := context.Background()

	created, err := svc.Create(ctx, "user-1", &model.Entry{
		Type: model.EntryTypeText, Label: "note", Data: []byte("original"),
	})
	require.NoError(t, err)

	chunks := [][]byte{[]byte("replacement-"), []byte("content")}
	session, err := svc.BeginUpload(ctx, "user-1", &model.Entry{
		ID:    created.ID,
		Type:  model.EntryTypeText,
		Label: "note-renamed",
	}, created.Version)
	require.NoError(t, err)
	for _, c := range chunks {
		require.NoError(t, session.AddChunk(c))
	}

	stored, err := session.Commit(ctx, digestOf(chunks...))
	require.NoError(t, err)
	assert.Equal(t, created.ID, stored.ID)
	assert.Equal(t, int64(2), stored.Version)
	assert.Equal(t, "note-renamed", stored.Label)
	assert.Equal(t, int64(len("replacement-content")), stored.DataSize)
	assert.Empty(t, stored.Data)

	got, err := svc.Get(ctx, "user-1", created.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), got.Version)
	assert.Equal(t, int64(len("replacement-content")), got.DataSize)

	_ = repo
}

func TestUploadSessionUpdateStaleVersion(t *testing.T) {
	svc, _ := newTestEntryService()
	ctx := context.Background()

	created, err := svc.Create(ctx, "user-1", &model.Entry{
		Type: model.EntryTypeText, Label: "note", Data: []byte("original"),
	})
	require.NoError(t, err)

	_, err = svc.BeginUpload(ctx, "user-1", &model.Entry{
		ID: created.ID, Type: model.EntryTypeText, Label: "note",
	}, created.Version+10)
	assert.ErrorIs(t, err, model.ErrConflict)

	_, err = svc.BeginUpload(ctx, "user-1", &model.Entry{
		ID: "missing-id", Type: model.EntryTypeText, Label: "note",
	}, 1)
	assert.ErrorIs(t, err, model.ErrNotFound)
}

func TestUploadSessionUpdateAbortKeepsOldContent(t *testing.T) {
	svc, _ := newTestEntryService()
	ctx := context.Background()

	created, err := svc.Create(ctx, "user-1", &model.Entry{
		Type: model.EntryTypeText, Label: "note", Data: []byte("keep-me"),
	})
	require.NoError(t, err)

	session, err := svc.BeginUpload(ctx, "user-1", &model.Entry{
		ID: created.ID, Type: model.EntryTypeText, Label: "note",
	}, created.Version)
	require.NoError(t, err)
	require.NoError(t, session.AddChunk([]byte("abandoned-chunk")))

	session.Abort(ctx)

	got, err := svc.Get(ctx, "user-1", created.ID)
	require.NoError(t, err)
	assert.Equal(t, []byte("keep-me"), got.Data)
	assert.Equal(t, created.Version, got.Version)
}

func TestUploadSessionCommitTwiceFails(t *testing.T) {
	svc, _ := newTestEntryService()
	ctx := context.Background()

	chunks := [][]byte{[]byte("data")}
	session, err := svc.BeginUpload(ctx, "user-1", &model.Entry{
		Type: model.EntryTypeBinary, Label: "once",
	}, 0)
	require.NoError(t, err)
	require.NoError(t, session.AddChunk(chunks[0]))

	_, err = session.Commit(ctx, digestOf(chunks...))
	require.NoError(t, err)

	// Second commit finds no pending row.
	_, err = session.Commit(ctx, digestOf(chunks...))
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrChecksumMismatch))
}

func TestUploadSessionUpdateChunkSeqContinuesAfterExisting(t *testing.T) {
	// Regression: the chunks of a chunked update must continue after
	// the already stored ones — reusing sequence 0 collides with the
	// (entry_id, seq) primary key of the real storage.
	repo := newMockEntryRepo()
	svc := NewEntryService(repo)
	ctx := context.Background()

	created, err := svc.Create(ctx, "user-1", &model.Entry{
		Type: model.EntryTypeBinary, Label: "file", Data: []byte("placeholder"),
	})
	require.NoError(t, err)

	// Give the entry a chunked payload of two chunks.
	require.NoError(t, repo.AppendChunk(ctx, created.ID, 0, []byte("old-0")))
	require.NoError(t, repo.AppendChunk(ctx, created.ID, 1, []byte("old-1")))

	session, err := svc.BeginUpload(ctx, "user-1", &model.Entry{
		ID: created.ID, Type: model.EntryTypeBinary, Label: "file",
	}, created.Version)
	require.NoError(t, err)
	require.NoError(t, session.AddChunk([]byte("new-0")))
	require.NoError(t, session.AddChunk([]byte("new-1")))
	require.NoError(t, session.AddChunk([]byte("new-2")))

	stored, err := session.Commit(ctx, digestOf([]byte("new-0"), []byte("new-1"), []byte("new-2")))
	require.NoError(t, err)
	assert.Equal(t, int64(len("new-0new-1new-2")), stored.DataSize)

	// The old chunks are gone, the new ones took their place,
	// renumbered to start at 0.
	count, err := repo.ChunkCount(ctx, "user-1", created.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
	for seq, want := range []string{"new-0", "new-1", "new-2"} {
		data, err := repo.Chunk(ctx, "user-1", created.ID, seq)
		require.NoError(t, err, "chunk %d", seq)
		assert.Equal(t, want, string(data))
	}
}
