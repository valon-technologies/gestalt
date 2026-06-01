package operator

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

func TestLockMaterializationPlatform(t *testing.T) {
	t.Parallel()

	host := providerpkg.CurrentPlatformString()
	if got := lockMaterializationPlatform(nil); got != host {
		t.Fatalf("lockMaterializationPlatform(nil) = %q, want host %q", got, host)
	}
	if got := lockMaterializationPlatform([]struct{ GOOS, GOARCH string }{}); got != host {
		t.Fatalf("lockMaterializationPlatform(empty) = %q, want host %q", got, host)
	}

	platforms := []struct{ GOOS, GOARCH string }{
		{GOOS: "linux", GOARCH: "amd64"},
		{GOOS: "darwin", GOARCH: "arm64"},
	}
	if got := lockMaterializationPlatform(platforms); got != "linux/amd64" {
		t.Fatalf("lockMaterializationPlatform = %q, want linux/amd64", got)
	}
}

func TestResolveMaterializationPlatform(t *testing.T) {
	t.Parallel()

	host := providerpkg.CurrentPlatformString()
	if got := resolveMaterializationPlatform(""); got != host {
		t.Fatalf("resolveMaterializationPlatform(\"\") = %q, want %q", got, host)
	}
	if got := resolveMaterializationPlatform("linux/amd64"); got != "linux/amd64" {
		t.Fatalf("resolveMaterializationPlatform = %q, want linux/amd64", got)
	}
}

func TestFormatLockFlagsIncludesPlatform(t *testing.T) {
	t.Parallel()

	flags := formatLockFlags([]string{"gestalt.yaml", "prod/gestalt.yaml"}, StatePaths{}, "linux/amd64")
	if !strings.Contains(flags, "--platform linux/amd64") {
		t.Fatalf("formatLockFlags = %q, want --platform linux/amd64", flags)
	}
	if !strings.Contains(flags, "--config gestalt.yaml") || !strings.Contains(flags, "--config prod/gestalt.yaml") {
		t.Fatalf("formatLockFlags = %q, want config paths preserved", flags)
	}
}
