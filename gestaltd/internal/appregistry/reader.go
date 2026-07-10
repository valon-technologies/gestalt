package appregistry

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// RegistryReader fetches registry documents over HTTP from a registry public root.
type RegistryReader struct {
	HTTPClient *http.Client
}

func (r *RegistryReader) client() *http.Client {
	if r != nil && r.HTTPClient != nil {
		return r.HTTPClient
	}
	return http.DefaultClient
}

// FetchAppIndex downloads apps/{app}/index.json from a registry public root.
func (r *RegistryReader) FetchAppIndex(ctx context.Context, publicRoot, appName string) (*Index, error) {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return nil, fmt.Errorf("app name is required")
	}
	publicRoot = strings.TrimRight(strings.TrimSpace(publicRoot), "/")
	if publicRoot == "" {
		return nil, fmt.Errorf("registry public URL is required")
	}

	url := PublicURL(publicRoot, AppIndexPath(appName))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build app registry index request: %w", err)
	}
	resp, err := r.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch app registry index %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return NewEmptyIndex(), nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch app registry index %s: unexpected status %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read app registry index %s: %w", url, err)
	}
	return DecodeIndex(body)
}

// VersionSummary is a lightweight view of one published app version from an index.
type VersionSummary struct {
	Version     string    `json:"version"`
	Metadata    string    `json:"metadata"`
	Platforms   []string  `json:"platforms,omitempty"`
	PublishedAt time.Time `json:"publishedAt"`
}

// VersionsFromIndex returns version summaries for appName, newest first.
func VersionsFromIndex(index *Index, appName string) []VersionSummary {
	if index == nil || len(index.Apps) == 0 {
		return nil
	}
	appVersions, ok := index.Apps[appName]
	if !ok || len(appVersions.Versions) == 0 {
		return nil
	}
	out := make([]VersionSummary, 0, len(appVersions.Versions))
	for version, summary := range appVersions.Versions {
		out = append(out, VersionSummary{
			Version:     version,
			Metadata:    summary.Metadata,
			Platforms:   append([]string(nil), summary.Platforms...),
			PublishedAt: summary.PublishedAt.UTC(),
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
