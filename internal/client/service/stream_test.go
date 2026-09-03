package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitriy/gophkeeper/internal/client/gateway"
	"github.com/dmitriy/gophkeeper/internal/client/model"
)

// fakeUploadStream records the driven upload and returns a canned
// result.
type fakeUploadStream struct {
	header     *model.Entry
	version    int64
	chunks     [][]byte
	digest     string
	result     *model.Entry
	headerErr  error
	chunkErr   error
	commitErr  error
	headerSent bool
}

func (s *fakeUploadStream) SendHeader(entry *model.Entry, expectedVersion int64) error {
	if s.headerErr != nil {
		return s.headerErr
	}
	s.headerSent = true
	s.header = entry
	s.version = expectedVersion
	return nil
}

func (s *fakeUploadStream) SendChunk(data []byte) error {
	if s.chunkErr != nil {
		return s.chunkErr
	}
	copied := make([]byte, len(data))
	copy(copied, data)
	s.chunks = append(s.chunks, copied)
	return nil
}

func (s *fakeUploadStream) CloseAndCommit(sha256hex string) (*model.Entry, error) {
	if s.commitErr != nil {
		return nil, s.commitErr
	}
	s.digest = sha256hex
	return s.result, nil
}

// fakeDownloadStream replays a canned payload: a size header followed
// by chunks.
type fakeDownloadStream struct {
	size     int64
	chunks   [][]byte
	sizeErr  error
	chunkErr error
	sizeRecv bool
	closed   bool
}

func (s *fakeDownloadStream) RecvSize() (int64, error) {
	if s.sizeErr != nil {
		return 0, s.sizeErr
	}
	s.sizeRecv = true
	return s.size, nil
}

func (s *fakeDownloadStream) RecvChunk() ([]byte, error) {
	if !s.sizeRecv {
		return nil, errors.New("size not received")
	}
	if s.chunkErr != nil {
		return nil, s.chunkErr
	}
	if len(s.chunks) == 0 {
		return nil, io.EOF
	}
	c := s.chunks[0]
	s.chunks = s.chunks[1:]
	return c, nil
}

func (s *fakeDownloadStream) Close() error {
	s.closed = true
	return nil
}

func TestUploadStreamsFromReader(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 2*1024*1024+17) // 2+ chunks
	result := &model.Entry{ID: "new-id", Version: 1, Label: "movie.bin"}
	stream := &fakeUploadStream{result: result}
	svc := NewEntryService(&fakeEntryGateway{uploadStream: stream})

	stored, err := svc.Upload(context.Background(), &model.Entry{
		Type:     model.EntryTypeBinary,
		Label:    "movie.bin",
		Metadata: "meta",
	}, 0, bytes.NewReader(payload))
	require.NoError(t, err)
	assert.Same(t, result, stored)

	// Header first, chunks in order, footer digest matches.
	require.True(t, stream.headerSent)
	assert.Equal(t, "movie.bin", stream.header.Label)
	assert.Equal(t, int64(0), stream.version)
	require.Len(t, stream.chunks, 3)
	var reassembled []byte
	for _, c := range stream.chunks {
		reassembled = append(reassembled, c...)
	}
	assert.Equal(t, payload, reassembled)
	// Chunks are at most ChunkSize.
	for i, c := range stream.chunks {
		if i < len(stream.chunks)-1 {
			assert.Len(t, c, gateway.ChunkSize)
		}
	}
	h := sha256.Sum256(payload)
	assert.Equal(t, hex.EncodeToString(h[:]), stream.digest)
}

func TestUploadValidation(t *testing.T) {
	svc := NewEntryService(&fakeEntryGateway{})
	ctx := context.Background()

	tests := []struct {
		name    string
		entry   *model.Entry
		version int64
		wantErr error
	}{
		{name: "nil entry", entry: nil, wantErr: ErrNilEntry},
		{name: "invalid type", entry: &model.Entry{Type: 99, Label: "l"}, wantErr: ErrInvalidEntryType},
		{name: "empty label", entry: &model.Entry{Type: model.EntryTypeBinary}, wantErr: ErrEmptyLabel},
		{name: "label too long", entry: &model.Entry{Type: model.EntryTypeBinary, Label: strings.Repeat("x", maxLabelLen+1)}, wantErr: ErrLabelTooLong},
		{name: "metadata too long", entry: &model.Entry{Type: model.EntryTypeBinary, Label: "l", Metadata: strings.Repeat("x", maxMetadataLen+1)}, wantErr: ErrMetadataTooLong},
		{name: "negative version", entry: &model.Entry{Type: model.EntryTypeBinary, Label: "l"}, version: -1, wantErr: ErrInvalidVersion},
		{name: "update without id", entry: &model.Entry{Type: model.EntryTypeBinary, Label: "l"}, version: 2, wantErr: ErrEmptyEntryID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Upload(ctx, tt.entry, tt.version, strings.NewReader("data"))
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestUploadEmptyReader(t *testing.T) {
	svc := NewEntryService(&fakeEntryGateway{uploadStream: &fakeUploadStream{}})
	_, err := svc.Upload(context.Background(), &model.Entry{
		Type: model.EntryTypeBinary, Label: "empty",
	}, 0, bytes.NewReader(nil))
	assert.ErrorIs(t, err, ErrEmptyData)
}

func TestUploadStreamErrors(t *testing.T) {
	ctx := context.Background()
	entry := &model.Entry{Type: model.EntryTypeBinary, Label: "x"}

	t.Run("gateway open error", func(t *testing.T) {
		svc := NewEntryService(&fakeEntryGateway{uploadErr: errors.New("no conn")})
		_, err := svc.Upload(ctx, entry, 0, strings.NewReader("data"))
		require.Error(t, err)
	})

	t.Run("header error", func(t *testing.T) {
		svc := NewEntryService(&fakeEntryGateway{
			uploadStream: &fakeUploadStream{headerErr: errors.New("boom")},
		})
		_, err := svc.Upload(ctx, entry, 0, strings.NewReader("data"))
		require.Error(t, err)
	})

	t.Run("chunk error", func(t *testing.T) {
		svc := NewEntryService(&fakeEntryGateway{
			uploadStream: &fakeUploadStream{chunkErr: errors.New("boom")},
		})
		_, err := svc.Upload(ctx, entry, 0, strings.NewReader("data"))
		require.Error(t, err)
	})

	t.Run("commit error", func(t *testing.T) {
		svc := NewEntryService(&fakeEntryGateway{
			uploadStream: &fakeUploadStream{commitErr: errors.New("checksum mismatch")},
		})
		_, err := svc.Upload(ctx, entry, 0, strings.NewReader("data"))
		require.Error(t, err)
	})
}

func TestDownloadWritesAllChunks(t *testing.T) {
	stream := &fakeDownloadStream{
		size:   12,
		chunks: [][]byte{[]byte("hello-"), []byte("world!")},
	}
	svc := NewEntryService(&fakeEntryGateway{downloadStream: stream})

	var out bytes.Buffer
	require.NoError(t, svc.Download(context.Background(), "entry-1", &out))
	assert.Equal(t, "hello-world!", out.String())
	assert.True(t, stream.closed)
}

func TestDownloadValidationAndErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("empty id", func(t *testing.T) {
		svc := NewEntryService(&fakeEntryGateway{})
		err := svc.Download(ctx, "", io.Discard)
		assert.ErrorIs(t, err, ErrEmptyEntryID)
	})

	t.Run("gateway error", func(t *testing.T) {
		svc := NewEntryService(&fakeEntryGateway{downloadErr: errors.New("nope")})
		err := svc.Download(ctx, "id", io.Discard)
		require.Error(t, err)
	})

	t.Run("size error", func(t *testing.T) {
		svc := NewEntryService(&fakeEntryGateway{
			downloadStream: &fakeDownloadStream{sizeErr: errors.New("boom")},
		})
		err := svc.Download(ctx, "id", io.Discard)
		require.Error(t, err)
	})

	t.Run("chunk error", func(t *testing.T) {
		svc := NewEntryService(&fakeEntryGateway{
			downloadStream: &fakeDownloadStream{chunkErr: errors.New("boom")},
		})
		err := svc.Download(ctx, "id", io.Discard)
		require.Error(t, err)
	})
}
