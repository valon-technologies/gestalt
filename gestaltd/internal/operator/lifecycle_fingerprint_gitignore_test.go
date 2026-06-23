package operator

import (
	"os"
	"path/filepath"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

// writeBuildableSourceProviderTree writes a provider dir whose manifest declares
// a build phase (so fingerprintLocalSourceDigest takes the build-inputs path)
// with a no-op build command and the given build output path. The provider
// binary at buildOutput is written so SourceBuildOutput can resolve.
func writeBuildableSourceProviderTree(t *testing.T, dir, source, buildOutput string, buildInputs []string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir provider dir: %v", err)
	}
	manifest := &providermanifestv1.Manifest{
		Source:      source,
		Version:     "1.0.0",
		Kind:        providermanifestv1.KindApp,
		DisplayName: "FP",
		Spec:        &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"true"},
			Inputs:  buildInputs,
		},
	}
	data, err := encodeSourceManifestForTest(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "provider.ts"), []byte("export const provider = {};\n"), 0o644); err != nil {
		t.Fatalf("write provider.ts: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, buildOutput)), 0o755); err != nil {
		t.Fatalf("mkdir artifact parent: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, buildOutput), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
}

// initGitRepo turns dir into a git repo with a single commit so the fingerprint
// matcher anchors patterns at the repo root rather than the provider dir.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	runGitTestCommand(t, dir, "init")
	runGitTestCommand(t, dir, "config", "user.email", "test@example.com")
	runGitTestCommand(t, dir, "config", "user.name", "Test")
	runGitTestCommand(t, dir, "add", ".")
	runGitTestCommand(t, dir, "commit", "-m", "init")
}

func fingerprintOf(t *testing.T, manifestPath string) string {
	t.Helper()
	digest, err := fingerprintLocalSourceDigest(manifestPath)
	if err != nil {
		t.Fatalf("fingerprintLocalSourceDigest: %v", err)
	}
	return digest
}

// TestFingerprintGitignoreRepoRootExclusion asserts the core repo-root
// anchoring contract: with no inputs, the fingerprint walks the whole provider
// dir, excludes gitignored paths (node_modules, .venv) resolved from the
// repo-root .gitignore, and is stable across changes to those ignored paths
// while changing when a tracked source file changes.
func TestFingerprintGitignoreRepoRootExclusion(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	providerDir := filepath.Join(repoDir, "apps", "alpha")
	writeBuildableSourceProviderTree(t, providerDir, "github.com/acme/apps/alpha", ".gestaltd/bin/alpha", nil)
	if err := os.WriteFile(filepath.Join(repoDir, ".gitignore"), []byte("node_modules/\n.venv/\n"), 0o644); err != nil {
		t.Fatalf("write repo gitignore: %v", err)
	}
	initGitRepo(t, repoDir)

	manifestPath := filepath.Join(providerDir, "manifest.yaml")
	base := fingerprintOf(t, manifestPath)

	for _, ignored := range []string{
		filepath.Join(providerDir, "node_modules", "dep", "index.js"),
		filepath.Join(providerDir, ".venv", "lib", "x.py"),
		filepath.Join(providerDir, ".gestaltd", "bin", "alpha"), // declared build output
	} {
		if err := os.MkdirAll(filepath.Dir(ignored), 0o755); err != nil {
			t.Fatalf("mkdir ignored: %v", err)
		}
		if err := os.WriteFile(ignored, []byte("noise"), 0o644); err != nil {
			t.Fatalf("write ignored: %v", err)
		}
	}
	if got := fingerprintOf(t, manifestPath); got != base {
		t.Fatalf("fingerprint changed after touching gitignored paths + build output: got %q, want %q", got, base)
	}

	if err := os.WriteFile(filepath.Join(providerDir, "provider.ts"), []byte("export const provider = { changed: true };\n"), 0o644); err != nil {
		t.Fatalf("edit tracked source: %v", err)
	}
	if got := fingerprintOf(t, manifestPath); got == base {
		t.Fatal("fingerprint unchanged after editing a tracked source file")
	}
}

// TestFingerprintNestedGitignoreHonored asserts a per-provider .gitignore
// nested under the source dir is honored alongside the repo-root one.
func TestFingerprintNestedGitignoreHonored(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	providerDir := filepath.Join(repoDir, "apps", "alpha")
	writeBuildableSourceProviderTree(t, providerDir, "github.com/acme/apps/alpha", ".gestaltd/bin/alpha", nil)
	if err := os.WriteFile(filepath.Join(repoDir, ".gitignore"), []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatalf("write repo gitignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, ".gitignore"), []byte("generated/\n"), 0o644); err != nil {
		t.Fatalf("write nested gitignore: %v", err)
	}
	initGitRepo(t, repoDir)

	manifestPath := filepath.Join(providerDir, "manifest.yaml")
	base := fingerprintOf(t, manifestPath)

	if err := os.MkdirAll(filepath.Join(providerDir, "generated"), 0o755); err != nil {
		t.Fatalf("mkdir generated: %v", err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, "generated", "out.js"), []byte("noise"), 0o644); err != nil {
		t.Fatalf("write generated: %v", err)
	}
	if got := fingerprintOf(t, manifestPath); got != base {
		t.Fatalf("fingerprint changed after touching nested-gitignored generated/: got %q, want %q", got, base)
	}
}

// TestFingerprintHashesNonGitignoredTargetDir proves the over-exclusion footgun
// is gone: a source dir literally named "target" (formerly denylisted) is now
// hashed when it is not gitignored, so edits there invalidate the cache.
func TestFingerprintHashesNonGitignoredTargetDir(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	providerDir := filepath.Join(repoDir, "apps", "alpha")
	writeBuildableSourceProviderTree(t, providerDir, "github.com/acme/apps/alpha", ".gestaltd/bin/alpha", nil)
	targetDir := filepath.Join(providerDir, "target")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "main.rs"), []byte("fn main() {}\n"), 0o644); err != nil {
		t.Fatalf("write target/main.rs: %v", err)
	}
	initGitRepo(t, repoDir)

	manifestPath := filepath.Join(providerDir, "manifest.yaml")
	base := fingerprintOf(t, manifestPath)

	if err := os.WriteFile(filepath.Join(targetDir, "main.rs"), []byte("fn main() { changed }"), 0o644); err != nil {
		t.Fatalf("edit target/main.rs: %v", err)
	}
	if got := fingerprintOf(t, manifestPath); got == base {
		t.Fatal("fingerprint unchanged after editing a non-gitignored target/ source file; denylist over-exclusion regressed")
	}
}

// TestFingerprintExplicitInputsBeatGitignore asserts an explicitly listed
// input file is hashed even when gitignored (explicit beats ignore), and that
// a directory input entry still applies the matcher.
func TestFingerprintExplicitInputsBeatGitignore(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	providerDir := filepath.Join(repoDir, "apps", "alpha")
	const lockedFile = "bun.lock"
	writeBuildableSourceProviderTree(t, providerDir, "github.com/acme/apps/alpha", ".gestaltd/bin/alpha", []string{lockedFile})
	if err := os.WriteFile(filepath.Join(repoDir, ".gitignore"), []byte("bun.lock\nnode_modules/\n"), 0o644); err != nil {
		t.Fatalf("write repo gitignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, lockedFile), []byte("lock-v1\n"), 0o644); err != nil {
		t.Fatalf("write bun.lock: %v", err)
	}
	initGitRepo(t, repoDir)

	manifestPath := filepath.Join(providerDir, "manifest.yaml")
	base := fingerprintOf(t, manifestPath)

	if err := os.WriteFile(filepath.Join(providerDir, lockedFile), []byte("lock-v2\n"), 0o644); err != nil {
		t.Fatalf("edit bun.lock: %v", err)
	}
	if got := fingerprintOf(t, manifestPath); got == base {
		t.Fatal("explicit gitignored input bun.lock was not hashed; explicit beats ignore regressed")
	}
}

// TestFingerprintNoEnclosingRepo asserts that with no enclosing git repo (and
// thus no .gitignore), the fingerprint succeeds and excludes only the declared
// output and .git, hashing everything else.
func TestFingerprintNoEnclosingRepo(t *testing.T) {
	t.Parallel()

	providerDir := t.TempDir()
	writeBuildableSourceProviderTree(t, providerDir, "github.com/acme/apps/alpha", ".gestaltd/bin/alpha", nil)
	if err := os.WriteFile(filepath.Join(providerDir, "extra.txt"), []byte("tracked\n"), 0o644); err != nil {
		t.Fatalf("write extra.txt: %v", err)
	}

	manifestPath := filepath.Join(providerDir, "manifest.yaml")
	base := fingerprintOf(t, manifestPath)

	if err := os.WriteFile(filepath.Join(providerDir, "node_modules"), []byte("would-be-ignored-in-repo"), 0o644); err != nil {
		t.Fatalf("write node_modules file: %v", err)
	}
	if got := fingerprintOf(t, manifestPath); got == base {
		t.Fatal("fingerprint unchanged after adding node_modules with no enclosing repo; outside a repo nothing should be gitignored")
	}
}

// writeInstallOnlyProviderTree writes a provider dir whose manifest declares an
// install phase with inputs but no build, so fingerprintLocalSourceDigest takes
// the DirectoryDigest + foldInstallInputsDigest path.
func writeInstallOnlyProviderTree(t *testing.T, dir, source string, installInputs []string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir provider dir: %v", err)
	}
	manifest := &providermanifestv1.Manifest{
		Source:      source,
		Version:     "1.0.0",
		Kind:        providermanifestv1.KindApp,
		DisplayName: "FP",
		Spec:        &providermanifestv1.Spec{},
		Install: &providermanifestv1.SourceInstall{
			Command: []string{"true"},
			Inputs:  installInputs,
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: "provider"},
	}
	data, err := encodeSourceManifestForTest(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	// The no-build path executes the provider to generate a static catalog, so
	// the artifact must be a runnable command that writes a minimal catalog.
	providerScript := "#!/bin/sh\ncat > \"$GESTALT_APP_WRITE_CATALOG\" <<'CATALOG'\nname: alpha\noperations:\n  - id: ping\n    method: GET\nCATALOG\n"
	if err := os.WriteFile(filepath.Join(dir, "provider"), []byte(providerScript), 0o755); err != nil {
		t.Fatalf("write provider: %v", err)
	}
	buildOutputDir := filepath.Join(dir, ".gestaltd", "bin")
	if err := os.MkdirAll(buildOutputDir, 0o755); err != nil {
		t.Fatalf("mkdir build output dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(buildOutputDir, "alpha"), []byte(providerScript), 0o755); err != nil {
		t.Fatalf("write build output: %v", err)
	}
}

// TestFingerprintInstallInputsDirHonorsMatcher asserts a directory install
// input in the no-build path is filtered by the matcher: gitignored files
// under it do not change the fingerprint.
func TestFingerprintInstallInputsDirHonorsMatcher(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	providerDir := filepath.Join(repoDir, "apps", "alpha")
	writeInstallOnlyProviderTree(t, providerDir, "github.com/acme/apps/alpha", []string{"deps"})
	if err := os.WriteFile(filepath.Join(repoDir, ".gitignore"), []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatalf("write repo gitignore: %v", err)
	}
	depsDir := filepath.Join(providerDir, "deps")
	if err := os.MkdirAll(filepath.Join(depsDir, "node_modules"), 0o755); err != nil {
		t.Fatalf("mkdir deps/node_modules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(depsDir, "real.txt"), []byte("real\n"), 0o644); err != nil {
		t.Fatalf("write deps/real.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(depsDir, "node_modules", "noise.js"), []byte("noise"), 0o644); err != nil {
		t.Fatalf("write deps/node_modules/noise.js: %v", err)
	}
	initGitRepo(t, repoDir)

	manifestPath := filepath.Join(providerDir, "manifest.yaml")
	base := fingerprintOf(t, manifestPath)

	if err := os.WriteFile(filepath.Join(depsDir, "node_modules", "noise.js"), []byte("more noise"), 0o644); err != nil {
		t.Fatalf("edit deps/node_modules/noise.js: %v", err)
	}
	if got := fingerprintOf(t, manifestPath); got != base {
		t.Fatalf("fingerprint changed after editing gitignored file under a directory install input: got %q, want %q", got, base)
	}
}

// TestFingerprintBuildInputsDirHonorsMatcher is the build-path analog: a
// directory listed in build inputs is walked through the matcher, so gitignored
// noise under it does not change the fingerprint while a tracked file there does.
func TestFingerprintBuildInputsDirHonorsMatcher(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	providerDir := filepath.Join(repoDir, "apps", "alpha")
	writeBuildableSourceProviderTree(t, providerDir, "github.com/acme/apps/alpha", ".gestaltd/bin/alpha", []string{"deps"})
	if err := os.WriteFile(filepath.Join(repoDir, ".gitignore"), []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatalf("write repo gitignore: %v", err)
	}
	depsDir := filepath.Join(providerDir, "deps")
	if err := os.MkdirAll(filepath.Join(depsDir, "node_modules"), 0o755); err != nil {
		t.Fatalf("mkdir deps/node_modules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(depsDir, "real.txt"), []byte("real\n"), 0o644); err != nil {
		t.Fatalf("write deps/real.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(depsDir, "node_modules", "noise.js"), []byte("noise"), 0o644); err != nil {
		t.Fatalf("write deps/node_modules/noise.js: %v", err)
	}
	initGitRepo(t, repoDir)

	manifestPath := filepath.Join(providerDir, "manifest.yaml")
	base := fingerprintOf(t, manifestPath)

	if err := os.WriteFile(filepath.Join(depsDir, "node_modules", "noise.js"), []byte("more noise"), 0o644); err != nil {
		t.Fatalf("edit deps/node_modules/noise.js: %v", err)
	}
	if got := fingerprintOf(t, manifestPath); got != base {
		t.Fatalf("fingerprint changed after editing gitignored file under a directory build input: got %q, want %q", got, base)
	}

	if err := os.WriteFile(filepath.Join(depsDir, "real.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("edit deps/real.txt: %v", err)
	}
	if got := fingerprintOf(t, manifestPath); got == base {
		t.Fatal("fingerprint unchanged after editing a tracked file under a directory build input")
	}
}
