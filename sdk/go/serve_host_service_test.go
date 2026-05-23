package gestalt_test

import (
	"context"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/sdk/go/hostservicetest"
	"google.golang.org/grpc"
)

type multiServiceIndexedDBStub struct {
	gestalt.IndexedDBProvider
}

func (multiServiceIndexedDBStub) CreateObjectStore(context.Context, string, gestalt.ObjectStoreSchema) error {
	return nil
}

type multiServiceExternalCredentialStub struct {
	gestalt.ExternalCredentialProvider
}

func (multiServiceExternalCredentialStub) Configure(context.Context, string, map[string]any) error {
	return nil
}

func TestServeHostServiceGRPCRegistersMultipleServices(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}

	hostservicetest.Start(t, func(srv *grpc.Server) {
		gestalt.RegisterIndexedDBHostService(srv, multiServiceIndexedDBStub{})
		gestalt.RegisterExternalCredentialHostService(srv, multiServiceExternalCredentialStub{})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	idb, err := gestalt.IndexedDB()
	if err != nil {
		t.Fatalf("IndexedDB: %v", err)
	}
	defer func() { _ = idb.Close() }()

	ext, err := gestalt.ExternalCredentials()
	if err != nil {
		t.Fatalf("ExternalCredentials: %v", err)
	}
	defer func() { _ = ext.Close() }()

	if err := idb.CreateObjectStore(ctx, "store", gestalt.ObjectStoreSchema{}); err != nil {
		t.Fatalf("IndexedDB.CreateObjectStore: %v", err)
	}
}
