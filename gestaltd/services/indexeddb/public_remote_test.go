package indexeddb

import (
	"context"
	"testing"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type bindingIndexedDBClient struct {
	proto.IndexedDBClient
	lastMD metadata.MD
}

func (c *bindingIndexedDBClient) Get(ctx context.Context, req *proto.ObjectStoreRequest, _ ...grpc.CallOption) (*proto.RecordResponse, error) {
	c.lastMD, _ = metadata.FromOutgoingContext(ctx)
	return &proto.RecordResponse{Record: &proto.Record{}}, nil
}

func TestNewPublicRemoteSetsHostBinding(t *testing.T) {
	t.Parallel()

	client := &bindingIndexedDBClient{}
	provider, err := NewPublicRemote(client, "archive")
	if err != nil {
		t.Fatalf("NewPublicRemote: %v", err)
	}
	db, ok := provider.(idb.Database)
	if !ok {
		t.Fatalf("provider type = %T, want idb.Database", provider)
	}
	if _, err := db.ObjectStore("items").Get(context.Background(), "k1"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	got := client.lastMD.Get("x-gestalt-host-binding")
	if len(got) != 1 || got[0] != "archive" {
		t.Fatalf("host binding = %#v, want archive", got)
	}
}

func TestNewPublicRemotePingWithoutLifecycle(t *testing.T) {
	t.Parallel()

	client := &bindingIndexedDBClient{}
	provider, err := NewPublicRemote(client, "archive")
	if err != nil {
		t.Fatalf("NewPublicRemote: %v", err)
	}
	if err := provider.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}
