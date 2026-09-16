package transport

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
	"github.com/dmitriy/gophkeeper/internal/server/model"
	"github.com/dmitriy/gophkeeper/internal/server/service"
)

// fakeUploadStream is an in-memory server stream for the Upload RPC.
type fakeUploadStream struct {
	gophkeeperv1.EntryService_UploadServer
	msgs  []*gophkeeperv1.UploadEntryRequest
	sent  int
	reply *gophkeeperv1.UploadEntryResponse
}

func (s *fakeUploadStream) Recv() (*gophkeeperv1.UploadEntryRequest, error) {
	if s.sent >= len(s.msgs) {
		return nil, io.EOF
	}
	m := s.msgs[s.sent]
	s.sent++
	return m, nil
}

func (s *fakeUploadStream) SendAndClose(resp *gophkeeperv1.UploadEntryResponse) error {
	s.reply = resp
	return nil
}

func (s *fakeUploadStream) Context() context.Context { return claimsContext("user-1") }

// recordingSession is a service.UploadSession mock recording the driven
// calls and returning preset results.
type recordingSession struct {
	chunks    [][]byte
	digest    string
	committed bool
	aborted   bool
	commitErr error
	addErr    error
	result    *model.Entry
}

func (s *recordingSession) AddChunk(data []byte) error {
	if s.addErr != nil {
		return s.addErr
	}
	s.chunks = append(s.chunks, data)
	return nil
}

func (s *recordingSession) Commit(_ context.Context, sha256hex string) (*model.Entry, error) {
	if s.commitErr != nil {
		return nil, s.commitErr
	}
	s.committed = true
	s.digest = sha256hex
	return s.result, nil
}

func (s *recordingSession) Abort(_ context.Context) { s.aborted = true }

// fakeDownloadStream collects the messages sent by the download handler.
type fakeDownloadStream struct {
	gophkeeperv1.EntryService_DownloadEntryDataServer
	msgs []*gophkeeperv1.DownloadEntryDataResponse
}

func (s *fakeDownloadStream) Send(m *gophkeeperv1.DownloadEntryDataResponse) error {
	s.msgs = append(s.msgs, m)
	return nil
}

func (s *fakeDownloadStream) Context() context.Context { return claimsContext("user-1") }

// uploadHeader/uploadChunk/uploadFooter build the upload stream
// messages (opaque API: oneof cases are set via the builder).
func uploadHeader(e *gophkeeperv1.Entry) *gophkeeperv1.UploadEntryRequest {
	return (&gophkeeperv1.UploadEntryRequest_builder{
		Header: (&gophkeeperv1.UploadEntryHeader_builder{Entry: e}).Build(),
	}).Build()
}

func uploadChunk(data []byte) *gophkeeperv1.UploadEntryRequest {
	return (&gophkeeperv1.UploadEntryRequest_builder{
		Chunk: (&gophkeeperv1.UploadEntryChunk_builder{Data: data}).Build(),
	}).Build()
}

func uploadFooter(sha string) *gophkeeperv1.UploadEntryRequest {
	return (&gophkeeperv1.UploadEntryRequest_builder{
		Footer: (&gophkeeperv1.UploadEntryFooter_builder{Sha256: sha}).Build(),
	}).Build()
}

func entryProto(mutate func(*gophkeeperv1.Entry_builder)) *gophkeeperv1.Entry {
	b := &gophkeeperv1.Entry_builder{}
	mutate(b)
	return b.Build()
}

func TestUploadHandler_HappyPath(t *testing.T) {
	result := sampleEntry()
	session := &recordingSession{result: result}
	fake := &fakeEntryService{
		beginUploadFn: func(_ context.Context, userID string, header *model.Entry, expectedVersion int64) (service.UploadSession, error) {
			assert.Equal(t, "user-1", userID)
			assert.Equal(t, "movie", header.Label)
			assert.Equal(t, int64(0), expectedVersion)
			return session, nil
		},
	}
	handler := NewEntryHandler(fake)

	stream := &fakeUploadStream{msgs: []*gophkeeperv1.UploadEntryRequest{
		uploadHeader(entryProto(func(b *gophkeeperv1.Entry_builder) {
			b.Type = gophkeeperv1.EntryType_ENTRY_TYPE_BINARY
			b.Label = "movie"
		})),
		uploadChunk([]byte("part1-")),
		uploadChunk([]byte("part2")),
		uploadFooter("abc"),
	}}

	err := handler.Upload(stream)
	require.NoError(t, err)

	require.Len(t, session.chunks, 2)
	assert.Equal(t, []byte("part1-"), session.chunks[0])
	assert.Equal(t, []byte("part2"), session.chunks[1])
	assert.Equal(t, "abc", session.digest)
	assert.True(t, session.committed)
	assert.False(t, session.aborted)

	require.NotNil(t, stream.reply)
	assert.Equal(t, "entry-1", stream.reply.GetEntry().GetId())
}

func TestUploadHandler_AbortsOnError(t *testing.T) {
	// Commit fails with a checksum mismatch; the handler must abort.
	session := &recordingSession{commitErr: service.ErrChecksumMismatch}
	fake := &fakeEntryService{
		beginUploadFn: func(context.Context, string, *model.Entry, int64) (service.UploadSession, error) {
			return session, nil
		},
	}
	handler := NewEntryHandler(fake)

	stream := &fakeUploadStream{msgs: []*gophkeeperv1.UploadEntryRequest{
		uploadHeader(entryProto(func(b *gophkeeperv1.Entry_builder) {
			b.Type = gophkeeperv1.EntryType_ENTRY_TYPE_BINARY
			b.Label = "x"
		})),
		uploadFooter("bad"),
	}}

	err := handler.Upload(stream)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.True(t, session.aborted)
	assert.False(t, session.committed)
}

func TestUploadHandler_MissingHeader(t *testing.T) {
	handler := NewEntryHandler(&fakeEntryService{})

	stream := &fakeUploadStream{msgs: []*gophkeeperv1.UploadEntryRequest{
		uploadChunk([]byte("x")),
	}}
	err := handler.Upload(stream)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Contains(t, status.Convert(err).Message(), "header")
}

func TestUploadHandler_MissingFooter(t *testing.T) {
	session := &recordingSession{}
	fake := &fakeEntryService{
		beginUploadFn: func(context.Context, string, *model.Entry, int64) (service.UploadSession, error) {
			return session, nil
		},
	}
	handler := NewEntryHandler(fake)

	stream := &fakeUploadStream{msgs: []*gophkeeperv1.UploadEntryRequest{
		uploadHeader(entryProto(func(b *gophkeeperv1.Entry_builder) {
			b.Type = gophkeeperv1.EntryType_ENTRY_TYPE_BINARY
			b.Label = "x"
		})),
		uploadChunk([]byte("x")),
	}}

	err := handler.Upload(stream)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.True(t, session.aborted)
}

func TestUploadHandler_BeginUploadError(t *testing.T) {
	fake := &fakeEntryService{
		beginUploadFn: func(context.Context, string, *model.Entry, int64) (service.UploadSession, error) {
			return nil, service.ErrEmptyLabel
		},
	}
	handler := NewEntryHandler(fake)

	stream := &fakeUploadStream{msgs: []*gophkeeperv1.UploadEntryRequest{
		uploadHeader(entryProto(func(b *gophkeeperv1.Entry_builder) {
			b.Type = gophkeeperv1.EntryType_ENTRY_TYPE_BINARY
		})),
	}}
	err := handler.Upload(stream)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestDownloadEntryDataHandler_Chunked(t *testing.T) {
	fake := &fakeEntryService{
		dataInfoFn: func(_ context.Context, _, entryID string) (int, int64, error) {
			assert.Equal(t, "entry-1", entryID)
			return 2, 10, nil
		},
		downloadChunkFn: func(_ context.Context, _, _ string, seq int) ([]byte, error) {
			return []byte{byte(seq)}, nil
		},
	}
	handler := NewEntryHandler(fake)

	stream := &fakeDownloadStream{}
	err := handler.DownloadEntryData((&gophkeeperv1.DownloadEntryDataRequest_builder{Id: "entry-1"}).Build(), stream)
	require.NoError(t, err)

	require.Len(t, stream.msgs, 3)
	assert.Equal(t, int64(10), stream.msgs[0].GetHeader().GetSize())
	assert.Equal(t, []byte{0}, stream.msgs[1].GetChunk().GetData())
	assert.Equal(t, []byte{1}, stream.msgs[2].GetChunk().GetData())
}

func TestDownloadEntryDataHandler_Inline(t *testing.T) {
	entry := sampleEntry() // Data = {0x01, 0x02, 0x03}
	fake := &fakeEntryService{
		dataInfoFn: func(context.Context, string, string) (int, int64, error) {
			return 0, 3, nil
		},
		getFn: func(context.Context, string, string) (*model.Entry, error) {
			return entry, nil
		},
	}
	handler := NewEntryHandler(fake)

	stream := &fakeDownloadStream{}
	err := handler.DownloadEntryData((&gophkeeperv1.DownloadEntryDataRequest_builder{Id: "entry-1"}).Build(), stream)
	require.NoError(t, err)

	require.Len(t, stream.msgs, 2)
	assert.Equal(t, int64(3), stream.msgs[0].GetHeader().GetSize())
	assert.Equal(t, []byte{0x01, 0x02, 0x03}, stream.msgs[1].GetChunk().GetData())
}

func TestDownloadEntryDataHandler_NotFound(t *testing.T) {
	fake := &fakeEntryService{
		dataInfoFn: func(context.Context, string, string) (int, int64, error) {
			return 0, 0, model.ErrNotFound
		},
	}
	handler := NewEntryHandler(fake)

	stream := &fakeDownloadStream{}
	err := handler.DownloadEntryData((&gophkeeperv1.DownloadEntryDataRequest_builder{Id: "nope"}).Build(), stream)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.Empty(t, stream.msgs)
}
