package providerpkg

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func ArchiveDigest(archivePath string) (string, error) {
	sum, err := FileSHA256(archivePath)
	if err != nil {
		return "", fmt.Errorf("digest archive: %w", err)
	}
	return sum, nil
}

func FileSHA256(path string) (string, error) {
	return fileSHA256(path)
}

func DirectoryDigest(dirPath string, manifest *providermanifestv1.Manifest) (string, error) {
	if manifest == nil {
		return "", fmt.Errorf("manifest is required")
	}

	var digests []string

	manifestPath, err := FindManifestFile(dirPath)
	if err != nil {
		return "", fmt.Errorf("digest manifest: %w", err)
	}
	manifestSum, err := FileSHA256(manifestPath)
	if err != nil {
		return "", fmt.Errorf("digest manifest: %w", err)
	}
	digests = append(digests, manifestSum)

	for _, artifact := range manifest.Artifacts {
		sum, err := FileSHA256(filepath.Join(dirPath, filepath.FromSlash(artifact.Path)))
		if err != nil {
			return "", fmt.Errorf("digest artifact %s: %w", artifact.Path, err)
		}
		digests = append(digests, sum)
	}

	for _, ref := range LocalPackageReferences(manifest) {
		sum, err := FileSHA256(filepath.Join(dirPath, filepath.FromSlash(ref.Path)))
		if err != nil {
			return "", fmt.Errorf("digest %s: %w", ref.Description, err)
		}
		digests = append(digests, sum)
	}
	staticCatalogPath := StaticCatalogPath(dirPath)
	if _, err := os.Stat(staticCatalogPath); err == nil {
		sum, err := FileSHA256(staticCatalogPath)
		if err != nil {
			return "", fmt.Errorf("digest provider static catalog: %w", err)
		}
		digests = append(digests, sum)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("digest provider static catalog: %w", err)
	} else if StaticCatalogRequired(manifest) {
		return "", fmt.Errorf("digest provider static catalog: %s does not exist", StaticCatalogFile)
	}

	if manifest.Spec != nil && manifest.Spec.AssetRoot != "" {
		assetDir := filepath.Join(dirPath, filepath.FromSlash(manifest.Spec.AssetRoot))
		if err := filepath.WalkDir(assetDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			sum, err := FileSHA256(path)
			if err != nil {
				return fmt.Errorf("digest asset %s: %w", path, err)
			}
			rel, _ := filepath.Rel(assetDir, path)
			digests = append(digests, rel+"="+sum)
			return nil
		}); err != nil {
			return "", fmt.Errorf("digest webui assets: %w", err)
		}
	}

	combined := sha256.Sum256([]byte(strings.Join(digests, "\n")))
	return hex.EncodeToString(combined[:]), nil
}
