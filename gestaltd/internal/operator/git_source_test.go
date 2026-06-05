package operator

import (
	"strings"
	"testing"
)

func TestSnapshotSourceFileURLAddsSourceRefQuery(t *testing.T) {
	t.Parallel()

	snapshotPath, err := NewSnapshotSourceRefPath(
		"https://github.com/valon-technologies/gestalt-providers.git",
		"CBF01DE477C362F96AACB814F7246F07C7ADB3A0",
		"app/vercel/manifest.yaml",
	)
	if err != nil {
		t.Fatalf("NewSnapshotSourceRefPath: %v", err)
	}
	got, err := snapshotSourceFileURL("https://storage.googleapis.com/snapshots", snapshotPath, "provider-release.yaml")
	if err != nil {
		t.Fatalf("snapshotSourceFileURL: %v", err)
	}
	if !strings.HasPrefix(got, "https://storage.googleapis.com/snapshots/github.com/valon-technologies/gestalt-providers/cbf01de477c362f96aacb814f7246f07c7adb3a0/app/vercel/provider-release.yaml?") {
		t.Fatalf("snapshot URL = %q", got)
	}
	if !strings.Contains(got, "sourceRef=cbf01de477c362f96aacb814f7246f07c7adb3a0") {
		t.Fatalf("snapshot URL = %q, want sourceRef query", got)
	}
}
