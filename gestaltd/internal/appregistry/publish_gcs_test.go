package appregistry

import (
	"context"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/storage"
)

func TestBuildSignedUploadHeaders(t *testing.T) {
	t.Parallel()

	headers, err := BuildSignedUploadHeaders(42, stringsRepeat("a", 64))
	if err != nil {
		t.Fatalf("BuildSignedUploadHeaders: %v", err)
	}
	if headers[UploadHeaderXGoogIfGenerationMatch] != "0" {
		t.Fatalf("generation header = %q", headers[UploadHeaderXGoogIfGenerationMatch])
	}
	if headers[UploadHeaderContentLength] != "42" {
		t.Fatalf("content length = %q", headers[UploadHeaderContentLength])
	}
	if headers[UploadHeaderXGoogMetaSHA256] != stringsRepeat("a", 64) {
		t.Fatalf("sha256 header = %q", headers[UploadHeaderXGoogMetaSHA256])
	}
}

func TestGCSBucketObjectFromURL(t *testing.T) {
	t.Parallel()

	bucket, object, err := gcsBucketObjectFromURL("gs://gestalt-app-registry/apps/g-issues/index.json")
	if err != nil {
		t.Fatalf("gcsBucketObjectFromURL: %v", err)
	}
	if bucket != "gestalt-app-registry" || object != "apps/g-issues/index.json" {
		t.Fatalf("bucket/object = %q/%q", bucket, object)
	}
}

func TestGCSRegistryStoreRejectsForeignBucket(t *testing.T) {
	t.Parallel()

	store, err := NewGCSRegistryStore("gestaltd-publish", "gs://gestalt-app-registry")
	if err != nil {
		t.Fatalf("NewGCSRegistryStore: %v", err)
	}
	_, _, err = store.validateStorageURL("gs://other-bucket/apps/g-issues/index.json")
	if err == nil {
		t.Fatal("expected foreign bucket rejection")
	}
}

func TestGCSUploadSignerRejectsCrossBucketURL(t *testing.T) {
	t.Parallel()

	signer := testBoundUploadSigner(t)
	_, err := signer.SignCreateUpload(SignCreateUploadInput{
		StorageURL:    "gs://other-bucket/apps/g-issues/publish-staging/0.1.0/digest/artifacts/linux/amd64/file.tgz",
		SHA256:        strings.Repeat("a", 64),
		ContentLength: 1,
		ExpiresAt:     time.Now().UTC().Add(time.Hour),
	})
	if err == nil || !strings.Contains(err.Error(), "outside bound registry bucket") {
		t.Fatalf("SignCreateUpload error = %v, want bound bucket rejection", err)
	}
}

func TestGCSUploadSignerRejectsSiblingBucketPrefix(t *testing.T) {
	t.Parallel()

	signer := testBoundUploadSigner(t)
	_, err := signer.SignCreateUpload(SignCreateUploadInput{
		StorageURL:    "gs://gestalt-app-registry-staging/apps/g-issues/publish-staging/0.1.0/digest/artifacts/linux/amd64/file.tgz",
		SHA256:        strings.Repeat("a", 64),
		ContentLength: 1,
		ExpiresAt:     time.Now().UTC().Add(time.Hour),
	})
	if err == nil || !strings.Contains(err.Error(), "outside bound registry bucket") {
		t.Fatalf("SignCreateUpload error = %v, want sibling bucket rejection", err)
	}
}

func TestGCSUploadSignerAcceptsBoundBucketURL(t *testing.T) {
	t.Parallel()

	signer := testBoundUploadSigner(t)
	result, err := signer.SignCreateUpload(SignCreateUploadInput{
		StorageURL:    "gs://gestalt-app-registry/apps/g-issues/publish-staging/0.1.0/digest/artifacts/linux/amd64/file.tgz",
		SHA256:        strings.Repeat("a", 64),
		ContentLength: 1,
		ExpiresAt:     time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("SignCreateUpload: %v", err)
	}
	if result.UploadURL != "https://signed.example/upload" {
		t.Fatalf("upload URL = %q", result.UploadURL)
	}
}

func TestGCSUploadSignerCheckSigningReadinessUsesBoundBucket(t *testing.T) {
	t.Parallel()

	signer := testBoundUploadSigner(t)
	if err := signer.CheckSigningReadiness(context.Background()); err != nil {
		t.Fatalf("CheckSigningReadiness: %v", err)
	}
}

func testBoundUploadSigner(t *testing.T) *GCSUploadSigner {
	t.Helper()
	store, err := NewGCSRegistryStore("gestaltd-publish", "gs://gestalt-app-registry")
	if err != nil {
		t.Fatalf("NewGCSRegistryStore: %v", err)
	}
	signer, err := NewGCSUploadSigner(store)
	if err != nil {
		t.Fatalf("NewGCSUploadSigner: %v", err)
	}
	signer.signURL = func(_ *storage.Client, _, _ string, _ *storage.SignedURLOptions) (string, error) {
		return "https://signed.example/upload", nil
	}
	return signer
}

func stringsRepeat(s string, count int) string {
	out := make([]byte, count)
	for i := range out {
		out[i] = s[0]
	}
	return string(out)
}
