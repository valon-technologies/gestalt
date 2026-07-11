package appregistry

import (
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
)

func TestApplyPendingInstallPreservesPromotedMetadata(t *testing.T) {
	t.Parallel()

	activeSince := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	installedAt := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	baseline := &core.AppInstallation{
		AppName:            "g-issues",
		VersionConstraint:  "0.0.0-snapshot.gold",
		ResolvedVersion:    "0.0.0-snapshot.gold",
		SourceRef:          "abc123def456abc123def456abc123def456abcd",
		ProviderReleaseURL: "https://example.com/gold.json",
		ArtifactChecksums:  map[string]string{"linux/amd64": "gold"},
		RolloutStatus:      core.AppInstallationRolloutStatusPromoted,
		ActiveSince:        &activeSince,
		InstalledAt:        installedAt,
	}
	dst := cloneInstallation(baseline)
	pending := &core.AppInstallation{
		VersionConstraint:       "0.0.0-snapshot.new",
		ResolvedVersion:         "0.0.0-snapshot.new",
		SourceRef:               "def456abc123def456abc123def456abc123abcd",
		ProviderReleaseURL:      "https://example.com/new.json",
		ArtifactChecksums:       map[string]string{"linux/amd64": "new"},
		RolloutStatus:           core.AppInstallationRolloutStatusPending,
		PreviousResolvedVersion: "0.0.0-snapshot.gold",
	}

	applyPendingInstall(dst, pending, baseline)

	if dst.RolloutStatus != core.AppInstallationRolloutStatusPending {
		t.Fatalf("rollout_status = %q", dst.RolloutStatus)
	}
	if dst.ResolvedVersion != "0.0.0-snapshot.new" {
		t.Fatalf("resolved_version = %q", dst.ResolvedVersion)
	}
	if dst.SourceRef != baseline.SourceRef {
		t.Fatalf("source_ref = %q, want gold metadata preserved during pending", dst.SourceRef)
	}
	if dst.ActiveSince == nil || !dst.ActiveSince.Equal(activeSince) {
		t.Fatalf("active_since = %v, want preserved during pending", dst.ActiveSince)
	}
	if !dst.InstalledAt.Equal(installedAt) {
		t.Fatalf("installed_at = %v, want preserved", dst.InstalledAt)
	}
}

func cloneInstallation(installation *core.AppInstallation) *core.AppInstallation {
	if installation == nil {
		return nil
	}
	cloned := *installation
	if installation.ActiveSince != nil {
		activeSince := *installation.ActiveSince
		cloned.ActiveSince = &activeSince
	}
	if len(installation.ArtifactChecksums) > 0 {
		cloned.ArtifactChecksums = make(map[string]string, len(installation.ArtifactChecksums))
		for platform, digest := range installation.ArtifactChecksums {
			cloned.ArtifactChecksums[platform] = digest
		}
	}
	return &cloned
}
