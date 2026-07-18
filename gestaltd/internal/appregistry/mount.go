package appregistry

import (
	"fmt"
	"os"
	"path/filepath"
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

func (m *MountService) BindInstalledAppIfPresent(name string, entry *config.ProviderEntry, version string) error {
	if m == nil {
		return nil
	}
	return BindInstalledAppIfPresent(name, entry, m.ArtifactsDir, version)
}

// BindInstalledAppIfPresent mounts a registry-materialized package when it exists
// on disk. Missing or incomplete installs are ignored so legacy restart-only
// catalog rows keep using the deploy-time pin.
func BindInstalledAppIfPresent(name string, entry *config.ProviderEntry, artifactsDir, version string) error {
	if strings.TrimSpace(version) == "" || strings.TrimSpace(artifactsDir) == "" {
		return nil
	}
	destDir := MaterializedPath(artifactsDir, name, version)
	if !registryInstallReady(destDir, name, version) {
		return nil
	}
	return BindInstalledApp(name, entry, destDir, version)
}

func registryInstallReady(destDir, name, version string) bool {
	return operator.ValidateInstalledPublishedPackage(destDir, name, version) == nil
}

// BindInstalledApp updates entry so the next provider build uses the
// registry-materialized package at destDir instead of the deploy-time pin.
func BindInstalledApp(name string, entry *config.ProviderEntry, destDir, version string) error {
	name = strings.TrimSpace(name)
	destDir = strings.TrimSpace(destDir)
	version = strings.TrimSpace(version)
	if name == "" {
		return fmt.Errorf("app name is required")
	}
	if entry == nil {
		return fmt.Errorf("app %q provider entry is required", name)
	}
	if destDir == "" {
		return fmt.Errorf("app %q registry install path is required", name)
	}
	if version == "" {
		return fmt.Errorf("app %q registry install version is required", name)
	}
	if err := operator.ValidateInstalledPublishedPackage(destDir, name, version); err != nil {
		return fmt.Errorf("validate registry installed app %q@%s: %w", name, version, err)
	}
	install, err := inspectInstalledApp(destDir)
	if err != nil {
		return fmt.Errorf("inspect registry installed app %q@%s: %w", name, version, err)
	}
	configMap, err := config.NodeToMap(entry.Config)
	if err != nil {
		return fmt.Errorf("decode app config for %q: %w", name, err)
	}
	manifest := providerpkg.ResolveManifestLocalReferences(install.manifest, install.manifestPath)
	if err := providerpkg.ValidateConfigForManifest(install.manifestPath, manifest, providermanifestv1.KindApp, configMap); err != nil {
		return fmt.Errorf("provider config validation for provider %q: %w", name, err)
	}
	entry.ResolvedManifestPath = install.manifestPath
	entry.ResolvedManifest = manifest
	if install.executablePath == "" {
		return nil
	}
	if _, err := os.Stat(install.executablePath); err != nil {
		return fmt.Errorf("registry installed executable for provider %q not found at %s: %w", name, install.executablePath, err)
	}
	args, err := providerEntrypointArgs(manifest)
	if err != nil {
		return fmt.Errorf("resolve entrypoint for provider %q: %w", name, err)
	}
	entry.Command = install.executablePath
	entry.Args = append([]string(nil), args...)
	if entry.Static != nil && !entry.DevActive {
		if install.assetRootPath == "" {
			return fmt.Errorf("app %q: static.mount configured but package has no static bundle", name)
		}
		entry.ResolvedStaticRoot = install.assetRootPath
	}
	return nil
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
