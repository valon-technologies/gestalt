package daemon

import (
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestPrepareAppPublishReleaseRejectsMismatchedApp(t *testing.T) { //nolint:paralleltest // chdirs
	_, distDir, _, base := setupRemotePublishFixture(t)
	_, err := prepareAppPublishRelease(prepareAppPublishReleaseInput{
		AppName:         "other-app",
		VersionGuard:    "0.3.0-dev.1",
		DistDirs:        []string{distDir},
		CollectArchives: base.collectArchives,
		ResolveManifest: base.resolveManifest,
	})
	if err == nil {
		t.Fatal("expected app name mismatch error")
	}
}

func TestPrepareAppPublishReleaseDerivesAppFromArchives(t *testing.T) { //nolint:paralleltest // chdirs
	_, distDir, _, base := setupRemotePublishFixture(t)
	prepared, err := prepareAppPublishRelease(prepareAppPublishReleaseInput{
		VersionGuard:         "0.3.0-dev.1",
		DistDirs:             []string{distDir},
		CollectArchives:      base.collectArchives,
		ResolveManifest:      base.resolveManifest,
		BuildReleaseMetadata: base.buildReleaseMetadata,
	})
	if err != nil {
		t.Fatalf("prepareAppPublishRelease() = %v", err)
	}
	if prepared.AppName != "demo" || prepared.ReleaseMetadata == nil || prepared.SourceManifest == nil {
		t.Fatalf("prepared = %#v", prepared)
	}
	if prepared.SourceManifest.Version != "0.3.0-dev.1" {
		t.Fatalf("source manifest version = %q", prepared.SourceManifest.Version)
	}
	if prepared.ReleaseManifest.Kind != providermanifestv1.KindApp {
		t.Fatalf("release manifest = %#v", prepared.ReleaseManifest)
	}
}
