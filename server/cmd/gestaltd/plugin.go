package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
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
	case "release":
		return runPluginRelease(args[1:])
	default:
		return fmt.Errorf("unknown plugin command %q", args[0])
	}
}

func runPluginPackage(args []string) error {
	fs := flag.NewFlagSet("gestaltd plugin package", flag.ContinueOnError)
	fs.Usage = func() { printPluginPackageUsage(fs.Output()) }
	input := fs.String("input", "", "path to plugin manifest or build directory")
	output := fs.String("output", "", "output path (directory, or .tar.gz for archive)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
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

	if isArchiveOutput(output) {
		if err := pluginpkg.CreatePackageFromDir(sourceDir, output); err != nil {
			return err
		}
	} else {
		if err := pluginpkg.CopyPackageDir(sourceDir, output); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintf(os.Stdout, "packaged %s -> %s\n", sourceDir, output)
	return nil
}

const defaultPlatforms = "darwin/amd64,darwin/arm64,linux/amd64,linux/arm64"
const defaultReleaseOutputDir = "dist/"
const releaseCmdBuildTarget = "./cmd"
const releaseRootBuildTarget = "."
const releaseBinaryPrefix = "gestalt-plugin-"
const windowsOS = "windows"
const windowsExecutableSuffix = ".exe"

type releasePlatform struct {
	GOOS   string
	GOARCH string
}

var errNoGoMainPackage = errors.New("no Go main package found")

func runPluginRelease(args []string) error {
	fs := flag.NewFlagSet("gestaltd plugin release", flag.ContinueOnError)
	fs.Usage = func() { printPluginReleaseUsage(fs.Output()) }
	version := fs.String("version", "", "semantic version string (required)")
	outputDir := fs.String("output", defaultReleaseOutputDir, "output directory")
	platforms := fs.String("platform", defaultPlatforms, "comma-separated platforms (os/arch)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *version == "" {
		return fmt.Errorf("--version is required")
	}

	if err := pluginsource.ValidateVersion(*version); err != nil {
		return fmt.Errorf("invalid --version: %w", err)
	}

	manifestPath, err := pluginpkg.FindManifestFile(".")
	if err != nil {
		return err
	}
	_, srcManifest, err := pluginpkg.ReadManifestFile(manifestPath)
	if err != nil {
		return fmt.Errorf("decode %s: %w", manifestPath, err)
	}

	src, err := pluginsource.Parse(srcManifest.Source)
	if err != nil {
		return fmt.Errorf("invalid source in manifest: %w", err)
	}

	if err := validateReleaseOutputDir(srcManifest, ".", *outputDir); err != nil {
		return err
	}
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	platList, err := parseReleasePlatforms(*platforms)
	if err != nil {
		return err
	}
	pluginDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	compiled := false
	for _, plat := range platList {
		if _, err := detectGoBuildTarget(pluginDir, src.Plugin, plat.GOOS, plat.GOARCH); err == nil {
			compiled = true
			break
		} else if !errors.Is(err, errNoGoMainPackage) {
			return fmt.Errorf("detect Go build target for %s/%s: %w", plat.GOOS, plat.GOARCH, err)
		}
	}
	if !compiled && releaseRequiresBuildTarget(srcManifest) {
		return errNoGoMainPackage
	}

	var archivePaths []string
	if compiled {
		for _, plat := range platList {
			buildTarget, err := detectGoBuildTarget(pluginDir, src.Plugin, plat.GOOS, plat.GOARCH)
			if err != nil {
				return fmt.Errorf("detect Go build target for %s/%s: %w", plat.GOOS, plat.GOARCH, err)
			}
			archivePath, err := buildPlatformArchive(srcManifest, src, *version, buildTarget, plat, pluginDir, *outputDir)
			if err != nil {
				return fmt.Errorf("build %s/%s: %w", plat.GOOS, plat.GOARCH, err)
			}
			archivePaths = append(archivePaths, archivePath)
		}
	} else {
		archivePath, err := buildSourceArchive(srcManifest, src, *version, *outputDir)
		if err != nil {
			return err
		}
		archivePaths = append(archivePaths, archivePath)
	}

	if err := writeChecksums(filepath.Join(*outputDir, src.ChecksumsName(*version)), archivePaths); err != nil {
		return fmt.Errorf("write checksums: %w", err)
	}

	return nil
}

func buildPlatformArchive(srcManifest *pluginmanifestv1.Manifest, src pluginsource.Source, version, buildTarget string, plat releasePlatform, pluginDir, outputDir string) (string, error) {
	stagingDir, err := os.MkdirTemp("", "gestalt-release-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(stagingDir) }()

	binaryName := releaseBinaryName(src.Plugin, plat.GOOS)
	binaryPath := filepath.Join(stagingDir, binaryName)

	cmd := exec.Command("go", "-C", pluginDir, "build", "-trimpath", "-ldflags", "-s -w", "-o", binaryPath, buildTarget)
	cmd.Dir = pluginDir
	cmd.Env = append(goPlatformEnv(pluginDir, plat.GOOS, plat.GOARCH), "CGO_ENABLED=0")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go build: %w", err)
	}

	digest, err := fileSHA256Hex(binaryPath)
	if err != nil {
		return "", fmt.Errorf("hash binary: %w", err)
	}

	manifest, err := buildReleaseManifest(srcManifest, version, binaryName, plat, digest)
	if err != nil {
		return "", err
	}

	data, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		return "", fmt.Errorf("encode manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, pluginpkg.ManifestFile), data, 0644); err != nil {
		return "", err
	}

	if err := copyReleasePackageFiles(srcManifest, pluginDir, stagingDir, false); err != nil {
		return "", err
	}

	archiveName := src.PlatformAssetName(version, plat.GOOS, plat.GOARCH)
	archivePath := filepath.Join(outputDir, archiveName)
	if err := pluginpkg.CreatePackageFromDir(stagingDir, archivePath); err != nil {
		return "", err
	}

	_, _ = fmt.Fprintf(os.Stdout, "created %s\n", archivePath)
	return archivePath, nil
}

func detectGoBuildTarget(dir, pluginName, goos, goarch string) (string, error) {
	var fatalErr error
	var targets []string
	if pluginName != "" {
		targets = append(targets, releaseCmdBuildTarget+"/"+pluginName)
	}
	targets = append(targets, releaseCmdBuildTarget, releaseRootBuildTarget)
	for _, target := range targets {
		name, err := goPackageName(dir, target, goos, goarch)
		if err != nil {
			if isMissingGoPackageError(err) {
				continue
			}
			if fatalErr == nil {
				fatalErr = fmt.Errorf("%s: %w", target, err)
			}
			continue
		}
		if name == "main" {
			return target, nil
		}
	}

	cmdTargets, err := goMainPackageTargets(dir, "./cmd/...", goos, goarch)
	switch {
	case err != nil:
		if !isMissingGoPackageError(err) && fatalErr == nil {
			fatalErr = err
		}
	case pluginName != "":
		var namedTargets []string
		for _, target := range cmdTargets {
			if path.Base(target) == pluginName {
				namedTargets = append(namedTargets, target)
			}
		}
		if len(namedTargets) == 1 {
			return namedTargets[0], nil
		}
		if len(namedTargets) > 1 {
			return "", fmt.Errorf("multiple Go main packages found under ./cmd matching plugin %q for %s/%s: %s", pluginName, goos, goarch, strings.Join(namedTargets, ", "))
		}
	}

	if fatalErr != nil {
		return "", fatalErr
	}
	return "", errNoGoMainPackage
}

func goPackageName(dir, target, goos, goarch string) (string, error) {
	cmd := exec.Command("go", "-C", dir, "list", "-f", "{{.Name}}", target)
	cmd.Dir = dir
	cmd.Env = goPlatformEnv(dir, goos, goarch)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

func goMainPackageTargets(dir, pattern, goos, goarch string) ([]string, error) {
	cmd := exec.Command("go", "-C", dir, "list", "-f", "{{if eq .Name \"main\"}}{{.Dir}}{{end}}", pattern)
	cmd.Dir = dir
	cmd.Env = goPlatformEnv(dir, goos, goarch)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s", strings.TrimSpace(stderr.String()))
	}

	var targets []string
	seen := map[string]struct{}{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		rel, err := filepath.Rel(dir, line)
		if err != nil {
			return nil, fmt.Errorf("compute relative path for %q: %w", line, err)
		}
		target := filepath.ToSlash("." + string(filepath.Separator) + rel)
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}
	return targets, nil
}

func parseReleasePlatforms(value string) ([]releasePlatform, error) {
	parts := strings.Split(value, ",")
	platforms := make([]releasePlatform, 0, len(parts))
	for _, part := range parts {
		plat := strings.TrimSpace(part)
		pieces := strings.SplitN(plat, "/", 2)
		if len(pieces) != 2 || pieces[0] == "" || pieces[1] == "" {
			return nil, fmt.Errorf("invalid platform %q, expected os/arch", plat)
		}
		platforms = append(platforms, releasePlatform{
			GOOS:   pieces[0],
			GOARCH: pieces[1],
		})
	}
	return platforms, nil
}

func buildSourceArchive(srcManifest *pluginmanifestv1.Manifest, src pluginsource.Source, version, outputDir string) (string, error) {
	stagingDir, err := os.MkdirTemp("", "gestalt-release-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(stagingDir) }()

	manifest, err := cloneManifest(srcManifest)
	if err != nil {
		return "", fmt.Errorf("clone manifest: %w", err)
	}
	manifest.Version = version

	data, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		return "", fmt.Errorf("encode manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, pluginpkg.ManifestFile), data, 0644); err != nil {
		return "", err
	}

	if err := validateReleaseArtifactDigests(srcManifest, "."); err != nil {
		return "", err
	}
	if err := copyReleasePackageFiles(srcManifest, ".", stagingDir, true); err != nil {
		return "", err
	}

	archiveName := src.AssetName(version)
	archivePath := filepath.Join(outputDir, archiveName)
	if err := pluginpkg.CreatePackageFromDir(stagingDir, archivePath); err != nil {
		return "", err
	}

	_, _ = fmt.Fprintf(os.Stdout, "created %s\n", archivePath)
	return archivePath, nil
}

func buildReleaseManifest(srcManifest *pluginmanifestv1.Manifest, version, binaryName string, plat releasePlatform, digest string) (*pluginmanifestv1.Manifest, error) {
	manifest, err := cloneManifest(srcManifest)
	if err != nil {
		return nil, fmt.Errorf("clone manifest: %w", err)
	}
	manifest.Version = version
	manifest.Artifacts = []pluginmanifestv1.Artifact{
		{OS: plat.GOOS, Arch: plat.GOARCH, Path: binaryName, SHA256: digest},
	}

	for _, kind := range manifest.Kinds {
		switch kind {
		case pluginmanifestv1.KindProvider:
			if manifest.Entrypoints.Provider == nil {
				manifest.Entrypoints.Provider = &pluginmanifestv1.Entrypoint{}
			}
			manifest.Entrypoints.Provider.ArtifactPath = binaryName
		case pluginmanifestv1.KindRuntime:
			if manifest.Entrypoints.Runtime == nil {
				manifest.Entrypoints.Runtime = &pluginmanifestv1.Entrypoint{}
			}
			manifest.Entrypoints.Runtime.ArtifactPath = binaryName
		}
	}

	return manifest, nil
}

func releaseBinaryName(pluginName, goos string) string {
	binaryName := releaseBinaryPrefix + pluginName
	if goos == windowsOS {
		return binaryName + windowsExecutableSuffix
	}
	return binaryName
}

func goCommandEnv(dir string) []string {
	return envWithPWD(os.Environ(), dir)
}

func goPlatformEnv(dir, goos, goarch string) []string {
	env := goCommandEnv(dir)
	if goos != "" {
		env = append(env, "GOOS="+goos)
	}
	if goarch != "" {
		env = append(env, "GOARCH="+goarch)
	}
	return env
}

func envWithPWD(env []string, dir string) []string {
	filtered := env[:0]
	for _, entry := range env {
		if !strings.HasPrefix(entry, "PWD=") {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, "PWD="+dir)
}

func isMissingGoPackageError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "directory not found") ||
		strings.Contains(msg, "no such file or directory") ||
		strings.Contains(msg, "go.mod file not found") ||
		strings.Contains(msg, "cannot find main module") ||
		strings.Contains(msg, "does not contain main module or its selected dependencies") ||
		strings.Contains(msg, "no Go files") ||
		strings.Contains(msg, "build constraints exclude all Go files") ||
		strings.Contains(msg, "matched no packages")
}

func releaseRequiresBuildTarget(manifest *pluginmanifestv1.Manifest) bool {
	if manifest == nil {
		return false
	}
	for _, kind := range manifest.Kinds {
		switch kind {
		case pluginmanifestv1.KindRuntime:
			return true
		case pluginmanifestv1.KindProvider:
			if manifest.Provider == nil || !manifest.Provider.IsDeclarative() {
				return true
			}
		}
	}
	return false
}

func writeChecksums(checksumPath string, archivePaths []string) error {
	var lines []string
	for _, archivePath := range archivePaths {
		digest, err := fileSHA256Hex(archivePath)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("%s  %s", digest, filepath.Base(archivePath)))
	}

	if len(lines) == 0 {
		return nil
	}

	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(checksumPath, []byte(content), 0644); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "created %s\n", checksumPath)
	return nil
}

func fileSHA256Hex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func cloneManifest(manifest *pluginmanifestv1.Manifest) (*pluginmanifestv1.Manifest, error) {
	if manifest == nil {
		return nil, nil
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}

	var cloned pluginmanifestv1.Manifest
	if err := json.Unmarshal(data, &cloned); err != nil {
		return nil, err
	}
	return &cloned, nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyReleaseFile(p, target)
	})
}

func copyReleaseFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func copyReleasePackageFiles(manifest *pluginmanifestv1.Manifest, sourceDir, stagingDir string, includeArtifacts bool) error {
	if manifest == nil {
		return nil
	}

	copied := make(map[string]struct{})
	copyPath := func(rel string, optional bool) error {
		if rel == "" {
			return nil
		}

		cleanRel, err := normalizeReleasePath(rel)
		if err != nil {
			return err
		}
		if _, seen := copied[cleanRel]; seen {
			return nil
		}
		copied[cleanRel] = struct{}{}

		srcPath := filepath.Join(sourceDir, filepath.FromSlash(cleanRel))
		info, err := os.Stat(srcPath)
		if err != nil {
			if optional && os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("stat support path %s: %w", rel, err)
		}

		dstPath := filepath.Join(stagingDir, filepath.FromSlash(cleanRel))
		if info.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return fmt.Errorf("copy support directory %s: %w", rel, err)
			}
			return nil
		}
		if err := copyReleaseFile(srcPath, dstPath); err != nil {
			return fmt.Errorf("copy support file %s: %w", rel, err)
		}
		return nil
	}

	if err := copyPath(manifest.IconFile, false); err != nil {
		return err
	}
	if manifest.Provider != nil {
		if err := copyPath(manifest.Provider.ConfigSchemaPath, false); err != nil {
			return err
		}
	}
	if manifest.WebUI != nil {
		if err := copyPath(manifest.WebUI.AssetRoot, false); err != nil {
			return err
		}
	}
	if includeArtifacts {
		for _, artifact := range manifest.Artifacts {
			if err := copyPath(artifact.Path, false); err != nil {
				return err
			}
		}
	}
	if err := copyPath(pluginpkg.RuntimeConfigSchemaPath, true); err != nil {
		return err
	}
	return nil
}

func validateReleaseArtifactDigests(manifest *pluginmanifestv1.Manifest, sourceDir string) error {
	if manifest == nil {
		return nil
	}

	for _, artifact := range manifest.Artifacts {
		cleanPath, err := normalizeReleasePath(artifact.Path)
		if err != nil {
			return err
		}

		digest, err := fileSHA256Hex(filepath.Join(sourceDir, filepath.FromSlash(cleanPath)))
		if err != nil {
			return fmt.Errorf("hash artifact %s: %w", artifact.Path, err)
		}
		if digest != artifact.SHA256 {
			return fmt.Errorf("artifact %s sha256 mismatch: manifest=%s actual=%s", artifact.Path, artifact.SHA256, digest)
		}
	}

	return nil
}

func validateReleaseOutputDir(manifest *pluginmanifestv1.Manifest, sourceDir, outputDir string) error {
	if manifest == nil || manifest.WebUI == nil || manifest.WebUI.AssetRoot == "" {
		return nil
	}

	assetRoot, err := normalizeReleasePath(manifest.WebUI.AssetRoot)
	if err != nil {
		return err
	}

	sourceAbs, err := filepath.Abs(sourceDir)
	if err != nil {
		return fmt.Errorf("resolve plugin root: %w", err)
	}

	assetRootAbs := filepath.Join(sourceAbs, filepath.FromSlash(assetRoot))
	outputDirAbs := outputDir
	if !filepath.IsAbs(outputDirAbs) {
		outputDirAbs = filepath.Join(sourceAbs, outputDirAbs)
	}
	outputDirAbs = filepath.Clean(outputDirAbs)

	insideAssetRoot, err := pathWithinBase(outputDirAbs, assetRootAbs)
	if err != nil {
		return fmt.Errorf("compare output dir to webui asset_root: %w", err)
	}
	if insideAssetRoot {
		return fmt.Errorf("--output %q must not be inside webui.asset_root %q", outputDir, manifest.WebUI.AssetRoot)
	}

	return nil
}

func pathWithinBase(path, base string) (bool, error) {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false, err
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))), nil
}

func normalizeReleasePath(rel string) (string, error) {
	if rel == "" {
		return "", nil
	}

	cleanPath := path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	if path.IsAbs(cleanPath) || cleanPath == ".." || strings.HasPrefix(cleanPath, "../") {
		return "", fmt.Errorf("release path %q must stay within plugin root", rel)
	}
	return cleanPath, nil
}

func isArchiveOutput(path string) bool {
	return strings.HasSuffix(path, ".tar.gz") || strings.HasSuffix(path, ".tgz")
}

func printPluginUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd plugin <command> [flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  package     Package a plugin for distribution")
	writeUsageLine(w, "  release     Cross-compile and package a plugin for release")
}

func printPluginPackageUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd plugin package --input PATH --output PATH")
	writeUsageLine(w, "")
	writeUsageLine(w, "Package an existing plugin directory for distribution.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Output format is determined by the --output path: paths ending in")
	writeUsageLine(w, ".tar.gz produce an archive, all other paths produce a directory.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --input     Path to plugin manifest or build directory")
	writeUsageLine(w, "  --output    Output path (directory, or .tar.gz for archive)")
}

func printPluginReleaseUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd plugin release --version VERSION [--output DIR] [--platform PLATFORMS]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Cross-compile a plugin for multiple platforms and produce per-platform")
	writeUsageLine(w, "tar.gz archives and a checksums file. Run from the plugin source directory.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --version    Semantic version string (required)")
	writeUsageLine(w, "  --output     Output directory (default: dist/)")
	writeUsageLine(w, "  --platform   Comma-separated platforms (default: darwin/amd64,darwin/arm64,linux/amd64,linux/arm64)")
}
