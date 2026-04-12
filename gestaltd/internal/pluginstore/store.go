package pluginstore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	pluginpkg "github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

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
	kind, err := pluginpkg.ManifestKind(manifest)
	return err == nil && kind == pluginmanifestv1.KindWebUI
}

func manifestNeedsExecutableArtifact(manifest *pluginmanifestv1.Manifest) bool {
	kind, err := pluginpkg.ManifestKind(manifest)
	if err != nil {
		return false
	}
	return pluginpkg.EntrypointForKind(manifest, kind) != nil
}

func Install(packagePath, destDir string) (*InstalledPlugin, error) {
	_, manifest, err := pluginpkg.ReadPackageManifest(packagePath)
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
		assetRoot := filepath.Join(destDir, filepath.FromSlash(manifest.Spec.AssetRoot))
		installed := buildInstalledPlugin(manifest, destDir, manifestPath, "", nil, assetRoot)
		return installed, nil
	}

	var artifact *pluginmanifestv1.Artifact
	if manifestNeedsExecutableArtifact(manifest) {
		artifact, err = pluginpkg.CurrentPlatformArtifact(manifest)
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
	manifest = pluginpkg.ResolveManifestLocalReferences(manifest, manifestPath)
	executablePath, err := executablePathForManifest(destDir, manifest)
	if err != nil {
		return nil, err
	}
	if err := adhocCodesignDarwin(executablePath); err != nil {
		return nil, err
	}

	installed := buildInstalledPlugin(manifest, destDir, manifestPath, executablePath, artifact, "")
	return installed, nil
}

func InstallFromDir(dirPath, destDir string) (*InstalledPlugin, error) {
	_, manifest, _, err := pluginpkg.LoadManifestFromPath(dirPath)
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
		assetRoot := filepath.Join(destDir, filepath.FromSlash(manifest.Spec.AssetRoot))
		installed := buildInstalledPlugin(manifest, destDir, mfPath, "", nil, assetRoot)
		return installed, nil
	}

	var artifact *pluginmanifestv1.Artifact
	if manifestNeedsExecutableArtifact(manifest) {
		artifact, err = pluginpkg.CurrentPlatformArtifact(manifest)
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

	if artifact != nil {
		artifactPath := filepath.Join(dirPath, filepath.FromSlash(artifact.Path))
		artifactDest := filepath.Join(destDir, filepath.FromSlash(artifact.Path))
		if err := os.MkdirAll(filepath.Dir(artifactDest), 0755); err != nil {
			return nil, fmt.Errorf("create artifact directory: %w", err)
		}
		if err := copyFile(artifactPath, artifactDest); err != nil {
			return nil, fmt.Errorf("copy artifact: %w", err)
		}
	}

	if err := copyManifestReferencedFiles(dirPath, destDir, manifest); err != nil {
		return nil, err
	}
	manifest = pluginpkg.ResolveManifestLocalReferences(manifest, manifestDest)

	executablePath, err := executablePathForManifest(destDir, manifest)
	if err != nil {
		return nil, err
	}

	installed := buildInstalledPlugin(manifest, destDir, manifestDest, executablePath, artifact, "")
	return installed, nil
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
	kind, err := pluginpkg.ManifestKind(manifest)
	if err != nil {
		return "", err
	}
	entry := pluginpkg.EntrypointForKind(manifest, kind)
	if entry == nil {
		if manifest.Spec != nil && manifest.Spec.IsManifestBacked() {
			return "", nil
		}
		return "", fmt.Errorf("manifest does not define an executable entrypoint")
	}
	if entry.ArtifactPath == "" {
		return "", fmt.Errorf("manifest entrypoint artifact_path is required")
	}
	return filepath.Join(root, filepath.FromSlash(entry.ArtifactPath)), nil
}

func copyManifestReferencedFiles(srcDir, destDir string, manifest *pluginmanifestv1.Manifest) error {
	for _, ref := range pluginpkg.LocalPackageReferences(manifest) {
		src := filepath.Join(srcDir, filepath.FromSlash(ref.Path))
		dest := filepath.Join(destDir, filepath.FromSlash(ref.Path))
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return fmt.Errorf("create %s directory: %w", ref.Description, err)
		}
		if err := copyFile(src, dest); err != nil {
			return fmt.Errorf("copy %s %s: %w", ref.Description, ref.Path, err)
		}
	}
	if manifest != nil && manifest.Spec != nil {
		src := pluginpkg.StaticCatalogPath(srcDir)
		dest := pluginpkg.StaticCatalogPath(destDir)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return fmt.Errorf("create provider static catalog directory: %w", err)
		}
		if err := copyFile(src, dest); err != nil {
			if os.IsNotExist(err) && !pluginpkg.StaticCatalogRequired(manifest) {
				return nil
			}
			return fmt.Errorf("copy provider static catalog %s: %w", pluginpkg.StaticCatalogFile, err)
		}
	}
	return nil
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
