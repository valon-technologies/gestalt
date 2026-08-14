package operator

import (
	"strings"
	"testing"
)

func TestNewSnapshotSourceRefPathRemoteForm(t *testing.T) {
	t.Parallel()

	const ref = "CBF01DE477C362F96AACB814F7246F07C7ADB3A0"
	const manifest = "app/vercel/manifest.yaml"
	wantRepo := "github.com/example/apps"

	tests := []struct {
		name    string
		repo    string
		wantErr bool
	}{
		{name: "https", repo: "https://github.com/example/apps.git"},
		{name: "ssh scp", repo: "git@github.com:example/apps.git"},
		{name: "http rejected", repo: "http://github.com/example/apps.git", wantErr: true},
		{name: "ssh url rejected", repo: "ssh://git@github.com/example/apps.git", wantErr: true},
		{name: "www host rejected", repo: "https://www.github.com/example/apps.git", wantErr: true},
		{name: "gitlab rejected", repo: "https://gitlab.example.com/group/app.git", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewSnapshotSourceRefPath(tc.repo, ref, manifest)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NewSnapshotSourceRefPath(%q) = %+v, want error", tc.repo, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewSnapshotSourceRefPath(%q): %v", tc.repo, err)
			}
			if got.SourceRepository != wantRepo {
				t.Fatalf("SourceRepository = %q, want %q", got.SourceRepository, wantRepo)
			}
			if got.ProviderDir != "app/vercel" {
				t.Fatalf("ProviderDir = %q, want app/vercel", got.ProviderDir)
			}
		})
	}
}

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
