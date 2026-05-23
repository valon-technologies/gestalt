package hostservicetest

import (
	"context"
	"io"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

// NoopS3Provider is a minimal S3 host-service stub for integration tests.
type NoopS3Provider struct{}

func (NoopS3Provider) Configure(context.Context, string, map[string]any) error { return nil }

func (NoopS3Provider) HeadObject(context.Context, gestalt.ObjectRef) (gestalt.ObjectMeta, error) {
	return gestalt.ObjectMeta{}, gestalt.ErrS3NotFound
}

func (NoopS3Provider) ReadObject(context.Context, gestalt.ObjectRef, *gestalt.ReadOptions) (gestalt.ObjectMeta, io.ReadCloser, error) {
	return gestalt.ObjectMeta{}, nil, gestalt.ErrS3NotFound
}

func (NoopS3Provider) WriteObject(context.Context, gestalt.ObjectRef, io.Reader, *gestalt.WriteOptions) (gestalt.ObjectMeta, error) {
	return gestalt.ObjectMeta{}, nil
}

func (NoopS3Provider) DeleteObject(context.Context, gestalt.ObjectRef) error { return nil }

func (NoopS3Provider) ListObjects(context.Context, gestalt.ListOptions) (gestalt.ListPage, error) {
	return gestalt.ListPage{}, nil
}

func (NoopS3Provider) CopyObject(context.Context, gestalt.ObjectRef, gestalt.ObjectRef, *gestalt.CopyOptions) (gestalt.ObjectMeta, error) {
	return gestalt.ObjectMeta{}, nil
}

func (NoopS3Provider) PresignObject(context.Context, gestalt.ObjectRef, *gestalt.PresignOptions) (gestalt.PresignResult, error) {
	return gestalt.PresignResult{}, nil
}

// NoopIndexedDBProvider is a minimal IndexedDB host-service stub for integration tests.
type NoopIndexedDBProvider struct{}

func (NoopIndexedDBProvider) Configure(context.Context, string, map[string]any) error { return nil }
func (NoopIndexedDBProvider) CreateObjectStore(context.Context, string, gestalt.ObjectStoreSchema) error {
	return nil
}
func (NoopIndexedDBProvider) DeleteObjectStore(context.Context, string) error { return nil }
func (NoopIndexedDBProvider) Get(context.Context, gestalt.IndexedDBObjectStoreRequest) (gestalt.Record, error) {
	return nil, gestalt.ErrNotFound
}
func (NoopIndexedDBProvider) GetKey(context.Context, gestalt.IndexedDBObjectStoreRequest) (string, error) {
	return "", gestalt.ErrNotFound
}
func (NoopIndexedDBProvider) Add(context.Context, gestalt.IndexedDBRecordRequest) error     { return nil }
func (NoopIndexedDBProvider) Put(context.Context, gestalt.IndexedDBRecordRequest) error     { return nil }
func (NoopIndexedDBProvider) Delete(context.Context, gestalt.IndexedDBObjectStoreRequest) error {
	return nil
}
func (NoopIndexedDBProvider) Clear(context.Context, string) error { return nil }
func (NoopIndexedDBProvider) GetAll(context.Context, gestalt.IndexedDBObjectStoreRangeRequest) ([]gestalt.Record, error) {
	return nil, nil
}
func (NoopIndexedDBProvider) GetAllKeys(context.Context, gestalt.IndexedDBObjectStoreRangeRequest) ([]string, error) {
	return nil, nil
}
func (NoopIndexedDBProvider) Count(context.Context, gestalt.IndexedDBObjectStoreRangeRequest) (int64, error) {
	return 0, nil
}
func (NoopIndexedDBProvider) DeleteRange(context.Context, gestalt.IndexedDBObjectStoreRangeRequest) (int64, error) {
	return 0, nil
}
func (NoopIndexedDBProvider) IndexGet(context.Context, gestalt.IndexedDBIndexQueryRequest) (gestalt.Record, error) {
	return nil, gestalt.ErrNotFound
}
func (NoopIndexedDBProvider) IndexGetKey(context.Context, gestalt.IndexedDBIndexQueryRequest) (string, error) {
	return "", gestalt.ErrNotFound
}
func (NoopIndexedDBProvider) IndexGetAll(context.Context, gestalt.IndexedDBIndexQueryRequest) ([]gestalt.Record, error) {
	return nil, nil
}
func (NoopIndexedDBProvider) IndexGetAllKeys(context.Context, gestalt.IndexedDBIndexQueryRequest) ([]string, error) {
	return nil, nil
}
func (NoopIndexedDBProvider) IndexCount(context.Context, gestalt.IndexedDBIndexQueryRequest) (int64, error) {
	return 0, nil
}
func (NoopIndexedDBProvider) IndexDelete(context.Context, gestalt.IndexedDBIndexQueryRequest) (int64, error) {
	return 0, nil
}
func (NoopIndexedDBProvider) OpenCursor(context.Context, gestalt.IndexedDBOpenCursorRequest) (gestalt.IndexedDBCursor, error) {
	return nil, gestalt.ErrNotFound
}
func (NoopIndexedDBProvider) BeginTransaction(context.Context, gestalt.IndexedDBBeginTransactionRequest) (gestalt.IndexedDBTransaction, error) {
	return nil, gestalt.ErrInvalidTransaction
}

// NoopExternalCredentialProvider is a minimal external-credentials host-service stub for integration tests.
type NoopExternalCredentialProvider struct{}

func (NoopExternalCredentialProvider) Configure(context.Context, string, map[string]any) error { return nil }
func (NoopExternalCredentialProvider) UpsertCredential(context.Context, *gestalt.UpsertExternalCredentialRequest) (*gestalt.ExternalCredential, error) {
	return &gestalt.ExternalCredential{}, nil
}
func (NoopExternalCredentialProvider) GetCredential(context.Context, *gestalt.GetExternalCredentialRequest) (*gestalt.ExternalCredential, error) {
	return nil, gestalt.ErrExternalCredentialNotFound
}
func (NoopExternalCredentialProvider) ListCredentials(context.Context, *gestalt.ListExternalCredentialsRequest) (*gestalt.ListExternalCredentialsResponse, error) {
	return &gestalt.ListExternalCredentialsResponse{}, nil
}
func (NoopExternalCredentialProvider) DeleteCredential(context.Context, *gestalt.DeleteExternalCredentialRequest) error {
	return nil
}
func (NoopExternalCredentialProvider) ValidateCredentialConfig(context.Context, *gestalt.ValidateExternalCredentialConfigRequest) error {
	return nil
}
func (NoopExternalCredentialProvider) ResolveCredential(context.Context, *gestalt.ResolveExternalCredentialRequest) (*gestalt.ResolveExternalCredentialResponse, error) {
	return &gestalt.ResolveExternalCredentialResponse{}, nil
}
func (NoopExternalCredentialProvider) ExchangeCredential(context.Context, *gestalt.ExchangeExternalCredentialRequest) (*gestalt.ExchangeExternalCredentialResponse, error) {
	return &gestalt.ExchangeExternalCredentialResponse{}, nil
}
