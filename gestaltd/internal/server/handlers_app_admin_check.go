package server

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/apps/mcpupstream"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type appAdminCheckResponse struct {
	App      string                               `json:"app"`
	OK       bool                                 `json:"ok"`
	Surfaces []mcpupstream.AllowedOperationsCheck `json:"surfaces"`
}

func (s *Server) mountAppAdminCheckRoutes(r chi.Router) {
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminUIObservabilityMiddleware, s.appAdminAuthorizationMiddleware).
		Get("/apps/{app}/admin/check", s.appAdminCheckHandler)
}

// appAdminCheckHandler lists an app's MCP tools live, using the caller's
// connection, and reports how the configured allowed_operations match them.
// Normal discovery fails outright on an unknown allowed_operations key; this
// endpoint reports every mismatch instead so admins can fix config up front.
func (s *Server) appAdminCheckHandler(w http.ResponseWriter, r *http.Request) {
	app := chi.URLParam(r, "app")
	if s.pluginDefs[app] == nil {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}
	prov, ok := s.getProvider(r.Context(), w, app)
	if !ok {
		return
	}
	if !core.SupportsSessionCatalog(prov) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("app %q has no MCP surface to check", app))
		return
	}
	requestedConnection := r.URL.Query().Get(httpConnectionParam)
	if requestedConnection != "" {
		requestedConnection, ok = s.resolveRequestedConnection(w, app, requestedConnection)
		if !ok {
			return
		}
	}
	requestedInstance := r.URL.Query().Get(httpInstanceParam)
	if requestedInstance != "" {
		requestedInstance, ok = resolveRequestedInstance(w, requestedInstance)
		if !ok {
			return
		}
	}
	resolver, _ := s.invoker.(invocation.TokenResolver)

	report := &mcpupstream.CheckReport{}
	ctx := mcpupstream.WithCheckReport(core.WithCatalogSurface(r.Context(), core.CatalogSurfaceAPI), report)
	_, _, err := invocation.ResolveCatalogForTargetsWithMetadata(
		ctx,
		prov,
		app,
		resolver,
		PrincipalFromContext(r.Context()),
		s.catalogSelectorConfig().APICatalogTargets(app, requestedConnection, requestedInstance),
		true,
	)
	checks := report.Checks()
	if err != nil && len(checks) == 0 {
		// Nothing reached an MCP upstream, e.g. the caller is not connected.
		s.writeInvocationError(w, r, app, "", err)
		return
	}
	if len(checks) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("app %q has no MCP surface to check", app))
		return
	}

	response := appAdminCheckResponse{App: app, OK: true, Surfaces: checks}
	for _, check := range checks {
		if !check.OK() {
			response.OK = false
		}
	}
	writeJSON(w, http.StatusOK, response)
}
