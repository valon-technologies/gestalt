package pluginpkg

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
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

func DirectoryDigest(dirPath string, manifest *pluginmanifestv1.Manifest) (string, error) {
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

	if manifest.Provider != nil && manifest.Provider.ConfigSchemaPath != "" {
		sum, err := FileSHA256(filepath.Join(dirPath, filepath.FromSlash(manifest.Provider.ConfigSchemaPath)))
		if err != nil {
			return "", fmt.Errorf("digest provider config schema: %w", err)
		}
		digests = append(digests, sum)
	}

	if manifest.WebUI != nil && manifest.WebUI.AssetRoot != "" {
		assetDir := filepath.Join(dirPath, filepath.FromSlash(manifest.WebUI.AssetRoot))
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
