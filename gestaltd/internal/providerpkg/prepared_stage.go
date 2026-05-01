package providerpkg

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
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
	BuildKind       string
	PluginName      string
	GOOS            string
	GOARCH          string
}

type StageSourcePreparedInstallOptions struct {
	Kind       string
	PluginName string
	GOOS       string
	GOARCH     string
}

type StagedPreparedInstall struct {
	Manifest       *providermanifestv1.Manifest
	ManifestPath   string
	ManifestFile   string
	ManifestFormat string
}

// StageSourcePreparedInstallDir stages a source tree into its prepared-install layout.
// It runs any source release build hook, determines whether a host-platform executable
// build is needed, and then delegates to StagePreparedInstallDir for the final layout.
func StageSourcePreparedInstallDir(manifestPath, stagingDir string, opts StageSourcePreparedInstallOptions) (*StagedPreparedInstall, error) {
	if strings.TrimSpace(manifestPath) == "" {
		return nil, fmt.Errorf("manifest path is required")
	}
	_, manifest, err := ReadSourceManifestFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", manifestPath, err)
	}
	if err := RunSourceReleaseBuild(manifestPath, manifest); err != nil {
		return nil, err
	}
	_, manifest, err = ReadSourceManifestFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read %s after release build: %w", manifestPath, err)
	}
	kind := strings.TrimSpace(opts.Kind)
	if kind == "" {
		kind, err = ManifestKind(manifest)
		if err != nil {
			return nil, err
		}
	}
	buildKind, err := resolvePreparedInstallBuildKind(filepath.Dir(manifestPath), manifest, kind)
	if err != nil {
		return nil, err
	}
	return StagePreparedInstallDir(manifestPath, stagingDir, StagePreparedInstallOptions{
		BuildKind:  buildKind,
		PluginName: opts.PluginName,
		GOOS:       opts.GOOS,
		GOARCH:     opts.GOARCH,
	})
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

	sourceDir := filepath.Dir(manifestPath)
	manifestFormat := ManifestFormatFromPath(manifestPath)
	manifestFile := preparedManifestFileName(manifestFormat)

	_, _, err = ReadSourceManifestFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", manifestPath, err)
	}
	_, srcManifest, err := PrepareSourceManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("prepare %s: %w", manifestPath, err)
	}

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

	var stagedManifest *providermanifestv1.Manifest
	switch buildKind := strings.TrimSpace(opts.BuildKind); {
	case buildKind != "":
		if opts.GOOS == "" || opts.GOARCH == "" {
			return nil, fmt.Errorf("goos and goarch are required when build kind is set")
		}
		binaryName := stagedReleaseBinaryName(pluginName, opts.GOOS)
		binaryPath := filepath.Join(stagingDir, binaryName)
		if _, err := buildPreparedInstallBinary(sourceDir, binaryPath, pluginName, buildKind, opts.GOOS, opts.GOARCH); err != nil {
			return nil, err
		}
		digest, err := FileSHA256(binaryPath)
		if err != nil {
			return nil, fmt.Errorf("hash binary: %w", err)
		}
		stagedManifest, err = buildPreparedInstallManifest(srcManifest, version, binaryName, opts.GOOS, opts.GOARCH, digest)
		if err != nil {
			return nil, err
		}
		if err := copyPreparedInstallSupportFiles(stagedManifest, sourceDir, stagingDir, false); err != nil {
			return nil, err
		}
	default:
		stagedManifest, err = buildPreparedInstallSourceManifest(srcManifest, version, sourceDir)
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

func buildPreparedInstallSourceManifest(srcManifest *providermanifestv1.Manifest, version, sourceDir string) (*providermanifestv1.Manifest, error) {
	manifest, err := cloneManifest(srcManifest)
	if err != nil {
		return nil, fmt.Errorf("clone manifest: %w", err)
	}
	manifest.Version = version
	manifest.Release = nil

	for i, artifact := range srcManifest.Artifacts {
		digest, err := FileSHA256(filepath.Join(sourceDir, filepath.FromSlash(artifact.Path)))
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

func buildPreparedInstallManifest(srcManifest *providermanifestv1.Manifest, version, binaryName, goos, goarch, digest string) (*providermanifestv1.Manifest, error) {
	manifest, err := cloneManifest(srcManifest)
	if err != nil {
		return nil, fmt.Errorf("clone manifest: %w", err)
	}
	manifest.Version = version
	manifest.Release = nil
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
	_, uiManifest, err := ReadSourceManifestFile(uiManifestPath)
	if err != nil {
		return fmt.Errorf("read owned ui manifest %s: %w", ownedUI.Path, err)
	}
	if err := RunSourceReleaseBuild(uiManifestPath, uiManifest); err != nil {
		return fmt.Errorf("build owned ui package %s: %w", ownedUI.Path, err)
	}
	packagedDir := filepath.Join(stagingDir, filepath.FromSlash(packagedOwnedUIDir(ownedUI.Path)))
	staged, err := StagePreparedInstallDir(uiManifestPath, packagedDir, StagePreparedInstallOptions{})
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

func stagedReleaseBinaryName(pluginName, goos string) string {
	binaryName := preparedReleaseBinaryPrefix + pluginName
	if goos == windowsOS {
		return binaryName + windowsExecutableSuffix
	}
	return binaryName
}
