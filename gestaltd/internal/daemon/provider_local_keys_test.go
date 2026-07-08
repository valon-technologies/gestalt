package daemon

import (
	"os"
	"path/filepath"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestMatchingPluginKeysMatchesGitConfiguredManifestPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	repoRoot := filepath.Join(dir, "valon-tools")
	appDir := filepath.Join(repoRoot, "apps", "ci-cd")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifestPath := filepath.Join(appDir, "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte("kind: app\nsource: github.com/valon-technologies/valon-tools/apps/ci-cd\nversion: 1.0.0\nspec: {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	baseCfg := filepath.Join(dir, "base.yaml")
	baseYAML := `apiVersion: gestaltd.config/v8
apps:
  ciCd:
    source:
      git:
        repo: https://github.com/valon-technologies/toolshed.git
        ref: abcdef0123456789abcdef0123456789abcdef01
        path: valon-tools/apps/ci-cd/manifest.yaml
    static:
      mount: /ci-cd
`
	if err := os.WriteFile(baseCfg, []byte(baseYAML), 0o644); err != nil {
		t.Fatalf("WriteFile base config: %v", err)
	}

	plugins, err := loadConfiguredPlugins([]string{baseCfg})
	if err != nil {
		t.Fatalf("loadConfiguredPlugins: %v", err)
	}
	keys, err := matchingPluginKeys(plugins, manifestPath)
	if err != nil {
		t.Fatalf("matchingPluginKeys: %v", err)
	}
	if len(keys) != 1 || keys[0] != "ciCd" {
		t.Fatalf("matchingPluginKeys = %#v, want [ciCd]", keys)
	}

	_, manifest, err := resolveProviderTargetManifest(manifestPath)
	if err != nil {
		t.Fatalf("resolveProviderTargetManifest: %v", err)
	}
	key, err := resolveProviderPluginKey([]string{baseCfg}, manifestPath, manifest, loadConfiguredPlugins)
	if err != nil {
		t.Fatalf("resolveProviderPluginKey: %v", err)
	}
	if key != "ciCd" {
		t.Fatalf("resolveProviderPluginKey = %q, want ciCd", key)
	}
}

func TestPrepareProviderLocalSessionUsesConfiguredGitAppKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	repoRoot := filepath.Join(dir, "valon-tools")
	appDir := filepath.Join(repoRoot, "apps", "ci-cd")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `kind: app
source: github.com/valon-technologies/valon-tools/apps/ci-cd
version: 1.0.0
displayName: CI/CD
spec: {}
run:
  command: [sh, -c, echo]
`
	manifestPath := filepath.Join(appDir, "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	baseCfg := filepath.Join(dir, "base.yaml")
	baseYAML := `apiVersion: gestaltd.config/v8
apps:
  ciCd:
    source:
      git:
        repo: https://github.com/valon-technologies/toolshed.git
        ref: abcdef0123456789abcdef0123456789abcdef01
        path: valon-tools/apps/ci-cd/manifest.yaml
    static:
      mount: /ci-cd
`
	if err := os.WriteFile(baseCfg, []byte(baseYAML), 0o644); err != nil {
		t.Fatalf("WriteFile base config: %v", err)
	}

	session, err := prepareProviderLocalSession(providerLocalCommandOptions{
		Paths:        []string{manifestPath},
		ConfigPaths:  []string{baseCfg},
		FleetOverlay: true,
	})
	if err != nil {
		t.Fatalf("prepareProviderLocalSession: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(session.Dir) })

	if got, want := session.TargetKey, "ciCd"; got != want {
		t.Fatalf("session.TargetKey = %q, want %q", got, want)
	}
	if got, want := session.Kind, providermanifestv1.KindApp; got != want {
		t.Fatalf("session.Kind = %q, want %q", got, want)
	}
}

func TestManifestPathSuffixMatch(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		configured string
		target     string
		want       bool
	}{
		{
			configured: "valon-tools/apps/ci-cd/manifest.yaml",
			target:     "/Users/dev/src/toolshed/valon-tools/apps/ci-cd/manifest.yaml",
			want:       true,
		},
		{
			configured: "apps/demo/manifest.yaml",
			target:     "/tmp/workspace/apps/demo/manifest.yaml",
			want:       true,
		},
		{
			configured: "valon-tools/apps/ci-cd/manifest.yaml",
			target:     "/tmp/workspace/valon-tools/apps/ci-cd-evil/manifest.yaml",
			want:       false,
		},
	} {
		tc := tc
		t.Run(tc.configured, func(t *testing.T) {
			t.Parallel()
			if got := manifestPathSuffixMatch(tc.configured, tc.target); got != tc.want {
				t.Fatalf("manifestPathSuffixMatch(%q, %q) = %v, want %v", tc.configured, tc.target, got, tc.want)
			}
		})
	}
}
