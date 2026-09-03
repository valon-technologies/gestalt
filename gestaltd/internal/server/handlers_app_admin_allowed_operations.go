package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/services/apps/operationexposure"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
)

type appAdminAllowedOperationRow struct {
	ID           string   `json:"id"`
	AllowedRoles []string `json:"allowedRoles,omitempty"`
	Source       string   `json:"source"`
}

type appAdminAllowedOperationsResponse struct {
	App        string                        `json:"app"`
	Operations []appAdminAllowedOperationRow `json:"operations"`
}

type appAdminAllowedOperationsUpdateRequest struct {
	Operations map[string]*operationexposure.OperationOverride `json:"operations"`
	Removed    []string                                        `json:"removed,omitempty"`
}

func (s *Server) mountAppAdminAllowedOperationsRoutes(r chi.Router) {
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminAuthorizationMiddleware).
		Get("/apps/{app}/admin/allowed-operations", s.getAppAdminAllowedOperations)
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminAuthorizationMiddleware).
		Put("/apps/{app}/admin/allowed-operations", s.putAppAdminAllowedOperations)
}

func (s *Server) getAppAdminAllowedOperations(w http.ResponseWriter, r *http.Request) {
	appName := strings.TrimSpace(chi.URLParam(r, "app"))
	if appName == "" {
		writeError(w, http.StatusBadRequest, "app is required")
		return
	}
	entry, ok := s.pluginDefs[appName]
	if !ok || entry == nil {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}
	rows, err := s.projectAppAdminAllowedOperationRows(r.Context(), appName, entry)
	if err != nil {
		slog.Error("app admin allowed operations projection failed", "app", appName, "error", err)
		writeError(w, http.StatusServiceUnavailable, "allowed operations are unavailable")
		return
	}
	writeJSON(w, http.StatusOK, appAdminAllowedOperationsResponse{
		App:        appName,
		Operations: rows,
	})
}

func (s *Server) putAppAdminAllowedOperations(w http.ResponseWriter, r *http.Request) {
	appName := strings.TrimSpace(chi.URLParam(r, "app"))
	if appName == "" {
		writeError(w, http.StatusBadRequest, "app is required")
		return
	}
	entry, ok := s.pluginDefs[appName]
	if !ok || entry == nil {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}
	if s.appAllowedOperations == nil {
		writeError(w, http.StatusServiceUnavailable, "allowed operations are unavailable")
		return
	}
	var request appAdminAllowedOperationsUpdateRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if request.Operations == nil {
		writeError(w, http.StatusBadRequest, "operations is required")
		return
	}
	if err := s.validateAppAdminAllowedOperationsUpdate(entry, request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.appAllowedOperations.EnsureStore(r.Context()); err != nil {
		slog.Error("app admin allowed operations store unavailable", "app", appName, "error", err)
		writeError(w, http.StatusServiceUnavailable, "allowed operations are unavailable")
		return
	}
	current, err := s.appAllowedOperations.GetOverlay(r.Context(), appName)
	if err != nil && !errors.Is(err, core.ErrNotFound) {
		slog.Error("app admin allowed operations load failed", "app", appName, "error", err)
		writeError(w, http.StatusServiceUnavailable, "allowed operations are unavailable")
		return
	}
	overlay := coredata.MergeOverlayPatch(
		current,
		appName,
		request.Operations,
		normalizeRemovedOperationIDs(request.Removed),
	)
	if overlay == nil {
		if err := s.appAllowedOperations.DeleteOverlay(r.Context(), appName); err != nil {
			slog.Error("app admin allowed operations delete failed", "app", appName, "error", err)
			writeError(w, http.StatusServiceUnavailable, "allowed operations are unavailable")
			return
		}
	} else if err := s.appAllowedOperations.SetOverlay(r.Context(), overlay); err != nil {
		slog.Error("app admin allowed operations update failed", "app", appName, "error", err)
		writeError(w, http.StatusServiceUnavailable, "allowed operations are unavailable")
		return
	}
	// Rebuild providers so invoke gates and catalogs pick up the merged overlay.
	if s.activateAppProviders != nil {
		s.activateAppProviders(r.Context())
	}
	rows, err := s.projectAppAdminAllowedOperationRows(r.Context(), appName, entry)
	if err != nil {
		slog.Error("app admin allowed operations projection failed", "app", appName, "error", err)
		writeError(w, http.StatusServiceUnavailable, "allowed operations are unavailable")
		return
	}
	writeJSON(w, http.StatusOK, appAdminAllowedOperationsResponse{
		App:        appName,
		Operations: rows,
	})
}

func (s *Server) projectAppAdminAllowedOperationRows(
	ctx context.Context,
	appName string,
	entry *config.ProviderEntry,
) ([]appAdminAllowedOperationRow, error) {
	static := entry.EffectiveAllowedOperations()
	var runtimeOps map[string]*operationexposure.OperationOverride
	var removed []string
	if s.appAllowedOperations != nil {
		overlay, err := s.appAllowedOperations.GetOverlay(ctx, appName)
		if err == nil && overlay != nil {
			runtimeOps = overlay.Operations
			removed = overlay.Removed
		} else if err != nil && !errors.Is(err, core.ErrNotFound) {
			return nil, err
		}
	}
	effective := operationexposure.MergeAllowedOperationsWithOverlay(static, runtimeOps, removed)
	ids := slices.Sorted(maps.Keys(effective))
	rows := make([]appAdminAllowedOperationRow, 0, len(ids))
	for _, id := range ids {
		override := effective[id]
		row := appAdminAllowedOperationRow{
			ID:     id,
			Source: allowedOperationSource(id, static, runtimeOps),
		}
		if override != nil && len(override.AllowedRoles) > 0 {
			row.AllowedRoles = append([]string(nil), override.AllowedRoles...)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func allowedOperationSource(
	id string,
	static map[string]*operationexposure.OperationOverride,
	runtimeOps map[string]*operationexposure.OperationOverride,
) string {
	if runtimeOps[id] != nil {
		return "runtime"
	}
	if static[id] != nil {
		return "config"
	}
	return "runtime"
}

func (s *Server) validateAppAdminAllowedOperationsUpdate(
	entry *config.ProviderEntry,
	request appAdminAllowedOperationsUpdateRequest,
) error {
	known := appManifestOperationIDs(entry)
	for _, id := range request.Removed {
		id = strings.TrimSpace(id)
		if id == "" {
			return errors.New("removed operation id is required")
		}
		if len(known) > 0 {
			if _, ok := known[id]; !ok {
				return errors.New("operation " + id + " is not in the app catalog")
			}
		}
	}
	for id, override := range request.Operations {
		id = strings.TrimSpace(id)
		if id == "" {
			return errors.New("operation id is required")
		}
		if len(known) > 0 {
			if _, ok := known[id]; !ok {
				return errors.New("operation " + id + " is not in the app catalog")
			}
		}
		if override == nil || len(override.AllowedRoles) == 0 {
			return errors.New("allowedRoles is required for operation " + id)
		}
		if _, err := packageio.NormalizeUIAllowedRoles("allowedRoles", override.AllowedRoles); err != nil {
			return err
		}
	}
	return nil
}

func appManifestOperationIDs(entry *config.ProviderEntry) map[string]struct{} {
	if entry == nil {
		return nil
	}
	if entry.ResolvedCatalog != nil {
		ids := make(map[string]struct{}, len(entry.ResolvedCatalog.Operations))
		for i := range entry.ResolvedCatalog.Operations {
			id := strings.TrimSpace(entry.ResolvedCatalog.Operations[i].ID)
			if id != "" {
				ids[id] = struct{}{}
			}
		}
		if len(ids) > 0 {
			return ids
		}
	}
	static := entry.EffectiveAllowedOperations()
	if len(static) == 0 {
		return nil
	}
	ids := make(map[string]struct{}, len(static))
	for id := range static {
		ids[id] = struct{}{}
	}
	return ids
}

func normalizeRemovedOperationIDs(removed []string) []string {
	if len(removed) == 0 {
		return nil
	}
	out := make([]string, 0, len(removed))
	seen := make(map[string]struct{}, len(removed))
	for _, id := range removed {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
