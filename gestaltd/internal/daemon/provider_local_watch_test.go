package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestProviderLocalWatchPlanIncludesConfigSourcesAndReleaseMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sourceAppDir := setupAppDir(t, filepath.Join(dir, "source-app"))
	releaseAppDir := setupPrebuiltAppDir(t, filepath.Join(dir, "release-app"))
	if err := writeLocalProviderReleaseMetadata(releaseAppDir); err != nil {
		t.Fatalf("write provider-release metadata: %v", err)
	}
	metadataPath := filepath.Join(releaseAppDir, "provider-release.yaml")
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgText := fmt.Sprintf(`apiVersion: %s
server:
  encryptionKey: watch-plan-test-key
apps:
  source:
    source:
      path: %s
  release:
    source: %s
`, config.ConfigAPIVersion, sourceAppDir, metadataPath)
	if err := os.WriteFile(cfgPath, []byte(cfgText), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load(%s): %v", cfgPath, err)
	}

	plan, err := newProviderLocalWatchPlan([]string{cfgPath}, cfg)
	if err != nil {
		t.Fatalf("newProviderLocalWatchPlan: %v", err)
	}
	for _, path := range []string{
		cfgPath,
		componentProviderManifestPath(t, sourceAppDir),
		filepath.Join(sourceAppDir, "provider.go"),
		metadataPath,
	} {
		if !plan.includesEventPath(path) {
			t.Fatalf("watch plan does not include %s; files=%v roots=%v", path, plan.Files, plan.Roots)
		}
	}
}
