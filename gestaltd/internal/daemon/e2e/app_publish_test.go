package e2e

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestRun_AppPublishDryRunPlansVersionedRegistryUploads(t *testing.T) {
	t.Parallel()

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

	out, err := runAppCommandResult(rootDir,
		"publish",
		"--registry", "toolshed",
		"--bucket", "gs://gestalt-app-registry",
		"--app", releaseTestAppName,
		"--version", version,
		"--ref", ref,
		"--dist-dir", outputDir,
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("app publish failed: %v\n%s", err, out)
	}
	archiveName := platformArchiveNameForTest(releaseTestAppName, version, runtime.GOOS, runtime.GOARCH)
	got := string(out)
	for _, want := range []string{
		"dry-run upload",
		"gs://gestalt-app-registry/apps/" + releaseTestAppName + "/artifacts/" + version + "/" + archiveName,
		"gs://gestalt-app-registry/apps/" + releaseTestAppName + "/versions/" + version + ".json",
		"dry-run update gs://gestalt-app-registry/apps/" + releaseTestAppName + "/index.json",
		"registry entry: https://storage.googleapis.com/gestalt-app-registry/apps/" + releaseTestAppName + "/versions/" + version + ".json",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("app publish output missing %q\n%s", want, got)
		}
	}
}

func TestRun_AppPublishDryRunJSONPlansVersionedRegistryUploads(t *testing.T) {
	t.Parallel()

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

	out, err := runAppCommandResult(rootDir,
		"publish",
		"--registry", "toolshed",
		"--bucket", "gs://gestalt-app-registry",
		"--app", releaseTestAppName,
		"--version", version,
		"--ref", ref,
		"--dist-dir", outputDir,
		"--dry-run",
		"--format", "json",
	)
	if err != nil {
		t.Fatalf("app publish failed: %v\n%s", err, out)
	}
	var plan struct {
		Schema      string `json:"schema"`
		AppName     string `json:"appName"`
		Version     string `json:"version"`
		EntryObject struct {
			PublicURL string `json:"publicUrl"`
		} `json:"entryObject"`
	}
	if err := json.Unmarshal(out, &plan); err != nil {
		t.Fatalf("decode app publish plan: %v\n%s", err, out)
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
}
