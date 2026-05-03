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

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
	"github.com/valon-technologies/gestalt/server/services/plugins/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/plugins/source"
	"gopkg.in/yaml.v3"
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
	case "attach":
		return runProviderAttach(args[1:])
	case "add":
		return runProviderAdd(args[1:])
	case "dev":
		return runProviderDev(args[1:])
	case "info":
		return runProviderInfo(args[1:])
	case "remove":
		return runProviderRemove(args[1:])
	case "repo":
		return runProviderRepo(args[1:])
	case "search":
		return runProviderSearch(args[1:])
	case "upgrade":
		return runProviderUpgrade(args[1:])
	case "validate":
		return runProviderValidate(args[1:])
	case "release":
		return runProviderRelease(args[1:])
	default:
		return fmt.Errorf("unknown provider command %q", args[0])
	}
}

const defaultPlatforms = "darwin/amd64,darwin/arm64,linux/amd64,linux/arm64"
const allPlatformsValue = "all"
const defaultReleaseOutputDir = "dist/"
const releaseBinaryPrefix = "gestalt-plugin-"
const providerReleaseMetadataFile = "provider-release.yaml"
const providerReleaseSchemaName = "gestaltd-provider-release"
const providerReleaseSchemaVersion = 1
const providerReleaseRuntimeKindExecutable = "executable"
const providerReleaseRuntimeKindDeclarative = "declarative"
const providerReleaseRuntimeKindUI = "ui"
const providerReleaseGenericTarget = "generic"
const windowsOS = "windows"
const windowsExecutableSuffix = ".exe"

type releasePlatform struct {
	GOOS   string
	GOARCH string
}

type releaseBuildTarget struct {
	Kind string
}

type releaseArchive struct {
	Path     string
	SHA256   string
	Platform *releasePlatform
}

type providerReleaseMetadata struct {
	Schema        string                             `yaml:"schema"`
	SchemaVersion int                                `yaml:"schemaVersion"`
	Package       string                             `yaml:"package"`
	Kind          string                             `yaml:"kind"`
	Version       string                             `yaml:"version"`
	Runtime       string                             `yaml:"runtime"`
	Artifacts     map[string]providerReleaseArtifact `yaml:"artifacts,omitempty"`
}

type providerReleaseArtifact struct {
	Path   string `yaml:"path"`
	SHA256 string `yaml:"sha256"`
}

func runProviderRelease(args []string) (err error) {
	fs := flag.NewFlagSet("gestaltd provider release", flag.ContinueOnError)
	fs.Usage = func() { printProviderReleaseUsage(fs.Output()) }
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
	if err := providerpkg.EnsureSourceStaticCatalog(manifestPath, releaseManifest); err != nil {
		return err
	}
	src, err := source.Parse(releaseManifest.Source)
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

	var releaseArchives []releaseArchive
	if len(buildPlatforms) > 0 {
		for _, platform := range buildPlatforms {
			archivePath, err := buildPlatformArchive(manifestPath, pluginName, *version, buildTarget.Kind, platform, *outputDir)
			if err != nil {
				return fmt.Errorf("build %s: %w", providerpkg.PlatformString(platform.GOOS, platform.GOARCH), err)
			}
			plat := platform
			releaseArchive, err := describeReleaseArchive(archivePath, &plat)
			if err != nil {
				return err
			}
			releaseArchives = append(releaseArchives, releaseArchive)
		}
	} else {
		archivePath, err := buildSourceArchive(manifestPath, pluginName, *version, *outputDir)
		if err != nil {
			return err
		}
		releaseArchive, err := describeReleaseArchive(archivePath, nil)
		if err != nil {
			return err
		}
		releaseArchives = append(releaseArchives, releaseArchive)
	}

	if err := writeChecksums(*outputDir, releaseArchives); err != nil {
		return fmt.Errorf("write checksums: %w", err)
	}
	if err := writeProviderReleaseMetadata(*outputDir, releaseManifest, *version, releaseArchives); err != nil {
		return fmt.Errorf("write release metadata: %w", err)
	}

	return nil
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
	hasSource, err := providerpkg.HasSourceReleaseTarget(root, kind)
	if err != nil {
		return nil, fmt.Errorf("detect source %s package: %w", kind, err)
	}
	if !hasSource {
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

	buildRequired := providerpkg.ReleaseRequiresBuild(manifest)
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
		if err := providerpkg.ValidateSourceReleaseTarget(root, target.Kind, platform.GOOS, platform.GOARCH); err != nil {
			if providerpkg.IsMissingSourceReleaseTarget(err, target.Kind) {
				missingSource = true
				continue
			}
			return nil, fmt.Errorf("detect source %s package for %s/%s: %w", target.Kind, platform.GOOS, platform.GOARCH, err)
		}
		builds = append(builds, platform)
	}

	if len(builds) == 0 {
		return nil, providerpkg.MissingSourceReleaseTargetError(target.Kind)
	}
	if missingSource {
		return nil, providerpkg.MissingSourceReleaseTargetError(target.Kind)
	}
	return builds, nil
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

func buildPlatformArchive(manifestPath, pluginName, version, buildKind string, platform releasePlatform, outputDir string) (string, error) {
	archiveName := platformArchiveName(pluginName, version, platform)
	return createReleaseArchive(outputDir, archiveName, func(stagingDir string) (*providerpkg.StagedPreparedInstall, error) {
		return providerpkg.StagePreparedInstallDir(manifestPath, stagingDir, providerpkg.StagePreparedInstallOptions{
			VersionOverride: version,
			BuildKind:       buildKind,
			PluginName:      pluginName,
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

func buildSourceArchive(manifestPath, pluginName, version, outputDir string) (string, error) {
	archiveName := fmt.Sprintf("gestalt-plugin-%s_v%s.tar.gz", pluginName, version)
	return createReleaseArchive(outputDir, archiveName, func(stagingDir string) (*providerpkg.StagedPreparedInstall, error) {
		return providerpkg.StagePreparedInstallDir(manifestPath, stagingDir, providerpkg.StagePreparedInstallOptions{
			VersionOverride: version,
		})
	})
}

func validateStagedReleaseCatalog(staged *providerpkg.StagedPreparedInstall) error {
	if staged == nil || staged.Manifest == nil || staged.Manifest.Kind != providermanifestv1.KindPlugin {
		return nil
	}
	src, err := source.Parse(staged.Manifest.Source)
	if err != nil {
		return fmt.Errorf("invalid source in staged manifest: %w", err)
	}
	return pluginservice.ValidateEffectiveManifest(context.Background(), src.PluginName(), staged.ManifestPath, staged.Manifest)
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

func writeChecksums(dir string, archives []releaseArchive) error {
	var lines []string
	for _, archive := range archives {
		lines = append(lines, fmt.Sprintf("%s  %s", archive.SHA256, filepath.Base(archive.Path)))
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

func describeReleaseArchive(path string, platform *releasePlatform) (releaseArchive, error) {
	digest, err := providerpkg.ArchiveDigest(path)
	if err != nil {
		return releaseArchive{}, fmt.Errorf("hash release archive %s: %w", path, err)
	}
	return releaseArchive{
		Path:     path,
		SHA256:   digest,
		Platform: platform,
	}, nil
}

func writeProviderReleaseMetadata(dir string, manifest *providermanifestv1.Manifest, version string, archives []releaseArchive) error {
	metadata, err := buildProviderReleaseMetadata(manifest, version, archives)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode %s: %w", providerReleaseMetadataFile, err)
	}
	path := filepath.Join(dir, providerReleaseMetadataFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "created %s\n", path)
	return nil
}

func buildProviderReleaseMetadata(manifest *providermanifestv1.Manifest, version string, archives []releaseArchive) (*providerReleaseMetadata, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}

	runtime, err := releaseRuntimeMetadata(manifest, archives)
	if err != nil {
		return nil, err
	}

	metadata := &providerReleaseMetadata{
		Schema:        providerReleaseSchemaName,
		SchemaVersion: providerReleaseSchemaVersion,
		Package:       manifest.Source,
		Kind:          manifest.Kind,
		Version:       version,
		Runtime:       runtime,
		Artifacts:     make(map[string]providerReleaseArtifact, len(archives)),
	}
	for _, archive := range archives {
		metadata.Artifacts[providerReleaseArtifactTarget(manifest, archive)] = providerReleaseArtifact{
			Path:   filepath.Base(archive.Path),
			SHA256: archive.SHA256,
		}
	}
	return metadata, nil
}

func providerReleaseArtifactTarget(manifest *providermanifestv1.Manifest, archive releaseArchive) string {
	if archive.Platform == nil {
		if manifest != nil && len(manifest.Artifacts) == 1 {
			artifact := manifest.Artifacts[0]
			if artifact.OS != "" && artifact.Arch != "" {
				return providerpkg.PlatformString(artifact.OS, artifact.Arch)
			}
		}
		return providerReleaseGenericTarget
	}
	return providerpkg.PlatformString(archive.Platform.GOOS, archive.Platform.GOARCH)
}

func releaseRuntimeMetadata(manifest *providermanifestv1.Manifest, archives []releaseArchive) (string, error) {
	kind, err := providerpkg.ManifestKind(manifest)
	if err != nil {
		return "", err
	}

	switch kind {
	case providermanifestv1.KindPlugin:
		if releaseIncludesBuiltPluginArtifact(archives) {
			return providerReleaseRuntimeKindExecutable, nil
		}
		if manifest.IsDeclarativeOnlyProvider() {
			return providerReleaseRuntimeKindDeclarative, nil
		}
		return providerReleaseRuntimeKindExecutable, nil
	case providermanifestv1.KindUI:
		return providerReleaseRuntimeKindUI, nil
	default:
		return providerReleaseRuntimeKindExecutable, nil
	}
}

func releaseIncludesBuiltPluginArtifact(archives []releaseArchive) bool {
	for _, archive := range archives {
		if archive.Platform != nil {
			return true
		}
	}
	return false
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
		return fmt.Errorf("compare output dir to ui asset_root: %w", err)
	}
	if insideAssetRoot {
		return fmt.Errorf("--output %q must not be inside ui.asset_root %q", outputDir, manifest.Spec.AssetRoot)
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
	writeUsageLine(w, "  attach      List, inspect, or detach remote provider-dev attachments")
	writeUsageLine(w, "  add         Add a provider package to config and update lock state")
	writeUsageLine(w, "  dev         Run a local source plugin inside a synthesized Gestalt config")
	writeUsageLine(w, "  info        Show provider package metadata from configured repositories")
	writeUsageLine(w, "  release     Build provider release archives")
	writeUsageLine(w, "  remove      Remove a provider entry from config and update lock state")
	writeUsageLine(w, "  repo        Manage provider package repositories")
	writeUsageLine(w, "  search      Search configured provider package repositories")
	writeUsageLine(w, "  upgrade     Refresh a provider package lock or version constraint")
	writeUsageLine(w, "  validate    Validate a local source plugin inside a synthesized Gestalt config")
}

func printProviderReleaseUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd provider release --version VERSION [--output DIR] [--platform PLATFORMS]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Build a provider release archive for the host platform by default.")
	writeUsageLine(w, "Pass --platform with a comma-separated os/arch list or --platform all")
	writeUsageLine(w, "to build multiple per-platform tar.gz archives plus a checksums file.")
	writeUsageLine(w, "Run from the provider source directory.")
	writeUsageLine(w, "For apiVersion v5 local deploy configs, point source.path at dist/provider-release.yaml.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --version    Semantic version string (required)")
	writeUsageLine(w, "  --output     Output directory (default: dist/)")
	writeUsageLine(w, "  --platform   Comma-separated platforms (os/arch) or all (default: host platform only)")
}
