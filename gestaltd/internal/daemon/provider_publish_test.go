package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
)

func TestRun_ProviderPublishDryRunPlansSourceRefUploads(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	pluginDir := newUIReleaseFixture(t, rootDir)
	initProviderPublishGitRepo(t, rootDir, "https://github.com/testowner/apps.git")
	outputDir := t.TempDir()
	const version = "0.0.0-snapshot.g651a5c30feb995c9364c38f63d0d5c3880bc2055"
	const ref = "651a5c30feb995c9364c38f63d0d5c3880bc2055"
	runProviderPackageCommand(t, pluginDir,
		"--version", version,
		"--output", outputDir,
	)
	configPath := filepath.Join(t.TempDir(), "gestaltd.yaml")
	if err := os.WriteFile(configPath, []byte(`
apiVersion: gestaltd.config/v6
providerSnapshotRepositories:
  valon:
    url: https://storage.example.test/providers
    publish:
      pathLayout: sourceRef
      immutable: true
      storage:
        kind: objectStore
        url: gs://provider-snapshots
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := runProviderCommandResult(pluginDir,
		"publish",
		"--config", configPath,
		"--repo", "valon",
		"--manifest", filepath.Join(pluginDir, "manifest.json"),
		"--version", version,
		"--ref", ref,
		"--dist-dir", outputDir,
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("provider publish failed: %v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{
		"dry-run upload",
		"gs://provider-snapshots/github.com/testowner/apps/" + ref + "/ui-test/gestalt-app-ui-test_v" + version + ".tar.gz",
		"provider-release metadata: https://storage.example.test/providers/github.com/testowner/apps/" + ref + "/ui-test/provider-release.yaml",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("provider publish output missing %q\n%s", want, got)
		}
	}
}

func TestRun_ProviderPublishDryRunJSONPlansSourceRefUploads(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	pluginDir := newUIReleaseFixture(t, rootDir)
	initProviderPublishGitRepo(t, rootDir, "git@github.com:testowner/apps.git")
	outputDir := t.TempDir()
	const version = "0.0.0-snapshot.g651a5c30feb995c9364c38f63d0d5c3880bc2055"
	const ref = "651a5c30feb995c9364c38f63d0d5c3880bc2055"
	runProviderPackageCommand(t, pluginDir,
		"--version", version,
		"--output", outputDir,
	)
	configPath := filepath.Join(t.TempDir(), "gestaltd.yaml")
	if err := os.WriteFile(configPath, []byte(`
apiVersion: gestaltd.config/v6
providerSnapshotRepositories:
  valon:
    url: https://storage.example.test/providers
    publish:
      pathLayout: sourceRef
      immutable: true
      storage:
        kind: objectStore
        url: gs://provider-snapshots
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := runProviderCommandResult(pluginDir,
		"publish",
		"--config", configPath,
		"--repo", "valon",
		"--manifest", filepath.Join(pluginDir, "manifest.json"),
		"--version", version,
		"--ref", strings.ToUpper(ref),
		"--dist-dir", outputDir,
		"--dry-run",
		"--format", "json",
	)
	if err != nil {
		t.Fatalf("provider publish failed: %v\n%s", err, out)
	}
	var plan providerPublishPlan
	if err := json.Unmarshal(out, &plan); err != nil {
		t.Fatalf("dry-run JSON did not parse: %v\n%s", err, out)
	}
	if plan.Schema != providerPublishPlanSchema {
		t.Fatalf("schema = %q, want %q", plan.Schema, providerPublishPlanSchema)
	}
	if plan.PublishRepository != "valon" || plan.SourceRepository != "github.com/testowner/apps" || plan.SourceRef != ref || plan.Version != version {
		t.Fatalf("unexpected plan identity: %#v", plan)
	}
	if plan.ProviderDir != "ui-test" || plan.ManifestPath != "ui-test/manifest.json" {
		t.Fatalf("unexpected source paths: %#v", plan)
	}
	if plan.Metadata.PublicURL != "https://storage.example.test/providers/github.com/testowner/apps/"+ref+"/ui-test/provider-release.yaml" {
		t.Fatalf("metadata public URL = %q", plan.Metadata.PublicURL)
	}
	if len(plan.Artifacts) != 1 {
		t.Fatalf("artifacts len = %d, want 1: %#v", len(plan.Artifacts), plan.Artifacts)
	}
	artifact := plan.Artifacts[0]
	if artifact.Kind != providerPublishFileKindArchive || artifact.Target != providerrelease.GenericTarget {
		t.Fatalf("unexpected artifact identity: %#v", artifact)
	}
	if artifact.PublicURL != "https://storage.example.test/providers/github.com/testowner/apps/"+ref+"/ui-test/gestalt-app-ui-test_v"+version+".tar.gz" {
		t.Fatalf("artifact public URL = %q", artifact.PublicURL)
	}
	if len(plan.Files) != 2 {
		t.Fatalf("files len = %d, want 2: %#v", len(plan.Files), plan.Files)
	}
}

func TestRun_ProviderPublishRejectsShortRef(t *testing.T) {
	t.Parallel()

	pluginDir := newUIReleaseFixture(t, t.TempDir())
	out, err := runProviderCommandResult(pluginDir,
		"publish",
		"--repo", "valon",
		"--manifest", filepath.Join(pluginDir, "manifest.json"),
		"--version", "1.0.0",
		"--ref", "abc123",
		"--dist-dir", t.TempDir(),
		"--dry-run",
	)
	if err == nil {
		t.Fatalf("expected provider publish to reject short ref\n%s", out)
	}
	if !strings.Contains(string(out), "--ref must be a 40-character commit SHA") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_ProviderPublishRejectsJSONFormatWithoutDryRun(t *testing.T) {
	t.Parallel()

	out, err := runProviderCommandResult(t.TempDir(),
		"publish",
		"--repo", "valon",
		"--format", "json",
	)
	if err == nil {
		t.Fatalf("expected provider publish to reject JSON format without dry-run\n%s", out)
	}
	if !strings.Contains(string(out), "--format json requires --dry-run") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_ProviderPublishPreflightsConflictsBeforeUploading(t *testing.T) {
	rootDir := t.TempDir()
	pluginDir := newUIReleaseFixture(t, rootDir)
	initProviderPublishGitRepo(t, rootDir, "https://github.com/testowner/apps.git")
	outputDir := t.TempDir()
	const version = "0.0.0-snapshot.g651a5c30feb995c9364c38f63d0d5c3880bc2055"
	const ref = "651a5c30feb995c9364c38f63d0d5c3880bc2055"
	runProviderPackageCommand(t, pluginDir,
		"--version", version,
		"--output", outputDir,
	)
	configPath := filepath.Join(t.TempDir(), "gestaltd.yaml")
	if err := os.WriteFile(configPath, []byte(`
apiVersion: gestaltd.config/v6
providerSnapshotRepositories:
  valon:
    url: https://storage.example.test/providers
    publish:
      pathLayout: sourceRef
      immutable: true
      storage:
        kind: objectStore
        url: gs://provider-snapshots
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	binDir := t.TempDir()
	callsPath := filepath.Join(t.TempDir(), "gcloud.calls")
	fakeGcloud := filepath.Join(binDir, "gcloud")
	if err := os.WriteFile(fakeGcloud, []byte(`#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${GCLOUD_CALLS}"
if [ "$1" = "storage" ] && [ "$2" = "objects" ] && [ "$3" = "describe" ]; then
  case "$4" in
    *provider-release.yaml)
      printf '{"metadata":{"sha256":"stale","source-ref":"651a5c30feb995c9364c38f63d0d5c3880bc2055"}}\n'
      exit 0
      ;;
    *)
      echo "not found" >&2
      exit 1
      ;;
  esac
fi
if [ "$1" = "storage" ] && [ "$2" = "cp" ]; then
  printf 'upload attempted\n' >> "${GCLOUD_CALLS}"
  exit 0
fi
exit 1
`), 0o755); err != nil {
		t.Fatalf("write fake gcloud: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GCLOUD_CALLS", callsPath)

	out, err := runProviderCommandResult(pluginDir,
		"publish",
		"--config", configPath,
		"--repo", "valon",
		"--manifest", filepath.Join(pluginDir, "manifest.json"),
		"--version", version,
		"--ref", ref,
		"--dist-dir", outputDir,
	)
	if err == nil {
		t.Fatalf("expected provider publish to reject stale metadata before upload\n%s", out)
	}
	if !strings.Contains(string(out), "provider-release.yaml already exists; delete the object or entire snapshot SHA prefix and republish") {
		t.Fatalf("unexpected output: %s", out)
	}
	calls, err := os.ReadFile(callsPath)
	if err != nil {
		t.Fatalf("read fake gcloud calls: %v", err)
	}
	if strings.Contains(string(calls), "upload attempted") {
		t.Fatalf("provider publish attempted upload before preflight completed:\n%s", calls)
	}
}

func TestRun_ProviderPublishDoesNotTreatDescribeErrorsAsMissing(t *testing.T) {
	rootDir := t.TempDir()
	pluginDir := newUIReleaseFixture(t, rootDir)
	initProviderPublishGitRepo(t, rootDir, "https://github.com/testowner/apps.git")
	outputDir := t.TempDir()
	const version = "0.0.0-snapshot.g651a5c30feb995c9364c38f63d0d5c3880bc2055"
	const ref = "651a5c30feb995c9364c38f63d0d5c3880bc2055"
	runProviderPackageCommand(t, pluginDir,
		"--version", version,
		"--output", outputDir,
	)
	configPath := filepath.Join(t.TempDir(), "gestaltd.yaml")
	if err := os.WriteFile(configPath, []byte(`
apiVersion: gestaltd.config/v6
providerSnapshotRepositories:
  valon:
    url: https://storage.example.test/providers
    publish:
      pathLayout: sourceRef
      immutable: true
      storage:
        kind: objectStore
        url: gs://provider-snapshots
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	binDir := t.TempDir()
	callsPath := filepath.Join(t.TempDir(), "gcloud.calls")
	fakeGcloud := filepath.Join(binDir, "gcloud")
	if err := os.WriteFile(fakeGcloud, []byte(`#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${GCLOUD_CALLS}"
if [ "$1" = "storage" ] && [ "$2" = "objects" ] && [ "$3" = "describe" ]; then
  echo "permission denied" >&2
  exit 1
fi
if [ "$1" = "storage" ] && [ "$2" = "cp" ]; then
  printf 'upload attempted\n' >> "${GCLOUD_CALLS}"
  exit 0
fi
exit 1
`), 0o755); err != nil {
		t.Fatalf("write fake gcloud: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GCLOUD_CALLS", callsPath)

	out, err := runProviderCommandResult(pluginDir,
		"publish",
		"--config", configPath,
		"--repo", "valon",
		"--manifest", filepath.Join(pluginDir, "manifest.json"),
		"--version", version,
		"--ref", ref,
		"--dist-dir", outputDir,
	)
	if err == nil {
		t.Fatalf("expected provider publish to reject describe error before upload\n%s", out)
	}
	if !strings.Contains(string(out), "permission denied") {
		t.Fatalf("unexpected output: %s", out)
	}
	calls, err := os.ReadFile(callsPath)
	if err != nil {
		t.Fatalf("read fake gcloud calls: %v", err)
	}
	if strings.Contains(string(calls), "upload attempted") {
		t.Fatalf("provider publish attempted upload after describe error:\n%s", calls)
	}
}

func TestRun_ProviderPublishUploadsArchivesBeforeMetadata(t *testing.T) {
	rootDir := t.TempDir()
	pluginDir := newUIReleaseFixture(t, rootDir)
	initProviderPublishGitRepo(t, rootDir, "https://github.com/testowner/apps.git")
	outputDir := t.TempDir()
	const version = "0.0.0-snapshot.g651a5c30feb995c9364c38f63d0d5c3880bc2055"
	const ref = "651a5c30feb995c9364c38f63d0d5c3880bc2055"
	runProviderPackageCommand(t, pluginDir,
		"--version", version,
		"--output", outputDir,
	)
	configPath := filepath.Join(t.TempDir(), "gestaltd.yaml")
	if err := os.WriteFile(configPath, []byte(`
apiVersion: gestaltd.config/v6
providerSnapshotRepositories:
  valon:
    url: https://storage.example.test/providers
    publish:
      pathLayout: sourceRef
      immutable: true
      storage:
        kind: objectStore
        url: gs://provider-snapshots
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	binDir := t.TempDir()
	callsPath := filepath.Join(t.TempDir(), "gcloud.calls")
	fakeGcloud := filepath.Join(binDir, "gcloud")
	if err := os.WriteFile(fakeGcloud, []byte(`#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${GCLOUD_CALLS}"
if [ "$1" = "storage" ] && [ "$2" = "objects" ] && [ "$3" = "describe" ]; then
  echo "not found" >&2
  exit 1
fi
if [ "$1" = "storage" ] && [ "$2" = "cp" ]; then
  exit 0
fi
exit 1
`), 0o755); err != nil {
		t.Fatalf("write fake gcloud: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GCLOUD_CALLS", callsPath)

	out, err := runProviderCommandResult(pluginDir,
		"publish",
		"--config", configPath,
		"--repo", "valon",
		"--manifest", filepath.Join(pluginDir, "manifest.json"),
		"--version", version,
		"--ref", ref,
		"--dist-dir", outputDir,
	)
	if err != nil {
		t.Fatalf("provider publish failed: %v\n%s", err, out)
	}
	calls, err := os.ReadFile(callsPath)
	if err != nil {
		t.Fatalf("read fake gcloud calls: %v", err)
	}
	got := string(calls)
	archiveUpload := strings.Index(got, "gestalt-app-ui-test_v"+version+".tar.gz gs://provider-snapshots")
	metadataUpload := strings.Index(got, "provider-release.yaml gs://provider-snapshots")
	if archiveUpload < 0 || metadataUpload < 0 {
		t.Fatalf("missing expected upload calls:\n%s", got)
	}
	if archiveUpload >= metadataUpload {
		t.Fatalf("uploads are not phased archive -> metadata:\n%s", got)
	}
}

func initProviderPublishGitRepo(t *testing.T, dir, remote string) {
	t.Helper()

	for _, args := range [][]string{
		{"init"},
		{"remote", "add", "origin", remote},
	} {
		out, err := runProviderPublishCommand("git", append([]string{"-C", dir}, args...)...)
		if err != nil {
			t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}
