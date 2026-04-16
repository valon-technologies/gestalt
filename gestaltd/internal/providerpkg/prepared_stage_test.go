package providerpkg

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestStageSourcePreparedInstallDir_BuildsHostBinaryWhenSourcePackageExists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	testutil.CopyExampleProviderPlugin(t, root)

	staleArtifactPath := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider"))
	mustWriteFile(t, filepath.Join(root, filepath.FromSlash(staleArtifactPath)), []byte("stale-binary"), 0o755)
	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      "github.com/test/plugins/provider",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Example Provider",
		Spec:        &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{{
			OS:   runtime.GOOS,
			Arch: runtime.GOARCH,
			Path: staleArtifactPath,
		}},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: staleArtifactPath},
	}))

	stagingDir := filepath.Join(t.TempDir(), "prepared")
	staged, err := StageSourcePreparedInstallDir(manifestPath, stagingDir, StageSourcePreparedInstallOptions{
		Kind:       providermanifestv1.KindPlugin,
		PluginName: "prepared-stage-test",
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
	})
	if err != nil {
		t.Fatalf("StageSourcePreparedInstallDir: %v", err)
	}

	wantBinary := stagedReleaseBinaryName("prepared-stage-test", runtime.GOOS)
	if staged.Manifest == nil || staged.Manifest.Entrypoint == nil || staged.Manifest.Entrypoint.ArtifactPath != wantBinary {
		var entrypoint any
		if staged.Manifest != nil {
			entrypoint = staged.Manifest.Entrypoint
		}
		t.Fatalf("staged manifest entrypoint = %#v, want artifact path %q", entrypoint, wantBinary)
	}

	stagedBinaryPath := filepath.Join(stagingDir, wantBinary)
	data, err := os.ReadFile(stagedBinaryPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", stagedBinaryPath, err)
	}
	if string(data) == "stale-binary" {
		t.Fatalf("staged binary reused stale checked-in artifact")
	}
}
