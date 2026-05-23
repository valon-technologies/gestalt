package hostservicetest

import (
	"context"
	"os"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func TestStartServesRegisteredHostService(t *testing.T) {
	StartS3(t, NoopS3Provider{})
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
	if len(page.Objects) != 0 {
		t.Fatalf("ListObjects objects = %#v, want empty page from NoopS3Provider", page.Objects)
	}
}
