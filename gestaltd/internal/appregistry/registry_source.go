package appregistry

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

type registryArtifact struct {
	URL    string
	SHA256 string
}

type configuredRegistryEntry struct {
	PublicRoot string
	Entry      *Entry
}

func fetchConfiguredRegistryEntry(
	ctx context.Context,
	registries map[string]config.AppRegistryConfig,
	reader *RegistryReader,
	registryName, appName, version string,
) (*configuredRegistryEntry, error) {
	registry, ok := registries[registryName]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrAppRegistryNotConfigured, registryName)
	}
	if strings.TrimSpace(registry.Kind) != config.AppRegistryKindGCS {
		return nil, fmt.Errorf("unsupported app registry kind")
	}
	publicRoot, err := registry.PublicURL()
	if err != nil {
		return nil, fmt.Errorf("app registry public URL is invalid: %w", err)
	}
	if reader == nil {
		reader = &RegistryReader{}
	}
	entry, err := reader.FetchEntry(ctx, publicRoot, appName, version)
	if err != nil {
		return nil, fmt.Errorf("fetch app registry entry: %w", err)
	}
	if entry.App != appName {
		return nil, fmt.Errorf("registry entry app %q does not match requested app %q", entry.App, appName)
	}
	if entry.Version != version {
		return nil, fmt.Errorf("registry entry version %q does not match requested version %q", entry.Version, version)
	}
	if err := relocateGCSArtifactURLs(entry, publicRoot); err != nil {
		return nil, err
	}
	return &configuredRegistryEntry{PublicRoot: publicRoot, Entry: entry}, nil
}

func relocateGCSArtifactURLs(entry *Entry, publicRoot string) error {
	for platform, artifact := range entry.Artifacts {
		storageURL := strings.TrimSpace(artifact.URL)
		parsed, err := url.Parse(storageURL)
		if err != nil || !strings.EqualFold(parsed.Scheme, "gs") {
			continue
		}
		objectPath := strings.TrimPrefix(parsed.Path, "/")
		expectedPrefix := AppArtifactPrefix(entry.App, entry.Version) + "/"
		if parsed.Host == "" || path.Clean(objectPath) != objectPath || !strings.HasPrefix(objectPath, expectedPrefix) {
			return fmt.Errorf("registry entry artifact for platform %q has invalid GCS URL", platform)
		}
		artifact.PublicURL = PublicURL(publicRoot, objectPath)
		entry.Artifacts[platform] = artifact
	}
	return nil
}

func resolveRegistryArtifact(entry *Entry, platform string) (*registryArtifact, error) {
	if entry == nil {
		return nil, fmt.Errorf("registry entry is required")
	}
	artifact, ok := entry.Artifacts[platform]
	if !ok {
		return nil, fmt.Errorf("registry entry has no artifact for platform %q", platform)
	}
	artifactURL := strings.TrimSpace(artifact.PublicURL)
	if artifactURL == "" {
		artifactURL = strings.TrimSpace(artifact.URL)
	}
	if artifactURL == "" {
		return nil, fmt.Errorf("registry entry artifact for platform %q has no download URL", platform)
	}
	expectedSHA := strings.TrimSpace(artifact.SHA256)
	if expectedSHA == "" {
		return nil, fmt.Errorf("registry entry artifact for platform %q is missing sha256", platform)
	}
	return &registryArtifact{URL: artifactURL, SHA256: expectedSHA}, nil
}
