package pluginstore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"slices"

	pluginpkg "github.com/valon-technologies/gestalt/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/internal/pluginsource"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/manifest/v1"
)

func storeRootForConfigPath(configPath string) string {
	if configPath == "" {
		return filepath.Join(".gestalt", "plugins")
	}
	return filepath.Join(filepath.Dir(configPath), ".gestalt", "plugins")
}

type Store struct {
	root string
}

func New(configPath string) *Store {
	return &Store{root: storeRootForConfigPath(configPath)}
}

type InstalledPlugin struct {
	Source         string
	Root           string
	ManifestPath   string
	ExecutablePath string
	AssetRoot      string
	ArtifactPath   string
	SHA256         string
	Manifest       *pluginmanifestv1.Manifest
	Artifact       *pluginmanifestv1.Artifact
}

func isWebUIOnly(manifest *pluginmanifestv1.Manifest) bool {
	return len(manifest.Kinds) == 1 && manifest.Kinds[0] == pluginmanifestv1.KindWebUI
}

func (s *Store) Install(packagePath string) (*InstalledPlugin, error) {
	if s == nil {
		return nil, fmt.Errorf("store is required")
	}
	_, manifest, err := pluginpkg.ReadPackageManifest(packagePath)
	if err != nil {
		return nil, err
	}

	destDir, err := s.destDirForManifest(manifest)
	if err != nil {
		return nil, err
	}

	if isWebUIOnly(manifest) {
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return nil, fmt.Errorf("create plugin directory: %w", err)
		}
		if err := pluginpkg.ExtractPackage(packagePath, destDir); err != nil {
			return nil, err
		}
		manifestPath, _ := pluginpkg.FindManifestFile(destDir)
		if manifestPath == "" {
			manifestPath = filepath.Join(destDir, pluginpkg.ManifestFile)
		}
		assetRoot := filepath.Join(destDir, filepath.FromSlash(manifest.WebUI.AssetRoot))
		installed := buildInstalledPlugin(manifest, destDir, manifestPath, "", nil, assetRoot)
		return installed, nil
	}

	if manifest.Provider.IsDeclarative() && !slices.Contains(manifest.Kinds, pluginmanifestv1.KindRuntime) {
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return nil, fmt.Errorf("create plugin directory: %w", err)
		}
		if err := pluginpkg.ExtractPackage(packagePath, destDir); err != nil {
			return nil, err
		}
		manifestPath, err := pluginpkg.FindManifestFile(destDir)
		if err != nil {
			manifestPath = filepath.Join(destDir, pluginpkg.ManifestFile)
		}
		installed := buildInstalledPlugin(manifest, destDir, manifestPath, "", nil, "")
		return installed, nil
	}

	artifact, err := pluginpkg.CurrentPlatformArtifact(manifest)
	if err != nil {
		return nil, err
	}
	artifactBytes, err := pluginpkg.ReadArchiveEntry(packagePath, artifact.Path)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(artifactBytes)
	if got := hex.EncodeToString(sum[:]); got != artifact.SHA256 {
		return nil, fmt.Errorf("artifact digest mismatch for %s: package has %s, manifest expects %s", artifact.Path, got, artifact.SHA256)
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("create plugin directory: %w", err)
	}
	if err := pluginpkg.ExtractPackage(packagePath, destDir); err != nil {
		return nil, err
	}

	manifestPath, _ := pluginpkg.FindManifestFile(destDir)
	if manifestPath == "" {
		manifestPath = filepath.Join(destDir, pluginpkg.ManifestFile)
	}
	executablePath, err := executablePathForManifest(destDir, manifest)
	if err != nil {
		return nil, err
	}

	installed := buildInstalledPlugin(manifest, destDir, manifestPath, executablePath, artifact, "")
	return installed, nil
}

func (s *Store) InstallFromDir(dirPath string) (*InstalledPlugin, error) {
	if s == nil {
		return nil, fmt.Errorf("store is required")
	}
	_, manifest, _, err := pluginpkg.LoadManifestFromPath(dirPath)
	if err != nil {
		return nil, err
	}

	destDir, err := s.destDirForManifest(manifest)
	if err != nil {
		return nil, err
	}

	if isWebUIOnly(manifest) {
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return nil, fmt.Errorf("create plugin directory: %w", err)
		}
		if err := copyDir(dirPath, destDir); err != nil {
			return nil, fmt.Errorf("copy plugin directory: %w", err)
		}
		mfPath, _ := pluginpkg.FindManifestFile(destDir)
		if mfPath == "" {
			mfPath = filepath.Join(destDir, pluginpkg.ManifestFile)
		}
		assetRoot := filepath.Join(destDir, filepath.FromSlash(manifest.WebUI.AssetRoot))
		installed := buildInstalledPlugin(manifest, destDir, mfPath, "", nil, assetRoot)
		return installed, nil
	}

	if manifest.Provider.IsDeclarative() && !slices.Contains(manifest.Kinds, pluginmanifestv1.KindRuntime) {
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return nil, fmt.Errorf("create plugin directory: %w", err)
		}
		manifestSrc, err := pluginpkg.FindManifestFile(dirPath)
		if err != nil {
			return nil, fmt.Errorf("find manifest: %w", err)
		}
		manifestDest := filepath.Join(destDir, filepath.Base(manifestSrc))
		if err := copyFile(manifestSrc, manifestDest); err != nil {
			return nil, fmt.Errorf("copy manifest: %w", err)
		}
		for _, schemaRel := range configSchemaPaths(manifest, dirPath) {
			schemaSrc := filepath.Join(dirPath, filepath.FromSlash(schemaRel))
			schemaDest := filepath.Join(destDir, filepath.FromSlash(schemaRel))
			if err := os.MkdirAll(filepath.Dir(schemaDest), 0755); err != nil {
				return nil, fmt.Errorf("create schema directory: %w", err)
			}
			if err := copyFile(schemaSrc, schemaDest); err != nil {
				return nil, fmt.Errorf("copy config schema %s: %w", schemaRel, err)
			}
		}
		installed := buildInstalledPlugin(manifest, destDir, manifestDest, "", nil, "")
		return installed, nil
	}

	artifact, err := pluginpkg.CurrentPlatformArtifact(manifest)
	if err != nil {
		return nil, err
	}

	artifactPath := filepath.Join(dirPath, filepath.FromSlash(artifact.Path))
	artifactFile, err := os.Open(artifactPath)
	if err != nil {
		return nil, fmt.Errorf("open artifact %s: %w", artifact.Path, err)
	}
	sum := sha256.New()
	if _, err := io.Copy(sum, artifactFile); err != nil {
		_ = artifactFile.Close()
		return nil, fmt.Errorf("read artifact %s: %w", artifact.Path, err)
	}
	_ = artifactFile.Close()
	if got := hex.EncodeToString(sum.Sum(nil)); got != artifact.SHA256 {
		return nil, fmt.Errorf("artifact digest mismatch for %s: directory has %s, manifest expects %s", artifact.Path, got, artifact.SHA256)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("create plugin directory: %w", err)
	}

	manifestSrc, err := pluginpkg.FindManifestFile(dirPath)
	if err != nil {
		return nil, fmt.Errorf("find manifest: %w", err)
	}
	manifestDest := filepath.Join(destDir, filepath.Base(manifestSrc))
	if err := copyFile(manifestSrc, manifestDest); err != nil {
		return nil, fmt.Errorf("copy manifest: %w", err)
	}

	artifactDest := filepath.Join(destDir, filepath.FromSlash(artifact.Path))
	if err := os.MkdirAll(filepath.Dir(artifactDest), 0755); err != nil {
		return nil, fmt.Errorf("create artifact directory: %w", err)
	}
	if err := copyFile(artifactPath, artifactDest); err != nil {
		return nil, fmt.Errorf("copy artifact: %w", err)
	}

	for _, schemaRel := range configSchemaPaths(manifest, dirPath) {
		schemaSrc := filepath.Join(dirPath, filepath.FromSlash(schemaRel))
		schemaDest := filepath.Join(destDir, filepath.FromSlash(schemaRel))
		if err := os.MkdirAll(filepath.Dir(schemaDest), 0755); err != nil {
			return nil, fmt.Errorf("create schema directory: %w", err)
		}
		if err := copyFile(schemaSrc, schemaDest); err != nil {
			return nil, fmt.Errorf("copy config schema %s: %w", schemaRel, err)
		}
	}

	executablePath, err := executablePathForManifest(destDir, manifest)
	if err != nil {
		return nil, err
	}

	installed := buildInstalledPlugin(manifest, destDir, manifestDest, executablePath, artifact, "")
	return installed, nil
}

func (s *Store) destDirForManifest(manifest *pluginmanifestv1.Manifest) (string, error) {
	if s == nil || s.root == "" {
		return "", fmt.Errorf("store root is required")
	}
	if manifest == nil {
		return "", fmt.Errorf("manifest is required")
	}
	src, err := pluginsource.Parse(manifest.Source)
	if err != nil {
		return "", fmt.Errorf("manifest source: %w", err)
	}
	destDir := filepath.Join(s.root, filepath.FromSlash(src.StorePath()), manifest.Version)
	return destDir, nil
}

func buildInstalledPlugin(manifest *pluginmanifestv1.Manifest, destDir, manifestPath, executablePath string, artifact *pluginmanifestv1.Artifact, assetRoot string) *InstalledPlugin {
	ip := &InstalledPlugin{
		Source:         manifest.Source,
		Root:           destDir,
		ManifestPath:   manifestPath,
		ExecutablePath: executablePath,
		AssetRoot:      assetRoot,
		Manifest:       manifest,
		Artifact:       artifact,
	}
	if artifact != nil {
		ip.ArtifactPath = artifact.Path
		ip.SHA256 = artifact.SHA256
	}
	return ip
}

func executablePathForManifest(root string, manifest *pluginmanifestv1.Manifest) (string, error) {
	if manifest == nil {
		return "", fmt.Errorf("manifest is required")
	}
	if isWebUIOnly(manifest) {
		return "", nil
	}
	if manifest.Provider.IsDeclarative() && !slices.Contains(manifest.Kinds, pluginmanifestv1.KindRuntime) {
		return "", nil
	}
	var entry *pluginmanifestv1.Entrypoint
	for _, kind := range manifest.Kinds {
		switch kind {
		case pluginmanifestv1.KindProvider:
			entry = manifest.Entrypoints.Provider
		case pluginmanifestv1.KindRuntime:
			entry = manifest.Entrypoints.Runtime
		default:
			continue
		}
		if entry != nil {
			break
		}
	}
	if entry == nil {
		return "", fmt.Errorf("manifest does not define an executable entrypoint")
	}
	if entry.ArtifactPath == "" {
		return "", fmt.Errorf("manifest entrypoint artifact_path is required")
	}
	return filepath.Join(root, filepath.FromSlash(entry.ArtifactPath)), nil
}

const runtimeConfigSchemaPath = "schemas/config.schema.json"

func configSchemaPaths(manifest *pluginmanifestv1.Manifest, dirPath string) []string {
	var paths []string
	if manifest.Provider != nil && manifest.Provider.ConfigSchemaPath != "" {
		paths = append(paths, manifest.Provider.ConfigSchemaPath)
	}
	runtimeSchema := filepath.Join(dirPath, filepath.FromSlash(runtimeConfigSchemaPath))
	if _, err := os.Stat(runtimeSchema); err == nil {
		paths = append(paths, runtimeConfigSchemaPath)
	}
	return paths
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
