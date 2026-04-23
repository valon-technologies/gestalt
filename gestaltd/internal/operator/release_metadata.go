package operator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

const (
	providerReleaseSchemaName         = "gestaltd-provider-release"
	providerReleaseSchemaVersion      = 1
	providerReleaseRuntimeExecutable  = "executable"
	providerReleaseRuntimeDeclarative = "declarative"
	providerReleaseRuntimeUI          = "ui"
	providerReleaseMetadataMaxBytes   = 4 << 20
	httpAcceptHeader                  = "Accept"
	httpAcceptOctetStream             = "application/octet-stream"
	httpAcceptGitHubAPI               = "application/vnd.github+json"
	httpAuthorizationHeader           = "Authorization"
	httpBearerAuthorizationPrefix     = "Bearer "
)

type gitHubReleaseLocation struct {
	Repo  string
	Tag   string
	Asset string
}

type gitHubReleaseAsset struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type gitHubReleaseByTagResponse struct {
	Assets []gitHubReleaseAsset `json:"assets"`
}

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
	metadata.Kind = providermanifestv1.NormalizeKind(metadata.Kind)
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
	case providermanifestv1.KindPlugin, providermanifestv1.KindAuthentication, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime, providermanifestv1.KindUI:
	default:
		return fmt.Errorf("provider release kind %q is not supported", metadata.Kind)
	}
	switch metadata.Runtime {
	case providerReleaseRuntimeExecutable:
		if metadata.Kind == providermanifestv1.KindUI {
			return fmt.Errorf("provider release runtime %q is invalid for kind %q", metadata.Runtime, metadata.Kind)
		}
	case providerReleaseRuntimeDeclarative:
		if metadata.Kind != providermanifestv1.KindPlugin {
			return fmt.Errorf("provider release runtime %q is only valid for kind %q", metadata.Runtime, providermanifestv1.KindPlugin)
		}
	case providerReleaseRuntimeUI:
		if metadata.Kind != providermanifestv1.KindUI {
			return fmt.Errorf("provider release runtime %q is only valid for kind %q", metadata.Runtime, providermanifestv1.KindUI)
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

func fetchProviderReleaseMetadata(ctx context.Context, client *http.Client, metadataLocation, token string) (*providerReleaseMetadata, string, map[string]string, error) {
	resolvedMetadataLocation, gitHubReleaseAssets, err := resolveProviderReleaseMetadataLocation(ctx, client, metadataLocation, token)
	if err != nil {
		return nil, "", nil, err
	}
	if !isRemoteReleaseMetadataLocation(resolvedMetadataLocation) {
		data, err := readProviderReleaseMetadataFile(resolvedMetadataLocation)
		if err != nil {
			return nil, "", nil, err
		}
		metadata, err := decodeProviderReleaseMetadata(data)
		if err != nil {
			return nil, "", nil, err
		}
		return metadata, resolvedMetadataLocation, gitHubReleaseAssets, nil
	}
	if client == nil {
		client = http.DefaultClient
	}
	req, err := newAuthenticatedFetchRequest(ctx, resolvedMetadataLocation, token)
	if err != nil {
		return nil, "", nil, fmt.Errorf("create provider release metadata request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", nil, fmt.Errorf("fetch provider release metadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", nil, fmt.Errorf("unexpected status %d fetching provider release metadata from %s", resp.StatusCode, resolvedMetadataLocation)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, providerReleaseMetadataMaxBytes+1))
	if err != nil {
		return nil, "", nil, fmt.Errorf("read provider release metadata: %w", err)
	}
	if len(data) > providerReleaseMetadataMaxBytes {
		return nil, "", nil, fmt.Errorf("provider release metadata exceeds %d byte limit", providerReleaseMetadataMaxBytes)
	}
	metadata, err := decodeProviderReleaseMetadata(data)
	if err != nil {
		return nil, "", nil, err
	}
	return metadata, resolvedMetadataLocation, gitHubReleaseAssets, nil
}

func newAuthenticatedFetchRequest(ctx context.Context, requestURL, token string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(httpAcceptHeader, httpAcceptOctetStream)
	if token = strings.TrimSpace(token); token != "" {
		req.Header.Set(httpAuthorizationHeader, httpBearerAuthorizationPrefix+token)
	}
	return req, nil
}

func readProviderReleaseMetadataFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read provider release metadata: %w", err)
	}
	if len(data) > providerReleaseMetadataMaxBytes {
		return nil, fmt.Errorf("provider release metadata exceeds %d byte limit", providerReleaseMetadataMaxBytes)
	}
	return data, nil
}

func resolveProviderReleaseMetadataLocation(ctx context.Context, client *http.Client, metadataLocation, token string) (string, map[string]string, error) {
	if ref, ok, err := parseGitHubReleaseLocation(metadataLocation); err != nil {
		return "", nil, err
	} else if ok {
		return resolveGitHubReleaseAssetURL(ctx, client, ref, token)
	}
	return metadataLocation, nil, nil
}

func parseGitHubReleaseLocation(location string) (gitHubReleaseLocation, bool, error) {
	parsed, err := url.Parse(strings.TrimSpace(location))
	if err != nil {
		return gitHubReleaseLocation{}, false, err
	}
	if parsed.Scheme != "github-release" {
		return gitHubReleaseLocation{}, false, nil
	}
	repo := strings.TrimSpace(strings.TrimPrefix(parsed.EscapedPath(), "/"))
	if repo == "" {
		repo = strings.TrimSpace(strings.Trim(parsed.Host+parsed.Path, "/"))
	}
	repo, err = url.PathUnescape(repo)
	if err != nil {
		return gitHubReleaseLocation{}, false, fmt.Errorf("decode github release repo: %w", err)
	}
	tag := strings.TrimSpace(parsed.Query().Get("tag"))
	asset := strings.TrimSpace(parsed.Query().Get("asset"))
	if repo == "" || tag == "" || asset == "" {
		return gitHubReleaseLocation{}, false, fmt.Errorf("github release source must include repo, tag, and asset")
	}
	return gitHubReleaseLocation{
		Repo:  repo,
		Tag:   tag,
		Asset: asset,
	}, true, nil
}

func resolveGitHubReleaseAssetURL(ctx context.Context, client *http.Client, ref gitHubReleaseLocation, token string) (string, map[string]string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	endpoint := "https://api.github.com/repos/" + strings.TrimSpace(ref.Repo) + "/releases/tags/" + url.PathEscape(strings.TrimSpace(ref.Tag))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", nil, fmt.Errorf("create github release lookup request: %w", err)
	}
	req.Header.Set(httpAcceptHeader, httpAcceptGitHubAPI)
	if token = strings.TrimSpace(token); token != "" {
		req.Header.Set(httpAuthorizationHeader, httpBearerAuthorizationPrefix+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("resolve github release %s@%s: %w", ref.Repo, ref.Tag, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("unexpected status %d resolving github release %s@%s", resp.StatusCode, ref.Repo, ref.Tag)
	}
	var release gitHubReleaseByTagResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", nil, fmt.Errorf("decode github release %s@%s: %w", ref.Repo, ref.Tag, err)
	}
	assetURLs := make(map[string]string, len(release.Assets))
	metadataAssetURL := ""
	for _, asset := range release.Assets {
		assetURL := (&url.URL{
			Scheme: "https",
			Host:   "api.github.com",
			Path:   fmt.Sprintf("/repos/%s/releases/assets/%d", strings.TrimSpace(ref.Repo), asset.ID),
		}).String()
		assetURLs[strings.TrimSpace(asset.Name)] = assetURL
		if strings.TrimSpace(asset.Name) == strings.TrimSpace(ref.Asset) {
			metadataAssetURL = assetURL
		}
	}
	if metadataAssetURL == "" {
		return "", nil, fmt.Errorf("github release %s@%s does not contain asset %q", ref.Repo, ref.Tag, ref.Asset)
	}
	return metadataAssetURL, assetURLs, nil
}

func providerReleaseArchives(metadataURL string, metadata *providerReleaseMetadata, gitHubReleaseAssets map[string]string) (map[string]LockArchive, error) {
	if metadata == nil {
		return nil, fmt.Errorf("provider release metadata is required")
	}
	archives := make(map[string]LockArchive, len(metadata.Artifacts))
	for target, artifact := range metadata.Artifacts {
		archiveRef, err := archiveReferenceForLock(metadataURL, artifact.Path, gitHubReleaseAssets)
		if err != nil {
			return nil, fmt.Errorf("resolve provider release artifact path for target %q: %w", target, err)
		}
		archives[target] = LockArchive{
			URL:    archiveRef,
			SHA256: strings.TrimSpace(artifact.SHA256),
		}
	}
	return archives, nil
}

func archiveReferenceNeedsIntegrityHash(archiveRef string) bool {
	return isRemoteReleaseMetadataLocation(archiveRef)
}

func normalizedLocalReleaseMetadataFingerprintPayload(data []byte) ([]byte, error) {
	metadata, err := decodeProviderReleaseMetadata(data)
	if err != nil {
		return nil, err
	}
	return json.Marshal(metadata)
}

func normalizedLocalReleaseMetadataFingerprintPayloadFromFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return normalizedLocalReleaseMetadataFingerprintPayload(data)
}

func localReleaseArchiveExpectedSHA(sourceLocation, resolvedKey, archiveLocation string) (string, error) {
	if isRemoteReleaseMetadataLocation(sourceLocation) || archiveReferenceNeedsIntegrityHash(archiveLocation) {
		return "", nil
	}

	metadata, resolvedMetadataLocation, gitHubReleaseAssets, err := fetchProviderReleaseMetadata(context.Background(), nil, sourceLocation, "")
	if err != nil {
		return "", err
	}
	artifact, ok := metadata.Artifacts[resolvedKey]
	if !ok {
		return "", fmt.Errorf("provider release metadata missing artifact for target %q", resolvedKey)
	}
	resolvedArchivePath, err := resolveArchiveSourceLocation(resolvedMetadataLocation, artifact.Path, gitHubReleaseAssets)
	if err != nil {
		return "", err
	}
	if filepath.Clean(resolvedArchivePath) != filepath.Clean(archiveLocation) {
		return "", fmt.Errorf("provider release metadata target %q resolved to %s, want %s", resolvedKey, resolvedArchivePath, archiveLocation)
	}
	return strings.TrimSpace(artifact.SHA256), nil
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
	case providermanifestv1.KindUI:
		return providerReleaseRuntimeUI
	default:
		return providerReleaseRuntimeExecutable
	}
}

func releaseRuntimeForManifest(manifest *providermanifestv1.Manifest, kind string) string {
	switch kind {
	case providermanifestv1.KindUI:
		return providerReleaseRuntimeUI
	case providermanifestv1.KindPlugin:
		if manifest != nil && manifest.IsDeclarativeOnlyProvider() {
			return providerReleaseRuntimeDeclarative
		}
	}
	return providerReleaseRuntimeExecutable
}

func downloadArchiveForSource(ctx context.Context, client *http.Client, token, archiveURL string) (*providerpkg.DownloadResult, error) {
	if !isRemoteReleaseMetadataLocation(archiveURL) {
		return copyLocalArchiveForSource(archiveURL)
	}
	if client == nil {
		client = http.DefaultClient
	}
	req, err := newAuthenticatedFetchRequest(ctx, archiveURL, token)
	if err != nil {
		return nil, fmt.Errorf("create archive download request: %w", err)
	}
	return providerpkg.DownloadRequest(client, req)
}

func isRemoteReleaseMetadataLocation(location string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(location))
	if err != nil {
		return false
	}
	switch parsed.Scheme {
	case "http", "https":
		return parsed.Host != ""
	default:
		return false
	}
}

func archiveReferenceForLock(metadataLocation, artifactPath string, gitHubReleaseAssets map[string]string) (string, error) {
	resolved, err := resolveArchiveSourceLocation(metadataLocation, artifactPath, gitHubReleaseAssets)
	if err != nil {
		return "", err
	}
	if isRemoteReleaseMetadataLocation(metadataLocation) || isRemoteReleaseMetadataLocation(resolved) {
		return resolved, nil
	}
	baseDir := filepath.Dir(metadataLocation)
	rel, err := filepath.Rel(baseDir, resolved)
	if err != nil {
		return "", fmt.Errorf("relativize local release archive path: %w", err)
	}
	return filepath.ToSlash(filepath.Clean(rel)), nil
}

func resolveArchiveSourceLocation(metadataLocation, archiveRef string, gitHubReleaseAssets map[string]string) (string, error) {
	archiveRef = strings.TrimSpace(archiveRef)
	if archiveRef == "" {
		return "", fmt.Errorf("archive reference is required")
	}
	if resolved := strings.TrimSpace(gitHubReleaseAssets[archiveRef]); resolved != "" {
		return resolved, nil
	}
	if !isRemoteReleaseMetadataLocation(archiveRef) {
		if resolved := strings.TrimSpace(gitHubReleaseAssets[path.Base(filepath.ToSlash(archiveRef))]); resolved != "" {
			return resolved, nil
		}
	}
	if isRemoteReleaseMetadataLocation(metadataLocation) {
		baseURL, err := url.Parse(metadataLocation)
		if err != nil {
			return "", fmt.Errorf("parse provider release metadata URL: %w", err)
		}
		artifactURL, err := url.Parse(archiveRef)
		if err != nil {
			return "", fmt.Errorf("parse provider release artifact path: %w", err)
		}
		return baseURL.ResolveReference(artifactURL).String(), nil
	}
	if isRemoteReleaseMetadataLocation(archiveRef) {
		return archiveRef, nil
	}
	baseDir := filepath.Dir(metadataLocation)
	if filepath.IsAbs(archiveRef) {
		return filepath.Clean(archiveRef), nil
	}
	return filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(archiveRef))), nil
}

func copyLocalArchiveForSource(path string) (*providerpkg.DownloadResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat local archive: %w", err)
	}
	if info.Size() > providerpkg.MaxPackageBytes {
		return nil, fmt.Errorf("download exceeds %d byte limit", providerpkg.MaxPackageBytes)
	}
	src, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open local archive: %w", err)
	}
	defer func() { _ = src.Close() }()

	tmp, err := os.CreateTemp("", "gestalt-plugin-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	removeTmp := func() { _ = os.Remove(tmpPath) }

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(src, providerpkg.MaxPackageBytes+1)); err != nil {
		_ = tmp.Close()
		removeTmp()
		return nil, fmt.Errorf("copy local archive: %w", err)
	}
	if err := tmp.Close(); err != nil {
		removeTmp()
		return nil, fmt.Errorf("close temp file: %w", err)
	}

	return &providerpkg.DownloadResult{
		LocalPath: tmpPath,
		Cleanup:   removeTmp,
		SHA256Hex: hex.EncodeToString(h.Sum(nil)),
	}, nil
}
