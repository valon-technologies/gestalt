package metricutil

import (
	"fmt"
	"strings"
	"testing"
)

func TestClassifyKnownCLIVersionRecognizesAllowlistedRelease(t *testing.T) {
	t.Parallel()

	if got := ClassifyKnownCLIVersion("0.0.2-alpha.17"); got != "0.0.2-alpha.17" {
		t.Fatalf("ClassifyKnownCLIVersion() = %q, want %q", got, "0.0.2-alpha.17")
	}
	if got := ClassifyKnownCLIVersion(" 0.0.2-alpha.17 "); got != "0.0.2-alpha.17" {
		t.Fatalf("ClassifyKnownCLIVersion(trimmed) = %q, want %q", got, "0.0.2-alpha.17")
	}
}

func TestClassifyKnownCLIVersionRejectsInvalidHeaders(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"",
		"   ",
		"not-a-version",
		"1.2",
		"1.2.3.4",
		"v1.2.3",
		"0.0.2-alpha.",
		"0.0.2-alpha.17-extra",
		"0.0.2-alpha.16",
		strings.Repeat("0", maxCLIVersionHeaderLen+1),
	} {
		if got := ClassifyKnownCLIVersion(raw); got != ClientVersionUnknown {
			t.Fatalf("ClassifyKnownCLIVersion(%q) = %q, want %q", raw, got, ClientVersionUnknown)
		}
	}
}

func TestAppendKnownCLIVersionEvictsOldestAtCapacity(t *testing.T) {
	t.Parallel()

	versions := make([]string, 0, maxKnownCLIVersions)
	for i := 1; i <= maxKnownCLIVersions; i++ {
		versions = appendKnownCLIVersion(versions, versionForIndex(i), maxKnownCLIVersions)
	}
	if len(versions) != maxKnownCLIVersions {
		t.Fatalf("len(versions) = %d, want %d", len(versions), maxKnownCLIVersions)
	}
	if versions[0] != versionForIndex(1) {
		t.Fatalf("oldest version = %q, want %q", versions[0], versionForIndex(1))
	}

	versions = appendKnownCLIVersion(versions, versionForIndex(maxKnownCLIVersions+1), maxKnownCLIVersions)
	if len(versions) != maxKnownCLIVersions {
		t.Fatalf("len(versions) after eviction = %d, want %d", len(versions), maxKnownCLIVersions)
	}
	if versions[0] != versionForIndex(2) {
		t.Fatalf("oldest version after eviction = %q, want %q", versions[0], versionForIndex(2))
	}
	if versions[len(versions)-1] != versionForIndex(maxKnownCLIVersions+1) {
		t.Fatalf("newest version = %q, want %q", versions[len(versions)-1], versionForIndex(maxKnownCLIVersions+1))
	}
}

func TestAppendKnownCLIVersionIsIdempotent(t *testing.T) {
	t.Parallel()

	versions := []string{"0.0.2-alpha.1"}
	got := appendKnownCLIVersion(versions, "0.0.2-alpha.1", maxKnownCLIVersions)
	if len(got) != 1 || got[0] != "0.0.2-alpha.1" {
		t.Fatalf("appendKnownCLIVersion() = %#v, want unchanged slice", got)
	}
}

func versionForIndex(i int) string {
	return fmt.Sprintf("0.0.2-alpha.%d", i)
}
