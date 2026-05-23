package hostservicetest

import (
	"context"
	"io"
	"os"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func TestStartServesRegisteredHostService(t *testing.T) {
	StartS3(t, testS3Provider{})
	if target := os.Getenv(gestalt.EnvHostServiceSocket); target == "" {
		t.Fatal("host service socket env not set")
	}
	client, err := gestalt.S3()
	if err != nil {
		t.Fatalf("S3: %v", err)
	}
	defer func() { _ = client.Close() }()

	page, err := client.ListObjects(context.Background(), gestalt.ListOptions{Bucket: "fixtures"})
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if len(page.Objects) != 1 || page.Objects[0].Ref.Key != "ready" {
		t.Fatalf("ListObjects objects = %#v, want ready object", page.Objects)
	}
}

type testS3Provider struct{}

func (testS3Provider) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (testS3Provider) HeadObject(context.Context, gestalt.ObjectRef) (gestalt.ObjectMeta, error) {
	return gestalt.ObjectMeta{}, gestalt.ErrS3NotFound
}

func (testS3Provider) ReadObject(context.Context, gestalt.ObjectRef, *gestalt.ReadOptions) (gestalt.ObjectMeta, io.ReadCloser, error) {
	return gestalt.ObjectMeta{}, nil, gestalt.ErrS3NotFound
}

func (testS3Provider) WriteObject(context.Context, gestalt.ObjectRef, io.Reader, *gestalt.WriteOptions) (gestalt.ObjectMeta, error) {
	return gestalt.ObjectMeta{}, nil
}

func (testS3Provider) DeleteObject(context.Context, gestalt.ObjectRef) error {
	return nil
}

func (testS3Provider) ListObjects(context.Context, gestalt.ListOptions) (gestalt.ListPage, error) {
	return gestalt.ListPage{
		Objects: []gestalt.ObjectMeta{{Ref: gestalt.ObjectRef{Bucket: "fixtures", Key: "ready"}}},
	}, nil
}

func (testS3Provider) CopyObject(context.Context, gestalt.ObjectRef, gestalt.ObjectRef, *gestalt.CopyOptions) (gestalt.ObjectMeta, error) {
	return gestalt.ObjectMeta{}, nil
}

func (testS3Provider) PresignObject(context.Context, gestalt.ObjectRef, *gestalt.PresignOptions) (gestalt.PresignResult, error) {
	return gestalt.PresignResult{}, nil
}
