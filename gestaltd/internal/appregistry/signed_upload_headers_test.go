package appregistry

import (
	"context"
	"strings"
	"testing"

	"cloud.google.com/go/storage"
)

func TestBuildSignedUploadHeadersContract(t *testing.T) {
	t.Parallel()

	digest := strings.Repeat("a", 64)
	headers, err := BuildSignedUploadHeaders(1234, digest)
	if err != nil {
		t.Fatalf("BuildSignedUploadHeaders: %v", err)
	}
	if err := validateSignedUploadHeaders(headers, 1234, digest); err != nil {
		t.Fatalf("validateSignedUploadHeaders: %v", err)
	}
	if headers[UploadHeaderContentLength] != "1234" {
		t.Fatalf("Content-Length = %q", headers[UploadHeaderContentLength])
	}
	if headers[UploadHeaderXGoogIfGenerationMatch] != "0" {
		t.Fatalf("if-generation-match = %q", headers[UploadHeaderXGoogIfGenerationMatch])
	}
	if headers[UploadHeaderXGoogMetaSHA256] != digest {
		t.Fatalf("meta sha256 = %q", headers[UploadHeaderXGoogMetaSHA256])
	}
	if headers[UploadHeaderXGoogContentSHA256] == "" {
		t.Fatal("content sha256 header is required")
	}
}

func TestGCSUploadSignerSignCreateUploadReturnsHeaders(t *testing.T) {
	t.Parallel()

	signer := &GCSUploadSigner{
		newClient: func(context.Context) (*storage.Client, error) {
			return &storage.Client{}, nil
		},
		signURL: func(_ *storage.Client, bucket, object string, opts *storage.SignedURLOptions) (string, error) {
			if bucket != "gestalt-app-registry" || object != "staging/object.tgz" {
				t.Fatalf("sign bucket/object = %q/%q", bucket, object)
			}
			if len(opts.Headers) != 4 {
				t.Fatalf("signed header lines = %#v", opts.Headers)
			}
			return "https://storage.googleapis.com/gestalt-app-registry/staging/object.tgz?signed", nil
		},
	}
	digest := strings.Repeat("b", 64)
	result, err := signer.SignCreateUpload(SignCreateUploadInput{
		StorageURL:    "gs://gestalt-app-registry/staging/object.tgz",
		SHA256:        digest,
		ContentLength: 4096,
	})
	if err != nil {
		t.Fatalf("SignCreateUpload: %v", err)
	}
	if result.UploadURL == "" {
		t.Fatal("upload URL is required")
	}
	if err := validateSignedUploadHeaders(result.Headers, 4096, digest); err != nil {
		t.Fatalf("result headers: %v", err)
	}
}

func TestMemoryUploadSignerReturnsSignedHeaders(t *testing.T) {
	t.Parallel()

	store := NewMemoryObjectStore()
	signer := NewMemoryRegistryUploadSigner(store, "memory-upload://")
	digest := strings.Repeat("c", 64)
	result, err := signer.SignCreateUpload(SignCreateUploadInput{
		StorageURL:    "gs://gestalt-app-registry/staging/object.tgz",
		SHA256:        digest,
		ContentLength: 99,
	})
	if err != nil {
		t.Fatalf("SignCreateUpload: %v", err)
	}
	if err := validateSignedUploadHeaders(result.Headers, 99, digest); err != nil {
		t.Fatalf("result headers: %v", err)
	}
}

func TestSignedUploadHeadersForResponseIsCanonical(t *testing.T) {
	t.Parallel()

	raw := map[string]string{
		"x-goog-meta-sha256":         strings.Repeat("d", 64),
		"Content-Length":             "1",
		"x-goog-if-generation-match": "0",
		"x-goog-content-sha256":      "abc",
		"ignored":                    "value",
	}
	got := SignedUploadHeadersForResponse(raw)
	if _, ok := got["ignored"]; ok {
		t.Fatalf("unexpected header leaked: %#v", got)
	}
	if got[UploadHeaderContentLength] != "1" {
		t.Fatalf("headers = %#v", got)
	}
}
