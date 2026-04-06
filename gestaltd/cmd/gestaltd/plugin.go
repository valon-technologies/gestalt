package main

import (
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
	"runtime"
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
		if !pluginpkg.IsManifestFile(input) {
			return fmt.Errorf("plugin package input must be a directory or one of %s, got %q", strings.Join(pluginpkg.ManifestFiles, ", "), input)
		}
		sourceDir = filepath.Dir(input)
	}
	manifestPath, err := pluginpkg.FindManifestFile(sourceDir)
	if err != nil {
		return err
	}
	_, srcManifest, err := pluginpkg.PrepareSourceManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("prepare source manifest %s: %w", manifestPath, err)
	}
	if err := validatePackageOutputPath(sourceDir, output); err != nil {
		return err
	}

	packageDir := sourceDir
	var cleanup func()
	if srcManifest != nil && srcManifest.Entrypoints.Provider == nil && manifestHasKind(srcManifest, pluginmanifestv1.KindProvider) {
		needsMaterialization := releaseRequiresBuildTarget(srcManifest)
		if !needsMaterialization {
			hasProviderPackage, detectErr := pluginpkg.HasGoProviderPackage(sourceDir)
			if detectErr != nil {
				return fmt.Errorf("detect Go provider package in %s: %w", sourceDir, detectErr)
			}
			needsMaterialization = hasProviderPackage
		}
		if needsMaterialization {
			packageDir, cleanup, err = materializeSourcePackageDir(sourceDir, manifestPath, srcManifest)
			if err != nil {
				return err
			}
			defer cleanup()
		}
	}

	if isArchiveOutput(output) {
		if err := pluginpkg.CreatePackageFromDir(packageDir, output); err != nil {
			return err
		}
	} else {
		if err := pluginpkg.CopyPackageDir(packageDir, output); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintf(os.Stdout, "packaged %s -> %s\n", sourceDir, output)
	return nil
}

const defaultPlatforms = "darwin/amd64,darwin/arm64,linux/amd64,linux/arm64"
const defaultReleaseOutputDir = "dist/"
const releaseBinaryPrefix = "gestalt-plugin-"
const windowsOS = "windows"
const windowsExecutableSuffix = ".exe"

type releasePlatform struct {
	GOOS   string
	GOARCH string
}

type releaseBuild struct {
	platform releasePlatform
	target   string
}

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
	_, srcManifest, err := pluginpkg.PrepareSourceManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("prepare %s: %w", manifestPath, err)
	}
	manifestFormat := pluginpkg.ManifestFormatFromPath(manifestPath)
	manifestFile := filepath.Base(manifestPath)

	src, err := pluginsource.Parse(srcManifest.Source)
	if err != nil {
		return fmt.Errorf("invalid source in manifest: %w", err)
	}
	pluginName := src.Plugin

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

	builds, err := detectReleaseBuilds(".", srcManifest, platList)
	if err != nil {
		return err
	}

	var archivePaths []string
	if len(builds) > 0 {
		for _, build := range builds {
			archivePath, err := buildPlatformArchive(srcManifest, pluginName, *version, build, *outputDir, manifestFile, manifestFormat)
			if err != nil {
				return fmt.Errorf("build %s/%s: %w", build.platform.GOOS, build.platform.GOARCH, err)
			}
			archivePaths = append(archivePaths, archivePath)
		}
	} else {
		archivePath, err := buildSourceArchive(srcManifest, pluginName, *version, *outputDir, manifestFile, manifestFormat)
		if err != nil {
			return err
		}
		archivePaths = append(archivePaths, archivePath)
	}

	if err := writeChecksums(*outputDir, archivePaths); err != nil {
		return fmt.Errorf("write checksums: %w", err)
	}

	return nil
}

func detectReleaseBuilds(root string, manifest *pluginmanifestv1.Manifest, platforms []releasePlatform) ([]releaseBuild, error) {
	builds := make([]releaseBuild, 0, len(platforms))
	hasProviderKind := manifestHasKind(manifest, pluginmanifestv1.KindProvider)
	providerBuildRequired := releaseRequiresBuildTarget(manifest)
	var missingErr error

	for _, plat := range platforms {
		if hasProviderKind {
			_, err := pluginpkg.DetectGoProviderImportPath(root, plat.GOOS, plat.GOARCH)
			switch {
			case err == nil:
				builds = append(builds, releaseBuild{platform: plat})
				continue
			case errors.Is(err, pluginpkg.ErrNoGoProviderPackage):
				if providerBuildRequired && missingErr == nil {
					missingErr = fmt.Errorf("detect Go provider package for %s/%s: %w", plat.GOOS, plat.GOARCH, err)
				}
			default:
				return nil, fmt.Errorf("detect Go provider package for %s/%s: %w", plat.GOOS, plat.GOARCH, err)
			}
		}

		target, err := pluginpkg.DetectGoMainBuildTarget(root, plat.GOOS, plat.GOARCH)
		switch {
		case err == nil:
			builds = append(builds, releaseBuild{platform: plat, target: target})
		case errors.Is(err, pluginpkg.ErrNoGoMainPackage):
			continue
		default:
			return nil, fmt.Errorf("detect Go main package for %s/%s: %w", plat.GOOS, plat.GOARCH, err)
		}
	}

	if len(builds) == 0 {
		if providerBuildRequired {
			return nil, pluginpkg.ErrNoGoProviderPackage
		}
		return nil, nil
	}
	if missingErr != nil {
		return nil, missingErr
	}
	return builds, nil
}

func buildPlatformArchive(srcManifest *pluginmanifestv1.Manifest, pluginName, version string, build releaseBuild, outputDir, manifestFile, manifestFormat string) (string, error) {
	plat := build.platform
	archiveName := fmt.Sprintf("gestalt-plugin-%s_v%s_%s_%s.tar.gz", pluginName, version, plat.GOOS, plat.GOARCH)
	return createReleaseArchive(outputDir, archiveName, manifestFile, manifestFormat, func(stagingDir string) (*pluginmanifestv1.Manifest, error) {
		return prepareBuiltPackageDir(stagingDir, ".", srcManifest, version, pluginName, build)
	})
}

func createReleaseArchive(outputDir, archiveName, manifestFile, manifestFormat string, prepare func(stagingDir string) (*pluginmanifestv1.Manifest, error)) (string, error) {
	stagingDir, err := os.MkdirTemp("", "gestalt-release-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(stagingDir) }()

	manifest, err := prepare(stagingDir)
	if err != nil {
		return "", err
	}
	archivePath := filepath.Join(outputDir, archiveName)
	if err := writeReleaseManifestFile(stagingDir, manifestFile, manifestFormat, manifest); err != nil {
		return "", err
	}
	if err := pluginpkg.CreatePackageFromDir(stagingDir, archivePath); err != nil {
		return "", err
	}

	_, _ = fmt.Fprintf(os.Stdout, "created %s\n", archivePath)
	return archivePath, nil
}

func buildReleaseBinary(root, buildTarget, binaryPath string, plat releasePlatform) error {
	if buildTarget == "" {
		return pluginpkg.BuildGoProviderBinary(root, binaryPath, plat.GOOS, plat.GOARCH)
	}

	cmd := exec.Command("go", "-C", root, "build", "-mod=readonly", "-trimpath", "-ldflags", "-s -w", "-o", binaryPath, buildTarget)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+plat.GOOS, "GOARCH="+plat.GOARCH)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}
	return nil
}

func writeReleaseManifestFile(stagingDir, manifestFile, manifestFormat string, manifest *pluginmanifestv1.Manifest) error {
	data, err := pluginpkg.EncodeManifestFormat(manifest, manifestFormat)
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	return os.WriteFile(filepath.Join(stagingDir, manifestFile), data, 0644)
}

func releaseRequiresBuildTarget(manifest *pluginmanifestv1.Manifest) bool {
	return manifestHasKind(manifest, pluginmanifestv1.KindProvider) && (manifest.Provider == nil || !manifest.Provider.IsManifestBacked())
}

func manifestHasKind(manifest *pluginmanifestv1.Manifest, kind string) bool {
	if manifest == nil {
		return false
	}
	for _, manifestKind := range manifest.Kinds {
		if manifestKind == kind {
			return true
		}
	}
	return false
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

func buildSourceArchive(srcManifest *pluginmanifestv1.Manifest, pluginName, version, outputDir, manifestFile, manifestFormat string) (string, error) {
	archiveName := fmt.Sprintf("gestalt-plugin-%s_v%s.tar.gz", pluginName, version)
	return createReleaseArchive(outputDir, archiveName, manifestFile, manifestFormat, func(stagingDir string) (*pluginmanifestv1.Manifest, error) {
		manifest, err := buildSourceReleaseManifest(srcManifest, version, ".")
		if err != nil {
			return nil, err
		}
		if err := copyReleasePackageFiles(manifest, ".", stagingDir, true); err != nil {
			return nil, err
		}
		return manifest, nil
	})
}

func materializeSourcePackageDir(sourceDir, manifestPath string, srcManifest *pluginmanifestv1.Manifest) (string, func(), error) {
	src, err := pluginsource.Parse(srcManifest.Source)
	if err != nil {
		return "", nil, fmt.Errorf("invalid source in manifest: %w", err)
	}

	builds, err := detectReleaseBuilds(sourceDir, srcManifest, []releasePlatform{{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}})
	if err != nil {
		return "", nil, err
	}
	if len(builds) != 1 {
		return "", nil, fmt.Errorf("expected a single build for current platform %s/%s, got %d", runtime.GOOS, runtime.GOARCH, len(builds))
	}

	stagingDir, err := os.MkdirTemp("", "gestalt-package-*")
	if err != nil {
		return "", nil, fmt.Errorf("create package staging dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(stagingDir) }

	manifest, err := prepareBuiltPackageDir(stagingDir, sourceDir, srcManifest, srcManifest.Version, src.Plugin, builds[0])
	if err != nil {
		cleanup()
		return "", nil, err
	}
	if err := writeReleaseManifestFile(stagingDir, filepath.Base(manifestPath), pluginpkg.ManifestFormatFromPath(manifestPath), manifest); err != nil {
		cleanup()
		return "", nil, err
	}
	return stagingDir, cleanup, nil
}

func prepareBuiltPackageDir(stagingDir, sourceDir string, srcManifest *pluginmanifestv1.Manifest, version, pluginName string, build releaseBuild) (*pluginmanifestv1.Manifest, error) {
	binaryName := releaseBinaryName(pluginName, build.platform.GOOS)
	binaryPath := filepath.Join(stagingDir, binaryName)
	if err := buildReleaseBinary(sourceDir, build.target, binaryPath, build.platform); err != nil {
		return nil, err
	}

	digest, err := pluginpkg.FileSHA256(binaryPath)
	if err != nil {
		return nil, fmt.Errorf("hash binary: %w", err)
	}
	if err := copyReleasePackageFiles(srcManifest, sourceDir, stagingDir, false); err != nil {
		return nil, err
	}
	return buildReleaseManifest(srcManifest, version, binaryName, build.platform, digest)
}

func buildSourceReleaseManifest(srcManifest *pluginmanifestv1.Manifest, version, sourceDir string) (*pluginmanifestv1.Manifest, error) {
	manifest, err := cloneManifest(srcManifest)
	if err != nil {
		return nil, fmt.Errorf("clone manifest: %w", err)
	}
	manifest.Version = version

	for i, artifact := range srcManifest.Artifacts {
		digest, err := pluginpkg.FileSHA256(filepath.Join(sourceDir, filepath.FromSlash(artifact.Path)))
		if err != nil {
			return nil, fmt.Errorf("hash artifact %s: %w", artifact.Path, err)
		}
		if artifact.SHA256 != "" && artifact.SHA256 != digest {
			return nil, fmt.Errorf("artifact %s sha256 mismatch: manifest=%s actual=%s", artifact.Path, artifact.SHA256, digest)
		}
		manifest.Artifacts[i].SHA256 = digest
	}

	return manifest, nil
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
		if kind == pluginmanifestv1.KindProvider {
			if manifest.Entrypoints.Provider == nil {
				manifest.Entrypoints.Provider = &pluginmanifestv1.Entrypoint{}
			}
			manifest.Entrypoints.Provider.ArtifactPath = binaryName
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

func writeChecksums(dir string, archivePaths []string) error {
	var lines []string
	for _, archivePath := range archivePaths {
		digest, err := pluginpkg.ArchiveDigest(archivePath)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("%s  %s", digest, filepath.Base(archivePath)))
	}

	if len(lines) == 0 {
		return nil
	}

	checksumPath := filepath.Join(dir, "checksums.txt")
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(checksumPath, []byte(content), 0644); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "created %s\n", checksumPath)
	return nil
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

func copyReleaseDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyReleaseFile(path, target)
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
			if err := copyReleaseDir(srcPath, dstPath); err != nil {
				return fmt.Errorf("copy support directory %s: %w", rel, err)
			}
			return nil
		}
		if err := copyReleaseFile(srcPath, dstPath); err != nil {
			return fmt.Errorf("copy support file %s: %w", rel, err)
		}
		return nil
	}

	copyMaybeLocalPath := func(value string, optional bool) error {
		if value == "" || filepath.IsAbs(value) || strings.Contains(value, "://") {
			return nil
		}
		return copyPath(value, optional)
	}

	if err := copyPath(manifest.IconFile, false); err != nil {
		return err
	}
	if manifest.Provider != nil {
		if err := copyPath(manifest.Provider.ConfigSchemaPath, false); err != nil {
			return err
		}
		if err := copyPath(pluginpkg.StaticCatalogFile, !pluginpkg.StaticCatalogRequired(manifest)); err != nil {
			return err
		}
		if err := copyMaybeLocalPath(manifest.Provider.OpenAPI, false); err != nil {
			return err
		}
		if err := copyMaybeLocalPath(manifest.Provider.GraphQLURL, false); err != nil {
			return err
		}
		if err := copyMaybeLocalPath(manifest.Provider.MCPURL, false); err != nil {
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

func validatePackageOutputPath(sourceDir, outputPath string) error {
	sourceAbs, err := filepath.Abs(sourceDir)
	if err != nil {
		return fmt.Errorf("resolve source dir: %w", err)
	}
	outputAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	inside, err := pathWithinBase(outputAbs, sourceAbs)
	if err != nil {
		return fmt.Errorf("compare output path to source dir: %w", err)
	}
	if inside {
		if isArchiveOutput(outputPath) {
			return fmt.Errorf("output archive %q must not be inside source directory %q", outputPath, sourceDir)
		}
		return fmt.Errorf("output directory %q must not be inside source directory %q", outputPath, sourceDir)
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
