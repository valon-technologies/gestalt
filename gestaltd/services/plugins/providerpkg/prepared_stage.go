package providerpkg

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/plugins/source"
)

const (
	releaseOwnedUIRoot          = "_owned_ui"
	preparedReleaseBinaryPrefix = "gestalt-plugin-"
	windowsOS                   = "windows"
	windowsExecutableSuffix     = ".exe"
)

type StagePreparedInstallOptions struct {
	VersionOverride string
	PluginName      string
	GOOS            string
	GOARCH          string
}

type StageSourcePreparedInstallOptions struct {
	Kind            string
	VersionOverride string
	PluginName      string
	GOOS            string
	GOARCH          string
}

type StagedPreparedInstall struct {
	Manifest       *providermanifestv1.Manifest
	ManifestPath   string
	ManifestFile   string
	ManifestFormat string
}

// StageSourcePreparedInstallDir stages a source tree into its prepared-install layout.
// It runs any declared source build and then delegates to StagePreparedInstallDir for
// the final prepared manifest and support-file layout.
func StageSourcePreparedInstallDir(manifestPath, stagingDir string, opts StageSourcePreparedInstallOptions) (*StagedPreparedInstall, error) {
	if strings.TrimSpace(manifestPath) == "" {
		return nil, fmt.Errorf("manifest path is required")
	}
	absoluteManifestPath, err := filepath.Abs(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("resolve manifest path: %w", err)
	}
	manifestPath = absoluteManifestPath

	_, manifest, err := ReadSourceManifestFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", manifestPath, err)
	}
	if strings.TrimSpace(opts.Kind) == "" {
		if _, err := ManifestKind(manifest); err != nil {
			return nil, err
		}
	}
	targetOpts := SourceBuildOptions{GOOS: opts.GOOS, GOARCH: opts.GOARCH}
	hostBuiltForCatalog, err := ensureHostBuildForSourceStaticCatalog(manifestPath, manifest)
	if err != nil {
		return nil, err
	}
	_, srcManifest, err := PrepareSourceManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("prepare %s: %w", manifestPath, err)
	}
	if !hostBuiltForCatalog || !sourceBuildTargetsHost(targetOpts) {
		if err := EnsureSourceBuildOutput(manifestPath, manifest, targetOpts); err != nil {
			return nil, err
		}
	}
	return stagePreparedInstallDir(manifestPath, stagingDir, srcManifest, StagePreparedInstallOptions{
		VersionOverride: opts.VersionOverride,
		PluginName:      opts.PluginName,
		GOOS:            opts.GOOS,
		GOARCH:          opts.GOARCH,
	})
}

func ensureHostBuildForSourceStaticCatalog(manifestPath string, manifest *providermanifestv1.Manifest) (bool, error) {
	mayGenerate, err := sourceStaticCatalogMayBeGenerated(manifestPath, manifest)
	if err != nil {
		return false, err
	}
	if EffectiveSourceBuild(manifest) == nil || !mayGenerate {
		return false, nil
	}
	if err := EnsureSourceBuildOutput(manifestPath, manifest, SourceBuildOptions{}); err != nil {
		return false, err
	}
	return true, nil
}

func sourceStaticCatalogMayBeGenerated(manifestPath string, manifest *providermanifestv1.Manifest) (bool, error) {
	if manifest == nil || manifest.Kind != providermanifestv1.KindPlugin {
		return false, nil
	}
	entry := EntrypointForKind(manifest, providermanifestv1.KindPlugin)
	if entry == nil || strings.TrimSpace(entry.ArtifactPath) == "" {
		return false, nil
	}
	if _, err := os.Stat(StaticCatalogPath(filepath.Dir(manifestPath))); err == nil {
		return false, nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("stat static catalog %q: %w", StaticCatalogFile, err)
	}
	return true, nil
}

func sourceBuildTargetsHost(opts SourceBuildOptions) bool {
	goos, goarch := SourceBuildTarget(opts)
	return goos == runtime.GOOS && goarch == runtime.GOARCH
}

// StagePreparedInstallDir stages a source manifest into its prepared-install layout.
// It is the shared host-platform staging layer used by release packaging and local preparation.
func StagePreparedInstallDir(manifestPath, stagingDir string, opts StagePreparedInstallOptions) (*StagedPreparedInstall, error) {
	if strings.TrimSpace(manifestPath) == "" {
		return nil, fmt.Errorf("manifest path is required")
	}
	if strings.TrimSpace(stagingDir) == "" {
		return nil, fmt.Errorf("staging directory is required")
	}

	absoluteManifestPath, err := filepath.Abs(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("resolve manifest path: %w", err)
	}
	manifestPath = absoluteManifestPath

	_, _, err = ReadSourceManifestFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", manifestPath, err)
	}
	_, srcManifest, err := PrepareSourceManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("prepare %s: %w", manifestPath, err)
	}
	return stagePreparedInstallDir(manifestPath, stagingDir, srcManifest, opts)
}

func stagePreparedInstallDir(manifestPath, stagingDir string, srcManifest *providermanifestv1.Manifest, opts StagePreparedInstallOptions) (*StagedPreparedInstall, error) {
	if strings.TrimSpace(stagingDir) == "" {
		return nil, fmt.Errorf("staging directory is required")
	}
	sourceDir := filepath.Dir(manifestPath)
	manifestFormat := ManifestFormatFromPath(manifestPath)
	manifestFile := preparedManifestFileName(manifestFormat)
	version := srcManifest.Version
	if strings.TrimSpace(opts.VersionOverride) != "" {
		version = strings.TrimSpace(opts.VersionOverride)
	}
	pluginName := strings.TrimSpace(opts.PluginName)
	if pluginName == "" {
		src, err := source.Parse(srcManifest.Source)
		if err != nil {
			return nil, fmt.Errorf("invalid source in manifest: %w", err)
		}
		pluginName = src.PluginName()
	}

	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := opts.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}

	var stagedManifest *providermanifestv1.Manifest
	buildKind := ""
	if EffectiveSourceBuild(srcManifest) == nil {
		var err error
		buildKind, err = resolvePreparedInstallBuildKind(sourceDir, srcManifest, "")
		if err != nil {
			return nil, err
		}
	}
	if buildKind != "" {
		binaryName := stagedReleaseBinaryName(pluginName, goos)
		binaryPath := filepath.Join(stagingDir, binaryName)
		if _, err := buildPreparedInstallBinary(sourceDir, binaryPath, pluginName, buildKind, goos, goarch); err != nil {
			return nil, err
		}
		digest, digestErr := FileSHA256(binaryPath)
		if digestErr != nil {
			return nil, fmt.Errorf("hash binary: %w", digestErr)
		}
		var err error
		stagedManifest, err = buildPreparedInstallManifest(srcManifest, version, binaryName, goos, goarch, digest)
		if err != nil {
			return nil, err
		}
		if err := copyPreparedInstallSupportFiles(stagedManifest, sourceDir, stagingDir, false); err != nil {
			return nil, err
		}
	} else {
		var err error
		stagedManifest, err = buildPreparedInstallSourceManifest(srcManifest, version, sourceDir, goos, goarch)
		if err != nil {
			return nil, err
		}
		if err := copyPreparedInstallSupportFiles(stagedManifest, sourceDir, stagingDir, true); err != nil {
			return nil, err
		}
	}

	stagedManifestPath := filepath.Join(stagingDir, manifestFile)
	if err := writePreparedManifestFile(stagedManifestPath, manifestFormat, stagedManifest); err != nil {
		return nil, err
	}

	return &StagedPreparedInstall{
		Manifest:       stagedManifest,
		ManifestPath:   stagedManifestPath,
		ManifestFile:   manifestFile,
		ManifestFormat: manifestFormat,
	}, nil
}

func preparedManifestFileName(format string) string {
	switch format {
	case ManifestFormatYAML:
		return "manifest.yaml"
	default:
		return ManifestFile
	}
}

func resolvePreparedInstallBuildKind(root string, manifest *providermanifestv1.Manifest, kind string) (string, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		var err error
		kind, err = ManifestKind(manifest)
		if err != nil {
			return "", err
		}
	}
	if kind == providermanifestv1.KindUI {
		return "", nil
	}

	if buildKind, err := resolvePreparedInstallBuildTarget(root, kind); err == nil {
		return buildKind, nil
	} else if !isMissingPreparedInstallBuildTarget(err, kind) {
		return "", err
	}

	entry := EntrypointForKind(manifest, kind)
	if artifactExistsForEntrypoint(root, entry) {
		return "", nil
	}

	if preparedInstallRequiresBuild(manifest, kind) {
		return "", missingPreparedInstallBuildTargetError(kind)
	}
	return "", nil
}

func preparedInstallRequiresBuild(manifest *providermanifestv1.Manifest, kind string) bool {
	switch kind {
	case providermanifestv1.KindPlugin:
		return manifest != nil && manifest.Entrypoint == nil && (manifest.Spec == nil || !manifest.Spec.IsManifestBacked())
	case providermanifestv1.KindAuthentication, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime:
		return EntrypointForKind(manifest, kind) == nil
	default:
		return false
	}
}

func resolvePreparedInstallBuildTarget(root, kind string) (string, error) {
	switch kind {
	case providermanifestv1.KindPlugin:
		ok, err := HasSourceProviderPackage(root)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", ErrNoSourceProviderPackage
		}
		return kind, nil
	case providermanifestv1.KindAuthentication, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime:
		ok, err := HasSourceComponentPackage(root, kind)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", ErrNoSourceComponentPackage
		}
		return kind, nil
	default:
		return "", fmt.Errorf("unsupported release build target kind %q", kind)
	}
}

func isMissingPreparedInstallBuildTarget(err error, kind string) bool {
	switch kind {
	case providermanifestv1.KindPlugin:
		return errors.Is(err, ErrNoSourceProviderPackage)
	case providermanifestv1.KindAuthentication, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime:
		return errors.Is(err, ErrNoSourceComponentPackage)
	default:
		return false
	}
}

func missingPreparedInstallBuildTargetError(kind string) error {
	switch kind {
	case providermanifestv1.KindPlugin:
		return ErrNoSourceProviderPackage
	case providermanifestv1.KindAuthentication, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime:
		return ErrNoSourceComponentPackage
	default:
		return fmt.Errorf("unsupported release build target kind %q", kind)
	}
}

func artifactExistsForEntrypoint(root string, entry *providermanifestv1.Entrypoint) bool {
	if entry == nil || strings.TrimSpace(entry.ArtifactPath) == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(entry.ArtifactPath)))
	return err == nil
}

func buildPreparedInstallBinary(root, outputPath, pluginName, kind, goos, goarch string) (string, error) {
	switch kind {
	case providermanifestv1.KindPlugin:
		return BuildSourceProviderReleaseBinary(root, outputPath, pluginName, goos, goarch)
	case providermanifestv1.KindAuthentication, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime:
		return BuildSourceComponentReleaseBinary(root, outputPath, kind, goos, goarch)
	default:
		return "", fmt.Errorf("unsupported release build target kind %q", kind)
	}
}

func buildPreparedInstallSourceManifest(srcManifest *providermanifestv1.Manifest, version, sourceDir, goos, goarch string) (*providermanifestv1.Manifest, error) {
	manifest, err := cloneManifest(srcManifest)
	if err != nil {
		return nil, fmt.Errorf("clone manifest: %w", err)
	}
	manifest.Version = version
	manifest.Build = nil
	manifest.Artifacts = nil

	kind, err := ManifestKind(manifest)
	if err != nil {
		return nil, err
	}
	entry := EntrypointForKind(manifest, kind)
	if entry != nil && strings.TrimSpace(entry.ArtifactPath) != "" {
		artifactPath := entry.ArtifactPath
		digest, err := FileSHA256(filepath.Join(sourceDir, filepath.FromSlash(artifactPath)))
		if err != nil {
			return nil, fmt.Errorf("hash artifact %s: %w", artifactPath, err)
		}
		manifest.Artifacts = []providermanifestv1.Artifact{
			{OS: goos, Arch: goarch, Path: artifactPath, SHA256: digest},
		}
	}

	return manifest, nil
}

func buildPreparedInstallManifest(srcManifest *providermanifestv1.Manifest, version, binaryName, goos, goarch, digest string) (*providermanifestv1.Manifest, error) {
	manifest, err := cloneManifest(srcManifest)
	if err != nil {
		return nil, fmt.Errorf("clone manifest: %w", err)
	}
	manifest.Version = version
	manifest.Build = nil
	manifest.Artifacts = []providermanifestv1.Artifact{
		{OS: goos, Arch: goarch, Path: binaryName, SHA256: digest},
	}
	EnsureEntrypoint(manifest).ArtifactPath = binaryName
	return manifest, nil
}

func writePreparedManifestFile(path, manifestFormat string, manifest *providermanifestv1.Manifest) error {
	data, err := EncodeManifestFormat(manifest, manifestFormat)
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

func copyPreparedInstallDir(src, dst string) error {
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
		return copyPreparedInstallFile(path, target)
	})
}

func copyPreparedInstallFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
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

func copyPreparedInstallSupportFiles(manifest *providermanifestv1.Manifest, sourceDir, stagingDir string, includeArtifacts bool) error {
	if manifest == nil {
		return nil
	}
	if err := stagePreparedOwnedUI(manifest, sourceDir, stagingDir); err != nil {
		return err
	}

	copied := make(map[string]struct{})
	copyPath := func(rel string, optional bool) error {
		if rel == "" {
			return nil
		}

		cleanRel, err := normalizePreparedInstallPath(rel)
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
			if err := copyPreparedInstallDir(srcPath, dstPath); err != nil {
				return fmt.Errorf("copy support directory %s: %w", rel, err)
			}
			return nil
		}
		if err := copyPreparedInstallFile(srcPath, dstPath); err != nil {
			return fmt.Errorf("copy support file %s: %w", rel, err)
		}
		return nil
	}

	if err := copyPath(manifest.IconFile, false); err != nil {
		return err
	}
	for _, ref := range LocalPackageReferences(manifest) {
		if err := copyPath(ref.Path, false); err != nil {
			return err
		}
	}
	if manifest.Kind == providermanifestv1.KindPlugin && manifest.Spec != nil {
		if err := copyPath(StaticCatalogFile, !StaticCatalogRequired(manifest)); err != nil {
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

func stagePreparedOwnedUI(manifest *providermanifestv1.Manifest, sourceDir, stagingDir string) error {
	if manifest == nil || manifest.Kind != providermanifestv1.KindPlugin || manifest.Spec == nil || manifest.Spec.UI == nil {
		return nil
	}
	ownedUI := manifest.Spec.UI
	if strings.TrimSpace(ownedUI.Path) == "" {
		return nil
	}

	uiManifestPath := filepath.Join(sourceDir, filepath.FromSlash(ownedUI.Path))
	_, _, err := ReadSourceManifestFile(uiManifestPath)
	if err != nil {
		return fmt.Errorf("read owned ui manifest %s: %w", ownedUI.Path, err)
	}
	packagedDir := filepath.Join(stagingDir, filepath.FromSlash(packagedOwnedUIDir(ownedUI.Path)))
	staged, err := StageSourcePreparedInstallDir(uiManifestPath, packagedDir, StageSourcePreparedInstallOptions{})
	if err != nil {
		return fmt.Errorf("stage owned ui package %s: %w", ownedUI.Path, err)
	}
	packagedRelPath, err := filepath.Rel(stagingDir, staged.ManifestPath)
	if err != nil {
		return fmt.Errorf("resolve staged owned ui manifest %s: %w", ownedUI.Path, err)
	}
	packagedRelPath, err = normalizePreparedInstallPath(filepath.ToSlash(packagedRelPath))
	if err != nil {
		return fmt.Errorf("normalize staged owned ui manifest %s: %w", ownedUI.Path, err)
	}

	ownedUI.Path = packagedRelPath
	return nil
}

func packagedOwnedUIDir(rel string) string {
	cleanRel := path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	parent := path.Base(path.Dir(cleanRel))
	if parent == "." || parent == "/" || parent == "" {
		return releaseOwnedUIRoot
	}
	return path.Join(releaseOwnedUIRoot, parent)
}

func stagedReleaseBinaryName(pluginName, goos string) string {
	binaryName := preparedReleaseBinaryPrefix + pluginName
	if goos == windowsOS {
		return binaryName + windowsExecutableSuffix
	}
	return binaryName
}

func normalizePreparedInstallPath(rel string) (string, error) {
	if rel == "" {
		return "", nil
	}

	cleanPath := path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	if path.IsAbs(cleanPath) || cleanPath == ".." || strings.HasPrefix(cleanPath, "../") {
		return "", fmt.Errorf("release path %q must stay within plugin root", rel)
	}
	return cleanPath, nil
}
