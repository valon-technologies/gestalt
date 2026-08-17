package appregistry

import "testing"

func TestBuildSignedUploadHeaders(t *testing.T) {
	t.Parallel()

	headers, err := BuildSignedUploadHeaders(42, stringsRepeat("a", 64))
	if err != nil {
		t.Fatalf("BuildSignedUploadHeaders: %v", err)
	}
	if err := validateSignedUploadHeaders(headers, 42, stringsRepeat("a", 64)); err != nil {
		t.Fatalf("validateSignedUploadHeaders: %v", err)
	}
	if headers[UploadHeaderXGoogIfGenerationMatch] != "0" {
		t.Fatalf("generation header = %q", headers[UploadHeaderXGoogIfGenerationMatch])
	}
}

func TestParseGCSStorageURL(t *testing.T) {
	t.Parallel()

	bucket, object, err := parseGCSStorageURL("gs://gestalt-app-registry/apps/g-issues/index.json")
	if err != nil {
		t.Fatalf("parseGCSStorageURL: %v", err)
	}
	if bucket != "gestalt-app-registry" || object != "apps/g-issues/index.json" {
		t.Fatalf("bucket/object = %q/%q", bucket, object)
	}
}

func stringsRepeat(s string, count int) string {
	out := make([]byte, count)
	for i := range out {
		out[i] = s[0]
	}
	return string(out)
}
