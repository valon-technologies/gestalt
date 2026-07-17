package appregistry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

// Materializer downloads registry app artifacts and extracts them on the local
// replica before catalog-driven restarts bring an app down.
type Materializer struct {
	Registries   map[string]config.AppRegistryConfig
	Reader       *RegistryReader
	ArtifactsDir string
}

// MaterializedPath returns the on-disk directory for one installed app version.
func MaterializedPath(artifactsDir, appName, version string) string {
	return filepath.Join(strings.TrimSpace(artifactsDir), RegistryInstallSubdir, strings.TrimSpace(appName), strings.TrimSpace(version))
}

// Materialize downloads and extracts the registry artifact for installation when
// it is not already present on disk.
func (m *Materializer) Materialize(ctx context.Context, installation *core.AppInstallation) (string, error) {
	if m == nil {
		return "", fmt.Errorf("app registry materializer is not configured")
	}
	if installation == nil {
		return "", fmt.Errorf("installation is required")
	}
	appName := strings.TrimSpace(installation.AppName)
	version := strings.TrimSpace(installation.Version)
	registryName := strings.TrimSpace(installation.Registry)
	if appName == "" || version == "" {
		return "", fmt.Errorf("installation app and version are required")
	}
	if registryName == "" {
		return "", fmt.Errorf("installation registry is required")
	}
	artifactsDir := strings.TrimSpace(m.ArtifactsDir)
	if artifactsDir == "" {
		return "", fmt.Errorf("artifacts directory is not configured")
	}

	destDir := MaterializedPath(artifactsDir, appName, version)
	if materialized, err := isMaterializedPackage(destDir); err != nil {
		return "", err
	} else if materialized {
		return destDir, nil
	}

	registry, ok := m.Registries[registryName]
	if !ok {
		return "", fmt.Errorf("app registry %q not found", registryName)
	}
	if strings.TrimSpace(registry.Kind) != config.AppRegistryKindGCS {
		return "", fmt.Errorf("unsupported app registry kind")
	}
	publicRoot, err := registry.PublicURL()
	if err != nil {
		return "", fmt.Errorf("app registry public URL is invalid: %w", err)
	}

	reader := m.Reader
	if reader == nil {
		reader = &RegistryReader{}
	}
	entry, err := reader.FetchEntry(ctx, publicRoot, appName, version)
	if err != nil {
		return "", fmt.Errorf("fetch app registry entry: %w", err)
	}
	if entry.App != appName {
		return "", fmt.Errorf("registry entry app %q does not match requested app %q", entry.App, appName)
	}
	if entry.Version != version {
		return "", fmt.Errorf("registry entry version %q does not match requested version %q", entry.Version, version)
	}

	platform := providerpkg.CurrentPlatformString()
	artifact, ok := entry.Artifacts[platform]
	if !ok {
		return "", fmt.Errorf("registry entry has no artifact for platform %q", platform)
	}
	artifactURL := strings.TrimSpace(artifact.PublicURL)
	if artifactURL == "" {
		artifactURL = strings.TrimSpace(artifact.URL)
	}
	if artifactURL == "" {
		return "", fmt.Errorf("registry entry artifact for platform %q has no download URL", platform)
	}
	expectedSHA := strings.TrimSpace(artifact.SHA256)
	if expectedSHA == "" {
		return "", fmt.Errorf("registry entry artifact for platform %q is missing sha256", platform)
	}

	download, err := downloadRegistryArtifact(ctx, reader.client(), artifactURL)
	if err != nil {
		return "", err
	}
	defer func() {
		if download.Cleanup != nil {
			download.Cleanup()
		}
	}()
	if !strings.EqualFold(strings.TrimSpace(download.SHA256Hex), expectedSHA) {
		return "", fmt.Errorf("artifact digest mismatch: got %s, want %s", download.SHA256Hex, expectedSHA)
	}

	if err := materializePublishedPackage(ctx, download.LocalPath, destDir, appName); err != nil {
		return "", fmt.Errorf("materialize app artifact: %w", err)
	}
	return destDir, nil
}

func isMaterializedPackage(destDir string) (bool, error) {
	destDir = strings.TrimSpace(destDir)
	if destDir == "" {
		return false, nil
	}
	info, err := os.Stat(destDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat materialized path %s: %w", destDir, err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("materialized path %s is not a directory", destDir)
	}
	if _, err := os.Stat(filepath.Join(destDir, "manifest.yaml")); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat materialized manifest %s: %w", destDir, err)
	}
	return true, nil
}

func downloadRegistryArtifact(ctx context.Context, client *http.Client, artifactURL string) (*providerpkg.DownloadResult, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artifactURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build artifact download request: %w", err)
	}
	result, err := providerpkg.DownloadRequest(client, req)
	if err != nil {
		return nil, fmt.Errorf("download registry artifact: %w", err)
	}
	return result, nil
}

func materializePublishedPackage(ctx context.Context, packagePath, destDir, appName string) error {
	_, err := operator.InstallPublishedPackage(ctx, packagePath, destDir, appName)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return fmt.Errorf("%w: %w", ErrInstallTimedOut, err)
		}
		return err
	}
	return nil
}
