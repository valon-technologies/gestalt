package providerpkg

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
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

	_, _, err := detectSourceComponent(root, providermanifestv1.KindAuth, runtime.GOOS, runtime.GOARCH)
	if !errors.Is(err, ErrNoSourceComponentPackage) {
		t.Fatalf("error = %v, want %v", err, ErrNoSourceComponentPackage)
	}
}

func TestDetectSourceComponent_CacheFallsBackToPython(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, pythonProjectFile), []byte(`[tool.gestalt]
cache = "provider:cache_provider"
`), 0o644)

	kind, target, err := detectSourceComponent(root, providermanifestv1.KindCache, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("detectSourceComponent(cache): %v", err)
	}
	if kind != sourceProviderKindPython {
		t.Fatalf("kind = %q, want %q", kind, sourceProviderKindPython)
	}
	if target != "provider:cache_provider" {
		t.Fatalf("target = %q, want %q", target, "provider:cache_provider")
	}
}

func TestDetectSourceComponent_AuthorizationRequiresGo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRustProviderCargoToml(t, root)
	mustWriteFile(t, filepath.Join(root, pythonProjectFile), []byte(`[tool.gestalt]
authorization = "provider:authorization_provider"
`), 0o644)

	_, _, err := detectSourceComponent(root, providermanifestv1.KindAuthorization, runtime.GOOS, runtime.GOARCH)
	if !errors.Is(err, ErrNoSourceComponentPackage) {
		t.Fatalf("error = %v, want %v", err, ErrNoSourceComponentPackage)
	}
}
