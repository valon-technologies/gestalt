package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type appAccessOperationInfo struct {
	ID          string   `json:"id"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Method      string   `json:"method,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	ReadOnly    bool     `json:"readOnly"`
	Enabled     bool     `json:"enabled"`
	Default     bool     `json:"default"`
}

type appAccessResponse struct {
	App                 string                   `json:"app"`
	Operations          []appAccessOperationInfo `json:"operations"`
	EnabledOperations   []string                 `json:"enabledOperations"`
	DefaultsInitialized bool                     `json:"defaultsInitialized"`
}

type updateAppAccessRequest struct {
	EnabledOperations []string `json:"enabledOperations"`
}

func (s *Server) getAppAccess(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	prov, ok := s.getProvider(r.Context(), w, name)
	if !ok {
		return
	}
	subjectID, err := s.resolveAppAccessSubject(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "a user session is required")
		return
	}
	cat, err := s.appAccessCatalog(r, name, prov)
	if err != nil {
		s.writeAppAccessError(w, err)
		return
	}
	response, err := s.appAccessResponse(r, subjectID, name, prov, cat)
	if err != nil {
		s.writeAppAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) updateAppAccess(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	prov, ok := s.getProvider(r.Context(), w, name)
	if !ok {
		return
	}
	subjectID, err := s.resolveAppAccessSubject(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "a user session is required")
		return
	}
	var req updateAppAccessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	cat, err := s.appAccessCatalog(r, name, prov)
	if err != nil {
		s.writeAppAccessError(w, err)
		return
	}
	valid := make(map[string]struct{})
	if cat != nil {
		for i := range cat.Operations {
			valid[cat.Operations[i].ID] = struct{}{}
		}
	}
	enabled, invalid := normalizeRequestedAppAccess(req.EnabledOperations, valid)
	if len(invalid) > 0 {
		writeError(w, http.StatusBadRequest, "that app operation is not available; choose an operation from the list and try again")
		return
	}
	if s.appAccessProfiles == nil {
		writeError(w, http.StatusServiceUnavailable, "app access settings are unavailable")
		return
	}
	if _, err := s.appAccessProfiles.SetAppAccessOperations(r.Context(), subjectID, name, enabled); err != nil {
		s.writeAppAccessError(w, err)
		return
	}
	response, err := s.appAccessResponse(r, subjectID, name, prov, cat)
	if err != nil {
		s.writeAppAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) resolveAppAccessSubject(r *http.Request) (string, error) {
	p := PrincipalFromContext(r.Context())
	if p == nil || principal.IsNonUserPrincipal(p) {
		return "", errors.New("user principal required")
	}
	return principal.ResolveAuthorizationSubjectID(r.Context(), s.credentialUserResolver(), p)
}

func (s *Server) appAccessResponse(r *http.Request, subjectID, app string, prov core.Provider, cat *catalog.Catalog) (*appAccessResponse, error) {
	defaults := defaultAppAccessOperationsForProvider(prov, cat)
	enabled := defaults
	initialized := false
	if s.appAccessProfiles != nil {
		profile, err := s.appAccessProfiles.GetAppAccessProfile(r.Context(), subjectID, app)
		if err == nil {
			enabled = profile.EnabledOperations
			initialized = profile.DefaultsInitialized
		} else if !errors.Is(err, core.ErrNotFound) {
			return nil, err
		}
	}
	enabledSet := make(map[string]struct{}, len(enabled))
	for _, operation := range enabled {
		enabledSet[operation] = struct{}{}
	}
	defaultSet := make(map[string]struct{}, len(defaults))
	for _, operation := range defaults {
		defaultSet[operation] = struct{}{}
	}
	response := &appAccessResponse{
		App:                 app,
		Operations:          make([]appAccessOperationInfo, 0),
		EnabledOperations:   append([]string(nil), enabled...),
		DefaultsInitialized: initialized,
	}
	if cat == nil {
		return response, nil
	}
	response.Operations = make([]appAccessOperationInfo, 0, len(cat.Operations))
	for i := range cat.Operations {
		op := &cat.Operations[i]
		readOnly := op.ReadOnly
		if op.Annotations.ReadOnlyHint != nil {
			readOnly = readOnly || *op.Annotations.ReadOnlyHint
		}
		_, isEnabled := enabledSet[op.ID]
		_, isDefault := defaultSet[op.ID]
		response.Operations = append(response.Operations, appAccessOperationInfo{
			ID:          op.ID,
			Title:       appAccessOperationTitle(op.ID, op.Title),
			Description: op.Description,
			Method:      op.Method,
			Tags:        append([]string(nil), op.Tags...),
			ReadOnly:    readOnly,
			Enabled:     isEnabled,
			Default:     isDefault,
		})
	}
	slices.SortFunc(response.Operations, func(a, b appAccessOperationInfo) int {
		return strings.Compare(a.ID, b.ID)
	})
	return response, nil
}

func (s *Server) appAccessCatalog(r *http.Request, app string, prov core.Provider) (*catalog.Catalog, error) {
	staticCat := appAccessCapabilityCatalog(prov, s.publicCatalog(app, prov, prov.Catalog()))
	if !core.SupportsSessionCatalog(prov) {
		return staticCat, nil
	}
	p := PrincipalFromContext(r.Context())
	resolver, _ := s.invoker.(invocation.TokenResolver)
	if p == nil || resolver == nil {
		return staticCat, nil
	}
	cat, _, err := invocation.ResolveCatalogForTargetsWithMetadata(
		core.WithCatalogSurface(r.Context(), core.CatalogSurfaceAPI),
		prov,
		app,
		resolver,
		p,
		s.catalogSelectorConfig().APICatalogTargets(app, "", ""),
		true,
	)
	if err != nil {
		if staticCat != nil {
			return staticCat, nil
		}
		return nil, err
	}
	return appAccessCapabilityCatalog(prov, s.publicCatalog(app, prov, cat)), nil
}

func appAccessCapabilityCatalog(prov core.Provider, cat *catalog.Catalog) *catalog.Catalog {
	if prov == nil {
		return cat
	}
	if _, ok := prov.(core.GraphQLSurfaceInvoker); !ok {
		return cat
	}
	if cat == nil {
		cat = &catalog.Catalog{
			Name:        prov.Name(),
			DisplayName: prov.DisplayName(),
			Description: prov.Description(),
		}
	} else {
		cat = cat.Clone()
	}
	if _, ok := catalog.OperationByID(cat, core.GraphQLCapabilityID); ok {
		return cat
	}
	cat.Operations = append(cat.Operations, catalog.CatalogOperation{
		ID:          core.GraphQLCapabilityID,
		Title:       "GraphQL",
		Description: "Run GraphQL queries against this app",
		Method:      "POST",
		Tags:        []string{"graphql"},
		Transport:   "graphql",
	})
	return cat
}

func defaultAppAccessOperationsForProvider(prov core.Provider, cat *catalog.Catalog) []string {
	operations := catOperations(cat)
	if provider, ok := prov.(core.AppAccessDefaultsProvider); ok {
		defaults, configured := provider.DefaultAppAccessOperations()
		if configured {
			valid := make(map[string]struct{}, len(operations))
			for _, operation := range operations {
				valid[operation] = struct{}{}
			}
			filtered, _ := normalizeRequestedAppAccess(defaults, valid)
			return filtered
		}
	}
	return operations
}

func catOperations(cat *catalog.Catalog) []string {
	if cat == nil {
		return nil
	}
	operations := make([]string, 0, len(cat.Operations))
	for i := range cat.Operations {
		operation := &cat.Operations[i]
		if catalog.OperationVisibleByDefault(*operation) {
			operations = append(operations, operation.ID)
		}
	}
	slices.Sort(operations)
	return operations
}

func appAccessOperationTitle(id, title string) string {
	if title = strings.TrimSpace(title); title != "" {
		return title
	}

	var words []string
	var word []rune
	flush := func() {
		if len(word) == 0 {
			return
		}
		word[0] = unicode.ToUpper(word[0])
		words = append(words, string(word))
		word = nil
	}
	var previous rune
	for _, r := range strings.TrimSpace(id) {
		if r == '.' || r == '_' || r == '-' {
			flush()
			previous = 0
			continue
		}
		if len(word) > 0 && unicode.IsUpper(r) && (unicode.IsLower(previous) || unicode.IsDigit(previous)) {
			flush()
		}
		word = append(word, unicode.ToLower(r))
		previous = r
	}
	flush()
	return strings.Join(words, " ")
}

func normalizeRequestedAppAccess(operations []string, valid map[string]struct{}) ([]string, []string) {
	seen := make(map[string]struct{}, len(operations))
	var normalized []string
	var invalid []string
	for _, operation := range operations {
		operation = strings.TrimSpace(operation)
		if operation == "" {
			continue
		}
		if _, ok := valid[operation]; !ok {
			invalid = append(invalid, operation)
			continue
		}
		if _, ok := seen[operation]; ok {
			continue
		}
		seen[operation] = struct{}{}
		normalized = append(normalized, operation)
	}
	slices.Sort(normalized)
	slices.Sort(invalid)
	return normalized, invalid
}

func (s *Server) writeAppAccessError(w http.ResponseWriter, err error) {
	if errors.Is(err, core.ErrNotFound) {
		writeError(w, http.StatusNotFound, "app access settings were not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "app access settings are unavailable")
}
