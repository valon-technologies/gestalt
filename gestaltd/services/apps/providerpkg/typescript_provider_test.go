package providerpkg

import (
	"path/filepath"
	"testing"
)

func TestDetectTypeScriptProviderTargetFormatsAppsAsAppTargets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "package.json"), []byte(`{
  "gestalt": {
    "provider": {
      "kind": "app",
      "target": "./provider.ts#provider"
    }
  }
}
`), 0o644)

	target, err := DetectTypeScriptProviderTarget(root)
	if err != nil {
		t.Fatalf("DetectTypeScriptProviderTarget() error = %v", err)
	}
	if target != "app:./provider.ts#provider" {
		t.Fatalf("DetectTypeScriptProviderTarget() = %q, want %q", target, "app:./provider.ts#provider")
	}
}
