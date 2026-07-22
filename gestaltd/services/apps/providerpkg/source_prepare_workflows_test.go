package providerpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestEnsureSourceStaticCatalog_generatesWorkflowsWhenCatalogExists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "catalog.yaml"), []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0o644)
	mustWriteFile(t, filepath.Join(dir, "run.sh"), []byte(`#!/bin/sh
set -eu
if [ -n "${GESTALT_APP_WRITE_CATALOG:-}" ]; then
  printf 'name: provider\noperations:\n  - id: echo\n    method: POST\n' > "$GESTALT_APP_WRITE_CATALOG"
fi
if [ -n "${GESTALT_APP_WRITE_WORKFLOWS:-}" ]; then
  cat > "$GESTALT_APP_WRITE_WORKFLOWS" <<'EOF'
definitions:
  - id: smoke_test
    steps:
      - app: g-issues
        operation: handle_slack_event
EOF
fi
`), 0o755)
	manifestPath := mustWriteManifestData(t, dir, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindApp,
		Source:  "github.com/testowner/apps/workflow-metadata",
		Version: "0.0.1-alpha.1",
		Run:     sourceRunCommand("sh", "./run.sh"),
		Spec:    &providermanifestv1.Spec{},
	}))

	_, manifest, err := ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile: %v", err)
	}
	if err := ensureSourceStaticCatalog(manifestPath, manifest, sourceCatalogOptions{}); err != nil {
		t.Fatalf("ensureSourceStaticCatalog: %v", err)
	}
	workflowsPath := StaticWorkflowsPath(dir)
	data, err := os.ReadFile(workflowsPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", workflowsPath, err)
	}
	if !strings.Contains(string(data), "handle_slack_event") {
		t.Fatalf("workflows = %q, want generated workflow metadata", data)
	}
}
