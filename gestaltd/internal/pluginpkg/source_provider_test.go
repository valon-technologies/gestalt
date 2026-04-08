package pluginpkg

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestDetectSourceProvider_FallsBackToPythonWhenCargoIsWorkspaceManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRustWorkspaceCargoToml(t, root)
	mustWriteFile(t, filepath.Join(root, pythonProjectFile), []byte(`[tool.gestalt]
plugin = "provider.plugin:plugin"
`), 0o644)

	kind, target, err := detectSourceProvider(root, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("detectSourceProvider: %v", err)
	}
	if kind != sourceProviderKindPython {
		t.Fatalf("kind = %q, want %q", kind, sourceProviderKindPython)
	}
	if target != "provider.plugin:plugin" {
		t.Fatalf("target = %q, want %q", target, "provider.plugin:plugin")
	}
}

func TestDetectSourceComponent_WorkspaceCargoManifestIsMiss(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRustWorkspaceCargoToml(t, root)

	_, err := detectSourceComponent(root, pluginmanifestv1.KindAuth, runtime.GOOS, runtime.GOARCH)
	if !errors.Is(err, ErrNoSourceComponentPackage) {
		t.Fatalf("error = %v, want %v", err, ErrNoSourceComponentPackage)
	}
}
