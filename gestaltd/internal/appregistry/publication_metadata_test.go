package appregistry

import (
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
)

func TestEntriesEqualIgnoringPublishedAt_DefaultsLegacyPublicationKind(t *testing.T) {
	t.Parallel()

	legacy := testPublishEntry(t)
	github := legacy
	github.PublicationKind = PublicationKindGitHub
	if !EntriesEqualIgnoringPublishedAt(legacy, github) {
		t.Fatal("expected legacy empty publicationKind to match explicit github")
	}
}

func TestPublicationMetadataValidation(t *testing.T) {
	t.Parallel()

	if err := validatePublicationKind("unsupported"); err == nil {
		t.Fatal("expected invalid publication kind to fail")
	}
	if err := validateLocalSourceState(&LocalSourceState{}); err == nil {
		t.Fatal("expected empty local source state to fail")
	}
	if err := validateLocalSourceState(&LocalSourceState{Dirty: true}); err != nil {
		t.Fatalf("validateLocalSourceState(dirty) = %v", err)
	}
}

func TestBuildEntryRecordsPublicationMetadata(t *testing.T) {
	t.Parallel()

	manifest := testPublishManifest(t)
	release := testPublishReleaseMetadata()
	entry, err := BuildEntry(BuildEntryInput{
		Manifest:          manifest,
		Version:           "0.0.1",
		SourceRef:         "651a5c30feb995c9364c38f63d0d5c3880bc2055",
		ManifestPath:      "apps/traffic-cop/manifest.yaml",
		PublicationKind:   PublicationKindGitHub,
		PublishID:         "pub-123",
		BuilderVersion:    "1.2.3",
		DeclarationDigest: "sha256:abc",
		LocalSource:       &LocalSourceState{CommitSHA: "651a5c30feb995c9364c38f63d0d5c3880bc2055"},
		Release:           release,
		Artifacts: []PublishArtifact{{
			Target:     "linux/amd64",
			StorageURL: "gs://bucket/apps/traffic-cop/artifacts/0.0.1/linux-amd64.tar.gz",
			PublicURL:  "https://example.com/linux-amd64.tar.gz",
			SHA256:     "deadbeef",
		}},
		PublishStartedAt: time.Date(2026, 7, 24, 19, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BuildEntry() = %v", err)
	}
	if entry.PublicationKind != PublicationKindGitHub || entry.PublishID != "pub-123" || entry.BuilderVersion != "1.2.3" {
		t.Fatalf("entry metadata = %#v", entry)
	}
}

func TestBuildEntryRecordsSourceDirForAppInRepositorySubdirectory(t *testing.T) {
	t.Parallel()

	manifest := testPublishManifest(t)
	manifest.Source = "github.com/valon-technologies/toolshed/valon-tools/apps/traffic-cop"
	entry, err := BuildEntry(BuildEntryInput{
		Manifest:        manifest,
		Version:         "0.0.1",
		SourceRef:       "651a5c30feb995c9364c38f63d0d5c3880bc2055",
		ManifestPath:    "valon-tools/apps/traffic-cop/manifest.yaml",
		PublicationKind: PublicationKindGitHub,
		Release:         testPublishReleaseMetadata(),
		Artifacts: []PublishArtifact{{
			Target:     "linux/amd64",
			StorageURL: "gs://bucket/apps/traffic-cop/artifacts/0.0.1/linux-amd64.tar.gz",
			PublicURL:  "https://example.com/linux-amd64.tar.gz",
			SHA256:     "deadbeef",
		}},
	})
	if err != nil {
		t.Fatalf("BuildEntry() = %v", err)
	}
	if entry.Repository != "github.com/valon-technologies/toolshed" || entry.SourceDir != "valon-tools/apps/traffic-cop" {
		t.Fatalf("repository = %q, sourceDir = %q", entry.Repository, entry.SourceDir)
	}
	const wantURL = "https://github.com/valon-technologies/toolshed/tree/651a5c30feb995c9364c38f63d0d5c3880bc2055/valon-tools/apps/traffic-cop"
	if got := entry.SourceTreeURL(); got != wantURL {
		t.Fatalf("SourceTreeURL = %q, want %q", got, wantURL)
	}
	indexVersion := indexVersionFromEntry(entry, "m.json")
	if indexVersion.SourceDir != "valon-tools/apps/traffic-cop" {
		t.Fatalf("index version sourceDir = %q", indexVersion.SourceDir)
	}

	indexVersion.SourceDir = "valon-tools/apps/other"
	if err := validateIndexVersionSourceRef("traffic-cop", "0.0.1", indexVersion); err == nil {
		t.Fatalf("index validation accepted a sourceDir for a different app")
	}
	entry.SourceDir = "valon-tools/apps/other"
	if err := validateEntry(&entry); err == nil {
		t.Fatalf("validateEntry accepted a sourceDir for a different app")
	}
}

func testPublishReleaseMetadata() *providerrelease.Metadata {
	return &providerrelease.Metadata{
		StaticValidation: &providerrelease.StaticValidation{},
	}
}
