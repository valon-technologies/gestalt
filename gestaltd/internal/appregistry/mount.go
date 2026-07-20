package appregistry

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

// MountService binds registry-materialized app packages for catalog-driven restarts.
type MountService struct {
	ArtifactsDir string
}

const activeVersionFile = "active-version"

// ResolveInstalledApp returns a provider entry backed by the requested
// registry install. The deploy-time entry is returned unchanged when no local
// install exists, which preserves restart-only convergence for legacy rows.
func (m *MountService) ResolveInstalledApp(name string, entry *config.ProviderEntry, version string) (*config.ProviderEntry, error) {
	if m == nil {
		return entry, nil
	}
	return ResolveInstalledAppIfPresent(name, entry, m.ArtifactsDir, version)
}

// ActivateInstalledApp atomically records the locally running version used by
// dynamic registry static mounts.
func (m *MountService) ActivateInstalledApp(name, version string) error {
	if m == nil {
		return fmt.Errorf("registry mount service is not configured")
	}
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	if name == "" || version == "" {
		return fmt.Errorf("app name and version are required")
	}
	destDir := MaterializedPath(m.ArtifactsDir, name, version)
	if err := operator.ValidateInstalledPublishedPackage(destDir, name, version); err != nil {
		return fmt.Errorf("validate active registry app %q@%s: %w", name, version, err)
	}
	appDir := filepath.Dir(destDir)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return fmt.Errorf("create registry app directory: %w", err)
	}
	temp, err := os.CreateTemp(appDir, activeVersionFile+".tmp-*")
	if err != nil {
		return fmt.Errorf("create active registry version marker: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err := temp.WriteString(version + "\n"); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write active registry version marker: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close active registry version marker: %w", err)
	}
	activePath := filepath.Join(appDir, activeVersionFile)
	if err := replaceActiveVersionMarker(tempPath, activePath); err != nil {
		return fmt.Errorf("activate registry app version: %w", err)
	}
	return nil
}

func replaceActiveVersionMarker(tempPath, activePath string) error {
	return replaceActiveVersionMarkerForOS(tempPath, activePath, runtime.GOOS)
}

func replaceActiveVersionMarkerForOS(tempPath, activePath, goos string) error {
	if goos != "windows" {
		return os.Rename(tempPath, activePath)
	}
	backup, err := os.CreateTemp(filepath.Dir(activePath), activeVersionFile+".backup-*")
	if err != nil {
		return err
	}
	backupPath := backup.Name()
	if err := backup.Close(); err != nil {
		_ = os.Remove(backupPath)
		return err
	}
	if err := os.Remove(backupPath); err != nil {
		return err
	}

	movedExisting := false
	if err := os.Rename(activePath, backupPath); err == nil {
		movedExisting = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tempPath, activePath); err != nil {
		if movedExisting {
			restoreErr := os.Rename(backupPath, activePath)
			return errors.Join(err, restoreErr)
		}
		return err
	}
	if movedExisting {
		_ = os.Remove(backupPath)
	}
	return nil
}

func (m *MountService) DeactivateInstalledApp(name string) error {
	if m == nil {
		return nil
	}
	path := filepath.Join(strings.TrimSpace(m.ArtifactsDir), RegistryInstallSubdir, strings.TrimSpace(name), activeVersionFile)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deactivate registry app version: %w", err)
	}
	return nil
}

func ActiveInstalledVersion(artifactsDir, name string) (string, error) {
	path := filepath.Join(strings.TrimSpace(artifactsDir), RegistryInstallSubdir, strings.TrimSpace(name), activeVersionFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// ResolveInstalledAppIfPresent returns an entry backed by a materialized
// registry package when one exists. It never mutates entry.
func ResolveInstalledAppIfPresent(name string, entry *config.ProviderEntry, artifactsDir, version string) (*config.ProviderEntry, error) {
	if strings.TrimSpace(version) == "" || strings.TrimSpace(artifactsDir) == "" {
		return entry, nil
	}
	destDir := MaterializedPath(artifactsDir, name, version)
	if _, err := os.Stat(destDir); err != nil {
		if os.IsNotExist(err) {
			return entry, nil
		}
		return nil, fmt.Errorf("stat registry installed app %q@%s: %w", name, version, err)
	}
	if err := operator.ValidateInstalledPublishedPackage(destDir, name, version); err != nil {
		return nil, fmt.Errorf("validate registry installed app %q@%s: %w", name, version, err)
	}
	return ResolveInstalledApp(name, entry, destDir, version)
}

// ResolveInstalledApp builds an isolated provider entry for the materialized
// package at destDir.
func ResolveInstalledApp(name string, entry *config.ProviderEntry, destDir, version string) (*config.ProviderEntry, error) {
	name = strings.TrimSpace(name)
	destDir = strings.TrimSpace(destDir)
	version = strings.TrimSpace(version)
	if name == "" {
		return nil, fmt.Errorf("app name is required")
	}
	if entry == nil {
		return nil, fmt.Errorf("app %q provider entry is required", name)
	}
	if destDir == "" {
		return nil, fmt.Errorf("app %q registry install path is required", name)
	}
	if version == "" {
		return nil, fmt.Errorf("app %q registry install version is required", name)
	}
	if err := operator.ValidateInstalledPublishedPackage(destDir, name, version); err != nil {
		return nil, fmt.Errorf("validate registry installed app %q@%s: %w", name, version, err)
	}
	install, err := inspectInstalledApp(destDir)
	if err != nil {
		return nil, fmt.Errorf("inspect registry installed app %q@%s: %w", name, version, err)
	}
	configMap, err := config.NodeToMap(entry.Config)
	if err != nil {
		return nil, fmt.Errorf("decode app config for %q: %w", name, err)
	}
	manifest := providerpkg.ResolveManifestLocalReferences(install.manifest, install.manifestPath)
	kind, err := providerpkg.ManifestKind(manifest)
	if err != nil {
		return nil, fmt.Errorf("app %q manifest is invalid: %w", name, err)
	}
	if kind != providermanifestv1.KindApp {
		return nil, fmt.Errorf("app %q manifest has kind %q, want %q", name, kind, providermanifestv1.KindApp)
	}
	if err := providerpkg.ValidateConfigForManifest(install.manifestPath, manifest, providermanifestv1.KindApp, configMap); err != nil {
		return nil, fmt.Errorf("provider config validation for provider %q: %w", name, err)
	}

	resolved := *entry
	resolved.Command = ""
	resolved.Args = nil
	resolved.ResolvedManifestPath = install.manifestPath
	resolved.ResolvedManifest = manifest
	resolved.ResolvedIconFile = ""
	resolved.ResolvedStaticRoot = ""

	if manifest.IconFile != "" {
		iconPath := filepath.Join(filepath.Dir(install.manifestPath), filepath.FromSlash(manifest.IconFile))
		if _, err := os.Stat(iconPath); err != nil {
			slog.Warn("registry installed app icon_file not found", "app", name, "path", iconPath, "error", err)
		} else {
			resolved.ResolvedIconFile = iconPath
		}
	}

	if install.executablePath != "" {
		if _, err := os.Stat(install.executablePath); err != nil {
			return nil, fmt.Errorf("registry installed executable for provider %q not found at %s: %w", name, install.executablePath, err)
		}
		args, err := providerEntrypointArgs(manifest)
		if err != nil {
			return nil, fmt.Errorf("resolve entrypoint for provider %q: %w", name, err)
		}
		resolved.Command = install.executablePath
		resolved.Args = args
	}

	if resolved.Static != nil && !resolved.DevActive {
		if install.assetRootPath == "" {
			return nil, fmt.Errorf("app %q: static.mount configured but package has no static bundle", name)
		}
		indexPath := filepath.Join(install.assetRootPath, "index.html")
		if _, err := os.Stat(indexPath); err != nil {
			return nil, fmt.Errorf("app %q: static bundle missing index.html at %s: %w", name, indexPath, err)
		}
		resolved.ResolvedStaticRoot = install.assetRootPath
	}
	return &resolved, nil
}

type installedAppLayout struct {
	manifestPath   string
	executablePath string
	assetRootPath  string
	manifest       *providermanifestv1.Manifest
}

func inspectInstalledApp(destDir string) (*installedAppLayout, error) {
	manifestPath, err := providerpkg.FindManifestFile(destDir)
	if err != nil {
		return nil, err
	}
	_, manifest, err := providerpkg.ReadManifestFile(manifestPath)
	if err != nil {
		return nil, err
	}
	install := &installedAppLayout{
		manifestPath: manifestPath,
		manifest:     manifest,
	}
	if entry := providerpkg.EntrypointForKind(manifest, ""); entry != nil {
		if strings.TrimSpace(entry.ArtifactPath) == "" {
			return nil, fmt.Errorf("manifest entrypoint artifact_path is required")
		}
		install.executablePath = filepath.Join(destDir, filepath.FromSlash(entry.ArtifactPath))
	}
	if manifest != nil && manifest.Spec != nil && strings.TrimSpace(manifest.Spec.AssetRoot) != "" {
		install.assetRootPath = filepath.Join(destDir, filepath.FromSlash(manifest.Spec.AssetRoot))
	}
	return install, nil
}

func providerEntrypointArgs(manifest *providermanifestv1.Manifest) ([]string, error) {
	entry := manifest.Entrypoint
	if entry == nil {
		return nil, fmt.Errorf("manifest does not define a provider entrypoint")
	}
	return append([]string(nil), entry.Args...), nil
}
