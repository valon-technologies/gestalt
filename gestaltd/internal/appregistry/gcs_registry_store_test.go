package appregistry

import "testing"

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

func TestVerifyPublishedEntryRejectsMismatch(t *testing.T) {
	t.Parallel()

	entry := &Entry{
		App:               "g-issues",
		Version:           "1.0.0",
		PublishID:         "pub_a",
		DeclarationDigest: "abc",
		SourceRef:         "deadbeef",
	}
	err := VerifyPublishedEntry(entry, PublishedCommitExpectation{
		App:               "g-issues",
		Version:           "1.0.0",
		PublishID:         "pub_b",
		DeclarationDigest: "abc",
		SourceRef:         "deadbeef",
	})
	if err == nil {
		t.Fatal("expected publishId mismatch")
	}
}
