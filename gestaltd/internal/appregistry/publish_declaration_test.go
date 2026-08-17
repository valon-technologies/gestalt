package appregistry_test

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

func TestDerivePublishIDIsDeterministic(t *testing.T) {
	t.Parallel()

	digest := "abc123"
	first := appregistry.DerivePublishID("g-issues", "0.3.0-dev.1", digest)
	second := appregistry.DerivePublishID("g-issues", "0.3.0-dev.1", digest)
	if first != second || first == "" {
		t.Fatalf("publish ids = %q vs %q", first, second)
	}
	if other := appregistry.DerivePublishID("g-issues", "0.3.0-dev.2", digest); other == first {
		t.Fatalf("different versions should produce different publish ids")
	}
}

func TestPublishStagingPrefixIncludesVersionAndDigest(t *testing.T) {
	t.Parallel()

	prefix := appregistry.PublishStagingPrefix("g-issues", "0.3.0-dev.1", "digest")
	want := "apps/g-issues/publish-staging/0.3.0-dev.1/digest"
	if prefix != want {
		t.Fatalf("prefix = %q, want %q", prefix, want)
	}
}
