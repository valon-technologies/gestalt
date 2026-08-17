package appregistry

import "testing"

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

func stringsRepeat(s string, count int) string {
	out := make([]byte, count)
	for i := range out {
		out[i] = s[0]
	}
	return string(out)
}
