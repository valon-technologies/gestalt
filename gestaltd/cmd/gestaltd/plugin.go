package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func runProvider(args []string) error {
	if len(args) == 0 {
		printProviderUsage(os.Stderr)
		return flag.ErrHelp
	}

	switch args[0] {
	case "-h", "--help", "help":
		printProviderUsage(os.Stderr)
		return flag.ErrHelp
	case "release":
		return runProviderRelease(args[1:])
	default:
		return fmt.Errorf("unknown provider command %q", args[0])
	}
}

const defaultPlatforms = "darwin/amd64,darwin/arm64,linux/amd64,linux/arm,linux/arm64"
const allPlatformsValue = "all"
const defaultReleaseOutputDir = "dist/"
const releaseBinaryPrefix = "gestalt-plugin-"
const windowsOS = "windows"
const windowsExecutableSuffix = ".exe"

type releasePlatform struct {
	GOOS   string
	GOARCH string
}

type releaseBuildTarget struct {
	Kind string
}

func runProviderRelease(args []string) error {
	fs := flag.NewFlagSet("gestaltd provider release", flag.ContinueOnError)
	fs.Usage = func() { printProviderReleaseUsage(fs.Output()) }
	version := fs.String("version", "", "semantic version string (required)")
	outputDir := fs.String("output", defaultReleaseOutputDir, "output directory")
	platforms := fs.String("platform", "", "comma-separated platforms (os/arch[/libc]) or 'all'")
	if err := fs.Parse(args); err != nil {
		return err
	}
	platformFlagExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "platform" {
			platformFlagExplicit = true
		}
	})
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *version == "" {
		return fmt.Errorf("--version is required")
	}

	if err := pluginsource.ValidateVersion(*version); err != nil {
		return fmt.Errorf("invalid --version: %w", err)
	}

	manifestPath, err := providerpkg.FindManifestFile(".")
	if err != nil {
		return err
	}
	sourceDir := filepath.Dir(manifestPath)
	_, releaseManifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", manifestPath, err)
	}
	if err := providerpkg.RunSourceReleaseBuild(manifestPath, releaseManifest); err != nil {
		return err
	}
	_, releaseManifest, err = providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read %s after release build: %w", manifestPath, err)
	}
	if err := validateReleaseOutputDir(releaseManifest, sourceDir, *outputDir); err != nil {
		return err
	}
	src, err := pluginsource.Parse(releaseManifest.Source)
	if err != nil {
		return fmt.Errorf("invalid source in manifest: %w", err)
	}
	pluginName := src.PluginName()

	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	buildTarget, err := resolveReleaseBuildTarget(sourceDir, releaseManifest)
	if err != nil {
		return err
	}

	buildPlatforms, err := resolveReleaseBuildPlatforms(sourceDir, releaseManifest, buildTarget, *platforms, platformFlagExplicit)
	if err != nil {
		return err
	}

	var archivePaths []string
	if len(buildPlatforms) > 0 {
		for _, platform := range buildPlatforms {
			archivePath, err := buildPlatformArchive(manifestPath, pluginName, *version, buildTarget.Kind, platform, *outputDir)
			if err != nil {
				return fmt.Errorf("build %s: %w", providerpkg.PlatformString(platform.GOOS, platform.GOARCH), err)
			}
			archivePaths = append(archivePaths, archivePath)
		}
	} else {
		archivePath, err := buildSourceArchive(manifestPath, pluginName, *version, *outputDir)
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

func resolveReleaseBuildTarget(root string, manifest *providermanifestv1.Manifest) (*releaseBuildTarget, error) {
	kind, err := providerpkg.ManifestKind(manifest)
	if err != nil {
		return nil, err
	}
	if kind == providermanifestv1.KindWebUI {
		return nil, nil
	}
	hasSource, err := detectReleaseSourceBuildTarget(root, kind)
	if err != nil {
		return nil, fmt.Errorf("detect source %s package: %w", kind, err)
	}
	if !hasSource {
		if releaseRequiresBuildTarget(manifest) {
			return nil, missingReleaseSourceBuildTargetError(kind)
		}
		return nil, nil
	}
	return &releaseBuildTarget{Kind: kind}, nil
}

func resolveReleaseBuildPlatforms(root string, manifest *providermanifestv1.Manifest, target *releaseBuildTarget, value string, explicit bool) ([]releasePlatform, error) {
	if target == nil {
		return nil, nil
	}

	buildRequired := releaseRequiresBuildTarget(manifest)
	if !buildRequired && !explicit {
		return nil, nil
	}
	if explicit {
		var err error
		value, err = expandReleasePlatformValue(value)
		if err != nil {
			return nil, err
		}
	} else {
		value = currentReleasePlatform()
	}
	platforms, err := parseReleasePlatforms(value)
	if err != nil {
		return nil, err
	}

	builds := make([]releasePlatform, 0, len(platforms))
	var missingSource bool
	for _, platform := range platforms {
		if err := validateReleaseBuildTarget(root, target.Kind, platform.GOOS, platform.GOARCH); err != nil {
			if isMissingReleaseSourceBuildTarget(err, target.Kind) {
				missingSource = true
				continue
			}
			return nil, fmt.Errorf("detect source %s package for %s/%s: %w", target.Kind, platform.GOOS, platform.GOARCH, err)
		}
		builds = append(builds, platform)
	}

	if len(builds) == 0 {
		return nil, missingReleaseSourceBuildTargetError(target.Kind)
	}
	if missingSource {
		return nil, missingReleaseSourceBuildTargetError(target.Kind)
	}
	return builds, nil
}

func expandReleasePlatformValue(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	switch {
	case trimmed == "":
		return "", fmt.Errorf("--platform requires a comma-separated os/arch[/libc] list or %q", allPlatformsValue)
	case strings.EqualFold(trimmed, allPlatformsValue):
		return defaultPlatforms, nil
	default:
		return value, nil
	}
}

func buildPlatformArchive(manifestPath, pluginName, version, buildKind string, platform releasePlatform, outputDir string) (string, error) {
	archiveName := platformArchiveName(pluginName, version, platform)
	return createReleaseArchive(outputDir, archiveName, func(stagingDir string) error {
		_, err := providerpkg.StagePreparedInstallDir(manifestPath, stagingDir, providerpkg.StagePreparedInstallOptions{
			VersionOverride: version,
			BuildKind:       buildKind,
			PluginName:      pluginName,
			GOOS:            platform.GOOS,
			GOARCH:          platform.GOARCH,
		})
		return err
	})
}

func createReleaseArchive(outputDir, archiveName string, prepare func(stagingDir string) error) (string, error) {
	stagingDir, err := os.MkdirTemp("", "gestalt-release-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(stagingDir) }()

	if err := prepare(stagingDir); err != nil {
		return "", err
	}
	archivePath := filepath.Join(outputDir, archiveName)
	if err := providerpkg.CreatePackageFromDir(stagingDir, archivePath); err != nil {
		return "", err
	}

	_, _ = fmt.Fprintf(os.Stdout, "created %s\n", archivePath)
	return archivePath, nil
}

func releaseRequiresBuildTarget(manifest *providermanifestv1.Manifest) bool {
	kind, err := providerpkg.ManifestKind(manifest)
	if err != nil {
		return false
	}
	switch kind {
	case providermanifestv1.KindPlugin:
		return manifest.Entrypoint == nil && (manifest.Spec == nil || !manifest.Spec.IsManifestBacked())
	case providermanifestv1.KindAuth, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindSecrets:
		return providerpkg.EntrypointForKind(manifest, kind) == nil
	default:
		return false
	}
}

func detectReleaseSourceBuildTarget(root, kind string) (bool, error) {
	switch kind {
	case providermanifestv1.KindPlugin:
		return providerpkg.HasSourceProviderPackage(root)
	case providermanifestv1.KindAuth, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindSecrets:
		return providerpkg.HasSourceComponentPackage(root, kind)
	default:
		return false, fmt.Errorf("unsupported release build target kind %q", kind)
	}
}

func validateReleaseBuildTarget(root, kind, goos, goarch string) error {
	switch kind {
	case providermanifestv1.KindPlugin:
		return providerpkg.ValidateSourceProviderRelease(root, goos, goarch)
	case providermanifestv1.KindAuth, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindSecrets:
		return providerpkg.ValidateSourceComponentRelease(root, kind, goos, goarch)
	default:
		return fmt.Errorf("unsupported release build target kind %q", kind)
	}
}

func isMissingReleaseSourceBuildTarget(err error, kind string) bool {
	switch kind {
	case providermanifestv1.KindPlugin:
		return errors.Is(err, providerpkg.ErrNoSourceProviderPackage)
	case providermanifestv1.KindAuth, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindSecrets:
		return errors.Is(err, providerpkg.ErrNoSourceComponentPackage)
	default:
		return false
	}
}

func missingReleaseSourceBuildTargetError(kind string) error {
	switch kind {
	case providermanifestv1.KindPlugin:
		return fmt.Errorf("no Go, Rust, Python, or TypeScript provider package found")
	case providermanifestv1.KindAuth, providermanifestv1.KindCache, providermanifestv1.KindIndexedDB, providermanifestv1.KindS3, providermanifestv1.KindSecrets:
		return fmt.Errorf("no Go, Rust, Python, or TypeScript %s source package found", kind)
	default:
		return fmt.Errorf("unsupported release build target kind %q", kind)
	}
}

func parseReleasePlatforms(value string) ([]releasePlatform, error) {
	parts := strings.Split(value, ",")
	platforms := make([]releasePlatform, 0, len(parts))
	for _, part := range parts {
		plat := strings.TrimSpace(part)
		goos, goarch, err := providerpkg.ParsePlatformString(plat)
		if err != nil {
			return nil, fmt.Errorf("invalid platform %q, expected os/arch", plat)
		}
		platforms = append(platforms, releasePlatform{
			GOOS:   goos,
			GOARCH: goarch,
		})
	}
	return platforms, nil
}

func currentReleasePlatform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

func buildSourceArchive(manifestPath, pluginName, version, outputDir string) (string, error) {
	archiveName := fmt.Sprintf("gestalt-plugin-%s_v%s.tar.gz", pluginName, version)
	return createReleaseArchive(outputDir, archiveName, func(stagingDir string) error {
		_, err := providerpkg.StagePreparedInstallDir(manifestPath, stagingDir, providerpkg.StagePreparedInstallOptions{
			VersionOverride: version,
		})
		return err
	})
}

func platformArchiveName(pluginName, version string, plat releasePlatform) string {
	return fmt.Sprintf("gestalt-plugin-%s_v%s_%s.tar.gz", pluginName, version, providerpkg.PlatformArchiveSuffix(plat.GOOS, plat.GOARCH))
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
		digest, err := providerpkg.ArchiveDigest(archivePath)
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

func validateReleaseOutputDir(manifest *providermanifestv1.Manifest, sourceDir, outputDir string) error {
	if manifest == nil || manifest.Spec == nil || manifest.Spec.AssetRoot == "" {
		return nil
	}

	assetRoot, err := normalizeReleasePath(manifest.Spec.AssetRoot)
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
		return fmt.Errorf("--output %q must not be inside webui.asset_root %q", outputDir, manifest.Spec.AssetRoot)
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

func printProviderUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd provider <command> [flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  release     Build provider release archives")
}

func printProviderReleaseUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd provider release --version VERSION [--output DIR] [--platform PLATFORMS]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Build a provider release archive for the host platform by default.")
	writeUsageLine(w, "Pass --platform with a comma-separated os/arch[/libc] list or --platform all")
	writeUsageLine(w, "to build multiple per-platform tar.gz archives plus a checksums file.")
	writeUsageLine(w, "Run from the provider source directory.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --version    Semantic version string (required)")
	writeUsageLine(w, "  --output     Output directory (default: dist/)")
	writeUsageLine(w, "  --platform   Comma-separated platforms (os/arch[/libc]) or all (default: host platform only)")
}
