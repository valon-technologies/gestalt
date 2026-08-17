package appregistryremote

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type fakeRunner map[string]string

func (f fakeRunner) Run(name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	if out, ok := f[key]; ok {
		return out, nil
	}
	return "", fmt.Errorf("unexpected command: %s", key)
}

func TestCollectLocalSourceStateClean(t *testing.T) {
	t.Parallel()
	manifestPath := "/repo/apps/demo/manifest.yaml"
	runner := fakeRunner{
		"git -C /repo/apps/demo rev-parse --show-toplevel": "/repo\n",
		"git -C /repo/apps/demo rev-parse HEAD":            "651a5c30feb995c9364c38f63d0d5c3880bc2055\n",
		"git -C /repo/apps/demo status --porcelain":        "",
	}
	state := collectLocalSourceState(manifestPath, runner)
	if state == nil || state.CommitSHA != "651a5c30feb995c9364c38f63d0d5c3880bc2055" {
		t.Fatalf("collectLocalSourceState() = %#v", state)
	}
	if state.Dirty || state.Untracked {
		t.Fatalf("collectLocalSourceState() = %#v, want clean tree", state)
	}
}

func TestCollectLocalSourceStateDirtyAndUntracked(t *testing.T) {
	t.Parallel()
	manifestPath := "/repo/apps/demo/manifest.yaml"
	runner := fakeRunner{
		"git -C /repo/apps/demo rev-parse --show-toplevel": "/repo\n",
		"git -C /repo/apps/demo rev-parse HEAD":            "651a5c30feb995c9364c38f63d0d5c3880bc2055\n",
		"git -C /repo/apps/demo status --porcelain":        " M apps/demo/manifest.yaml\n?? scratch.txt\n",
	}
	state := collectLocalSourceState(manifestPath, runner)
	if state == nil || !state.Dirty || !state.Untracked {
		t.Fatalf("collectLocalSourceState() = %#v", state)
	}
}

func TestCollectLocalSourceStateNonGit(t *testing.T) {
	t.Parallel()
	runner := fakeRunner{}
	if state := collectLocalSourceState("/tmp/apps/demo/manifest.yaml", runner); state != nil {
		t.Fatalf("collectLocalSourceState() = %#v, want nil for non-git", state)
	}
}

func TestValidateRequiredPlatformsFailsBeforeNetwork(t *testing.T) {
	t.Parallel()
	err := validateRequiredPlatforms(map[string]struct{}{"linux/amd64": {}}, []string{"linux/amd64", "darwin/arm64"})
	if err == nil || !errors.Is(err, appregistry.ErrPublishRequiredPlatform) {
		t.Fatalf("validateRequiredPlatforms() = %v, want required platform error", err)
	}
	if !strings.Contains(err.Error(), "darwin/arm64") {
		t.Fatalf("validateRequiredPlatforms() = %v, want missing platform detail", err)
	}
}

func TestBuildPublishDeclarationLocalPublication(t *testing.T) {
	t.Parallel()
	declaration, err := buildPublishDeclaration(buildDeclarationInput{
		AppName:         "demo",
		Version:         "0.3.0-dev.1",
		ManifestPath:    "apps/demo/manifest.yaml",
		SourceManifest:  testManifest("demo", "0.3.0-dev.1"),
		ReleaseMetadata: testReleaseMetadata("demo", "0.3.0-dev.1"),
		Archives: []releaseArchive{
			{Target: "linux/amd64", Filename: "linux-amd64.tar.gz", SHA256: strings.Repeat("a", 64), Size: 10},
			{Target: "darwin/arm64", Filename: "darwin-arm64.tar.gz", SHA256: strings.Repeat("b", 64), Size: 11},
		},
		LocalSource:    &appregistry.LocalSourceState{CommitSHA: "651a5c30feb995c9364c38f63d0d5c3880bc2055", Dirty: true},
		BuilderVersion: "1.2.3",
	})
	if err != nil {
		t.Fatalf("buildPublishDeclaration() error = %v", err)
	}
	if declaration.PublicationKind != appregistry.PublicationKindLocal {
		t.Fatalf("PublicationKind = %q", declaration.PublicationKind)
	}
	if declaration.SourceRef != "" {
		t.Fatalf("SourceRef = %q, want empty for local publish", declaration.SourceRef)
	}
	if declaration.BuilderVersion != "1.2.3" || !declaration.LocalSource.Dirty {
		t.Fatalf("declaration = %#v", declaration)
	}
}

func testManifest(appName, version string) *providermanifestv1.Manifest {
	return &providermanifestv1.Manifest{
		Kind:    "app",
		Source:  "github.com/valon-technologies/valon-tools/apps/" + appName,
		Version: version,
		Spec:    &providermanifestv1.Spec{},
	}
}

func testReleaseMetadata(appName, version string) *providerrelease.Metadata {
	return &providerrelease.Metadata{
		Schema:        providerrelease.SchemaName,
		SchemaVersion: providerrelease.SchemaVersion,
		Package:       "github.com/valon-technologies/valon-tools/apps/" + appName,
		Kind:          "app",
		Version:       version,
		Runtime:       providerrelease.RuntimeExecutable,
		Artifacts: providerrelease.Artifacts{
			"linux/amd64": {Path: "linux-amd64.tar.gz", SHA256: strings.Repeat("a", 64)},
		},
		StaticValidation: &providerrelease.StaticValidation{
			Manifest: testManifest(appName, version),
			Catalog:  &catalog.Catalog{Name: appName, Operations: []catalog.CatalogOperation{{ID: "echo", Method: "POST"}}},
		},
	}
}
