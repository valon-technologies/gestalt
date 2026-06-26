package daemon

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

func TestProviderPackageExpandsRequestedPlatformSets(t *testing.T) {
	t.Parallel()

	platforms, err := parseReleasePlatforms(defaultPlatforms)
	if err != nil {
		t.Fatalf("parseReleasePlatforms(defaultPlatforms): %v", err)
	}
	expanded, err := expandReleasePlatformValue(allPlatformsValue)
	if err != nil {
		t.Fatalf("expandReleasePlatformValue(%q): %v", allPlatformsValue, err)
	}
	if expanded != defaultPlatforms {
		t.Fatalf("expandReleasePlatformValue(%q) = %q, want %q", allPlatformsValue, expanded, defaultPlatforms)
	}
	targets := releaseArchiveTargets(platforms)
	if len(targets) != len(platforms) {
		t.Fatalf("releaseArchiveTargets len = %d, want %d", len(targets), len(platforms))
	}
	for i, platform := range platforms {
		wantSuffix := providerpkg.PlatformArchiveSuffix(platform.GOOS, platform.GOARCH)
		if targets[i].Generic || targets[i].PlatformSuffix != wantSuffix {
			t.Fatalf("target[%d] = %+v, want suffix %q", i, targets[i], wantSuffix)
		}
	}
}
