package daemon

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintAppRegistryPublishUsageIncludesRemoteMode(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	printAppRegistryPublishUsage(&buf)
	out := buf.String()
	for _, want := range []string{"--remote", "GESTALT_URL", "gestalt auth login"} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage missing %q:\n%s", want, out)
		}
	}
}

func TestRunAppRegistryPublishRejectsRemoteWithoutVersion(t *testing.T) {
	t.Parallel()
	err := runAppRegistryPublish([]string{"--remote", "--dist-dir", t.TempDir()}, "test")
	if err == nil || !strings.Contains(err.Error(), "--version is required") {
		t.Fatalf("runAppRegistryPublish() = %v", err)
	}
}

func TestRunAppRegistryPublishRejectsRemoteWithoutDistDir(t *testing.T) {
	t.Parallel()
	err := runAppRegistryPublish([]string{"--remote", "--version", "0.3.0-dev.1"}, "test")
	if err == nil || !strings.Contains(err.Error(), "--dist-dir is required") {
		t.Fatalf("runAppRegistryPublish() = %v", err)
	}
}

func TestRunAppRegistryPublishDirectModeStillRequiresBucket(t *testing.T) {
	t.Parallel()
	err := runAppRegistryPublish([]string{
		"--app", "demo", "--version", "0.3.0-dev.1",
		"--ref", "651a5c30feb995c9364c38f63d0d5c3880bc2055", "--dist-dir", t.TempDir(),
	}, "test")
	if err == nil || !strings.Contains(err.Error(), "--bucket is required") {
		t.Fatalf("runAppRegistryPublish() = %v", err)
	}
}

func TestRunAppRegistryPublishRemoteFalseUsesDirectMode(t *testing.T) {
	t.Parallel()
	err := runAppRegistryPublish([]string{
		"--remote=false",
		"--app", "demo", "--version", "0.3.0-dev.1",
		"--ref", "651a5c30feb995c9364c38f63d0d5c3880bc2055", "--dist-dir", t.TempDir(),
	}, "test")
	if err == nil || !strings.Contains(err.Error(), "--bucket is required") {
		t.Fatalf("runAppRegistryPublish(--remote=false) = %v", err)
	}
}

func TestRunAppRegistryPublishRejectsRemoteWithDirectFlags(t *testing.T) {
	t.Parallel()
	err := runAppRegistryPublish([]string{
		"--remote", "--bucket", "gestalt-app-registry", "--version", "0.3.0-dev.1", "--dist-dir", t.TempDir(),
	}, "test")
	if err == nil || !strings.Contains(err.Error(), "--bucket is only supported in direct GCS mode") {
		t.Fatalf("runAppRegistryPublish() = %v", err)
	}
}

func TestRejectDirectOnlyPublishFlags(t *testing.T) {
	t.Parallel()
	err := rejectDirectOnlyPublishFlags("b", "", "", false, "", 0, "", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "--bucket") {
		t.Fatalf("rejectDirectOnlyPublishFlags() = %v", err)
	}
}
