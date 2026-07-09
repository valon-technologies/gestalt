package appregistry

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestBuildEntryIncludesOperationsAndArtifacts(t *testing.T) {
	t.Parallel()

	manifest := &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      "github.com/valon-technologies/valon-tools/apps/g-issues",
		Version:     "0.0.0-snapshot.gabc123",
		DisplayName: "g-issues",
	}
	release := &providerrelease.Metadata{
		Schema:        providerrelease.SchemaName,
		SchemaVersion: providerrelease.SchemaVersion,
		Package:       manifest.Source,
		Kind:          manifest.Kind,
		Version:       "0.0.0-snapshot.gabc123",
		Runtime:       providerrelease.RuntimeUI,
		Artifacts: providerrelease.Artifacts{
			"linux/amd64": {Path: "gestalt-app-g-issues_v0.0.0-snapshot.gabc123_linux_amd64.tar.gz", SHA256: "abc"},
		},
		StaticValidation: &providerrelease.StaticValidation{
			Manifest: manifest,
			Catalog: &catalog.Catalog{
				Name: "g-issues",
				Operations: []catalog.CatalogOperation{{
					ID:          "issues.list",
					Method:      "GET",
					InputSchema: json.RawMessage(`{"type":"object"}`),
				}},
			},
		},
	}
	entry, err := BuildEntry(BuildEntryInput{
		Manifest:     manifest,
		Version:      "0.0.0-snapshot.gabc123",
		SourceRef:    "abc123def456abc123def456abc123def456abcd",
		ManifestPath: "valon-tools/apps/g-issues/manifest.yaml",
		Release:      release,
		Artifacts: []PublishArtifact{{
			Target:     "linux/amd64",
			StorageURL: "gs://bucket/apps/g-issues/artifacts/0.0.0-snapshot.gabc123/gestalt-app-g-issues_v0.0.0-snapshot.gabc123_linux_amd64.tar.gz",
			PublicURL:  "https://storage.example.test/apps/g-issues/artifacts/0.0.0-snapshot.gabc123/gestalt-app-g-issues_v0.0.0-snapshot.gabc123_linux_amd64.tar.gz",
			SHA256:     "abc",
		}},
		PublishedAt: time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BuildEntry: %v", err)
	}
	if entry.App != "g-issues" {
		t.Fatalf("entry.App = %q", entry.App)
	}
	if entry.Repository != "github.com/valon-technologies/valon-tools" {
		t.Fatalf("entry.Repository = %q", entry.Repository)
	}
	if _, ok := entry.Interface.Operations["issues.list"]; !ok {
		t.Fatalf("entry missing issues.list operation")
	}
	if entry.Artifacts["linux/amd64"].SHA256 != "abc" {
		t.Fatalf("artifact sha256 = %q", entry.Artifacts["linux/amd64"].SHA256)
	}
}

func TestValidatePublishInputRejectsNonAppKind(t *testing.T) {
	t.Parallel()

	err := ValidatePublishInput(&providermanifestv1.Manifest{
		Kind:   providermanifestv1.KindIndexedDB,
		Source: "github.com/valon-technologies/gestalt-providers/indexeddb/relationaldb",
	}, "0.0.1", "abc123def456abc123def456abc123def456abcd")
	if err == nil {
		t.Fatal("expected error for non-app kind")
	}
}

func TestUpsertAppIndexAddsVersion(t *testing.T) {
	t.Parallel()

	index := NewEmptyIndex()
	entry := Entry{
		SchemaVersion: EntrySchemaVersion,
		App:           "g-issues",
		Version:       "0.0.1",
		SourceRef:     "abc123def456abc123def456abc123def456abcd",
		ManifestPath:  "valon-tools/apps/g-issues/manifest.yaml",
		Repository:   "github.com/valon-technologies/valon-tools",
		Artifacts: map[string]Artifact{
			"linux/amd64": {URL: "gs://x", PublicURL: "https://x", SHA256: "abc"},
		},
		PublishedAt: time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
	}
	updated, changed, err := UpsertAppIndex(index, entry, "apps/g-issues/versions/0.0.1.json", "g-issues", "Issues workspace")
	if err != nil {
		t.Fatalf("UpsertAppIndex: %v", err)
	}
	if !changed {
		t.Fatal("expected index change on first upsert")
	}
	if updated.Apps["g-issues"].DisplayName != "g-issues" {
		t.Fatalf("displayName = %q", updated.Apps["g-issues"].DisplayName)
	}
	if updated.Apps["g-issues"].Versions["0.0.1"].Metadata == "" {
		t.Fatal("expected metadata path")
	}
	if got := updated.Apps["g-issues"].Versions["0.0.1"].Platforms; len(got) != 1 || got[0] != "linux/amd64" {
		t.Fatalf("platforms = %#v", got)
	}
}

func TestUpsertAppIndexIsIdempotentForSameVersion(t *testing.T) {
	t.Parallel()

	index := NewEmptyIndex()
	entry := Entry{
		SchemaVersion: EntrySchemaVersion,
		App:           "g-issues",
		Version:       "0.0.1",
		SourceRef:     "abc123def456abc123def456abc123def456abcd",
		ManifestPath:  "valon-tools/apps/g-issues/manifest.yaml",
		Repository:   "github.com/valon-technologies/valon-tools",
		Artifacts: map[string]Artifact{
			"linux/amd64": {URL: "gs://x", PublicURL: "https://x", SHA256: "abc"},
		},
		PublishedAt: time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
	}
	metadataPath := "apps/g-issues/versions/0.0.1.json"
	updated, changed, err := UpsertAppIndex(index, entry, metadataPath, "g-issues", "Issues workspace")
	if err != nil {
		t.Fatalf("UpsertAppIndex: %v", err)
	}
	if !changed {
		t.Fatal("expected index change on first upsert")
	}
	again, changed, err := UpsertAppIndex(updated, entry, metadataPath, "g-issues", "Issues workspace")
	if err != nil {
		t.Fatalf("UpsertAppIndex retry: %v", err)
	}
	if changed {
		t.Fatal("expected unchanged index on identical republish")
	}
	if again.Apps["g-issues"].Versions["0.0.1"].Metadata != metadataPath {
		t.Fatalf("metadata path = %q", again.Apps["g-issues"].Versions["0.0.1"].Metadata)
	}
}

func TestUpsertAppIndexUpdatesDisplayNameOnRepublish(t *testing.T) {
	t.Parallel()

	index := NewEmptyIndex()
	entry := Entry{
		SchemaVersion: EntrySchemaVersion,
		App:           "g-issues",
		Version:       "0.0.1",
		SourceRef:     "abc123def456abc123def456abc123def456abcd",
		ManifestPath:  "valon-tools/apps/g-issues/manifest.yaml",
		Repository:   "github.com/valon-technologies/valon-tools",
		Artifacts: map[string]Artifact{
			"linux/amd64": {URL: "gs://x", PublicURL: "https://x", SHA256: "abc"},
		},
		PublishedAt: time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
	}
	metadataPath := "apps/g-issues/versions/0.0.1.json"
	updated, _, err := UpsertAppIndex(index, entry, metadataPath, "g-issues", "Old description")
	if err != nil {
		t.Fatalf("UpsertAppIndex: %v", err)
	}
	again, changed, err := UpsertAppIndex(updated, entry, metadataPath, "g-issues", "Issues workspace")
	if err != nil {
		t.Fatalf("UpsertAppIndex retry: %v", err)
	}
	if !changed {
		t.Fatal("expected index change when description is updated")
	}
	if again.Apps["g-issues"].Description != "Issues workspace" {
		t.Fatalf("description = %q", again.Apps["g-issues"].Description)
	}
}

func TestValidateEntryRejectsInvalidSourceRef(t *testing.T) {
	t.Parallel()

	entry := Entry{
		SchemaVersion: EntrySchemaVersion,
		App:           "g-issues",
		Version:       "0.0.1",
		SourceRef:     "ABC123DEF456ABC123DEF456ABC123DEF456ABCD",
		ManifestPath:  "valon-tools/apps/g-issues/manifest.yaml",
		Repository:   "github.com/valon-technologies/valon-tools",
		Artifacts: map[string]Artifact{
			"linux/amd64": {URL: "gs://x", PublicURL: "https://x", SHA256: "abc"},
		},
		PublishedAt: time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
	}
	if err := validateEntry(&entry); err == nil {
		t.Fatal("expected uppercase sourceRef error")
	}
}

func TestValidateEntryRejectsInvalidArtifactPlatform(t *testing.T) {
	t.Parallel()

	entry := Entry{
		SchemaVersion: EntrySchemaVersion,
		App:           "g-issues",
		Version:       "0.0.1",
		SourceRef:     "abc123def456abc123def456abc123def456abcd",
		ManifestPath:  "valon-tools/apps/g-issues/manifest.yaml",
		Repository:   "github.com/valon-technologies/valon-tools",
		Artifacts: map[string]Artifact{
			"not-a-platform": {URL: "gs://x", PublicURL: "https://x", SHA256: "abc"},
		},
		PublishedAt: time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
	}
	if err := validateEntry(&entry); err == nil {
		t.Fatal("expected invalid artifact platform error")
	}
}

func TestResolvePublishLayout(t *testing.T) {
	t.Parallel()

	layout, err := ResolvePublishLayout("github.com/valon-technologies/valon-tools/apps/g-issues", "0.0.1")
	if err != nil {
		t.Fatalf("ResolvePublishLayout: %v", err)
	}
	if layout.AppName != "g-issues" {
		t.Fatalf("appName = %q", layout.AppName)
	}
	if layout.ArtifactPrefix != "apps/g-issues/artifacts/0.0.1" {
		t.Fatalf("artifactPrefix = %q", layout.ArtifactPrefix)
	}
	if layout.EntryPath != "apps/g-issues/versions/0.0.1.json" {
		t.Fatalf("entryPath = %q", layout.EntryPath)
	}
	if layout.IndexPath != "apps/g-issues/index.json" {
		t.Fatalf("indexPath = %q", layout.IndexPath)
	}
	if got := AppSourceAddress("github.com/valon-technologies/valon-tools", layout.AppName); got != "github.com/valon-technologies/valon-tools/apps/g-issues" {
		t.Fatalf("AppSourceAddress = %q", got)
	}
}

func TestValidatePublishInputRejectsNonHexSourceRef(t *testing.T) {
	t.Parallel()

	err := ValidatePublishInput(&providermanifestv1.Manifest{
		Kind:   providermanifestv1.KindApp,
		Source: "github.com/valon-technologies/valon-tools/apps/g-issues",
	}, "0.0.1", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	if err == nil {
		t.Fatal("expected error for non-hex sourceRef")
	}
}

func TestValidatePublishInputRejectsNonAppsSourcePath(t *testing.T) {
	t.Parallel()

	err := ValidatePublishInput(&providermanifestv1.Manifest{
		Kind:   providermanifestv1.KindApp,
		Source: "github.com/valon-technologies/valon-tools/tools/g-issues",
	}, "0.0.1", "abc123def456abc123def456abc123def456abcd")
	if err == nil {
		t.Fatal("expected error for non-apps source path")
	}
}

func TestRequiresFromRelease(t *testing.T) {
	t.Parallel()

	release := &providerrelease.Metadata{
		StaticValidation: &providerrelease.StaticValidation{
			Requires: &providerrelease.Requires{
				Apps: map[string]providerrelease.AppRequirement{
					"slack": {Version: "^1.4.0"},
				},
			},
			Compatibility: &providerrelease.Compatibility{MinGestaltdVersion: "0.20.0"},
		},
	}
	requires := RequiresFromRelease(release)
	if requires.Apps["slack"].Version != "^1.4.0" {
		t.Fatalf("requires = %#v", requires)
	}
	compatibility := CompatibilityFromRelease(release)
	if compatibility.MinGestaltdVersion != "0.20.0" {
		t.Fatalf("compatibility = %#v", compatibility)
	}
}

func TestEntriesEqualIgnoringPublishedAt(t *testing.T) {
	t.Parallel()

	base := Entry{
		SchemaVersion: EntrySchemaVersion,
		App:           "g-issues",
		Version:       "0.0.1",
		SourceRef:     "abc123def456abc123def456abc123def456abcd",
		ManifestPath:  "apps/g-issues/manifest.yaml",
		Repository:    "github.com/valon-technologies/valon-tools",
		Artifacts: map[string]Artifact{
			"linux/amd64": {URL: "gs://x", PublicURL: "https://x", SHA256: "abc"},
		},
	}
	first := base
	first.PublishedAt = time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	second := base
	second.PublishedAt = time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	if !EntriesEqualIgnoringPublishedAt(first, second) {
		t.Fatal("expected entries to match when only publishedAt differs")
	}
	second.Version = "0.0.2"
	if EntriesEqualIgnoringPublishedAt(first, second) {
		t.Fatal("expected version mismatch to fail equivalence check")
	}
}

func TestBuildEntryAcceptsDarwin(t *testing.T) {
	t.Parallel()

	manifest := &providermanifestv1.Manifest{
		Kind:   providermanifestv1.KindApp,
		Source: "github.com/valon-technologies/valon-tools/apps/g-issues",
	}
	release := &providerrelease.Metadata{
		SchemaVersion: providerrelease.SchemaVersion,
		Package:       manifest.Source,
		Kind:          manifest.Kind,
		Version:       "0.0.1",
		Runtime:       providerrelease.RuntimeUI,
	}
	_, err := BuildEntry(BuildEntryInput{
		Manifest:     manifest,
		Version:      "0.0.1",
		SourceRef:    "abc123def456abc123def456abc123def456abcd",
		ManifestPath: "valon-tools/apps/g-issues/manifest.yaml",
		Release:      release,
		Artifacts: []PublishArtifact{{
			Target:     "darwin/arm64",
			StorageURL: "gs://bucket/apps/g-issues/artifacts/0.0.1/gestalt-app-g-issues_v0.0.1_darwin_arm64.tar.gz",
			PublicURL:  "https://storage.example.test/apps/g-issues/artifacts/0.0.1/gestalt-app-g-issues_v0.0.1_darwin_arm64.tar.gz",
			SHA256:     "abc",
		}},
	})
	if err != nil {
		t.Fatalf("BuildEntry: %v", err)
	}
}
