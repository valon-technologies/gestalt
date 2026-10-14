package packageio

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/source"
)

const (
	sourceBuildRootDir     = ".gestaltd"
	sourceBuildBinDir      = "bin"
	installedBinDir        = "bin"
	windowsOS              = "windows"
	windowsExecutableExt   = ".exe"
	sourceEntrypointReject = "source manifests do not declare entrypoint.artifactPath; build output and runtime entrypoint paths are derived by Gestalt"
)

func SourceNameFromManifest(manifest *providermanifestv1.Manifest) (string, error) {
	if manifest == nil {
		return "", fmt.Errorf("manifest is required")
	}
	if strings.TrimSpace(manifest.Source) == "" {
		return "", fmt.Errorf("manifest source is required")
	}
	src, err := source.Parse(manifest.Source)
	if err != nil {
		return "", fmt.Errorf("manifest source: %w", err)
	}
	return src.AppName(), nil
}

func SourceBuildOutputPath(manifest *providermanifestv1.Manifest, goos string) (string, error) {
	name, err := SourceNameFromManifest(manifest)
	if err != nil {
		return "", err
	}
	return executableRelPath(path.Join(sourceBuildRootDir, sourceBuildBinDir, name), goos), nil
}

func PackageExecutablePath(name, goos string) string {
	return executableRelPath(path.Join(installedBinDir, name), goos)
}

func InstalledExecutablePath(configuredName, goos string) string {
	return PackageExecutablePath(configuredName, goos)
}

func executableRelPath(base, goos string) string {
	if goos == windowsOS {
		return base + windowsExecutableExt
	}
	return base
}

func validateSourceEntrypointDecl(manifest *providermanifestv1.Manifest, _ string, sourceMode bool) error {
	if !sourceMode || manifest == nil || manifest.Entrypoint == nil {
		return nil
	}
	return fmt.Errorf("%s", sourceEntrypointReject)
}

// ResolveManifestRelativeSpecURL resolves a relative spec path beside its manifest.
func ResolveManifestRelativeSpecURL(manifestPath, raw string) string {
	if manifestPath == "" || raw == "" {
		return raw
	}
	if filepath.IsAbs(raw) || strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if strings.HasPrefix(raw, "file://") {
		path := strings.TrimPrefix(raw, "file://")
		if filepath.IsAbs(path) {
			return raw
		}
		return "file://" + filepath.Clean(filepath.Join(filepath.Dir(manifestPath), path))
	}
	return filepath.Clean(filepath.Join(filepath.Dir(manifestPath), raw))
}
