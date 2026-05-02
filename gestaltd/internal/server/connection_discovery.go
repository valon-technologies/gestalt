package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
)

const maxDiscoveryResponseSize = 5 * 1024 * 1024 // 5 MB

func runDiscovery(ctx context.Context, cfg *core.DiscoveryConfig, client *http.Client) ([]core.DiscoveryCandidate, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating discovery request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing discovery request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("discovery request failed: HTTP %d: %s", resp.StatusCode, body)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDiscoveryResponseSize))
	if err != nil {
		return nil, fmt.Errorf("reading discovery response: %w", err)
	}

	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parsing discovery response: %w", err)
	}

	items, err := extractDiscoveryItems(raw, cfg.ItemsPath)
	if err != nil {
		return nil, err
	}

	candidates := make([]core.DiscoveryCandidate, 0, len(items))
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		c := core.DiscoveryCandidate{
			Metadata: make(map[string]string),
		}
		if cfg.IDPath != "" {
			if v, ok := extractDiscoveryPath(obj, cfg.IDPath); ok {
				c.ID = fmt.Sprintf("%v", v)
			}
		}
		if cfg.NamePath != "" {
			if v, ok := extractDiscoveryPath(obj, cfg.NamePath); ok {
				c.Name = fmt.Sprintf("%v", v)
			}
		}
		for metaKey, jsonPath := range cfg.Metadata {
			if v, ok := extractDiscoveryPath(obj, jsonPath); ok {
				c.Metadata[metaKey] = fmt.Sprintf("%v", v)
			}
		}
		candidates = append(candidates, c)
	}

	return candidates, nil
}

func extractDiscoveryPath(data any, path string) (any, bool) {
	current := data
	for _, part := range strings.Split(path, ".") {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func extractDiscoveryItems(data any, itemsPath string) ([]any, error) {
	target := data
	if itemsPath != "" {
		v, ok := extractDiscoveryPath(data, itemsPath)
		if !ok {
			return nil, fmt.Errorf("discovery: path %q not found", itemsPath)
		}
		target = v
	}
	arr, ok := target.([]any)
	if !ok {
		if itemsPath == "" {
			return nil, fmt.Errorf("discovery response is not an array")
		}
		return nil, fmt.Errorf("discovery: path %q is not an array", itemsPath)
	}
	return arr, nil
}
