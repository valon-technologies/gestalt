package daemon

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/apps/source"
)

func runProviderPackage(args []string) (err error) {
	fs := flag.NewFlagSet("gestaltd provider package", flag.ContinueOnError)
	fs.Usage = func() { printProviderPackageUsage(fs.Output()) }
	version := fs.String("version", "", "semantic version string (required)")
	outputDir := fs.String("output", defaultReleaseOutputDir, "output directory")
	platforms := fs.String("platform", "", "comma-separated platforms (os/arch) or 'all'")
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

	if err := source.ValidateVersion(*version); err != nil {
		return fmt.Errorf("invalid --version: %w", err)
	}

	manifestPath, err := providerpkg.FindManifestFile(".")
	if err != nil {
		return err
	}
	manifestPath, err = filepath.Abs(manifestPath)
	if err != nil {
		return fmt.Errorf("resolve manifest path: %w", err)
	}
	sourceDir := filepath.Dir(manifestPath)
	catalogSnapshot, err := snapshotSourceStaticCatalog(sourceDir)
	if err != nil {
		return err
	}
	defer func() {
		if restoreErr := catalogSnapshot.Restore(); restoreErr != nil && err == nil {
			err = fmt.Errorf("restore synthesized static catalog state: %w", restoreErr)
		}
	}()
	_, releaseManifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", manifestPath, err)
	}
	if err := validateReleaseOutputDir(releaseManifest, sourceDir, *outputDir); err != nil {
		return err
	}
	src, err := source.Parse(releaseManifest.Source)
	if err != nil {
		return fmt.Errorf("invalid source in manifest: %w", err)
	}
	appName := src.AppName()

	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	if err := removeStaleReleaseFinalizationFiles(*outputDir); err != nil {
		return err
	}

	buildTarget, err := resolveReleaseBuildTarget(sourceDir, releaseManifest)
	if err != nil {
		return err
	}

	buildPlatforms, err := resolveReleaseBuildPlatforms(sourceDir, releaseManifest, buildTarget, *platforms, platformFlagExplicit)
	if err != nil {
		return err
	}

	if err := removeStalePackageArchives(*outputDir, appName, releaseArchiveTargets(buildPlatforms)); err != nil {
		return err
	}

	if len(buildPlatforms) > 0 {
		for _, platform := range buildPlatforms {
			_, err := buildPlatformArchive(manifestPath, appName, *version, platform, *outputDir)
			if err != nil {
				return fmt.Errorf("build %s: %w", providerpkg.PlatformString(platform.GOOS, platform.GOARCH), err)
			}
		}
	} else {
		_, err := buildSourceArchive(manifestPath, appName, *version, *outputDir)
		if err != nil {
			return err
		}
	}

	return nil
}

func removeStaleReleaseFinalizationFiles(outputDir string) error {
	p := filepath.Join(outputDir, providerrelease.MetadataFile)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale %s: %w", providerrelease.MetadataFile, err)
	}
	return nil
}

type packageArchiveTarget struct {
	PlatformSuffix string
	Generic        bool
}

func releaseArchiveTargets(platforms []releasePlatform) []packageArchiveTarget {
	if len(platforms) == 0 {
		return []packageArchiveTarget{{Generic: true}}
	}
	targets := make([]packageArchiveTarget, 0, len(platforms))
	for _, platform := range platforms {
		targets = append(targets, packageArchiveTarget{
			PlatformSuffix: providerpkg.PlatformArchiveSuffix(platform.GOOS, platform.GOARCH),
		})
	}
	return targets
}

func removeStalePackageArchives(outputDir, appName string, targets []packageArchiveTarget) error {
	if len(targets) == 0 {
		return nil
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return fmt.Errorf("read output dir for stale archives: %w", err)
	}
	prefix := fmt.Sprintf("gestalt-app-%s_v", appName)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".tar.gz") {
			continue
		}
		if !matchesPackageArchiveTargets(name, prefix, targets) {
			continue
		}
		if err := os.Remove(filepath.Join(outputDir, name)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove stale archive %s: %w", name, err)
		}
	}
	return nil
}

func matchesPackageArchiveTargets(name, prefix string, targets []packageArchiveTarget) bool {
	for _, target := range targets {
		if target.Generic {
			remainder := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".tar.gz")
			if !strings.Contains(remainder, "_") {
				return true
			}
			continue
		}
		if target.PlatformSuffix != "" && strings.HasSuffix(name, "_"+target.PlatformSuffix+".tar.gz") {
			return true
		}
	}
	return false
}

type sourceStaticCatalogSnapshot struct {
	path   string
	data   []byte
	mode   fs.FileMode
	exists bool
}

func snapshotSourceStaticCatalog(sourceDir string) (*sourceStaticCatalogSnapshot, error) {
	path := providerpkg.StaticCatalogPath(sourceDir)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &sourceStaticCatalogSnapshot{path: path}, nil
		}
		return nil, fmt.Errorf("stat source static catalog: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read source static catalog: %w", err)
	}
	return &sourceStaticCatalogSnapshot{
		path:   path,
		data:   data,
		mode:   info.Mode().Perm(),
		exists: true,
	}, nil
}

func (s *sourceStaticCatalogSnapshot) Restore() error {
	if s == nil || s.path == "" {
		return nil
	}
	current, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		if s.exists && bytes.Equal(current, s.data) {
			return nil
		}
	case os.IsNotExist(err):
		if !s.exists {
			return nil
		}
	default:
		return fmt.Errorf("read current static catalog: %w", err)
	}
	if !s.exists {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove generated static catalog: %w", err)
		}
		return nil
	}
	if err := os.WriteFile(s.path, s.data, s.mode); err != nil {
		return fmt.Errorf("restore static catalog: %w", err)
	}
	return nil
}

func resolveReleaseBuildTarget(root string, manifest *providermanifestv1.Manifest) (*releaseBuildTarget, error) {
	kind, err := providerpkg.ManifestKind(manifest)
	if err != nil {
		return nil, err
	}
	if kind == providermanifestv1.KindUI {
		return nil, nil
	}
	if err := providerpkg.ValidateExplicitRunPackaging(root, manifest); err != nil {
		return nil, err
	}
	if providerpkg.EffectiveSourceBuild(manifest) != nil {
		entry := providerpkg.EntrypointForKind(manifest, kind)
		if entry == nil || strings.TrimSpace(entry.ArtifactPath) == "" {
			return nil, nil
		}
		return &releaseBuildTarget{Kind: kind, DeclaredBuild: true}, nil
	}
	hasSource, err := providerpkg.HasSourceReleaseTarget(root, kind)
	if err != nil {
		return nil, fmt.Errorf("detect source %s package: %w", kind, err)
	}
	if !hasSource {
		entry := providerpkg.EntrypointForKind(manifest, kind)
		if entry != nil && strings.TrimSpace(entry.ArtifactPath) != "" {
			return &releaseBuildTarget{Kind: kind, Prebuilt: true}, nil
		}
		if providerpkg.ReleaseRequiresBuild(manifest) {
			return nil, providerpkg.MissingSourceReleaseTargetError(kind)
		}
		return nil, nil
	}
	return &releaseBuildTarget{Kind: kind}, nil
}

func resolveReleaseBuildPlatforms(root string, manifest *providermanifestv1.Manifest, target *releaseBuildTarget, value string, explicit bool) ([]releasePlatform, error) {
	if target == nil {
		return nil, nil
	}
	if target.Prebuilt {
		if explicit {
			return nil, fmt.Errorf("--platform requires build.command for executable source providers")
		}
		return nil, fmt.Errorf("provider package requires build.command for executable source providers")
	}

	buildRequired := target.DeclaredBuild || providerpkg.ReleaseRequiresBuild(manifest)
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
	if !target.DeclaredBuild {
		for _, platform := range platforms {
			if err := providerpkg.ValidateSourceReleaseTarget(root, target.Kind, platform.GOOS, platform.GOARCH); err != nil {
				return nil, fmt.Errorf("validate %s source release target for %s: %w", target.Kind, providerpkg.PlatformString(platform.GOOS, platform.GOARCH), err)
			}
		}
	}
	return platforms, nil
}

func expandReleasePlatformValue(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	switch {
	case trimmed == "":
		return "", fmt.Errorf("--platform requires a comma-separated os/arch list or %q", allPlatformsValue)
	case strings.EqualFold(trimmed, allPlatformsValue):
		return defaultPlatforms, nil
	default:
		return value, nil
	}
}

func buildPlatformArchive(manifestPath, appName, version string, platform releasePlatform, outputDir string) (string, error) {
	archiveName := platformArchiveName(appName, version, platform)
	return createReleaseArchive(outputDir, archiveName, func(stagingDir string) (*providerpkg.StagedPreparedInstall, error) {
		return providerpkg.StageSourcePreparedInstallDir(manifestPath, stagingDir, providerpkg.StageSourcePreparedInstallOptions{
			VersionOverride: version,
			AppName:         appName,
			GOOS:            platform.GOOS,
			GOARCH:          platform.GOARCH,
		})
	})
}

func createReleaseArchive(outputDir, archiveName string, prepare func(stagingDir string) (*providerpkg.StagedPreparedInstall, error)) (string, error) {
	stagingDir, err := os.MkdirTemp("", "gestalt-release-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(stagingDir) }()

	staged, err := prepare(stagingDir)
	if err != nil {
		return "", err
	}
	if err := validateStagedReleaseCatalog(staged); err != nil {
		return "", err
	}
	archivePath := filepath.Join(outputDir, archiveName)
	if err := providerpkg.CreatePackageFromDir(stagingDir, archivePath); err != nil {
		return "", err
	}

	_, _ = fmt.Fprintf(os.Stdout, "created %s\n", archivePath)
	return archivePath, nil
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

func buildSourceArchive(manifestPath, appName, version, outputDir string) (string, error) {
	archiveName := fmt.Sprintf("gestalt-app-%s_v%s.tar.gz", appName, version)
	return createReleaseArchive(outputDir, archiveName, func(stagingDir string) (*providerpkg.StagedPreparedInstall, error) {
		return providerpkg.StageSourcePreparedInstallDir(manifestPath, stagingDir, providerpkg.StageSourcePreparedInstallOptions{
			VersionOverride: version,
		})
	})
}

func validateStagedReleaseCatalog(staged *providerpkg.StagedPreparedInstall) error {
	if staged == nil || staged.Manifest == nil || staged.Manifest.Kind != providermanifestv1.KindApp {
		return nil
	}
	src, err := source.Parse(staged.Manifest.Source)
	if err != nil {
		return fmt.Errorf("invalid source in staged manifest: %w", err)
	}
	return appservice.ValidateEffectiveManifest(context.Background(), src.AppName(), staged.ManifestPath, staged.Manifest)
}

func platformArchiveName(appName, version string, plat releasePlatform) string {
	return fmt.Sprintf("gestalt-app-%s_v%s_%s.tar.gz", appName, version, providerpkg.PlatformArchiveSuffix(plat.GOOS, plat.GOARCH))
}

func validateReleaseOutputDir(manifest *providermanifestv1.Manifest, sourceDir, outputDir string) error {
	assetRootValue := providerpkg.SourceUIBuildOutput(manifest)
	if manifest == nil || assetRootValue == "" {
		return nil
	}

	assetRoot, err := normalizeReleasePath(assetRootValue)
	if err != nil {
		return err
	}

	sourceAbs, err := filepath.Abs(sourceDir)
	if err != nil {
		return fmt.Errorf("resolve app root: %w", err)
	}

	assetRootAbs := filepath.Join(sourceAbs, filepath.FromSlash(assetRoot))
	outputDirAbs := outputDir
	if !filepath.IsAbs(outputDirAbs) {
		outputDirAbs = filepath.Join(sourceAbs, outputDirAbs)
	}
	outputDirAbs = filepath.Clean(outputDirAbs)

	insideAssetRoot, err := pathWithinBase(outputDirAbs, assetRootAbs)
	if err != nil {
		return fmt.Errorf("compare output dir to ui asset_root: %w", err)
	}
	if insideAssetRoot {
		return fmt.Errorf("--output %q must not be inside ui asset root %q", outputDir, assetRootValue)
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
		return "", fmt.Errorf("release path %q must stay within app root", rel)
	}
	return cleanPath, nil
}

func printProviderPackageUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd provider package --version VERSION [--output DIR] [--platform PLATFORMS]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Build provider release archives.")
	writeUsageLine(w, "Executable source providers use SDK-native source packages or build.command and default to the host platform.")
	writeUsageLine(w, "UI and declarative providers default to a generic archive.")
	writeUsageLine(w, "Pass --platform with a comma-separated os/arch list or --platform all")
	writeUsageLine(w, "to build multiple per-platform tar.gz archives.")
	writeUsageLine(w, "Run from the provider source directory.")
	writeUsageLine(w, "Run gestaltd provider release afterward to create provider-release.yaml.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --version    Semantic version string (required)")
	writeUsageLine(w, "  --output     Output directory (default: dist/)")
	writeUsageLine(w, "  --platform   Comma-separated platforms (os/arch) or all")
}
