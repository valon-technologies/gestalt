package providerpkg

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
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

type StagedPreparedInstall struct {
	Manifest       *providermanifestv1.Manifest
	ManifestPath   string
	ManifestFile   string
	ManifestFormat string
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

	sourceDir := filepath.Dir(manifestPath)
	manifestFile := filepath.Base(manifestPath)
	manifestFormat := ManifestFormatFromPath(manifestPath)

	_, _, err := ReadSourceManifestFile(manifestPath)
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
		src, err := pluginsource.Parse(srcManifest.Source)
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

func buildPreparedInstallBinary(root, outputPath, pluginName, kind, goos, goarch string) (string, error) {
	switch kind {
	case providermanifestv1.KindPlugin:
		return BuildSourceProviderReleaseBinary(root, outputPath, pluginName, goos, goarch)
	case providermanifestv1.KindAuth, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindSecrets:
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
	packagedRelPath := packagedOwnedUIManifestPath(ownedUI.Path)
	packagedDir := filepath.Join(stagingDir, filepath.FromSlash(path.Dir(packagedRelPath)))
	if _, err := StagePreparedInstallDir(uiManifestPath, packagedDir, StagePreparedInstallOptions{}); err != nil {
		return fmt.Errorf("stage owned ui package %s: %w", ownedUI.Path, err)
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
