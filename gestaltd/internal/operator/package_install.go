package operator

import (
	"fmt"
	"os"
	"path/filepath"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
)

type installedPackage struct {
	Root           string
	ManifestPath   string
	ExecutablePath string
	AssetRoot      string
	Manifest       *providermanifestv1.Manifest
}

func isUIOnly(manifest *providermanifestv1.Manifest) bool {
	kind, err := packageio.ManifestKind(manifest)
	return err == nil && kind == providermanifestv1.KindUI
}

func manifestNeedsExecutableArtifact(manifest *providermanifestv1.Manifest) bool {
	kind, err := packageio.ManifestKind(manifest)
	if err != nil {
		return false
	}
	return packageio.EntrypointForKind(manifest, kind) != nil
}

func installPackage(packagePath, destDir string) (*installedPackage, error) {
	_, manifest, err := packageio.ReadPackageManifest(packagePath)
	if err != nil {
		return nil, err
	}

	if isUIOnly(manifest) {
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return nil, fmt.Errorf("create app directory: %w", err)
		}
		if err := packageio.ExtractPackage(packagePath, destDir); err != nil {
			return nil, err
		}
		manifestPath, _ := packageio.FindManifestFile(destDir)
		if manifestPath == "" {
			manifestPath = filepath.Join(destDir, packageio.ManifestFile)
		}
		assetRoot := filepath.Join(destDir, filepath.FromSlash(manifest.Spec.AssetRoot))
		return &installedPackage{
			Root:         destDir,
			ManifestPath: manifestPath,
			AssetRoot:    assetRoot,
			Manifest:     manifest,
		}, nil
	}

	var artifact *providermanifestv1.Artifact
	if manifestNeedsExecutableArtifact(manifest) {
		artifact, err = packageio.CurrentPlatformArtifact(manifest)
		if err != nil {
			return nil, err
		}
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("create app directory: %w", err)
	}
	if err := packageio.ExtractPackage(packagePath, destDir); err != nil {
		return nil, err
	}

	if artifact != nil {
		// Verify the artifact digest against the extracted file rather than
		// decompressing the whole archive a second time to read it. The package
		// is never executed before this check succeeds.
		got, err := packageio.FileSHA256(filepath.Join(destDir, filepath.FromSlash(artifact.Path)))
		if err != nil {
			return nil, err
		}
		if got != artifact.SHA256 {
			return nil, fmt.Errorf("artifact digest mismatch for %s: package has %s, manifest expects %s", artifact.Path, got, artifact.SHA256)
		}
	}

	manifestPath, _ := packageio.FindManifestFile(destDir)
	if manifestPath == "" {
		manifestPath = filepath.Join(destDir, packageio.ManifestFile)
	}
	manifest = packageio.ResolveManifestLocalReferences(manifest, manifestPath)
	executablePath, err := executablePathForManifest(destDir, manifest)
	if err != nil {
		return nil, err
	}

	return &installedPackage{
		Root:           destDir,
		ManifestPath:   manifestPath,
		ExecutablePath: executablePath,
		Manifest:       manifest,
	}, nil
}

func executablePathForManifest(root string, manifest *providermanifestv1.Manifest) (string, error) {
	if manifest == nil {
		return "", fmt.Errorf("manifest is required")
	}
	if isUIOnly(manifest) {
		return "", nil
	}
	kind, err := packageio.ManifestKind(manifest)
	if err != nil {
		return "", err
	}
	entry := packageio.EntrypointForKind(manifest, kind)
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
