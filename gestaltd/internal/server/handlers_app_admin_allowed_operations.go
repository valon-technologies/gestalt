package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
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
	Operations map[string]struct {
		AllowedRoles []string `json:"allowedRoles"`
	} `json:"operations"`
	Removed []string `json:"removed,omitempty"`
}

func (s *Server) mountAppAdminAllowedOperationsRoutes(r chi.Router) {
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminUIObservabilityMiddleware, s.appAdminAuthorizationMiddleware).
		Get("/apps/{app}/admin/allowed-operations", s.appAdminAllowedOperationsHandler)
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminUIObservabilityMiddleware, s.appAdminAuthorizationMiddleware).
		Put("/apps/{app}/admin/allowed-operations", s.appAdminAllowedOperationsHandler)
}

func (s *Server) appAdminAllowedOperationsHandler(w http.ResponseWriter, r *http.Request) {
	app := chi.URLParam(r, "app")
	if s.pluginDefs[app] == nil {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}
	prov, ok := s.getProvider(r.Context(), w, app)
	if !ok {
		return
	}
	if s.appAllowedOperations == nil {
		writeError(w, http.StatusServiceUnavailable, "allowed operations are unavailable")
		return
	}
	// Use the provider's exposed catalog, including aliases and unrestricted
	// operations. Runtime permissions must never become the definition baseline.
	baseline, err := s.appAccessBaselineCatalog(r, app, prov)
	if err != nil || baseline == nil {
		slog.ErrorContext(r.Context(), "app operation catalog unavailable", "app", app, "error", err)
		writeError(w, http.StatusServiceUnavailable, "app catalog is unavailable")
		return
	}
	policy, err := s.appAllowedOperations.GetAppOperationPolicy(r.Context(), app)
	if err != nil {
		s.writeAppOperationPolicyError(w, r, app, err)
		return
	}
	if r.Method == http.MethodPut {
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
		patch, err := request.permissionPatch(baseline, policy)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.appAllowedOperations.Patch(r.Context(), app, patch); err != nil {
			s.writeAppOperationPolicyError(w, r, app, err)
			return
		}
		policy, err = s.appAllowedOperations.GetAppOperationPolicy(r.Context(), app)
		if err != nil {
			s.writeAppOperationPolicyError(w, r, app, err)
			return
		}
	}
	rows := make([]appAdminAllowedOperationRow, 0, len(baseline.Operations))
	effective := policy.Catalog(baseline)
	for i := range effective.Operations {
		operation := &effective.Operations[i]
		source := "config"
		if _, changed := policy[operation.ID]; changed {
			source = "runtime"
		}
		rows = append(rows, appAdminAllowedOperationRow{
			ID: operation.ID, AllowedRoles: operation.AllowedRoles, Source: source,
		})
	}
	slices.SortFunc(rows, func(a, b appAdminAllowedOperationRow) int { return strings.Compare(a.ID, b.ID) })
	writeJSON(w, http.StatusOK, appAdminAllowedOperationsResponse{App: app, Operations: rows})
}

func (s *Server) writeAppOperationPolicyError(w http.ResponseWriter, r *http.Request, app string, err error) {
	slog.ErrorContext(r.Context(), "app operation permissions unavailable", "app", app, "error", err)
	writeError(w, http.StatusServiceUnavailable, "allowed operations are unavailable")
}

func (request appAdminAllowedOperationsUpdateRequest) permissionPatch(baseline *catalog.Catalog, current core.AppOperationPolicy) (core.AppOperationPolicy, error) {
	if request.Operations == nil {
		return nil, errors.New("operations is required")
	}
	known := make(map[string]bool, len(baseline.Operations)+len(current))
	for i := range baseline.Operations {
		known[baseline.Operations[i].ID] = true
	}
	// A temporarily unavailable session catalog must not strand existing policy.
	for id := range current {
		known[id] = true
	}
	patch := make(core.AppOperationPolicy, len(request.Operations)+len(request.Removed))
	for _, id := range request.Removed {
		patch[id] = nil
	}
	for id, permission := range request.Operations {
		if _, removed := patch[id]; removed {
			return nil, errors.New("operation " + id + " cannot be both updated and removed")
		}
		roles, err := packageio.NormalizeUIAllowedRoles("allowedRoles", permission.AllowedRoles)
		if err != nil {
			return nil, err
		}
		patch[id] = roles
	}
	for id := range patch {
		if id == "" || strings.TrimSpace(id) != id || !known[id] {
			return nil, errors.New("operation " + id + " is not in the app catalog")
		}
	}
	return patch, nil
}
