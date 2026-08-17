package appregistry_test

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

func TestJoinRegistryObjectPathRejectsTraversal(t *testing.T) {
	t.Parallel()

	prefix := appregistry.PublishVersionStagingPrefix("g-issues", "0.3.0-dev.1")
	cases := []struct {
		name     string
		segments []string
	}{
		{name: "dot segment", segments: []string{"..", "artifact.tar.gz"}},
		{name: "embedded slash", segments: []string{"artifacts", "linux/amd64", "artifact.tar.gz"}},
		{name: "backslash", segments: []string{"artifacts", "..\\escape", "artifact.tar.gz"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := appregistry.JoinRegistryObjectPath(prefix, tc.segments...); err == nil {
				t.Fatalf("JoinRegistryObjectPath(%q, %v) expected error", prefix, tc.segments)
			}
		})
	}
}

func TestPublishStagingArtifactPathStaysUnderVersionPrefix(t *testing.T) {
	t.Parallel()

	versionPrefix := appregistry.PublishVersionStagingPrefix("g-issues", "0.3.0-dev.1")
	stagingPrefix := appregistry.PublishStagingPrefix("g-issues", "0.3.0-dev.1", "digest")
	joined, err := appregistry.PublishStagingArtifactPath(stagingPrefix, "linux/amd64", "linux-amd64.tar.gz")
	if err != nil {
		t.Fatalf("PublishStagingArtifactPath: %v", err)
	}
	if !strings.HasPrefix(joined, versionPrefix+"/") {
		t.Fatalf("path %q escaped version prefix %q", joined, versionPrefix)
	}
}

func TestPublishArtifactFinalRelStaysUnderVersionPrefix(t *testing.T) {
	t.Parallel()

	artifactPrefix := appregistry.AppArtifactPrefix("g-issues", "0.3.0-dev.1")
	joined, err := appregistry.PublishArtifactFinalRel(artifactPrefix, "linux-amd64.tar.gz")
	if err != nil {
		t.Fatalf("PublishArtifactFinalRel: %v", err)
	}
	if !strings.HasPrefix(joined, artifactPrefix+"/") {
		t.Fatalf("path %q escaped artifact prefix %q", joined, artifactPrefix)
	}
}
