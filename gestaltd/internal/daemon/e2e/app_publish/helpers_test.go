package app_publish

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

const (
	releaseTestAppName = "release-test"
	releaseTestSource  = "github.com/testowner/apps/apps/release-test"
)

func gestaltdCommand(args ...string) *exec.Cmd {
	return exec.Command(gestaltdBin, args...)
}

func runAppCommandStreams(workDir string, args ...string) ([]byte, []byte, error) {
	return runGestaltdCommandStreams(workDir, append([]string{"app"}, args...)...)
}

func runGestaltdCommandStreams(workDir string, args ...string) ([]byte, []byte, error) {
	cmd := gestaltdCommand(args...)
	cmd.Dir = workDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func runProviderPackageCommand(t *testing.T, pluginDir string, args ...string) {
	t.Helper()

	cmd := gestaltdCommand(append([]string{"provider", "package"}, args...)...)
	cmd.Dir = pluginDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("provider package failed: %v\n%s", err, out)
	}
}

func runProviderPublishCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s failed: %w\n%s%s", name, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), nil
}

func initProviderPublishGitRepo(t *testing.T, dir, remote string) {
	t.Helper()

	for _, args := range [][]string{
		{"init"},
		{"remote", "add", "origin", remote},
	} {
		out, err := runProviderPublishCommand("git", append([]string{"-C", dir}, args...)...)
		if err != nil {
			t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

func newAppRegistryPublishFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, "apps", releaseTestAppName)
	testutil.CopyExampleProviderPlugin(t, pluginDir)
	artifactRel := ".gestaltd/bin/" + releaseTestAppName
	writeGoAppBuildFixture(t, pluginDir, "github.com/valon-technologies/gestalt/testdata/provider-go", releaseTestAppName, artifactRel)
	writeReleaseTestManifestFormat(t, pluginDir, "manifest.yaml", &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      releaseTestSource,
		Version:     "0.0.1",
		DisplayName: "Release Test",
		Spec:        hostedHTTPMetadataSpec("echo"),
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"go.mod", "go.sum", "provider.go", "cmd", "build.sh"},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
	})
	return pluginDir
}

func platformArchiveNameForTest(appName, version, goos, goarch string) string {
	return fmt.Sprintf("gestalt-app-%s_v%s_%s_%s.tar.gz", appName, version, goos, goarch)
}

func hostedHTTPMetadataSpec(target string) *providermanifestv1.Spec {
	return &providermanifestv1.Spec{
		Connections: map[string]*providermanifestv1.ManifestConnectionDef{
			"default": {
				Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
			},
		},
		SecuritySchemes: map[string]*providermanifestv1.HTTPSecurityScheme{
			"signed": {
				Type:            providermanifestv1.HTTPSecuritySchemeTypeHMAC,
				Secret:          &providermanifestv1.HTTPSecretRef{Env: "REQUEST_SIGNING_SECRET"},
				SignatureHeader: "X-Request-Signature",
				SignaturePrefix: "v0=",
				PayloadTemplate: "v0:{header:X-Request-Timestamp}:{raw_body}",
				TimestampHeader: "X-Request-Timestamp",
				MaxAgeSeconds:   300,
			},
		},
		HTTP: map[string]*providermanifestv1.HTTPBinding{
			"command": {
				Path:     "/command",
				Method:   http.MethodPost,
				Security: "signed",
				Target:   target,
				RequestBody: &providermanifestv1.HTTPRequestBody{
					Required: true,
					Content: map[string]*providermanifestv1.HTTPMediaType{
						"application/x-www-form-urlencoded": {},
					},
				},
			},
		},
	}
}

func writeGoAppBuildFixture(t *testing.T, providerDir, importPath, appName, artifactRel string) {
	t.Helper()

	mainSource := fmt.Sprintf(`package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	providerpkg %q
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := gestalt.ServeProvider(ctx, providerpkg.New(), providerpkg.Router.WithName(%q)); err != nil {
		fmt.Fprintf(os.Stderr, "error: %%v\n", err)
		os.Exit(1)
	}
}
`, importPath, appName)
	writeTestFile(t, providerDir, filepath.Join("cmd", "provider", "main.go"), []byte(mainSource), 0o644)
	buildScript := fmt.Sprintf("mkdir -p %q\ngo build -o %q ./cmd/provider\n", filepath.ToSlash(filepath.Dir(artifactRel)), artifactRel)
	writeTestFile(t, providerDir, "build.sh", []byte(buildScript), 0o755)
}

func writeReleaseTestManifestFormat(t *testing.T, dir, manifestFile string, manifest *providermanifestv1.Manifest) {
	t.Helper()

	clone := *manifest
	clone.Entrypoint = nil
	data, err := providerpkg.EncodeSourceManifestFormat(&clone, providerpkg.ManifestFormatFromPath(manifestFile))
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat(%s): %v", manifestFile, err)
	}
	writeTestFile(t, dir, manifestFile, data, 0o644)
	if manifest.Kind == providermanifestv1.KindApp && manifest.Spec != nil {
		writeTestFile(t, dir, providerpkg.StaticCatalogFile, []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0o644)
	}
}

func writeTestFile(t *testing.T, dir, rel string, data []byte, mode os.FileMode) {
	t.Helper()

	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", rel, err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatalf("WriteFile(%s): %v", rel, err)
	}
}
