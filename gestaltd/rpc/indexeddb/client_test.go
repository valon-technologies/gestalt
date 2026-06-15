package indexeddb

import (
	"context"
	"io"
	"testing"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

type txnStreamClient struct {
	proto.IndexedDBClient
	streamCtx context.Context
}

func (c *txnStreamClient) Transaction(ctx context.Context, _ ...grpc.CallOption) (grpc.BidiStreamingClient[proto.TransactionClientMessage, proto.TransactionServerMessage], error) {
	c.streamCtx = ctx
	return &txnStreamStub{}, nil
}

type txnStreamStub struct {
	grpc.ClientStream
	recvCount int
}

func (s *txnStreamStub) Send(*proto.TransactionClientMessage) error { return nil }

func (s *txnStreamStub) Recv() (*proto.TransactionServerMessage, error) {
	switch s.recvCount {
	case 0:
		s.recvCount++
		return &proto.TransactionServerMessage{
			Msg: &proto.TransactionServerMessage_Begin{Begin: &proto.TransactionBeginResponse{}},
		}, nil
	case 1:
		s.recvCount++
		return &proto.TransactionServerMessage{
			Msg: &proto.TransactionServerMessage_Abort{Abort: &proto.TransactionAbortResponse{}},
		}, nil
	default:
		return nil, io.EOF
	}
}

func (s *txnStreamStub) CloseSend() error { return nil }

type cursorStreamClient struct {
	proto.IndexedDBClient
	streamCtx context.Context
}

func (c *cursorStreamClient) OpenCursor(ctx context.Context, _ ...grpc.CallOption) (grpc.BidiStreamingClient[proto.CursorClientMessage, proto.CursorResponse], error) {
	c.streamCtx = ctx
	return &cursorStreamStub{}, nil
}

type cursorStreamStub struct {
	grpc.ClientStream
	recvStage int
}

func (s *cursorStreamStub) Send(*proto.CursorClientMessage) error { return nil }

func (s *cursorStreamStub) Recv() (*proto.CursorResponse, error) {
	switch s.recvStage {
	case 0:
		s.recvStage++
		return &proto.CursorResponse{Result: &proto.CursorResponse_Done{}}, nil
	case 1:
		s.recvStage++
		return &proto.CursorResponse{Result: &proto.CursorResponse_Done{Done: true}}, nil
	default:
		return nil, io.EOF
	}
}

func (s *cursorStreamStub) CloseSend() error { return nil }

func assertStreamContextSurvivesUnaryTimeout(t *testing.T, streamCtx context.Context) {
	t.Helper()

	time.Sleep(15 * time.Millisecond)
	if err := streamCtx.Err(); err != nil {
		t.Fatalf("stream context cancelled after return: %v", err)
	}
}

func TestBidiStreamSurvivesUnaryTimeoutAfterReturn(t *testing.T) {
	t.Parallel()

	t.Run("transaction", func(t *testing.T) {
		stub := &txnStreamClient{}
		db := NewClient(stub, Options{UnaryTimeout: 5 * time.Millisecond})

		tx, err := db.Transaction(context.Background(), []string{"events"}, idb.TransactionReadwrite, idb.TransactionOptions{})
		if err != nil {
			t.Fatalf("Transaction: %v", err)
		}

		assertStreamContextSurvivesUnaryTimeout(t, stub.streamCtx)

		if err := tx.Abort(context.Background()); err != nil {
			t.Fatalf("Abort: %v", err)
		}
	})

	t.Run("cursor", func(t *testing.T) {
		stub := &cursorStreamClient{}
		db := NewClient(stub, Options{UnaryTimeout: 5 * time.Millisecond})

		cursor, err := db.ObjectStore("events").OpenCursor(context.Background(), nil, idb.CursorNext)
		if err != nil {
			t.Fatalf("OpenCursor: %v", err)
		}

		assertStreamContextSurvivesUnaryTimeout(t, stub.streamCtx)

		if cursor.Continue() {
			t.Fatal("Continue returned true, want false")
		}
		if err := cursor.Err(); err != nil {
			t.Fatalf("cursor.Err() = %v, want nil", err)
		}
	})
}
