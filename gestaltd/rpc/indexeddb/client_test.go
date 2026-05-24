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

func TestTransactionStreamSurvivesUnaryTimeoutAfterReturn(t *testing.T) {
	t.Parallel()

	stub := &txnStreamClient{}
	db := NewClient(stub, Options{UnaryTimeout: 5 * time.Millisecond})

	tx, err := db.Transaction(context.Background(), []string{"events"}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		t.Fatalf("Transaction: %v", err)
	}

	time.Sleep(15 * time.Millisecond)
	if err := stub.streamCtx.Err(); err != nil {
		t.Fatalf("transaction stream context cancelled after return: %v", err)
	}

	if err := tx.Abort(context.Background()); err != nil {
		t.Fatalf("Abort: %v", err)
	}
}
