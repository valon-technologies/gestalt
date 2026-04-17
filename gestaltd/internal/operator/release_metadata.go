package operator

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	ghresolver "github.com/valon-technologies/gestalt/server/internal/pluginsource/github"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

const (
	providerReleaseSchemaName         = "gestaltd-provider-release"
	providerReleaseSchemaVersion      = 1
	providerReleaseRuntimeExecutable  = "executable"
	providerReleaseRuntimeDeclarative = "declarative"
	providerReleaseRuntimeWebUI       = "webui"
	providerReleaseMetadataMaxBytes   = 4 << 20
	httpAuthorizationHeader           = "Authorization"
	httpBearerAuthorizationPrefix     = "Bearer "
)

type providerReleaseMetadata struct {
	Schema        string                             `yaml:"schema"`
	SchemaVersion int                                `yaml:"schemaVersion"`
	Package       string                             `yaml:"package"`
	Kind          string                             `yaml:"kind"`
	Version       string                             `yaml:"version"`
	Runtime       string                             `yaml:"runtime"`
	Artifacts     map[string]providerReleaseArtifact `yaml:"artifacts,omitempty"`
}

type providerReleaseArtifact struct {
	Path   string `yaml:"path"`
	SHA256 string `yaml:"sha256"`
}

func sourceAuthToken(entry *config.ProviderEntry) string {
	if entry == nil || entry.Source.Auth == nil {
		return ""
	}
	return strings.TrimSpace(entry.Source.Auth.Token)
}

func decodeProviderReleaseMetadata(data []byte) (*providerReleaseMetadata, error) {
	var metadata providerReleaseMetadata
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&metadata); err != nil {
		return nil, fmt.Errorf("decode provider release metadata: %w", err)
	}
	if err := validateProviderReleaseMetadata(&metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}

func validateProviderReleaseMetadata(metadata *providerReleaseMetadata) error {
	if metadata == nil {
		return fmt.Errorf("provider release metadata is required")
	}
	if metadata.Schema != providerReleaseSchemaName {
		return fmt.Errorf("unsupported provider release schema %q", metadata.Schema)
	}
	if metadata.SchemaVersion != providerReleaseSchemaVersion {
		return fmt.Errorf("unsupported provider release schema version %d", metadata.SchemaVersion)
	}
	if _, err := pluginsource.Parse(strings.TrimSpace(metadata.Package)); err != nil {
		return fmt.Errorf("provider release package: %w", err)
	}
	if err := pluginsource.ValidateVersion(strings.TrimSpace(metadata.Version)); err != nil {
		return fmt.Errorf("provider release version: %w", err)
	}
	switch metadata.Kind {
	case providermanifestv1.KindPlugin, providermanifestv1.KindAuth, providermanifestv1.KindAuthorization, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindSecrets, providermanifestv1.KindWebUI:
	default:
		return fmt.Errorf("provider release kind %q is not supported", metadata.Kind)
	}
	switch metadata.Runtime {
	case providerReleaseRuntimeExecutable:
		if metadata.Kind == providermanifestv1.KindWebUI {
			return fmt.Errorf("provider release runtime %q is invalid for kind %q", metadata.Runtime, metadata.Kind)
		}
	case providerReleaseRuntimeDeclarative:
		if metadata.Kind != providermanifestv1.KindPlugin {
			return fmt.Errorf("provider release runtime %q is only valid for kind %q", metadata.Runtime, providermanifestv1.KindPlugin)
		}
	case providerReleaseRuntimeWebUI:
		if metadata.Kind != providermanifestv1.KindWebUI {
			return fmt.Errorf("provider release runtime %q is only valid for kind %q", metadata.Runtime, providermanifestv1.KindWebUI)
		}
	default:
		return fmt.Errorf("provider release runtime %q is not supported", metadata.Runtime)
	}
	if len(metadata.Artifacts) == 0 {
		return fmt.Errorf("provider release artifacts are required")
	}
	for target, artifact := range metadata.Artifacts {
		switch {
		case strings.TrimSpace(target) == "":
			return fmt.Errorf("provider release artifact target is required")
		case target != platformKeyGeneric:
			if _, _, err := providerpkg.ParsePlatformString(target); err != nil {
				return fmt.Errorf("provider release artifact target %q: %w", target, err)
			}
		}
		if strings.TrimSpace(artifact.Path) == "" {
			return fmt.Errorf("provider release artifact path is required for target %q", target)
		}
		if strings.TrimSpace(artifact.SHA256) == "" {
			return fmt.Errorf("provider release artifact sha256 is required for target %q", target)
		}
	}
	return nil
}

func fetchProviderReleaseMetadata(ctx context.Context, client *http.Client, metadataURL, token string) (*providerReleaseMetadata, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create provider release metadata request: %w", err)
	}
	if authHeader := authorizationHeaderForSourceLocation(metadataURL, token); authHeader != "" {
		req.Header.Set(httpAuthorizationHeader, authHeader)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch provider release metadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d fetching provider release metadata from %s", resp.StatusCode, metadataURL)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, providerReleaseMetadataMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read provider release metadata: %w", err)
	}
	if len(data) > providerReleaseMetadataMaxBytes {
		return nil, fmt.Errorf("provider release metadata exceeds %d byte limit", providerReleaseMetadataMaxBytes)
	}
	return decodeProviderReleaseMetadata(data)
}

func providerReleaseArchives(metadataURL string, metadata *providerReleaseMetadata) (map[string]LockArchive, error) {
	if metadata == nil {
		return nil, fmt.Errorf("provider release metadata is required")
	}
	baseURL, err := url.Parse(metadataURL)
	if err != nil {
		return nil, fmt.Errorf("parse provider release metadata URL: %w", err)
	}
	archives := make(map[string]LockArchive, len(metadata.Artifacts))
	for target, artifact := range metadata.Artifacts {
		artifactURL, err := url.Parse(strings.TrimSpace(artifact.Path))
		if err != nil {
			return nil, fmt.Errorf("parse provider release artifact path for target %q: %w", target, err)
		}
		archives[target] = LockArchive{
			URL:    baseURL.ResolveReference(artifactURL).String(),
			SHA256: strings.TrimSpace(artifact.SHA256),
		}
	}
	return archives, nil
}

func lockEntryPackage(entry LockEntry) string {
	if value := strings.TrimSpace(entry.Package); value != "" {
		return value
	}
	return strings.TrimSpace(entry.Source)
}

func lockEntryKind(entry LockEntry, fallback string) string {
	if value := strings.TrimSpace(entry.Kind); value != "" {
		return value
	}
	switch fallback {
	case providerLockKindTelemetry, providerLockKindAudit:
		return providermanifestv1.KindPlugin
	default:
		return fallback
	}
}

func lockEntryRuntime(entry LockEntry, fallbackKind string) string {
	if value := strings.TrimSpace(entry.Runtime); value != "" {
		return value
	}
	switch lockEntryKind(entry, fallbackKind) {
	case providermanifestv1.KindWebUI:
		return providerReleaseRuntimeWebUI
	default:
		return providerReleaseRuntimeExecutable
	}
}

func releaseRuntimeForManifest(manifest *providermanifestv1.Manifest, kind string) string {
	switch kind {
	case providermanifestv1.KindWebUI:
		return providerReleaseRuntimeWebUI
	case providermanifestv1.KindPlugin:
		if manifest != nil && manifest.IsDeclarativeOnlyProvider() {
			return providerReleaseRuntimeDeclarative
		}
	}
	return providerReleaseRuntimeExecutable
}

func usesGitHubAssetTransport(sourceLocation, archiveURL string) bool {
	if src, err := pluginsource.Parse(sourceLocation); err == nil && src.Host == pluginsource.HostGitHub {
		return true
	}
	parsed, err := url.Parse(strings.TrimSpace(archiveURL))
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "api.github.com":
		return strings.Contains(parsed.Path, "/releases/assets/")
	case "github.com":
		return strings.Contains(parsed.Path, "/releases/download/")
	default:
		return false
	}
}

func downloadArchiveForSource(ctx context.Context, client *http.Client, sourceLocation, token, archiveURL string) (*providerpkg.DownloadResult, error) {
	if client == nil {
		client = http.DefaultClient
	}
	token = strings.TrimSpace(token)
	if usesGitHubAssetTransport(sourceLocation, archiveURL) {
		return ghresolver.DownloadResolvedAsset(ctx, client, archiveURL, token)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create archive download request: %w", err)
	}
	if token != "" {
		req.Header.Set(httpAuthorizationHeader, httpBearerAuthorizationPrefix+token)
	}
	return providerpkg.DownloadRequest(client, req)
}

func authorizationHeaderForSourceLocation(sourceLocation, token string) string {
	token = strings.TrimSpace(token)
	if usesGitHubMetadataTransport(sourceLocation) {
		token = ghresolver.ResolveGitHubToken(token)
		if token == "" {
			return ""
		}
		return "token " + token
	}
	if token == "" {
		return ""
	}
	return httpBearerAuthorizationPrefix + token
}

func usesGitHubMetadataTransport(sourceLocation string) bool {
	parsed, err := url.Parse(strings.TrimSpace(sourceLocation))
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "github.com", "api.github.com":
		return true
	default:
		return false
	}
}
