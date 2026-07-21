package appregistry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

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
	mu           sync.Mutex
	appLocks     map[string]*sync.Mutex
}

type MaterializationResult struct {
	Path    string
	Changed bool
}

// MaterializedPath returns the on-disk directory for one installed app version.
func MaterializedPath(artifactsDir, appName, version string) string {
	return filepath.Join(strings.TrimSpace(artifactsDir), RegistryInstallSubdir, strings.TrimSpace(appName), strings.TrimSpace(version))
}

// Ensure validates the local artifact and downloads and extracts it when needed.
func (m *Materializer) Ensure(ctx context.Context, installation *core.AppInstallation) (*MaterializationResult, error) {
	if m == nil {
		return nil, fmt.Errorf("app registry materializer is not configured")
	}
	if installation == nil {
		return nil, fmt.Errorf("installation is required")
	}
	appName := strings.TrimSpace(installation.AppName)
	version := strings.TrimSpace(installation.Version)
	registryName := strings.TrimSpace(installation.Registry)
	if appName == "" || version == "" {
		return nil, fmt.Errorf("installation app and version are required")
	}
	appLock := m.appLock(appName)
	appLock.Lock()
	defer appLock.Unlock()
	if registryName == "" {
		return nil, fmt.Errorf("installation registry is required")
	}
	artifactsDir := strings.TrimSpace(m.ArtifactsDir)
	if artifactsDir == "" {
		return nil, fmt.Errorf("artifacts directory is not configured")
	}

	destDir := MaterializedPath(artifactsDir, appName, version)
	if installedPackageReady(destDir, appName, version) {
		return &MaterializationResult{Path: destDir}, nil
	}
	if err := removePartialMaterializedPackage(destDir); err != nil {
		return nil, err
	}

	reader := m.Reader
	if reader == nil {
		reader = &RegistryReader{}
	}
	source, err := fetchConfiguredRegistryEntry(ctx, m.Registries, reader, registryName, appName, version)
	if err != nil {
		return nil, err
	}

	platform := providerpkg.CurrentPlatformString()
	artifact, err := resolveRegistryArtifact(source.Entry, platform)
	if err != nil {
		return nil, err
	}

	download, err := downloadRegistryArtifact(ctx, reader.client(), artifact.URL)
	if err != nil {
		return nil, err
	}
	defer func() {
		if download.Cleanup != nil {
			download.Cleanup()
		}
	}()
	if !strings.EqualFold(strings.TrimSpace(download.SHA256Hex), artifact.SHA256) {
		return nil, fmt.Errorf("artifact digest mismatch: got %s, want %s", download.SHA256Hex, artifact.SHA256)
	}

	if err := materializePublishedPackage(ctx, download.LocalPath, destDir, appName); err != nil {
		return nil, fmt.Errorf("materialize app artifact: %w", err)
	}
	return &MaterializationResult{Path: destDir, Changed: true}, nil
}

// PruneSuperseded removes all locally materialized versions for an app except
// keepVersion. An empty keepVersion removes every locally materialized version.
func (m *Materializer) PruneSuperseded(app, keepVersion string) error {
	if m == nil {
		return fmt.Errorf("app registry materializer is not configured")
	}
	app = strings.TrimSpace(app)
	keepVersion = strings.TrimSpace(keepVersion)
	if app == "" {
		return fmt.Errorf("app is required")
	}
	artifactsDir := strings.TrimSpace(m.ArtifactsDir)
	if artifactsDir == "" {
		return fmt.Errorf("artifacts directory is not configured")
	}
	appLock := m.appLock(app)
	appLock.Lock()
	defer appLock.Unlock()

	appDir := filepath.Join(artifactsDir, RegistryInstallSubdir, app)
	entries, err := readMaterializedAppDir(appDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == keepVersion || (keepVersion != "" && name == "active-version") {
			continue
		}
		path := filepath.Join(appDir, name)
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove superseded materialized path %s: %w", path, err)
		}
	}
	return nil
}

// SupersededPruned reports whether no locally materialized package other than
// keepVersion remains for app.
func (m *Materializer) SupersededPruned(app, keepVersion string) (bool, error) {
	if m == nil {
		return false, fmt.Errorf("app registry materializer is not configured")
	}
	app = strings.TrimSpace(app)
	keepVersion = strings.TrimSpace(keepVersion)
	if app == "" {
		return false, fmt.Errorf("app is required")
	}
	artifactsDir := strings.TrimSpace(m.ArtifactsDir)
	if artifactsDir == "" {
		return false, fmt.Errorf("artifacts directory is not configured")
	}
	appLock := m.appLock(app)
	appLock.Lock()
	defer appLock.Unlock()

	appDir := filepath.Join(artifactsDir, RegistryInstallSubdir, app)
	entries, err := readMaterializedAppDir(appDir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name != keepVersion && name != "active-version" {
			return false, nil
		}
	}
	return true, nil
}

func readMaterializedAppDir(appDir string) ([]os.DirEntry, error) {
	info, err := os.Lstat(appDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat materialized app directory %s: %w", appDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("materialized app path %s is not a directory", appDir)
	}
	entries, err := os.ReadDir(appDir)
	if err != nil {
		return nil, fmt.Errorf("read materialized app directory %s: %w", appDir, err)
	}
	return entries, nil
}

func (m *Materializer) appLock(app string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.appLocks == nil {
		m.appLocks = make(map[string]*sync.Mutex)
	}
	if lock := m.appLocks[app]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	m.appLocks[app] = lock
	return lock
}

func installedPackageReady(destDir, appName, version string) bool {
	destDir = strings.TrimSpace(destDir)
	if destDir == "" {
		return false
	}
	return operator.ValidateInstalledPublishedPackage(destDir, appName, version) == nil
}

func removePartialMaterializedPackage(destDir string) error {
	destDir = strings.TrimSpace(destDir)
	if destDir == "" {
		return nil
	}
	if _, err := os.Stat(destDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat partial materialized path %s: %w", destDir, err)
	}
	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("remove partial materialized path %s: %w", destDir, err)
	}
	return nil
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
