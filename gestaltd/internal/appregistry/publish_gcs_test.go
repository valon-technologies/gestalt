package appregistry

import (
	"context"
	"fmt"
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

func TestBuildSignedUploadHeadersExactSetOmitsSourceRef(t *testing.T) {
	t.Parallel()

	headers, err := BuildSignedUploadHeaders(42, stringsRepeat("a", 64))
	if err != nil {
		t.Fatalf("BuildSignedUploadHeaders: %v", err)
	}
	want := map[string]string{
		UploadHeaderContentLength:          "42",
		UploadHeaderXGoogIfGenerationMatch: "0",
		UploadHeaderXGoogMetaSHA256:        stringsRepeat("a", 64),
		UploadHeaderXGoogContentSHA256:     stringsRepeat("a", 64),
	}
	if len(headers) != len(want) {
		t.Fatalf("headers = %#v, want exact set %#v", headers, want)
	}
	for name, value := range want {
		if headers[name] != value {
			t.Fatalf("header %q = %q, want %q", name, headers[name], value)
		}
	}
	if _, ok := headers["x-goog-meta-source-ref"]; ok {
		t.Fatal("signed upload headers must not include staging source-ref metadata")
	}
}

func TestGCSUploadSignerReturnsExactSignedHeaders(t *testing.T) {
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
	expected, err := BuildSignedUploadHeaders(1, strings.Repeat("a", 64))
	if err != nil {
		t.Fatalf("BuildSignedUploadHeaders: %v", err)
	}
	if len(result.Headers) != len(expected) {
		t.Fatalf("returned headers = %#v, want %#v", result.Headers, expected)
	}
	for name, value := range expected {
		if result.Headers[name] != value {
			t.Fatalf("header %q = %q, want %q", name, result.Headers[name], value)
		}
	}
}

func TestGCSRegistryStoreIAMPermissions(t *testing.T) {
	t.Parallel()

	want := []string{
		"storage.objects.get",
		"storage.objects.create",
		"storage.objects.delete",
	}
	if len(GCSRegistryStoreIAMPermissions) != len(want) {
		t.Fatalf("permissions = %#v, want %#v", GCSRegistryStoreIAMPermissions, want)
	}
	seen := make(map[string]struct{}, len(GCSRegistryStoreIAMPermissions))
	for _, permission := range GCSRegistryStoreIAMPermissions {
		seen[permission] = struct{}{}
	}
	for _, permission := range want {
		if _, ok := seen[permission]; !ok {
			t.Fatalf("missing permission %q in %#v", permission, GCSRegistryStoreIAMPermissions)
		}
	}
	if _, ok := seen["storage.objects.update"]; ok {
		t.Fatal("storage.objects.update is not required for NewWriter catalog rewrites")
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

	signer := testBoundUploadSigner(t, mustNotSignURL(t))
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

	signer := testBoundUploadSigner(t, mustNotSignURL(t))
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

	var signedBucket string
	signer := testBoundUploadSigner(t, func(_ *storage.Client, bucket, _ string, _ *storage.SignedURLOptions) (string, error) {
		signedBucket = bucket
		return "https://signed.example/upload", nil
	})
	if err := signer.CheckSigningReadiness(context.Background()); err != nil {
		t.Fatalf("CheckSigningReadiness: %v", err)
	}
	if signedBucket != "gestalt-app-registry" {
		t.Fatalf("signed bucket = %q, want gestalt-app-registry", signedBucket)
	}
}

func testBoundUploadSigner(t *testing.T, signURL ...func(*storage.Client, string, string, *storage.SignedURLOptions) (string, error)) *GCSUploadSigner {
	t.Helper()
	store, err := NewGCSRegistryStore("gestaltd-publish", "gs://gestalt-app-registry")
	if err != nil {
		t.Fatalf("NewGCSRegistryStore: %v", err)
	}
	signer, err := NewGCSUploadSigner(store)
	if err != nil {
		t.Fatalf("NewGCSUploadSigner: %v", err)
	}
	signer.newClient = func(context.Context) (*storage.Client, error) {
		return nil, nil
	}
	switch len(signURL) {
	case 0:
		signer.signURL = func(_ *storage.Client, _, _ string, _ *storage.SignedURLOptions) (string, error) {
			return "https://signed.example/upload", nil
		}
	default:
		signer.signURL = signURL[0]
	}
	return signer
}

func mustNotSignURL(t *testing.T) func(*storage.Client, string, string, *storage.SignedURLOptions) (string, error) {
	t.Helper()
	return func(_ *storage.Client, _, _ string, _ *storage.SignedURLOptions) (string, error) {
		t.Fatal("signURL must not be called for out-of-bound storage URLs")
		return "", fmt.Errorf("signURL must not be called")
	}
}

func stringsRepeat(s string, count int) string {
	out := make([]byte, count)
	for i := range out {
		out[i] = s[0]
	}
	return string(out)
}
