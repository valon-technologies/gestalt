package providerpkg

import (
	"path/filepath"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestHasSourceComponentPackage_RuntimeWithoutSourceReturnsFalse(t *testing.T) {
	t.Parallel()

	ok, err := HasSourceComponentPackage(t.TempDir(), providermanifestv1.KindRuntime)
	if err != nil {
		t.Fatalf("HasSourceComponentPackage(runtime) error = %v", err)
	}
	if ok {
		t.Fatal("HasSourceComponentPackage(runtime) = true, want false")
	}
}

func TestHasSourceComponentPackage_DetectsPythonRuntimeProvider(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "pyproject.toml"), []byte(`[tool.gestalt]
runtime = "provider:runtime"
`), 0o644)

	ok, err := HasSourceComponentPackage(root, providermanifestv1.KindRuntime)
	if err != nil {
		t.Fatalf("HasSourceComponentPackage(runtime) error = %v", err)
	}
	if !ok {
		t.Fatal("HasSourceComponentPackage(runtime) = false, want true")
	}
}

func TestHasSourceComponentPackage_DetectsTypeScriptRuntimeProvider(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "package.json"), []byte(`{
  "gestalt": {
    "provider": {
      "kind": "runtime",
      "target": "./runtime.ts#provider"
    }
  }
}
`), 0o644)

	ok, err := HasSourceComponentPackage(root, providermanifestv1.KindRuntime)
	if err != nil {
		t.Fatalf("HasSourceComponentPackage(runtime) error = %v", err)
	}
	if !ok {
		t.Fatal("HasSourceComponentPackage(runtime) = false, want true")
	}
}
