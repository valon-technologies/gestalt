package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
)

const (
	DefaultBaseURL      = "https://api.github.com"
	headerAccept        = "Accept"
	headerAuthorization = "Authorization"
	acceptOctetStream   = "application/octet-stream"
	envGitHubToken      = "GITHUB_TOKEN"
	authTokenPrefix     = "token "
)

type releaseResponse struct {
	Assets []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	URL                string `json:"url"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type GitHubResolver struct {
	Token      string
	BaseURL    string
	HTTPClient *http.Client
}

func (r *GitHubResolver) Resolve(ctx context.Context, src pluginsource.Source, version string) (*pluginsource.ResolvedPackage, error) {
	baseURL := r.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	client := r.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	token := r.Token
	if token == "" {
		token = os.Getenv(envGitHubToken)
	}

	tag := src.ReleaseTag(version)
	releaseURL := fmt.Sprintf("%s/repos/%s/releases/tags/%s", baseURL, src.RepoSlug(), url.PathEscape(tag))

	release, err := r.fetchRelease(ctx, client, releaseURL, token, tag, src.RepoSlug())
	if err != nil {
		return nil, err
	}

	expectedName := src.AssetName(version)
	asset, err := findAsset(release.Assets, expectedName, tag, src.RepoSlug())
	if err != nil {
		return nil, err
	}

	dl, err := r.downloadAsset(ctx, client, asset.URL, token)
	if err != nil {
		return nil, err
	}

	return &pluginsource.ResolvedPackage{
		LocalPath:     dl.LocalPath,
		Cleanup:       dl.Cleanup,
		ArchiveSHA256: dl.SHA256Hex,
		ResolvedURL:   asset.BrowserDownloadURL,
	}, nil
}

func (r *GitHubResolver) fetchRelease(ctx context.Context, client *http.Client, url, token, tag, slug string) (*releaseResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create release request: %w", err)
	}
	req.Header.Set(headerAccept, core.ContentTypeJSON)
	if token != "" {
		req.Header.Set(headerAuthorization, authTokenPrefix+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("release tag %s not found for %s", tag, slug)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d fetching release %s for %s", resp.StatusCode, tag, slug)
	}

	var release releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decode release response: %w", err)
	}
	return &release, nil
}

func findAsset(assets []releaseAsset, expectedName, tag, slug string) (releaseAsset, error) {
	for _, a := range assets {
		if a.Name == expectedName {
			return a, nil
		}
	}
	names := make([]string, len(assets))
	for i, a := range assets {
		names[i] = a.Name
	}
	return releaseAsset{}, fmt.Errorf(
		"release %s for %s does not contain asset %s; available assets: %s",
		tag, slug, expectedName, strings.Join(names, ", "),
	)
}

func (r *GitHubResolver) downloadAsset(ctx context.Context, client *http.Client, assetURL, token string) (*pluginpkg.DownloadResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create asset download request: %w", err)
	}
	req.Header.Set(headerAccept, acceptOctetStream)
	if token != "" {
		req.Header.Set(headerAuthorization, authTokenPrefix+token)
	}
	return pluginpkg.DownloadRequest(client, req)
}
