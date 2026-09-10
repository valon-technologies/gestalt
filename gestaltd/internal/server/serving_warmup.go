package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

type servingRouteStatus struct {
	Path   string `json:"path"`
	Status int    `json:"status"`
}

type activateWarmupResponse struct {
	Status     string               `json:"status"`
	InstanceID string               `json:"instanceId"`
	Routes     []servingRouteStatus `json:"routes"`
}

func servingWarmupPaths(appDefs map[string]*config.ProviderEntry, mounted []MountedUI) []string {
	paths := []string{"/", "/icon.svg"}
	seen := map[string]bool{"/": true, "/icon.svg": true}
	for i := range mounted {
		ui := mounted[i]
		path := normalizeServingWarmupPath(ui.Path)
		if path == "" || seen[path] {
			continue
		}
		paths = append(paths, path)
		seen[path] = true
		if appDefs != nil {
			if entry := appDefs[ui.AppName]; entry != nil && entry.Static != nil {
				opsPath := fmt.Sprintf("/api/v1/apps/%s/operations", ui.AppName)
				if !seen[opsPath] {
					paths = append(paths, opsPath)
					seen[opsPath] = true
				}
			}
		}
	}
	return paths
}

func normalizeServingWarmupPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if path != "/" && !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return path
}

func probeServingRoutes(handler http.Handler, paths []string) []servingRouteStatus {
	statuses := make([]servingRouteStatus, 0, len(paths))
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, "http://warmup.local"+path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		statuses = append(statuses, servingRouteStatus{
			Path:   path,
			Status: rec.Code,
		})
	}
	return statuses
}

func servingRouteFailures(statuses []servingRouteStatus) []string {
	var failures []string
	for _, route := range statuses {
		if route.Status >= http.StatusInternalServerError {
			failures = append(failures, fmt.Sprintf("%s returned HTTP %d", route.Path, route.Status))
		}
	}
	return failures
}
