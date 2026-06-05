package operator

import (
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
	"time"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

const (
	httpAcceptHeader              = "Accept"
	httpAcceptOctetStream         = "application/octet-stream"
	httpAcceptGitHubAPI           = "application/vnd.github+json"
	httpAuthorizationHeader       = "Authorization"
	httpBearerAuthorizationPrefix = "Bearer "
)

var providerReleaseFetchRetryDelays = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
}

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

type providerReleaseValidationBundle struct {
	Metadata *providerrelease.Metadata
	Manifest *providermanifestv1.Manifest
	Catalog  *catalog.Catalog
}

func sourceAuthToken(entry *config.ProviderEntry) string {
	if entry == nil || entry.Source.Auth == nil {
		return ""
	}
	return strings.TrimSpace(entry.Source.Auth.Token)
}

func fetchProviderReleaseBundle(ctx context.Context, client *http.Client, metadataLocation, token string) (providerReleaseValidationBundle, string, map[string]string, error) {
	resolvedMetadataLocation, gitHubReleaseAssets, err := resolveProviderReleaseMetadataLocation(ctx, client, metadataLocation, token)
	if err != nil {
		return providerReleaseValidationBundle{}, "", nil, err
	}
	var data []byte
	if !isRemoteReleaseMetadataLocation(resolvedMetadataLocation) {
		data, err = providerrelease.ReadLocalFile(resolvedMetadataLocation)
		if err != nil {
			return providerReleaseValidationBundle{}, "", nil, err
		}
	} else {
		if client == nil {
			client = http.DefaultClient
		}
		resp, err := doProviderReleaseHTTPRequestWithRetry(ctx, client, func(ctx context.Context) (*http.Request, error) {
			return newAuthenticatedFetchRequest(ctx, resolvedMetadataLocation, token)
		})
		if err != nil {
			return providerReleaseValidationBundle{}, "", nil, fmt.Errorf("fetch provider release metadata: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return providerReleaseValidationBundle{}, "", nil, fmt.Errorf("unexpected status %d fetching provider release metadata from %s", resp.StatusCode, resolvedMetadataLocation)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, providerrelease.MaxBytes+1))
		if err != nil {
			return providerReleaseValidationBundle{}, "", nil, fmt.Errorf("read provider release metadata: %w", err)
		}
		if len(data) > providerrelease.MaxBytes {
			return providerReleaseValidationBundle{}, "", nil, fmt.Errorf("provider release metadata exceeds %d byte limit", providerrelease.MaxBytes)
		}
	}
	metadata, err := providerrelease.Decode(data)
	if err != nil {
		return providerReleaseValidationBundle{}, "", nil, err
	}
	manifest, cat, err := fetchProviderReleaseSidecars(ctx, client, resolvedMetadataLocation, token, metadata, gitHubReleaseAssets)
	if err != nil {
		return providerReleaseValidationBundle{}, "", nil, err
	}
	if err := providerrelease.ValidateBundle(metadata, manifest, cat); err != nil {
		return providerReleaseValidationBundle{}, "", nil, err
	}
	return providerReleaseValidationBundle{Metadata: metadata, Manifest: manifest, Catalog: cat}, resolvedMetadataLocation, gitHubReleaseAssets, nil
}

func fetchProviderReleaseSidecars(ctx context.Context, client *http.Client, metadataLocation, token string, metadata *providerrelease.Metadata, gitHubReleaseAssets map[string]string) (*providermanifestv1.Manifest, *catalog.Catalog, error) {
	if metadata == nil {
		return nil, nil, fmt.Errorf("provider release metadata is required")
	}
	manifestData, err := fetchProviderReleaseSidecar(ctx, client, metadataLocation, token, gitHubReleaseAssets, providerrelease.ValidationManifestFile, metadata.ValidationManifestSHA256)
	if err != nil {
		return nil, nil, err
	}
	manifest, err := providerrelease.DecodeManifest(manifestData)
	if err != nil {
		return nil, nil, fmt.Errorf("decode %s: %w", providerrelease.ValidationManifestFile, err)
	}
	var staticCatalog *catalog.Catalog
	if metadata.ValidationCatalogSHA256 != "" {
		catalogData, err := fetchProviderReleaseSidecar(ctx, client, metadataLocation, token, gitHubReleaseAssets, providerrelease.ValidationCatalogFile, metadata.ValidationCatalogSHA256)
		if err != nil {
			return nil, nil, err
		}
		cat, err := providerrelease.DecodeCatalog(catalogData)
		if err != nil {
			return nil, nil, fmt.Errorf("decode %s: %w", providerrelease.ValidationCatalogFile, err)
		}
		staticCatalog = cat
	}
	return manifest, staticCatalog, nil
}

func fetchProviderReleaseSidecar(ctx context.Context, client *http.Client, metadataLocation, token string, gitHubReleaseAssets map[string]string, name, wantSHA string) ([]byte, error) {
	sidecarLocation, err := resolveArchiveSourceLocation(metadataLocation, name, gitHubReleaseAssets)
	if err != nil {
		return nil, fmt.Errorf("resolve provider release validation sidecar %s: %w", name, err)
	}
	data, err := readProviderReleaseSidecar(ctx, client, token, sidecarLocation)
	if err != nil {
		return nil, err
	}
	if got := providerrelease.SHA256Hex(data); got != strings.TrimSpace(wantSHA) {
		return nil, fmt.Errorf("provider release validation sidecar %s sha256 %q does not match %q", name, got, strings.TrimSpace(wantSHA))
	}
	return data, nil
}

func readProviderReleaseSidecar(ctx context.Context, client *http.Client, token, location string) ([]byte, error) {
	if !isRemoteReleaseMetadataLocation(location) {
		return providerrelease.ReadLocalFile(location)
	}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := doProviderReleaseHTTPRequestWithRetry(ctx, client, func(ctx context.Context) (*http.Request, error) {
		return newAuthenticatedFetchRequest(ctx, location, token)
	})
	if err != nil {
		return nil, fmt.Errorf("fetch provider release validation sidecar %s: %w", location, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d fetching provider release validation sidecar from %s", resp.StatusCode, location)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, providerrelease.MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read provider release validation sidecar: %w", err)
	}
	if len(data) > providerrelease.MaxBytes {
		return nil, fmt.Errorf("provider release validation sidecar exceeds %d byte limit", providerrelease.MaxBytes)
	}
	return data, nil
}

func doProviderReleaseHTTPRequestWithRetry(ctx context.Context, client *http.Client, newRequest func(context.Context) (*http.Request, error)) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}
	var lastErr error
	for attempt := 0; ; attempt++ {
		req, err := newRequest(ctx)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err == nil && !isTransientProviderReleaseHTTPStatus(resp.StatusCode) {
			return resp, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = nil
		}
		if attempt >= len(providerReleaseFetchRetryDelays) {
			return resp, lastErr
		}
		if resp != nil && resp.Body != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
			_ = resp.Body.Close()
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		timer := time.NewTimer(providerReleaseFetchRetryDelays[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func isTransientProviderReleaseHTTPStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
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
	token = strings.TrimSpace(token)
	resp, err := doProviderReleaseHTTPRequestWithRetry(ctx, client, func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set(httpAcceptHeader, httpAcceptGitHubAPI)
		if token != "" {
			req.Header.Set(httpAuthorizationHeader, httpBearerAuthorizationPrefix+token)
		}
		return req, nil
	})
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

func providerReleaseArchives(metadataURL string, metadata *providerrelease.Metadata, gitHubReleaseAssets map[string]string) (map[string]LockArchive, error) {
	if metadata == nil {
		return nil, fmt.Errorf("provider release metadata is required")
	}
	artifacts, err := providerrelease.ArtifactsByTarget(metadata.Artifacts)
	if err != nil {
		return nil, err
	}
	archives := make(map[string]LockArchive, len(artifacts))
	for target, artifact := range artifacts {
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
		return providermanifestv1.KindApp
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
		return providerLockRuntimeUI
	default:
		return providerLockRuntimeExecutable
	}
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
		resolved := baseURL.ResolveReference(artifactURL)
		if inheritReleaseMetadataQuery(baseURL, archiveRef, artifactURL) {
			resolved.RawQuery = baseURL.RawQuery
		}
		return resolved.String(), nil
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

func inheritReleaseMetadataQuery(base *url.URL, rawRef string, ref *url.URL) bool {
	return base != nil &&
		base.Query().Get("sourceRef") != "" &&
		ref != nil &&
		ref.Scheme == "" &&
		ref.Host == "" &&
		ref.RawQuery == "" &&
		!strings.HasPrefix(strings.TrimSpace(rawRef), "/")
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

	tmp, err := os.CreateTemp("", "gestalt-app-*.tar.gz")
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
