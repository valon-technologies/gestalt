package gestalt_test

import (
	"context"
	"sync"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/sdk/go/hostservicetest"
	"google.golang.org/grpc"
)

func TestSharedHostServiceConnReusedAcrossServices(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}

	hostservicetest.Start(t, func(*grpc.Server) {})

	before := gestalt.TestingHostServiceConnCount()

	idb, err := gestalt.IndexedDB()
	if err != nil {
		t.Fatalf("IndexedDB: %v", err)
	}
	defer func() { _ = idb.Close() }()

	s3, err := gestalt.S3()
	if err != nil {
		t.Fatalf("S3: %v", err)
	}
	defer func() { _ = s3.Close() }()

	ext, err := gestalt.ExternalCredentials()
	if err != nil {
		t.Fatalf("ExternalCredentials: %v", err)
	}
	defer func() { _ = ext.Close() }()

	if got := gestalt.TestingHostServiceConnCount() - before; got != 1 {
		t.Fatalf("new pooled host-service connections = %d, want 1", got)
	}
}

func TestSharedHostServiceConnReusedConcurrently(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}

	hostservicetest.Start(t, func(*grpc.Server) {})

	before := gestalt.TestingHostServiceConnCount()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			idb, err := gestalt.IndexedDB()
			if err != nil {
				t.Errorf("IndexedDB: %v", err)
				return
			}
			defer func() { _ = idb.Close() }()

			s3, err := gestalt.S3()
			if err != nil {
				t.Errorf("S3: %v", err)
				return
			}
			defer func() { _ = s3.Close() }()
		}()
	}
	wg.Wait()

	if got := gestalt.TestingHostServiceConnCount() - before; got != 1 {
		t.Fatalf("new pooled host-service connections = %d, want 1", got)
	}
}

func TestServeHostServiceGRPCRegistersMultipleServices(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}

	hostservicetest.Start(t, func(srv *grpc.Server) {
		gestalt.RegisterIndexedDBHostService(srv, hostservicetest.NoopIndexedDBProvider{})
		gestalt.RegisterExternalCredentialHostService(srv, hostservicetest.NoopExternalCredentialProvider{})
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
	if _, err := ext.ListCredentials(ctx, &gestalt.ListExternalCredentialsRequest{}); err != nil {
		t.Fatalf("ExternalCredentials.ListCredentials: %v", err)
	}
}
