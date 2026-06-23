package providerpkg

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const (
	releaseOwnedUIRoot          = "_owned_ui"
	preparedReleaseBinaryPrefix = "gestalt-app-"
	windowsOS                   = "windows"
	windowsExecutableSuffix     = ".exe"
)

type StagePreparedInstallOptions struct {
	VersionOverride string
	AppName         string
	GOOS            string
	GOARCH          string
	BuildOutput     CommandOutput
}

type StageSourcePreparedInstallOptions struct {
	Kind            string
	VersionOverride string
	AppName         string
	GOOS            string
	GOARCH          string
	BuildOutput     CommandOutput
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
	if err := ValidateExplicitRunPackaging(filepath.Dir(manifestPath), manifest); err != nil {
		return nil, err
	}
	targetOpts := SourceBuildOptions{GOOS: opts.GOOS, GOARCH: opts.GOARCH, Output: opts.BuildOutput}
	hostBuiltForCatalog, err := ensureHostBuildForSourceStaticCatalog(manifestPath, manifest, SourceBuildOptions{Output: opts.BuildOutput})
	if err != nil {
		return nil, err
	}
	_, srcManifest, err := prepareSourceManifestForPreparedInstallWithOptions(manifestPath, SourceBuildOptions{Output: opts.BuildOutput})
	if err != nil {
		return nil, fmt.Errorf("prepare %s: %w", manifestPath, err)
	}
	if sourceBuildProducesOutputOrImplicitGo(filepath.Dir(manifestPath), manifest) && (!hostBuiltForCatalog || !sourceBuildTargetsHost(targetOpts)) {
		if err := EnsureSourceBuildOutput(manifestPath, manifest, targetOpts); err != nil {
			return nil, err
		}
	}
	return stagePreparedInstallDir(manifestPath, stagingDir, srcManifest, StagePreparedInstallOptions{
		VersionOverride: opts.VersionOverride,
		AppName:         opts.AppName,
		GOOS:            opts.GOOS,
		GOARCH:          opts.GOARCH,
		BuildOutput:     opts.BuildOutput,
	})
}

func ensureHostBuildForSourceStaticCatalog(manifestPath string, manifest *providermanifestv1.Manifest, opts SourceBuildOptions) (bool, error) {
	shouldPrepare, err := sourceStaticCatalogShouldBePreparedForPackaging(manifestPath, manifest)
	if err != nil {
		return false, err
	}
	if !SourceBuildProducesOutput(manifest) || !shouldPrepare {
		return false, nil
	}
	if err := EnsureSourceBuildOutput(manifestPath, manifest, opts); err != nil {
		return false, err
	}
	return true, nil
}

func sourceStaticCatalogShouldBePreparedForPackaging(manifestPath string, manifest *providermanifestv1.Manifest) (bool, error) {
	if manifest == nil || manifest.Kind != providermanifestv1.KindApp {
		return false, nil
	}
	entry := EntrypointForKind(manifest, providermanifestv1.KindApp)
	if entry == nil || strings.TrimSpace(entry.ArtifactPath) == "" {
		return false, nil
	}
	if explicitRunStaleCatalog(manifest) {
		return true, nil
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

	_, manifest, err := ReadSourceManifestFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", manifestPath, err)
	}
	if err := ValidateExplicitRunPackaging(filepath.Dir(manifestPath), manifest); err != nil {
		return nil, err
	}
	_, srcManifest, err := prepareSourceManifestForPreparedInstallWithOptions(manifestPath, SourceBuildOptions{Output: opts.BuildOutput})
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
	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := opts.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}

	var stagedManifest *providermanifestv1.Manifest
	if !SourceBuildProducesOutput(srcManifest) {
		if err := validatePreparedInstallDeclaredBuild(sourceDir, srcManifest, ""); err != nil {
			return nil, err
		}
	}
	stagedManifest, err := buildPreparedInstallSourceManifest(srcManifest, version, sourceDir, goos, goarch)
	if err != nil {
		return nil, err
	}
	if err := copyPreparedInstallSupportFiles(stagedManifest, sourceDir, stagingDir, true); err != nil {
		return nil, err
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

func validatePreparedInstallDeclaredBuild(root string, manifest *providermanifestv1.Manifest, kind string) error {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		var err error
		kind, err = ManifestKind(manifest)
		if err != nil {
			return err
		}
	}
	if kind == providermanifestv1.KindUI {
		return nil
	}
	entry := EntrypointForKind(manifest, kind)
	if artifactExistsForEntrypoint(root, entry) {
		return nil
	}
	if releaseRequiresBuildForKind(manifest, kind) {
		return missingDeclaredSourceBuildError(manifest, kind)
	}
	return nil
}

func artifactExistsForEntrypoint(root string, entry *providermanifestv1.Entrypoint) bool {
	if entry == nil || strings.TrimSpace(entry.ArtifactPath) == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(entry.ArtifactPath)))
	return err == nil
}

func buildPreparedInstallSourceManifest(srcManifest *providermanifestv1.Manifest, version, sourceDir, goos, goarch string) (*providermanifestv1.Manifest, error) {
	manifest, err := cloneManifest(srcManifest)
	if err != nil {
		return nil, fmt.Errorf("clone manifest: %w", err)
	}
	uiAssetRoot := SourceUIBuildOutput(manifest)
	manifest.Version = version
	manifest.Install = nil
	manifest.Build = nil
	manifest.Run = nil
	manifest.Dev = nil
	manifest.Artifacts = nil

	kind, err := ManifestKind(manifest)
	if err != nil {
		return nil, err
	}
	if kind == providermanifestv1.KindUI && uiAssetRoot != "" {
		if manifest.Spec == nil {
			manifest.Spec = &providermanifestv1.Spec{}
		}
		manifest.Spec.AssetRoot = uiAssetRoot
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
	if manifest.Kind == providermanifestv1.KindApp && manifest.Spec != nil {
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
	if manifest == nil || manifest.Kind != providermanifestv1.KindApp || manifest.Spec == nil || manifest.Spec.UI == nil {
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
		return "", fmt.Errorf("release path %q must stay within app root", rel)
	}
	return cleanPath, nil
}
