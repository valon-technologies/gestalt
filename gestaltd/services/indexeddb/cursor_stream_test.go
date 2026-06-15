package indexeddb

import (
	"context"
	"errors"
	"io"
	"testing"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newEmptyStoreCursorClient(t *testing.T) proto.IndexedDBClient {
	t.Helper()

	stub := &coretesting.StubIndexedDB{}
	ctx := context.Background()
	if _, err := stub.CreateObjectStore(ctx, "empty", idb.ObjectStoreOptions{}); err != nil {
		t.Fatal(err)
	}

	conn := newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterIndexedDBServer(srv, NewServer(stub, "", ServerOptions{}))
	})
	return proto.NewIndexedDBClient(conn)
}

func openEmptyStoreCursorStream(t *testing.T, client proto.IndexedDBClient) grpc.BidiStreamingClient[proto.CursorClientMessage, proto.CursorResponse] {
	t.Helper()

	stream, err := client.OpenCursor(context.Background())
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}

	if err := stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Open{
			Open: &proto.OpenCursorRequest{
				Store:     "empty",
				Direction: proto.CursorDirection_CURSOR_NEXT,
			},
		},
	}); err != nil {
		t.Fatalf("Send open: %v", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv open ack: %v", err)
	}
	done, ok := resp.GetResult().(*proto.CursorResponse_Done)
	if !ok || done.Done {
		t.Fatalf("open ack = %#v, want Done{Done:false}", resp.GetResult())
	}
	return stream
}

func TestCursorStream_ClientHalfClose(t *testing.T) {
	t.Parallel()

	client := newEmptyStoreCursorClient(t)

	t.Run("after_open", func(t *testing.T) {
		stream := openEmptyStoreCursorStream(t, client)
		if err := stream.CloseSend(); err != nil {
			t.Fatalf("CloseSend: %v", err)
		}
		assertCursorStreamTerminalEOF(t, stream)
	})

	t.Run("after_exhaustion", func(t *testing.T) {
		stream := openEmptyStoreCursorStream(t, client)

		if err := stream.Send(&proto.CursorClientMessage{
			Msg: &proto.CursorClientMessage_Command{
				Command: &proto.CursorCommand{
					Command: &proto.CursorCommand_Next{Next: true},
				},
			},
		}); err != nil {
			t.Fatalf("Send next: %v", err)
		}

		resp, err := stream.Recv()
		if err != nil {
			t.Fatalf("Recv exhaustion: %v", err)
		}
		done, ok := resp.GetResult().(*proto.CursorResponse_Done)
		if !ok || !done.Done {
			t.Fatalf("exhaustion response = %#v, want Done{Done:true}", resp.GetResult())
		}

		if err := stream.CloseSend(); err != nil {
			t.Fatalf("CloseSend: %v", err)
		}
		assertCursorStreamTerminalEOF(t, stream)
	})
}

func assertCursorStreamTerminalEOF(t *testing.T, stream grpc.BidiStreamingClient[proto.CursorClientMessage, proto.CursorResponse]) {
	t.Helper()

	for {
		_, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			if st, ok := status.FromError(err); ok && st.Code() != codes.OK {
				t.Fatalf("terminal Recv error = %v (code=%v), want io.EOF", err, st.Code())
			}
			t.Fatalf("terminal Recv error = %v, want io.EOF", err)
		}
		t.Fatal("terminal Recv returned message after CloseSend, want io.EOF")
	}
}
