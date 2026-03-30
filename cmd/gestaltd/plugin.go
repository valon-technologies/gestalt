package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/valon-technologies/gestalt/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/internal/pluginsource"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/manifest/v1"
)

func runPlugin(args []string) error {
	if len(args) == 0 {
		printPluginUsage(os.Stderr)
		return flag.ErrHelp
	}

	switch args[0] {
	case "-h", "--help", "help":
		printPluginUsage(os.Stderr)
		return flag.ErrHelp
	case "package":
		return runPluginPackage(args[1:])
	default:
		return fmt.Errorf("unknown plugin command %q", args[0])
	}
}

func runPluginPackage(args []string) error {
	fs := flag.NewFlagSet("gestaltd plugin package", flag.ContinueOnError)
	fs.Usage = func() { printPluginPackageUsage(fs.Output()) }
	input := fs.String("input", "", "path to plugin manifest or build directory")
	output := fs.String("output", "", "path to write the packaged archive")
	binary := fs.String("binary", "", "path to pre-built binary (scaffolds manifest automatically)")
	source := fs.String("source", "", "plugin source (github.com/owner/repo/plugin), required with --binary")
	kind := fs.String("kind", "provider", "plugin kind (provider or runtime), used with --binary")
	targetOS := fs.String("os", runtime.GOOS, "target OS for the artifact, used with --binary")
	targetArch := fs.String("arch", runtime.GOARCH, "target architecture, used with --binary")
	version := fs.String("version", "0.0.0-alpha.1", "manifest version, used with --binary")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	if *binary != "" {
		return packageFromBinary(*binary, *source, *kind, *version, *targetOS, *targetArch, *output)
	}
	return packageFromDir(*input, *output)
}

func packageFromDir(input, output string) error {
	if input == "" || output == "" {
		return fmt.Errorf("usage: gestaltd plugin package --input PATH --output PATH")
	}

	sourceDir := input
	if info, err := os.Stat(input); err != nil {
		return err
	} else if !info.IsDir() {
		if filepath.Base(input) != pluginpkg.ManifestFile {
			return fmt.Errorf("plugin package input must be a directory or %s, got %q", pluginpkg.ManifestFile, input)
		}
		sourceDir = filepath.Dir(input)
	}

	if err := pluginpkg.CreatePackageFromDir(sourceDir, output); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "packaged %s -> %s\n", sourceDir, output)
	return nil
}

func packageFromBinary(binaryPath, source, kind, version, targetOS, targetArch, output string) error {
	if source == "" {
		return fmt.Errorf("usage: gestaltd plugin package --binary PATH --source SOURCE --output PATH")
	}
	if output == "" {
		return fmt.Errorf("--output is required")
	}
	if kind != pluginmanifestv1.KindProvider && kind != pluginmanifestv1.KindRuntime {
		return fmt.Errorf("kind must be %q or %q", pluginmanifestv1.KindProvider, pluginmanifestv1.KindRuntime)
	}

	if _, err := pluginsource.Parse(source); err != nil {
		return fmt.Errorf("invalid --source: %w", err)
	}
	if err := pluginsource.ValidateVersion(version); err != nil {
		return fmt.Errorf("invalid --version for source plugin: %w", err)
	}

	workDir, err := os.MkdirTemp("", "gestalt-plugin-pkg-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	artifactRel := filepath.ToSlash(filepath.Join("artifacts", targetOS, targetArch, kind))
	artifactAbs := filepath.Join(workDir, filepath.FromSlash(artifactRel))

	if err := os.MkdirAll(filepath.Dir(artifactAbs), 0755); err != nil {
		return err
	}

	digest, err := copyAndDigest(binaryPath, artifactAbs)
	if err != nil {
		return fmt.Errorf("copying binary: %w", err)
	}

	manifest := buildManifest(source, kind, version, targetOS, targetArch, artifactRel, digest)
	data, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, pluginpkg.ManifestFile), data, 0644); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}

	if err := pluginpkg.CreatePackageFromDir(workDir, output); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "packaged %s (%s) -> %s\n", source, binaryPath, output)
	return nil
}

func buildManifest(source, kind, version, targetOS, targetArch, artifactRel, digest string) *pluginmanifestv1.Manifest {
	m := newManifestSkeleton(kind, version, targetOS, targetArch, artifactRel, digest)
	m.SchemaVersion = pluginmanifestv1.SchemaVersion
	m.Source = source
	return m
}

func newManifestSkeleton(kind, version, targetOS, targetArch, artifactRel, digest string) *pluginmanifestv1.Manifest {
	m := &pluginmanifestv1.Manifest{
		Version: version,
		Kinds:   []string{kind},
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

func printPluginUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd plugin <command> [flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  package     Package a plugin for distribution")
}

func printPluginPackageUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd plugin package --input PATH --output PATH")
	writeUsageLine(w, "  gestaltd plugin package --binary PATH --source SOURCE --output PATH")
	writeUsageLine(w, "")
	writeUsageLine(w, "Package a plugin for distribution. Use --input with an existing plugin")
	writeUsageLine(w, "directory containing a manifest, or --binary to scaffold and package")
	writeUsageLine(w, "a pre-built binary in one step.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --input     Path to plugin manifest or build directory")
	writeUsageLine(w, "  --output    Path to write the packaged archive")
	writeUsageLine(w, "  --binary    Path to pre-built binary (scaffolds manifest automatically)")
	writeUsageLine(w, "  --source    Plugin source (github.com/owner/repo/plugin), required with --binary")
	writeUsageLine(w, "  --kind      Plugin kind: provider or runtime (default: provider)")
	writeUsageLine(w, "  --os        Target OS (default: current platform)")
	writeUsageLine(w, "  --arch      Target architecture (default: current platform)")
	writeUsageLine(w, "  --version   Manifest version (default: 0.0.0-alpha.1)")
}
