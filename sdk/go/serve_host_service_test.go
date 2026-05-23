package gestalt_test

import (
	"context"
	"io"
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

	hostservicetest.Start(t, func(srv *grpc.Server) {
		gestalt.RegisterIndexedDBHostService(srv, multiServiceIndexedDBStub{})
		gestalt.RegisterS3HostService(srv, multiServiceS3Stub{})
		gestalt.RegisterExternalCredentialHostService(srv, multiServiceExternalCredentialStub{})
	})

	before := gestalt.TestingHostServiceConnCount()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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

	if err := idb.CreateObjectStore(ctx, "store", gestalt.ObjectStoreSchema{}); err != nil {
		t.Fatalf("IndexedDB.CreateObjectStore: %v", err)
	}
	if _, err := s3.ListObjects(ctx, gestalt.ListOptions{Bucket: "fixtures"}); err != nil {
		t.Fatalf("S3.ListObjects: %v", err)
	}
	if _, err := ext.ListCredentials(ctx, &gestalt.ListExternalCredentialsRequest{}); err != nil {
		t.Fatalf("ExternalCredentials.ListCredentials: %v", err)
	}

	if got := gestalt.TestingHostServiceConnCount() - before; got != 1 {
		t.Fatalf("new pooled host-service connections = %d, want 1", got)
	}
}

type multiServiceIndexedDBStub struct{}

func (multiServiceIndexedDBStub) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (multiServiceIndexedDBStub) CreateObjectStore(context.Context, string, gestalt.ObjectStoreSchema) error {
	return nil
}

func (multiServiceIndexedDBStub) DeleteObjectStore(context.Context, string) error { return nil }
func (multiServiceIndexedDBStub) Get(context.Context, gestalt.IndexedDBObjectStoreRequest) (gestalt.Record, error) {
	return nil, gestalt.ErrNotFound
}
func (multiServiceIndexedDBStub) GetKey(context.Context, gestalt.IndexedDBObjectStoreRequest) (string, error) {
	return "", gestalt.ErrNotFound
}
func (multiServiceIndexedDBStub) Add(context.Context, gestalt.IndexedDBRecordRequest) error     { return nil }
func (multiServiceIndexedDBStub) Put(context.Context, gestalt.IndexedDBRecordRequest) error     { return nil }
func (multiServiceIndexedDBStub) Delete(context.Context, gestalt.IndexedDBObjectStoreRequest) error {
	return nil
}
func (multiServiceIndexedDBStub) Clear(context.Context, string) error { return nil }
func (multiServiceIndexedDBStub) GetAll(context.Context, gestalt.IndexedDBObjectStoreRangeRequest) ([]gestalt.Record, error) {
	return nil, nil
}
func (multiServiceIndexedDBStub) GetAllKeys(context.Context, gestalt.IndexedDBObjectStoreRangeRequest) ([]string, error) {
	return nil, nil
}
func (multiServiceIndexedDBStub) Count(context.Context, gestalt.IndexedDBObjectStoreRangeRequest) (int64, error) {
	return 0, nil
}
func (multiServiceIndexedDBStub) DeleteRange(context.Context, gestalt.IndexedDBObjectStoreRangeRequest) (int64, error) {
	return 0, nil
}
func (multiServiceIndexedDBStub) IndexGet(context.Context, gestalt.IndexedDBIndexQueryRequest) (gestalt.Record, error) {
	return nil, gestalt.ErrNotFound
}
func (multiServiceIndexedDBStub) IndexGetKey(context.Context, gestalt.IndexedDBIndexQueryRequest) (string, error) {
	return "", gestalt.ErrNotFound
}
func (multiServiceIndexedDBStub) IndexGetAll(context.Context, gestalt.IndexedDBIndexQueryRequest) ([]gestalt.Record, error) {
	return nil, nil
}
func (multiServiceIndexedDBStub) IndexGetAllKeys(context.Context, gestalt.IndexedDBIndexQueryRequest) ([]string, error) {
	return nil, nil
}
func (multiServiceIndexedDBStub) IndexCount(context.Context, gestalt.IndexedDBIndexQueryRequest) (int64, error) {
	return 0, nil
}
func (multiServiceIndexedDBStub) IndexDelete(context.Context, gestalt.IndexedDBIndexQueryRequest) (int64, error) {
	return 0, nil
}
func (multiServiceIndexedDBStub) OpenCursor(context.Context, gestalt.IndexedDBOpenCursorRequest) (gestalt.IndexedDBCursor, error) {
	return nil, gestalt.ErrNotFound
}
func (multiServiceIndexedDBStub) BeginTransaction(context.Context, gestalt.IndexedDBBeginTransactionRequest) (gestalt.IndexedDBTransaction, error) {
	return nil, gestalt.ErrInvalidTransaction
}

type multiServiceS3Stub struct{}

func (multiServiceS3Stub) Configure(context.Context, string, map[string]any) error { return nil }
func (multiServiceS3Stub) HeadObject(context.Context, gestalt.ObjectRef) (gestalt.ObjectMeta, error) {
	return gestalt.ObjectMeta{}, gestalt.ErrS3NotFound
}
func (multiServiceS3Stub) ReadObject(context.Context, gestalt.ObjectRef, *gestalt.ReadOptions) (gestalt.ObjectMeta, io.ReadCloser, error) {
	return gestalt.ObjectMeta{}, nil, gestalt.ErrS3NotFound
}
func (multiServiceS3Stub) WriteObject(context.Context, gestalt.ObjectRef, io.Reader, *gestalt.WriteOptions) (gestalt.ObjectMeta, error) {
	return gestalt.ObjectMeta{}, nil
}
func (multiServiceS3Stub) DeleteObject(context.Context, gestalt.ObjectRef) error { return nil }
func (multiServiceS3Stub) ListObjects(context.Context, gestalt.ListOptions) (gestalt.ListPage, error) {
	return gestalt.ListPage{}, nil
}
func (multiServiceS3Stub) CopyObject(context.Context, gestalt.ObjectRef, gestalt.ObjectRef, *gestalt.CopyOptions) (gestalt.ObjectMeta, error) {
	return gestalt.ObjectMeta{}, nil
}
func (multiServiceS3Stub) PresignObject(context.Context, gestalt.ObjectRef, *gestalt.PresignOptions) (gestalt.PresignResult, error) {
	return gestalt.PresignResult{}, nil
}

type multiServiceExternalCredentialStub struct{}

func (multiServiceExternalCredentialStub) Configure(context.Context, string, map[string]any) error {
	return nil
}
func (multiServiceExternalCredentialStub) UpsertCredential(context.Context, *gestalt.UpsertExternalCredentialRequest) (*gestalt.ExternalCredential, error) {
	return &gestalt.ExternalCredential{}, nil
}
func (multiServiceExternalCredentialStub) GetCredential(context.Context, *gestalt.GetExternalCredentialRequest) (*gestalt.ExternalCredential, error) {
	return nil, gestalt.ErrExternalCredentialNotFound
}
func (multiServiceExternalCredentialStub) ListCredentials(context.Context, *gestalt.ListExternalCredentialsRequest) (*gestalt.ListExternalCredentialsResponse, error) {
	return &gestalt.ListExternalCredentialsResponse{}, nil
}
func (multiServiceExternalCredentialStub) DeleteCredential(context.Context, *gestalt.DeleteExternalCredentialRequest) error {
	return nil
}
func (multiServiceExternalCredentialStub) ValidateCredentialConfig(context.Context, *gestalt.ValidateExternalCredentialConfigRequest) error {
	return nil
}
func (multiServiceExternalCredentialStub) ResolveCredential(context.Context, *gestalt.ResolveExternalCredentialRequest) (*gestalt.ResolveExternalCredentialResponse, error) {
	return &gestalt.ResolveExternalCredentialResponse{}, nil
}
func (multiServiceExternalCredentialStub) ExchangeCredential(context.Context, *gestalt.ExchangeExternalCredentialRequest) (*gestalt.ExchangeExternalCredentialResponse, error) {
	return &gestalt.ExchangeExternalCredentialResponse{}, nil
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
	if _, err := ext.ListCredentials(ctx, &gestalt.ListExternalCredentialsRequest{}); err != nil {
		t.Fatalf("ExternalCredentials.ListCredentials: %v", err)
	}
}
