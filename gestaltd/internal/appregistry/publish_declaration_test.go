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

func TestDeclarationDigestIgnoresWhitespaceAndArtifactOrder(t *testing.T) {
	t.Parallel()

	base, _ := testPublishDeclaration(t, "g-issues", "0.3.0-dev.7")
	permute := *base
	permute.Artifacts = append([]appregistry.PublishDeclarationArtifact(nil), base.Artifacts...)
	permute.Artifacts[0].Platform = "  linux/amd64  "
	permute.Artifacts[0].Filename = " linux-amd64.tar.gz "
	permute.Artifacts[0].SHA256 = "  " + permute.Artifacts[0].SHA256 + "  "
	permute.SourceRef = "  "
	permute.ManifestPath = "  " + permute.ManifestPath + "  "

	first, err := appregistry.DeclarationDigest(base)
	if err != nil {
		t.Fatalf("DeclarationDigest base: %v", err)
	}
	second, err := appregistry.DeclarationDigest(&permute)
	if err != nil {
		t.Fatalf("DeclarationDigest permute: %v", err)
	}
	if first != second {
		t.Fatalf("digests differ: %q vs %q", first, second)
	}
}

func TestValidatePublishDeclarationRejectsUnsafeFilename(t *testing.T) {
	t.Parallel()

	decl, _ := testPublishDeclaration(t, "g-issues", "0.3.0-dev.8")
	decl.Artifacts[0].Filename = "../escape.tar.gz"
	err := appregistry.ValidatePublishDeclaration("g-issues", decl, appregistry.PublishLimits{RequiredPlatforms: []string{"linux/amd64"}})
	if err == nil {
		t.Fatal("expected unsafe filename rejection")
	}
}

func TestValidatePublishDeclarationRequiresPositiveSize(t *testing.T) {
	t.Parallel()

	decl, _ := testPublishDeclaration(t, "g-issues", "0.3.0-dev.9")
	decl.Artifacts[0].Size = 0
	err := appregistry.ValidatePublishDeclaration("g-issues", decl, appregistry.PublishLimits{RequiredPlatforms: []string{"linux/amd64"}})
	if err == nil {
		t.Fatal("expected zero size rejection")
	}
}
