package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
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

const defaultPlatforms = "darwin/amd64,darwin/arm64,linux/amd64,linux/arm64"
const allPlatformsValue = "all"
const defaultReleaseOutputDir = "dist/"
const releaseBinaryPrefix = "gestalt-plugin-"
const releaseOwnedUIRoot = "_owned_ui"
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
	if err := validateReleaseOutputDir(releaseManifest, sourceDir, *outputDir); err != nil {
		return err
	}
	if err := providerpkg.RunSourceReleaseBuild(manifestPath, releaseManifest); err != nil {
		return err
	}
	_, srcManifest, err := providerpkg.PrepareSourceManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("prepare %s: %w", manifestPath, err)
	}
	manifestFormat := providerpkg.ManifestFormatFromPath(manifestPath)
	manifestFile := filepath.Base(manifestPath)

	src, err := pluginsource.Parse(srcManifest.Source)
	if err != nil {
		return fmt.Errorf("invalid source in manifest: %w", err)
	}
	pluginName := src.PluginName()

	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	buildTarget, err := resolveReleaseBuildTarget(sourceDir, srcManifest)
	if err != nil {
		return err
	}

	buildPlatforms, err := resolveReleaseBuildPlatforms(sourceDir, srcManifest, buildTarget, *platforms, platformFlagExplicit)
	if err != nil {
		return err
	}

	var archivePaths []string
	if len(buildPlatforms) > 0 {
		for _, platform := range buildPlatforms {
			archivePath, err := buildPlatformArchive(sourceDir, srcManifest, pluginName, *version, buildTarget.Kind, platform, *outputDir, manifestFile, manifestFormat)
			if err != nil {
				return fmt.Errorf("build %s: %w", providerpkg.PlatformString(platform.GOOS, platform.GOARCH), err)
			}
			archivePaths = append(archivePaths, archivePath)
		}
	} else {
		archivePath, err := buildSourceArchive(sourceDir, srcManifest, pluginName, *version, *outputDir, manifestFile, manifestFormat)
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

func buildPlatformArchive(sourceDir string, srcManifest *providermanifestv1.Manifest, pluginName, version, buildKind string, platform releasePlatform, outputDir, manifestFile, manifestFormat string) (string, error) {
	stagingDir, err := os.MkdirTemp("", "gestalt-release-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(stagingDir) }()

	manifest, plat, err := prepareBuiltPackageDir(stagingDir, sourceDir, srcManifest, version, pluginName, buildKind, platform)
	if err != nil {
		return "", err
	}
	archivePath := filepath.Join(outputDir, platformArchiveName(pluginName, version, plat))
	if err := writeReleaseManifestFile(stagingDir, manifestFile, manifestFormat, manifest); err != nil {
		return "", err
	}
	if err := providerpkg.CreatePackageFromDir(stagingDir, archivePath); err != nil {
		return "", err
	}

	_, _ = fmt.Fprintf(os.Stdout, "created %s\n", archivePath)
	return archivePath, nil
}

func createReleaseArchive(outputDir, archiveName, manifestFile, manifestFormat string, prepare func(stagingDir string) (*providermanifestv1.Manifest, error)) (string, error) {
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
	if err := providerpkg.CreatePackageFromDir(stagingDir, archivePath); err != nil {
		return "", err
	}

	_, _ = fmt.Fprintf(os.Stdout, "created %s\n", archivePath)
	return archivePath, nil
}

func writeReleaseManifestFile(stagingDir, manifestFile, manifestFormat string, manifest *providermanifestv1.Manifest) error {
	data, err := providerpkg.EncodeManifestFormat(manifest, manifestFormat)
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	return os.WriteFile(filepath.Join(stagingDir, manifestFile), data, 0644)
}

func releaseRequiresBuildTarget(manifest *providermanifestv1.Manifest) bool {
	kind, err := providerpkg.ManifestKind(manifest)
	if err != nil {
		return false
	}
	switch kind {
	case providermanifestv1.KindPlugin:
		return manifest.Entrypoint == nil && (manifest.Spec == nil || !manifest.Spec.IsManifestBacked())
	case providermanifestv1.KindAuth, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindSecrets:
		return providerpkg.EntrypointForKind(manifest, kind) == nil
	default:
		return false
	}
}

func detectReleaseSourceBuildTarget(root, kind string) (bool, error) {
	switch kind {
	case providermanifestv1.KindPlugin:
		return providerpkg.HasSourceProviderPackage(root)
	case providermanifestv1.KindAuth, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindSecrets:
		return providerpkg.HasSourceComponentPackage(root, kind)
	default:
		return false, fmt.Errorf("unsupported release build target kind %q", kind)
	}
}

func validateReleaseBuildTarget(root, kind, goos, goarch string) error {
	switch kind {
	case providermanifestv1.KindPlugin:
		return providerpkg.ValidateSourceProviderRelease(root, goos, goarch)
	case providermanifestv1.KindAuth, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindSecrets:
		return providerpkg.ValidateSourceComponentRelease(root, kind, goos, goarch)
	default:
		return fmt.Errorf("unsupported release build target kind %q", kind)
	}
}

func buildReleaseTargetBinary(root, outputPath, pluginName, kind, goos, goarch string) (string, error) {
	switch kind {
	case providermanifestv1.KindPlugin:
		return providerpkg.BuildSourceProviderReleaseBinary(root, outputPath, pluginName, goos, goarch)
	case providermanifestv1.KindAuth, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindSecrets:
		return providerpkg.BuildSourceComponentReleaseBinary(root, outputPath, kind, goos, goarch)
	default:
		return "", fmt.Errorf("unsupported release build target kind %q", kind)
	}
}

func isMissingReleaseSourceBuildTarget(err error, kind string) bool {
	switch kind {
	case providermanifestv1.KindPlugin:
		return errors.Is(err, providerpkg.ErrNoSourceProviderPackage)
	case providermanifestv1.KindAuth, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindSecrets:
		return errors.Is(err, providerpkg.ErrNoSourceComponentPackage)
	default:
		return false
	}
}

func missingReleaseSourceBuildTargetError(kind string) error {
	switch kind {
	case providermanifestv1.KindPlugin:
		return fmt.Errorf("no Go, Rust, Python, or TypeScript provider package found")
	case providermanifestv1.KindAuth, providermanifestv1.KindIndexedDB, providermanifestv1.KindSecrets:
		return fmt.Errorf("no Go, Rust, Python, or TypeScript %s source package found", kind)
	case providermanifestv1.KindCache:
		return fmt.Errorf("no Go cache source package found")
	default:
		return fmt.Errorf("unsupported release build target kind %q", kind)
	}
}

func parseReleasePlatforms(value string) ([]releasePlatform, error) {
	parts := strings.Split(value, ",")
	platforms := make([]releasePlatform, 0, len(parts))
	for _, part := range parts {
		plat := strings.TrimSpace(part)
		pieces := strings.Split(plat, "/")
		if len(pieces) < 2 || len(pieces) > 3 || pieces[0] == "" || pieces[1] == "" {
			return nil, fmt.Errorf("invalid platform %q, expected os/arch", plat)
		}
		platforms = append(platforms, releasePlatform{
			GOOS:   pieces[0],
			GOARCH: pieces[1],
		})
	}
	return platforms, nil
}

func currentReleasePlatform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

func buildSourceArchive(sourceDir string, srcManifest *providermanifestv1.Manifest, pluginName, version, outputDir, manifestFile, manifestFormat string) (string, error) {
	archiveName := fmt.Sprintf("gestalt-plugin-%s_v%s.tar.gz", pluginName, version)
	return createReleaseArchive(outputDir, archiveName, manifestFile, manifestFormat, func(stagingDir string) (*providermanifestv1.Manifest, error) {
		manifest, err := buildSourceReleaseManifest(srcManifest, version, sourceDir)
		if err != nil {
			return nil, err
		}
		if err := copyReleasePackageFiles(manifest, sourceDir, stagingDir, true); err != nil {
			return nil, err
		}
		return manifest, nil
	})
}

func prepareBuiltPackageDir(stagingDir, sourceDir string, srcManifest *providermanifestv1.Manifest, version, pluginName, buildKind string, platform releasePlatform) (*providermanifestv1.Manifest, releasePlatform, error) {
	plat := platform
	binaryName := releaseBinaryName(pluginName, plat.GOOS)
	binaryPath := filepath.Join(stagingDir, binaryName)
	if _, err := buildReleaseTargetBinary(sourceDir, binaryPath, pluginName, buildKind, plat.GOOS, plat.GOARCH); err != nil {
		return nil, releasePlatform{}, err
	}

	digest, err := providerpkg.FileSHA256(binaryPath)
	if err != nil {
		return nil, releasePlatform{}, fmt.Errorf("hash binary: %w", err)
	}
	manifest, err := buildReleaseManifest(srcManifest, version, binaryName, buildKind, plat, digest)
	if err != nil {
		return nil, releasePlatform{}, err
	}
	if err := copyReleasePackageFiles(manifest, sourceDir, stagingDir, false); err != nil {
		return nil, releasePlatform{}, err
	}
	return manifest, plat, nil
}

func buildSourceReleaseManifest(srcManifest *providermanifestv1.Manifest, version, sourceDir string) (*providermanifestv1.Manifest, error) {
	manifest, err := cloneManifest(srcManifest)
	if err != nil {
		return nil, fmt.Errorf("clone manifest: %w", err)
	}
	manifest.Version = version
	manifest.Release = nil

	for i, artifact := range srcManifest.Artifacts {
		digest, err := providerpkg.FileSHA256(filepath.Join(sourceDir, filepath.FromSlash(artifact.Path)))
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

func buildReleaseManifest(srcManifest *providermanifestv1.Manifest, version, binaryName, buildKind string, plat releasePlatform, digest string) (*providermanifestv1.Manifest, error) {
	manifest, err := cloneManifest(srcManifest)
	if err != nil {
		return nil, fmt.Errorf("clone manifest: %w", err)
	}
	manifest.Version = version
	manifest.Release = nil
	manifest.Artifacts = []providermanifestv1.Artifact{
		{OS: plat.GOOS, Arch: plat.GOARCH, Path: binaryName, SHA256: digest},
	}

	providerpkg.EnsureEntrypoint(manifest).ArtifactPath = binaryName

	return manifest, nil
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

func cloneManifest(manifest *providermanifestv1.Manifest) (*providermanifestv1.Manifest, error) {
	if manifest == nil {
		return nil, nil
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}

	var cloned providermanifestv1.Manifest
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

func copyReleasePackageFiles(manifest *providermanifestv1.Manifest, sourceDir, stagingDir string, includeArtifacts bool) error {
	if manifest == nil {
		return nil
	}
	if err := stageReleaseOwnedUI(manifest, sourceDir, stagingDir); err != nil {
		return err
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

	if err := copyPath(manifest.IconFile, false); err != nil {
		return err
	}
	for _, ref := range providerpkg.LocalPackageReferences(manifest) {
		if err := copyPath(ref.Path, false); err != nil {
			return err
		}
	}
	if manifest.Kind == providermanifestv1.KindPlugin && manifest.Spec != nil {
		if err := copyPath(providerpkg.StaticCatalogFile, !providerpkg.StaticCatalogRequired(manifest)); err != nil {
			return err
		}
	}
	if manifest.Spec != nil && manifest.Spec.ConfigSchemaPath != "" {
		if err := copyPath(manifest.Spec.ConfigSchemaPath, false); err != nil {
			return err
		}
	}
	if manifest.Spec != nil && manifest.Spec.AssetRoot != "" {
		if err := copyPath(manifest.Spec.AssetRoot, false); err != nil {
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

func stageReleaseOwnedUI(manifest *providermanifestv1.Manifest, sourceDir, stagingDir string) error {
	if manifest == nil || manifest.Kind != providermanifestv1.KindPlugin || manifest.Spec == nil || manifest.Spec.UI == nil {
		return nil
	}
	ownedUI := manifest.Spec.UI
	if strings.TrimSpace(ownedUI.Path) == "" {
		return nil
	}

	uiManifestPath := filepath.Join(sourceDir, filepath.FromSlash(ownedUI.Path))
	_, uiManifest, err := providerpkg.ReadSourceManifestFile(uiManifestPath)
	if err != nil {
		return fmt.Errorf("read owned ui manifest %s: %w", ownedUI.Path, err)
	}
	if err := providerpkg.RunSourceReleaseBuild(uiManifestPath, uiManifest); err != nil {
		return fmt.Errorf("build owned ui package %s: %w", ownedUI.Path, err)
	}

	packagedManifest, err := cloneManifest(uiManifest)
	if err != nil {
		return fmt.Errorf("clone owned ui manifest %s: %w", ownedUI.Path, err)
	}
	packagedManifest.Release = nil

	packagedRelPath := packagedOwnedUIManifestPath(ownedUI.Path)
	packagedDir := filepath.Join(stagingDir, filepath.FromSlash(path.Dir(packagedRelPath)))
	if err := copyReleasePackageFiles(packagedManifest, filepath.Dir(uiManifestPath), packagedDir, true); err != nil {
		return fmt.Errorf("copy owned ui package %s: %w", ownedUI.Path, err)
	}
	if err := writeReleaseManifestFile(packagedDir, path.Base(packagedRelPath), providerpkg.ManifestFormatFromPath(uiManifestPath), packagedManifest); err != nil {
		return fmt.Errorf("write owned ui manifest %s: %w", ownedUI.Path, err)
	}

	ownedUI.Path = packagedRelPath
	return nil
}

func packagedOwnedUIManifestPath(rel string) string {
	cleanRel := path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	manifestFile := path.Base(cleanRel)
	parent := path.Base(path.Dir(cleanRel))
	if parent == "." || parent == "/" || parent == "" {
		return path.Join(releaseOwnedUIRoot, manifestFile)
	}
	return path.Join(releaseOwnedUIRoot, parent, manifestFile)
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
