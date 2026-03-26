package pluginpkg

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/pluginmanifest/v1"
)

func ArchiveDigest(archivePath string) (string, error) {
	sum, err := fileSHA256(archivePath)
	if err != nil {
		return "", fmt.Errorf("digest archive: %w", err)
	}
	return sum, nil
}

func DirectoryDigest(dirPath string, manifest *pluginmanifestv1.Manifest) (string, error) {
	if manifest == nil {
		return "", fmt.Errorf("manifest is required")
	}

	var digests []string

	manifestSum, err := fileSHA256(filepath.Join(dirPath, ManifestFile))
	if err != nil {
		return "", fmt.Errorf("digest manifest: %w", err)
	}
	digests = append(digests, manifestSum)

	for _, artifact := range manifest.Artifacts {
		sum, err := fileSHA256(filepath.Join(dirPath, filepath.FromSlash(artifact.Path)))
		if err != nil {
			return "", fmt.Errorf("digest artifact %s: %w", artifact.Path, err)
		}
		digests = append(digests, sum)
	}

	if manifest.Provider != nil && manifest.Provider.ConfigSchemaPath != "" {
		sum, err := fileSHA256(filepath.Join(dirPath, filepath.FromSlash(manifest.Provider.ConfigSchemaPath)))
		if err != nil {
			return "", fmt.Errorf("digest provider config schema: %w", err)
		}
		digests = append(digests, sum)
	}

	runtimeSchema := filepath.Join(dirPath, filepath.FromSlash(runtimeConfigSchemaPath))
	if _, err := os.Stat(runtimeSchema); err == nil {
		sum, err := fileSHA256(runtimeSchema)
		if err != nil {
			return "", fmt.Errorf("digest runtime config schema: %w", err)
		}
		digests = append(digests, sum)
	}

	combined := sha256.Sum256([]byte(strings.Join(digests, "\n")))
	return hex.EncodeToString(combined[:]), nil
}
