package appregistry_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
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

	prefix, err := appregistry.PublishStagingPrefix("g-issues", "0.3.0-dev.1", "digest")
	if err != nil {
		t.Fatalf("PublishStagingPrefix: %v", err)
	}
	want := "apps/g-issues/publish-staging/0.3.0-dev.1/digest"
	if prefix != want {
		t.Fatalf("prefix = %q, want %q", prefix, want)
	}
}

func TestPublishStagingPrefixRejectsInvalidDigestSegment(t *testing.T) {
	t.Parallel()

	if _, err := appregistry.PublishStagingPrefix("g-issues", "0.3.0-dev.1", "../escape"); err == nil {
		t.Fatal("expected invalid digest segment rejection")
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

func TestDeclarationDigestStableAcrossAcceptedVariants(t *testing.T) {
	t.Parallel()

	base, _ := testPublishDeclaration(t, "g-issues", "0.3.0-dev.7b")
	base.BuilderVersion = "v1.2.3"
	baseDigest, err := appregistry.DeclarationDigest(base)
	if err != nil {
		t.Fatalf("DeclarationDigest base: %v", err)
	}

	cases := []struct {
		name string
		mut  func(*appregistry.PublishDeclaration)
	}{
		{
			name: "schema whitespace",
			mut: func(d *appregistry.PublishDeclaration) {
				d.Schema = "  " + d.Schema + "  "
			},
		},
		{
			name: "manifest kind casing",
			mut: func(d *appregistry.PublishDeclaration) {
				d.Manifest.Kind = " APP "
			},
		},
		{
			name: "manifest source whitespace",
			mut: func(d *appregistry.PublishDeclaration) {
				d.Manifest.Source = "  " + d.Manifest.Source + "  "
			},
		},
		{
			name: "manifest version whitespace",
			mut: func(d *appregistry.PublishDeclaration) {
				d.Manifest.Version = "  " + d.Manifest.Version + "  "
			},
		},
		{
			name: "release package whitespace",
			mut: func(d *appregistry.PublishDeclaration) {
				d.ReleaseMetadata.Package = "  " + d.ReleaseMetadata.Package + "  "
			},
		},
		{
			name: "release kind casing",
			mut: func(d *appregistry.PublishDeclaration) {
				d.ReleaseMetadata.Kind = " APP "
			},
		},
		{
			name: "builder version whitespace",
			mut: func(d *appregistry.PublishDeclaration) {
				d.BuilderVersion = "  v1.2.3  "
			},
		},
		{
			name: "local source commit casing",
			mut: func(d *appregistry.PublishDeclaration) {
				d.LocalSource.CommitSHA = strings.ToUpper(d.LocalSource.CommitSHA)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clone := clonePublishDeclaration(t, base)
			tc.mut(clone)
			got, err := appregistry.DeclarationDigest(clone)
			if err != nil {
				t.Fatalf("DeclarationDigest: %v", err)
			}
			if got != baseDigest {
				t.Fatalf("digests differ: %q vs %q", got, baseDigest)
			}
		})
	}
}

func TestDeclarationDigestStableAcrossArtifactOrder(t *testing.T) {
	t.Parallel()

	base, _ := testPublishDeclarationMultiPlatform(t, "g-issues", "0.3.0-dev.7c")
	baseDigest, err := appregistry.DeclarationDigest(base)
	if err != nil {
		t.Fatalf("DeclarationDigest base: %v", err)
	}
	permute := clonePublishDeclaration(t, base)
	permute.Artifacts[0], permute.Artifacts[1] = permute.Artifacts[1], permute.Artifacts[0]
	got, err := appregistry.DeclarationDigest(permute)
	if err != nil {
		t.Fatalf("DeclarationDigest permute: %v", err)
	}
	if got != baseDigest {
		t.Fatalf("digests differ: %q vs %q", got, baseDigest)
	}
}

func TestNormalizeAndValidatePublishDeclarationIsPure(t *testing.T) {
	t.Parallel()

	original, _ := testPublishDeclaration(t, "g-issues", "0.3.0-dev.10")
	before, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal before: %v", err)
	}

	canonical, err := appregistry.NormalizeAndValidatePublishDeclaration("g-issues", original, appregistry.PublishLimits{RequiredPlatforms: []string{"linux/amd64"}})
	if err != nil {
		t.Fatalf("NormalizeAndValidatePublishDeclaration: %v", err)
	}
	if canonical == nil {
		t.Fatal("expected canonical declaration")
	}

	after, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("caller declaration mutated")
	}
}

func TestValidatePublishDeclarationRejectsExtraReleaseArtifact(t *testing.T) {
	t.Parallel()

	decl, _ := testPublishDeclaration(t, "g-issues", "0.3.0-dev.11")
	decl.ReleaseMetadata.Artifacts["darwin/arm64"] = providerrelease.Artifact{
		Path: "darwin-arm64.tar.gz", SHA256: strings.Repeat("a", 64),
	}
	err := appregistry.ValidatePublishDeclaration("g-issues", decl, appregistry.PublishLimits{RequiredPlatforms: []string{"linux/amd64"}})
	if err == nil || !strings.Contains(err.Error(), "declaration has no artifact for releaseMetadata platform") {
		t.Fatalf("ValidatePublishDeclaration error = %v", err)
	}
}

func TestValidatePublishDeclarationRejectsKindMismatch(t *testing.T) {
	t.Parallel()

	decl, _ := testPublishDeclaration(t, "g-issues", "0.3.0-dev.12")
	decl.ReleaseMetadata.Kind = providermanifestv1.KindIdentity
	err := appregistry.ValidatePublishDeclaration("g-issues", decl, appregistry.PublishLimits{RequiredPlatforms: []string{"linux/amd64"}})
	if err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("ValidatePublishDeclaration error = %v", err)
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

func clonePublishDeclaration(t *testing.T, declaration *appregistry.PublishDeclaration) *appregistry.PublishDeclaration {
	t.Helper()
	data, err := json.Marshal(declaration)
	if err != nil {
		t.Fatalf("marshal declaration: %v", err)
	}
	var clone appregistry.PublishDeclaration
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatalf("unmarshal declaration: %v", err)
	}
	return &clone
}
