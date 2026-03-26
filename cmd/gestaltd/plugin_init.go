package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/valon-technologies/gestalt/internal/pluginpkg"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/pluginmanifest/v1"
)

const manifestTemplate = "plugin.json.tmpl"

func runPluginInit(args []string) error {
	fs := flag.NewFlagSet("gestaltd plugin init", flag.ContinueOnError)
	fs.Usage = func() { printPluginInitUsage(fs.Output()) }
	id := fs.String("id", "", "plugin ID (publisher/name)")
	kind := fs.String("kind", "", "plugin kind (provider or runtime)")
	output := fs.String("output", "", "output directory")
	binary := fs.String("binary", "", "path to pre-built binary (generates packageable manifest)")
	targetOS := fs.String("os", runtime.GOOS, "target OS for the artifact")
	targetArch := fs.String("arch", runtime.GOARCH, "target architecture for the artifact")
	version := fs.String("version", "0.0.0-alpha.1", "manifest version")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *id == "" || *kind == "" || *output == "" {
		return fmt.Errorf("usage: gestaltd plugin init --id ID --kind KIND --output DIR [--binary PATH]")
	}
	if *kind != pluginmanifestv1.KindProvider && *kind != pluginmanifestv1.KindRuntime {
		return fmt.Errorf("kind must be %q or %q", pluginmanifestv1.KindProvider, pluginmanifestv1.KindRuntime)
	}

	if err := os.MkdirAll(*output, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	if *binary != "" {
		return initWithBinary(*id, *kind, *version, *binary, *targetOS, *targetArch, *output)
	}
	return initTemplate(*id, *kind, *version, *targetOS, *targetArch, *output)
}

func initWithBinary(id, kind, version, binaryPath, targetOS, targetArch, outputDir string) error {
	artifactRel := artifactRelPath(kind, targetOS, targetArch)
	artifactAbs := filepath.Join(outputDir, filepath.FromSlash(artifactRel))

	if err := os.MkdirAll(filepath.Dir(artifactAbs), 0755); err != nil {
		return fmt.Errorf("create artifact dir: %w", err)
	}

	digest, err := copyAndDigest(binaryPath, artifactAbs)
	if err != nil {
		return fmt.Errorf("copy binary: %w", err)
	}

	manifest := buildManifest(id, kind, version, targetOS, targetArch, artifactRel, digest)
	data, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, pluginpkg.ManifestFile), data, 0644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	_, _ = fmt.Fprintf(os.Stdout, "initialized %s at %s (packageable)\n", id, outputDir)
	return nil
}

func initTemplate(id, kind, version, targetOS, targetArch, outputDir string) error {
	artifactRel := artifactRelPath(kind, targetOS, targetArch)
	manifest := buildManifest(id, kind, version, targetOS, targetArch, artifactRel, "BUILD_YOUR_BINARY_THEN_RERUN_WITH_--binary")

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal template: %w", err)
	}

	tmplPath := filepath.Join(outputDir, manifestTemplate)
	if err := os.WriteFile(tmplPath, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("write template: %w", err)
	}

	_, _ = fmt.Fprintf(os.Stdout, "initialized %s at %s (template only, not packageable)\n", id, outputDir)
	_, _ = fmt.Fprintf(os.Stdout, "build your binary and re-run with --binary to generate a packageable manifest\n")
	return nil
}

func buildManifest(id, kind, version, targetOS, targetArch, artifactRel, digest string) *pluginmanifestv1.Manifest {
	m := &pluginmanifestv1.Manifest{
		SchemaVersion: pluginmanifestv1.SchemaVersion,
		ID:            id,
		Version:       version,
		Kinds:         []string{kind},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     targetOS,
				Arch:   targetArch,
				Path:   artifactRel,
				SHA256: digest,
			},
		},
	}

	switch kind {
	case pluginmanifestv1.KindProvider:
		m.Provider = &pluginmanifestv1.Provider{
			Protocol: pluginmanifestv1.ProtocolRange{Min: 1, Max: 1},
		}
		m.Entrypoints.Provider = &pluginmanifestv1.Entrypoint{
			ArtifactPath: artifactRel,
		}
	case pluginmanifestv1.KindRuntime:
		m.Entrypoints.Runtime = &pluginmanifestv1.Entrypoint{
			ArtifactPath: artifactRel,
		}
	}

	return m
}

func artifactRelPath(kind, targetOS, targetArch string) string {
	entryName := kind
	return filepath.ToSlash(filepath.Join("artifacts", targetOS, targetArch, entryName))
}

func copyAndDigest(src, dst string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return "", err
	}

	h := sha256.New()
	w := io.MultiWriter(out, h)
	if _, err := io.Copy(w, in); err != nil {
		_ = out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func printPluginInitUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd plugin init --id ID --kind KIND --output DIR [--binary PATH]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Scaffold a plugin package directory. With --binary, generates a packageable")
	writeUsageLine(w, "manifest with the correct SHA256 digest. Without --binary, generates a template.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --id        Plugin ID (publisher/name)")
	writeUsageLine(w, "  --kind      Plugin kind (provider or runtime)")
	writeUsageLine(w, "  --output    Output directory")
	writeUsageLine(w, "  --binary    Path to pre-built binary (optional)")
	writeUsageLine(w, "  --os        Target OS (default: current platform)")
	writeUsageLine(w, "  --arch      Target architecture (default: current platform)")
	writeUsageLine(w, "  --version   Manifest version (default: 0.0.0-alpha.1)")
}
