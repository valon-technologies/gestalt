package appregistry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
)

// ErrRegistryDocumentNotFound indicates a registry document path does not exist.
var ErrRegistryDocumentNotFound = errors.New("app registry document not found")

// RegistryReader fetches registry documents over HTTP from a registry public root.
type RegistryReader struct {
	HTTPClient *http.Client
}

type AppIndexFetchResult struct {
	Index       *Index
	ETag        string
	NotModified bool
}

func (r *RegistryReader) client() *http.Client {
	if r != nil && r.HTTPClient != nil {
		return r.HTTPClient
	}
	return http.DefaultClient
}

func (r *RegistryReader) fetchJSON(ctx context.Context, url string, notFoundEmpty bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build app registry request: %w", err)
	}
	resp, err := r.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch app registry document %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		if notFoundEmpty {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: %s", ErrRegistryDocumentNotFound, url)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch app registry document %s: unexpected status %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read app registry document %s: %w", url, err)
	}
	return body, nil
}

// FetchPendingIndex downloads apps/{app}/pending.json from a registry public root.
func (r *RegistryReader) FetchPendingIndex(ctx context.Context, publicRoot, appName string) (*PendingIndex, error) {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return nil, fmt.Errorf("app name is required")
	}
	if err := providerregistry.ValidateRepositoryName(appName); err != nil {
		return nil, err
	}
	publicRoot = strings.TrimRight(strings.TrimSpace(publicRoot), "/")
	if publicRoot == "" {
		return nil, fmt.Errorf("registry public URL is required")
	}

	url := PublicURL(publicRoot, AppPendingPath(appName))
	body, err := r.fetchJSON(ctx, url, true)
	if err != nil {
		return nil, err
	}
	if body == nil {
		return NewEmptyPendingIndex(appName), nil
	}
	return DecodePendingIndex(body)
}

// FetchFailedIndex downloads apps/{app}/failed.json from a registry public root.
func (r *RegistryReader) FetchFailedIndex(ctx context.Context, publicRoot, appName string) (*FailedIndex, error) {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return nil, fmt.Errorf("app name is required")
	}
	if err := providerregistry.ValidateRepositoryName(appName); err != nil {
		return nil, err
	}
	publicRoot = strings.TrimRight(strings.TrimSpace(publicRoot), "/")
	if publicRoot == "" {
		return nil, fmt.Errorf("registry public URL is required")
	}

	url := PublicURL(publicRoot, AppFailedPath(appName))
	body, err := r.fetchJSON(ctx, url, true)
	if err != nil {
		return nil, err
	}
	if body == nil {
		return NewEmptyFailedIndex(appName), nil
	}
	return DecodeFailedIndex(body)
}

// FetchRetentionIndex downloads apps/{app}/retention.json from a registry public root.
func (r *RegistryReader) FetchRetentionIndex(ctx context.Context, publicRoot, appName string) (*RetentionIndex, error) {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return nil, fmt.Errorf("app name is required")
	}
	if err := providerregistry.ValidateRepositoryName(appName); err != nil {
		return nil, err
	}
	publicRoot = strings.TrimRight(strings.TrimSpace(publicRoot), "/")
	if publicRoot == "" {
		return nil, fmt.Errorf("registry public URL is required")
	}

	url := PublicURL(publicRoot, AppRetentionPath(appName))
	body, err := r.fetchJSON(ctx, url, true)
	if err != nil {
		return nil, err
	}
	if body == nil {
		return NewEmptyRetentionIndex(), nil
	}
	return DecodeRetentionIndex(body)
}

// FetchAppIndex downloads apps/{app}/index.json from a registry public root.
func (r *RegistryReader) FetchAppIndex(ctx context.Context, publicRoot, appName string) (*Index, error) {
	result, err := r.FetchAppIndexConditional(ctx, publicRoot, appName, "")
	if err != nil {
		return nil, err
	}
	return result.Index, nil
}

// FetchAppIndexConditional downloads apps/{app}/index.json and optionally
// sends an If-None-Match validator. A 304 response has NotModified set and no
// decoded index.
func (r *RegistryReader) FetchAppIndexConditional(
	ctx context.Context,
	publicRoot, appName, ifNoneMatch string,
) (*AppIndexFetchResult, error) {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return nil, fmt.Errorf("app name is required")
	}
	if err := providerregistry.ValidateRepositoryName(appName); err != nil {
		return nil, err
	}
	publicRoot = strings.TrimRight(strings.TrimSpace(publicRoot), "/")
	if publicRoot == "" {
		return nil, fmt.Errorf("registry public URL is required")
	}

	url := PublicURL(publicRoot, AppIndexPath(appName))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build app registry request: %w", err)
	}
	if ifNoneMatch = strings.TrimSpace(ifNoneMatch); ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	resp, err := r.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch app registry document %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNotModified:
		return &AppIndexFetchResult{NotModified: true}, nil
	case http.StatusNotFound:
		return &AppIndexFetchResult{Index: NewEmptyIndex()}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch app registry document %s: unexpected status %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read app registry document %s: %w", url, err)
	}
	index, err := DecodeIndex(body)
	if err != nil {
		return nil, err
	}
	return &AppIndexFetchResult{
		Index: index,
		ETag:  strings.TrimSpace(resp.Header.Get("ETag")),
	}, nil
}

// FetchEntry downloads apps/{app}/versions/{version}.json from a registry public root.
func (r *RegistryReader) FetchEntry(ctx context.Context, publicRoot, appName, version string) (*Entry, error) {
	appName = strings.TrimSpace(appName)
	version = strings.TrimSpace(version)
	if appName == "" {
		return nil, fmt.Errorf("app name is required")
	}
	if version == "" {
		return nil, fmt.Errorf("version is required")
	}
	if err := providerregistry.ValidateRepositoryName(appName); err != nil {
		return nil, err
	}
	publicRoot = strings.TrimRight(strings.TrimSpace(publicRoot), "/")
	if publicRoot == "" {
		return nil, fmt.Errorf("registry public URL is required")
	}

	url := PublicURL(publicRoot, AppVersionEntryPath(appName, version))
	body, err := r.fetchJSON(ctx, url, false)
	if err != nil {
		return nil, err
	}
	return DecodeEntry(body)
}

// VersionSummary is a lightweight view of one published app version from an index.
type VersionSummary struct {
	Version          string       `json:"version"`
	Metadata         string       `json:"metadata"`
	Platforms        []string     `json:"platforms,omitempty"`
	PublishedAt      time.Time    `json:"publishedAt"`
	PublishStartedAt *time.Time   `json:"publishStartedAt,omitempty"`
	SourceRef        string       `json:"sourceRef,omitempty"`
	Repository       string       `json:"repository,omitempty"`
	Publication      *Publication `json:"publication,omitempty"`
}

// VersionsFromIndex returns version summaries for appName, newest first.
func VersionsFromIndex(index *Index, appName string) []VersionSummary {
	if index == nil || len(index.Apps) == 0 {
		return []VersionSummary{}
	}
	appVersions, ok := index.Apps[appName]
	if !ok || len(appVersions.Versions) == 0 {
		return []VersionSummary{}
	}
	out := make([]VersionSummary, 0, len(appVersions.Versions))
	for version := range appVersions.Versions {
		summary := appVersions.Versions[version]
		out = append(out, VersionSummary{
			Version:          version,
			Metadata:         summary.Metadata,
			Platforms:        append([]string(nil), summary.Platforms...),
			PublishedAt:      summary.PublishedAt.UTC(),
			PublishStartedAt: cloneTimePtr(summary.PublishStartedAt),
			SourceRef:        summary.SourceRef,
			Repository:       summary.Repository,
			Publication:      clonePublication(summary.Publication),
		})
	}
	sortVersionsNewestFirst(out)
	return out
}

func sortVersionsNewestFirst(versions []VersionSummary) {
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].PublishedAt.Equal(versions[j].PublishedAt) {
			return versions[i].Version > versions[j].Version
		}
		return versions[i].PublishedAt.After(versions[j].PublishedAt)
	})
}
