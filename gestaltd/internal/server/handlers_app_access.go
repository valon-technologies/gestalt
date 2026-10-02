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

func (s *Server) appAccessHandler(w http.ResponseWriter, r *http.Request) {
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
		s.writeAppOperationPolicyError(w, r, name, err)
		return
	}
	if r.Method == http.MethodPut {
		var req updateAppAccessRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if s.appAccessProfiles == nil {
			writeError(w, http.StatusServiceUnavailable, "app access settings are unavailable")
			return
		}
		// Ids the catalog no longer lists are dropped rather than rejected: the
		// page echoes back the user's saved list, which can name an operation
		// the app has since removed, and that must not block every later save.
		disabled, extra := core.AppAccessOverrides(req.EnabledOperations, cat, core.AppAccessDefaultsFor(prov))
		// The save replaces every decision, but the page only offered the
		// operations in cat. Keep the user's choices about the rest.
		existing, err := s.appAccessProfiles.GetAppAccessProfile(r.Context(), subjectID, name)
		if err != nil && !errors.Is(err, core.ErrNotFound) {
			s.writeAppAccessError(w, err)
			return
		}
		existing = s.appAccess.Resolve(r.Context(), prov, existing)
		keptDisabled, keptExtra := existing.OverridesOutside(cat, prov.Catalog())
		disabled = core.MergeAppAccessIDs(disabled, keptDisabled)
		extra = core.MergeAppAccessIDs(extra, keptExtra)
		if _, err := s.appAccessProfiles.SetAppAccessOverrides(r.Context(), subjectID, name, disabled, extra); err != nil {
			s.writeAppAccessError(w, err)
			return
		}
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
	defaults := core.AppAccessDefaultsFor(prov)
	var profile *core.AppAccessProfile
	initialized := false
	if s.appAccessProfiles != nil {
		stored, err := s.appAccessProfiles.GetAppAccessProfile(r.Context(), subjectID, app)
		if err == nil {
			profile = s.appAccess.Resolve(r.Context(), prov, stored)
			initialized = profile.DefaultsInitialized
		} else if !errors.Is(err, core.ErrNotFound) {
			return nil, err
		}
	}
	enabled := profile.EnabledOperations(cat, defaults)
	response := &appAccessResponse{
		App:                 app,
		Operations:          make([]appAccessOperationInfo, 0),
		EnabledOperations:   enabled,
		DefaultsInitialized: initialized,
	}
	if cat == nil {
		return response, nil
	}
	response.Operations = make([]appAccessOperationInfo, 0, len(cat.Operations))
	for i := range cat.Operations {
		op := &cat.Operations[i]
		isEnabled := profile.Allows(*op, defaults)
		isDefault := defaults.Includes(*op)
		response.Operations = append(response.Operations, appAccessOperationInfo{
			ID:          op.ID,
			Title:       appAccessOperationTitle(op.ID, op.Title),
			Description: op.Description,
			Method:      op.Method,
			Tags:        append([]string(nil), op.Tags...),
			ReadOnly:    catalog.OperationIsReadOnly(*op),
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
	baseline, err := s.appAccessBaselineCatalog(r, app, prov)
	baseline = s.publicCatalog(app, prov, baseline)
	if err != nil || s.appAllowedOperations == nil {
		return baseline, err
	}
	policy, err := s.appAllowedOperations.GetAppOperationPolicy(r.Context(), app)
	if err != nil {
		return nil, err
	}
	return policy.Catalog(baseline), nil
}

func (s *Server) appAccessBaselineCatalog(r *http.Request, app string, prov core.Provider) (*catalog.Catalog, error) {
	staticCat := appAccessCapabilityCatalog(prov, prov.Catalog())
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
	return appAccessCapabilityCatalog(prov, cat), nil
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

func (s *Server) writeAppAccessError(w http.ResponseWriter, err error) {
	if errors.Is(err, core.ErrNotFound) {
		writeError(w, http.StatusNotFound, "app access settings were not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "app access settings are unavailable")
}
