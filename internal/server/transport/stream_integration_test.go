package transport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	v1 "github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
	"github.com/dmitriy/gophkeeper/internal/server/auth"
	"github.com/dmitriy/gophkeeper/internal/server/middleware"
	"github.com/dmitriy/gophkeeper/internal/server/model"
	"github.com/dmitriy/gophkeeper/internal/server/service"
)

// TestUploadStreamWithAuth runs a real gRPC server (bufnet) with the
// auth stream interceptor and the real Upload handler over a fake
// service, then drives the client-streaming Upload end to end.
func TestUploadStreamWithAuth(t *testing.T) {
	jwt, err := auth.New("test-secret", time.Hour)
	require.NoError(t, err)
	token, err := jwt.Generate("user-1", "alice")
	require.NoError(t, err)

	var gotHeader *model.Entry
	session := &recordingSession{
		result: sampleEntry(),
	}
	fake := &fakeEntryService{
		beginUploadFn: func(_ context.Context, userID string, header *model.Entry, expectedVersion int64) (service.UploadSession, error) {
			gotHeader = header
			_ = userID
			_ = expectedVersion
			return session, nil
		},
	}

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.ChainStreamInterceptor(middleware.NewAuthStreamInterceptor(jwt)))
	v1.RegisterEntryServiceServer(srv, NewEntryHandler(fake))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStreamInterceptor(func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
			return streamer(ctx, desc, cc, method, opts...)
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	client := v1.NewEntryServiceClient(conn)
	ctx := context.Background()

	stream, err := client.Upload(ctx)
	require.NoError(t, err)

	require.NoError(t, stream.Send(v1.UploadEntryRequest_builder{
		Header: (&v1.UploadEntryHeader_builder{
			Entry: (&v1.Entry_builder{
				Type: v1.EntryType_ENTRY_TYPE_BINARY, Label: "file.bin",
			}).Build(),
		}).Build(),
	}.Build()))

	payload := strings.Repeat("x", 3*1024)
	h := sha256.New()
	for i := 0; i < 3; i++ {
		chunk := []byte(payload[:1024])
		require.NoError(t, stream.Send(v1.UploadEntryRequest_builder{
			Chunk: (&v1.UploadEntryChunk_builder{Data: chunk}).Build(),
		}.Build()), "chunk %d", i)
		h.Write(chunk)
	}
	require.NoError(t, stream.Send(v1.UploadEntryRequest_builder{
		Footer: (&v1.UploadEntryFooter_builder{Sha256: hex.EncodeToString(h.Sum(nil))}).Build(),
	}.Build()))
	resp, err := stream.CloseAndRecv()
	require.NoError(t, err)
	require.NotNil(t, resp.GetEntry())

	require.NotNil(t, gotHeader)
	require.Equal(t, "file.bin", gotHeader.Label)
	require.Len(t, session.chunks, 3)
}
