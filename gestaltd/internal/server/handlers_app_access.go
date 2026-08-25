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
	response, err := s.appAccessResponse(r, subjectID, name, prov)
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
	cat := s.publicCatalog(name, prov, prov.Catalog())
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
	response, err := s.appAccessResponse(r, subjectID, name, prov)
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

func (s *Server) appAccessResponse(r *http.Request, subjectID, app string, prov core.Provider) (*appAccessResponse, error) {
	cat := s.publicCatalog(app, prov, prov.Catalog())
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

func defaultAppAccessOperations(cat *catalog.Catalog) []string {
	if cat == nil {
		return nil
	}
	readOnly := make([]string, 0, len(cat.Operations))
	all := make([]string, 0, len(cat.Operations))
	for i := range cat.Operations {
		op := &cat.Operations[i]
		if !catalog.OperationVisibleByDefault(*op) {
			continue
		}
		all = append(all, op.ID)
		isReadOnly := op.ReadOnly
		if op.Annotations.ReadOnlyHint != nil {
			isReadOnly = isReadOnly || *op.Annotations.ReadOnlyHint
		}
		if isReadOnly {
			readOnly = append(readOnly, op.ID)
		}
	}
	// Providers that declare read-only hints get a safe read-first profile.
	// Older providers without that metadata retain their existing behavior until
	// they publish capability annotations.
	if len(readOnly) > 0 {
		slices.Sort(readOnly)
		return readOnly
	}
	slices.Sort(all)
	return all
}

func defaultAppAccessOperationsForProvider(prov core.Provider, cat *catalog.Catalog) []string {
	if provider, ok := prov.(core.AppAccessDefaultsProvider); ok {
		operations, configured := provider.DefaultAppAccessOperations()
		if configured {
			valid := make(map[string]struct{}, len(catOperations(cat)))
			for _, operation := range catOperations(cat) {
				valid[operation] = struct{}{}
			}
			filtered, _ := normalizeRequestedAppAccess(operations, valid)
			return filtered
		}
	}
	return defaultAppAccessOperations(cat)
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
