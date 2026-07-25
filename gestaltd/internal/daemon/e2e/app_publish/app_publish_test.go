package app_publish

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRun_AppPublishDryRunPlansVersionedRegistryUploads(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	pluginDir := newAppRegistryPublishFixture(t, rootDir)
	initProviderPublishGitRepo(t, rootDir, "https://github.com/testowner/apps.git")
	fakeGcloudEnv := installFakeGcloudForAppPublishDryRun(t)
	outputDir := t.TempDir()
	const version = "0.0.0-snapshot.g651a5c30feb995c9364c38f63d0d5c3880bc2055"
	const ref = "651a5c30feb995c9364c38f63d0d5c3880bc2055"
	runProviderPackageCommand(t, pluginDir,
		"--version", version,
		"--output", outputDir,
	)

	stdout, stderr, err := runAppCommandStreamsWithEnv(rootDir, fakeGcloudEnv,
		"publish",
		"--bucket", "gs://gestalt-app-registry",
		"--app", releaseTestAppName,
		"--version", version,
		"--ref", ref,
		"--dist-dir", outputDir,
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("app publish failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var plan struct {
		Schema      string `json:"schema"`
		AppName     string `json:"appName"`
		Version     string `json:"version"`
		EntryObject struct {
			PublicURL string `json:"publicUrl"`
		} `json:"entryObject"`
		ArtifactObjects []struct {
			StorageURL string `json:"storageUrl"`
		} `json:"artifactObjects"`
		IndexObject struct {
			StorageURL string `json:"storageUrl"`
		} `json:"indexObject"`
	}
	if err := json.Unmarshal(stdout, &plan); err != nil {
		t.Fatalf("decode app publish plan: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if strings.Contains(string(stdout), "[progress]") {
		t.Fatalf("progress leaked into app publish JSON stdout: %s", stdout)
	}
	if !strings.Contains(string(stderr), "Inspecting and hashing 1 release archives") ||
		!strings.Contains(string(stderr), "Hashing 2 app publish files") {
		t.Fatalf("app publish progress missing from stderr: %s", stderr)
	}
	if plan.Schema != "gestaltd.app.publish.plan.v1" {
		t.Fatalf("schema = %q", plan.Schema)
	}
	if plan.AppName != releaseTestAppName {
		t.Fatalf("appName = %q", plan.AppName)
	}
	if plan.Version != version {
		t.Fatalf("version = %q", plan.Version)
	}
	wantEntry := "https://storage.googleapis.com/gestalt-app-registry/apps/" + releaseTestAppName + "/versions/" + version + ".json"
	if plan.EntryObject.PublicURL != wantEntry {
		t.Fatalf("entry publicUrl = %q, want %q", plan.EntryObject.PublicURL, wantEntry)
	}
	archiveName := platformArchiveNameForTest(releaseTestAppName, version, runtime.GOOS, runtime.GOARCH)
	wantArtifact := "gs://gestalt-app-registry/apps/" + releaseTestAppName + "/artifacts/" + version + "/" + archiveName
	if len(plan.ArtifactObjects) != 1 || plan.ArtifactObjects[0].StorageURL != wantArtifact {
		t.Fatalf("artifactObjects = %#v, want %q", plan.ArtifactObjects, wantArtifact)
	}
	wantIndex := "gs://gestalt-app-registry/apps/" + releaseTestAppName + "/index.json"
	if plan.IndexObject.StorageURL != wantIndex {
		t.Fatalf("indexObject.storageUrl = %q, want %q", plan.IndexObject.StorageURL, wantIndex)
	}
}

func TestRun_AppPublishUploadsAndRetriesIndexWithProgress(t *testing.T) {
	rootDir := t.TempDir()
	pluginDir := newAppRegistryPublishFixture(t, rootDir)
	initProviderPublishGitRepo(t, rootDir, "https://github.com/testowner/apps.git")
	outputDir := t.TempDir()
	const version = "0.0.0-snapshot.g651a5c30feb995c9364c38f63d0d5c3880bc2055"
	const ref = "651a5c30feb995c9364c38f63d0d5c3880bc2055"
	runProviderPackageCommand(t, pluginDir,
		"--version", version,
		"--output", outputDir,
	)

	binDir := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "index-first-update.failed")
	fakeGcloud := filepath.Join(binDir, "gcloud")
	if err := os.WriteFile(fakeGcloud, []byte(`#!/usr/bin/env bash
set -euo pipefail
if [ "$1" = "storage" ] && [ "$2" = "objects" ] && [ "$3" = "describe" ]; then
  if [[ "$4" == */index.json ]]; then
    if [ -e "`+statePath+`" ]; then
      printf '{"generation":"8"}\n'
    else
      printf '{"generation":"7"}\n'
    fi
    exit 0
  fi
  echo "not found" >&2
  exit 1
fi
if [ "$1" = "storage" ] && [ "$2" = "cat" ]; then
  printf '{"schemaVersion":1,"apps":{}}\n'
  exit 0
fi
if [ "$1" = "storage" ] && [ "$2" = "cp" ]; then
  if [[ "$5" == */index.json ]] && [ ! -e "`+statePath+`" ]; then
    touch "`+statePath+`"
    echo "precondition failed" >&2
    exit 1
  fi
  exit 0
fi
echo "unexpected gcloud command" >&2
exit 1
`), 0o755); err != nil {
		t.Fatalf("write fake gcloud: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	stdout, stderr, err := runAppCommandStreams(rootDir,
		"publish",
		"--bucket", "gs://gestalt-app-registry",
		"--app", releaseTestAppName,
		"--version", version,
		"--ref", ref,
		"--dist-dir", outputDir,
	)
	if err != nil {
		t.Fatalf("app publish failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	gotStdout := string(stdout)
	gotStderr := string(stderr)
	for _, want := range []string{"uploaded gs://gestalt-app-registry/", "updated gs://gestalt-app-registry/apps/release-test/index.json"} {
		if !strings.Contains(gotStdout, want) {
			t.Fatalf("app publish stdout missing %q:\n%s", want, gotStdout)
		}
	}
	for _, want := range []string{
		"Checking 3 remote app objects before upload",
		"Uploading 2 immutable app objects",
		"Updating app registry index",
		"App registry index update conflict; retrying",
		"App registry index changed concurrently; retrying attempt 2/5",
	} {
		if !strings.Contains(gotStderr, want) {
			t.Fatalf("app publish stderr missing %q:\n%s", want, gotStderr)
		}
	}
	if strings.Contains(gotStderr, "not found") || strings.Contains(gotStderr, "precondition failed") {
		t.Fatalf("raw gcloud diagnostics leaked into normal stderr:\n%s", gotStderr)
	}
	if !strings.Contains(gotStderr, "gestaltd app publish is deprecated; use gestaltd app registry publish") {
		t.Fatalf("app publish stderr missing deprecation warning:\n%s", gotStderr)
	}
}

func TestRun_AppRegistryPublishDryRunMatchesDeprecatedAlias(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	pluginDir := newAppRegistryPublishFixture(t, rootDir)
	initProviderPublishGitRepo(t, rootDir, "https://github.com/testowner/apps.git")
	fakeGcloudEnv := installFakeGcloudForAppPublishDryRun(t)
	outputDir := t.TempDir()
	const version = "0.0.0-snapshot.g651a5c30feb995c9364c38f63d0d5c3880bc2055"
	const ref = "651a5c30feb995c9364c38f63d0d5c3880bc2055"
	runProviderPackageCommand(t, pluginDir,
		"--version", version,
		"--output", outputDir,
	)

	stdout, stderr, err := runAppCommandStreamsWithEnv(rootDir, fakeGcloudEnv,
		"registry", "publish",
		"--bucket", "gs://gestalt-app-registry",
		"--app", releaseTestAppName,
		"--version", version,
		"--ref", ref,
		"--dist-dir", outputDir,
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("app registry publish failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if strings.Contains(string(stderr), "gestaltd app publish is deprecated") {
		t.Fatalf("registry publish should not print deprecation warning:\n%s", stderr)
	}
	var plan struct {
		Schema  string `json:"schema"`
		AppName string `json:"appName"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(stdout, &plan); err != nil {
		t.Fatalf("decode app registry publish plan: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if plan.Schema != "gestaltd.app.publish.plan.v1" || plan.AppName != releaseTestAppName || plan.Version != version {
		t.Fatalf("plan = %#v", plan)
	}
}
