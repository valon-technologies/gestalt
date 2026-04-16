package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/apiexec"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/principal"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const defaultTokenInstance = "default"
const httpInstanceParam = "_instance"
const httpConnectionParam = "_connection"
const legacyHTTPInstanceParam = "instance"
const legacyHTTPConnectionParam = "connection"

const cliStatePrefix = "cli:"
const maxPort = 65535

const sessionCookieName = "session_token"
const defaultSessionCookieTTL = 24 * time.Hour

var (
	errNotAuthenticated  = errors.New("not authenticated")
	errResolveUser       = errors.New("failed to resolve user")
	errWorkloadForbidden = errors.New("workload callers are not allowed on this route")
	errOperationAccess   = errors.New("operation access denied")
	errWorkloadSelector  = errors.New("workload callers may not override connection or instance bindings")
)

var (
	safeParamValue         = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	safeInstanceValue      = regexp.MustCompile(`^[a-zA-Z0-9._ -]+$`)
	safeTokenResponseValue = regexp.MustCompile(`^[a-zA-Z0-9._:/-]+$`)
)

type instanceInfo struct {
	Name       string `json:"name"`
	Connection string `json:"connection,omitempty"`
}

type credentialFieldInfo struct {
	Name        string `json:"name"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
}

type connectionDefInfo struct {
	DisplayName      string                `json:"displayName,omitempty"`
	Name             string                `json:"name"`
	AuthTypes        []string              `json:"authTypes"`
	CredentialFields []credentialFieldInfo `json:"credentialFields"`
}

type integrationInfo struct {
	Name             string                         `json:"name"`
	DisplayName      string                         `json:"displayName,omitempty"`
	Description      string                         `json:"description,omitempty"`
	IconSVG          string                         `json:"iconSvg,omitempty"`
	MountedPath      string                         `json:"mountedPath,omitempty"`
	Connected        bool                           `json:"connected"`
	Instances        []instanceInfo                 `json:"instances"`
	AuthTypes        []string                       `json:"authTypes"`
	ConnectionParams map[string]connectionParamInfo `json:"connectionParams"`
	Connections      []connectionDefInfo            `json:"connections"`
	CredentialFields []credentialFieldInfo          `json:"credentialFields"`
}

type connectionParamInfo struct {
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
	Default     string `json:"default,omitempty"`
}

func (s *Server) resolveUserID(w http.ResponseWriter, r *http.Request) (string, error) {
	if p := PrincipalFromContext(r.Context()); p != nil && p.Kind == principal.KindWorkload {
		writeError(w, http.StatusForbidden, "workload callers are not allowed on this route")
		return "", errWorkloadForbidden
	}
	user := UserFromContext(r.Context())
	if user == nil || user.Email == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return "", errNotAuthenticated
	}
	if id := UserIDFromContext(r.Context()); id != "" {
		return id, nil
	}
	dbUser, err := s.users.FindOrCreateUser(r.Context(), user.Email)
	if err != nil || dbUser == nil || dbUser.ID == "" {
		writeError(w, http.StatusInternalServerError, "failed to resolve user")
		return "", errResolveUser
	}
	return dbUser.ID, nil
}

func (s *Server) healthCheck(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) readinessCheck(w http.ResponseWriter, _ *http.Request) {
	if s.readiness != nil {
		if reason := s.readiness(); reason != "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": reason})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) listIntegrations(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFromContext(r.Context())
	connected := map[string][]instanceInfo{}
	if p == nil || p.Kind != principal.KindWorkload {
		var err error
		connected, err = s.userConnectedIntegrations(r)
		if err != nil {
			slog.ErrorContext(r.Context(), "listing integrations", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to check integration status")
			return
		}
	}

	names := s.providers.List()
	out := make([]integrationInfo, 0, len(names))
	for _, name := range names {
		if !s.allowProvider(p, name) {
			continue
		}
		prov, err := s.providers.Get(name)
		if err != nil {
			continue
		}
		info := integrationInfo{
			Name:             name,
			DisplayName:      prov.DisplayName(),
			Description:      prov.Description(),
			Connected:        false,
			Instances:        []instanceInfo{},
			AuthTypes:        []string{},
			ConnectionParams: map[string]connectionParamInfo{},
			Connections:      []connectionDefInfo{},
			CredentialFields: []credentialFieldInfo{},
		}
		if cat := prov.Catalog(); cat != nil {
			info.IconSVG = cat.IconSVG
		}
		if entry, ok := s.pluginDefs[name]; ok && entry != nil {
			info.MountedPath = strings.TrimSpace(entry.MountPath)
		}
		if p != nil && p.Kind == principal.KindWorkload {
			if binding, ok := s.workloadBinding(p, name); ok {
				bindingConnected, err := s.workloadBindingConnected(r.Context(), binding, name)
				if err != nil {
					slog.ErrorContext(r.Context(), "checking workload integration status", "provider", name, "error", err)
					writeError(w, http.StatusInternalServerError, "failed to check integration status")
					return
				}
				info.Connected = bindingConnected
			}
			info.MountedPath = s.integrationMountedPathForPrincipal(p, name, info.MountedPath)
			if !s.integrationHasUsableSurface(p, name, prov, info) {
				continue
			}
			out = append(out, info)
			continue
		}

		authTypes := integrationAuthTypesForProvider(prov)
		instances := connected[name]
		info.Connected = len(instances) > 0
		info.Instances = append(make([]instanceInfo, 0, len(instances)), instances...)
		info.CredentialFields = credentialFieldInfosFromProvider(prov)
		for name, def := range prov.ConnectionParamDefs() {
			if def.From == "" {
				info.ConnectionParams[name] = connectionParamInfo{
					Required:    def.Required,
					Description: def.Description,
					Default:     def.Default,
				}
			}
		}
		info.Connections = s.integrationConnectionInfos(name, authTypes, info.CredentialFields)
		if len(authTypes) == 0 {
			authTypes = authTypesFromConnections(info.Connections)
			if len(authTypes) == 0 {
				if _, ok := prov.(core.OAuthProvider); ok {
					authTypes = []string{"oauth"}
				}
			}
			info.Connections = s.integrationConnectionInfos(name, authTypes, info.CredentialFields)
		}
		if authTypes == nil {
			authTypes = []string{}
		}
		info.AuthTypes = authTypes
		info.MountedPath = s.integrationMountedPathForPrincipal(p, name, info.MountedPath)
		if !s.integrationHasUsableSurface(p, name, prov, info) {
			continue
		}
		out = append(out, info)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) userConnectedIntegrations(r *http.Request) (map[string][]instanceInfo, error) {
	user := UserFromContext(r.Context())
	if user == nil || user.Email == "" {
		return nil, nil
	}
	userID := UserIDFromContext(r.Context())
	if userID == "" {
		dbUser, err := s.users.FindOrCreateUser(r.Context(), user.Email)
		if err != nil {
			return nil, fmt.Errorf("resolving user: %w", err)
		}
		if dbUser == nil || dbUser.ID == "" {
			return nil, fmt.Errorf("resolving user: empty result")
		}
		userID = dbUser.ID
	}
	tokens, err := s.tokens.ListTokens(r.Context(), userID)
	if err != nil {
		return nil, fmt.Errorf("listing tokens: %w", err)
	}
	m := make(map[string][]instanceInfo, len(tokens))
	for _, tok := range tokens {
		m[tok.Integration] = append(m[tok.Integration], instanceInfo{
			Name:       tok.Instance,
			Connection: userFacingConnectionName(tok.Connection),
		})
	}
	return m, nil
}

func (s *Server) disconnectIntegration(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	auditAllowed := false
	auditErr := errors.New("connection disconnect failed")
	auditTarget := auditTarget{}
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), name, "connection.disconnect", auditAllowed, auditErr, auditTarget)
	}()

	userID, err := s.resolveUserID(w, r)
	if err != nil {
		auditErr = err
		return
	}

	if _, ok := s.getProvider(w, name); !ok {
		auditErr = errors.New("integration not found")
		return
	}

	requestedInstance := queryParamValue(r, httpInstanceParam, legacyHTTPInstanceParam)
	if requestedInstance != "" {
		var ok bool
		requestedInstance, ok = resolveRequestedInstance(w, requestedInstance)
		if !ok {
			auditErr = errors.New("invalid instance parameter")
			return
		}
	}
	requestedConnection := queryParamValue(r, httpConnectionParam, legacyHTTPConnectionParam)
	if requestedConnection != "" {
		var ok bool
		requestedConnection, ok = s.resolveRequestedConnection(w, name, requestedConnection)
		if !ok {
			auditErr = errors.New("invalid connection parameter")
			return
		}
	}
	if requestedConnection != "" && requestedInstance != "" {
		auditTarget = connectionAuditTarget(name, requestedConnection, requestedInstance)
	}

	tokens, err := s.tokens.ListTokensForIntegration(r.Context(), userID, name)
	if err != nil {
		auditErr = errors.New("failed to list tokens")
		writeError(w, http.StatusInternalServerError, "failed to list tokens")
		return
	}

	var matched []*core.IntegrationToken
	for _, tok := range tokens {
		if requestedConnection != "" && tok.Connection != requestedConnection {
			continue
		}
		matched = append(matched, tok)
	}

	if len(matched) == 0 {
		auditErr = errors.New("connection not found")
		writeError(w, http.StatusNotFound, fmt.Sprintf("no connection found for integration %q", name))
		return
	}

	if requestedInstance != "" {
		var instanceMatched []*core.IntegrationToken
		for _, tok := range matched {
			if tok.Instance == requestedInstance {
				instanceMatched = append(instanceMatched, tok)
			}
		}
		matched = instanceMatched
	}

	if len(matched) == 0 {
		auditErr = errors.New("connection instance not found")
		writeError(w, http.StatusNotFound, fmt.Sprintf("no connection found for integration %q instance %q", name, requestedInstance))
		return
	}
	if len(matched) > 1 {
		auditErr = errors.New("multiple matching connections")
		labels := make([]string, len(matched))
		for i, t := range matched {
			labels[i] = fmt.Sprintf("%s/%s", t.Connection, t.Instance)
		}
		hint := "?" + httpInstanceParam + "=NAME"
		if requestedInstance != "" {
			hint = "?" + httpConnectionParam + "=NAME"
		}
		writeError(w, http.StatusConflict, fmt.Sprintf("multiple connections exist for %q (%v); specify %s", name, labels, hint))
		return
	}

	tokenID := matched[0].ID
	auditTarget = connectionAuditTarget(name, matched[0].Connection, matched[0].Instance)
	if tokenID == "" {
		auditErr = errors.New("connection token is missing an ID")
		writeError(w, http.StatusNotFound, fmt.Sprintf("no connection found for integration %q", name))
		return
	}

	if err := s.tokens.DeleteToken(r.Context(), tokenID); err != nil {
		auditErr = errors.New("failed to disconnect integration")
		writeError(w, http.StatusInternalServerError, "failed to disconnect integration")
		return
	}

	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

func (s *Server) getProvider(w http.ResponseWriter, name string) (core.Provider, bool) {
	prov, err := s.providers.Get(name)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("integration %q not found", name))
			return nil, false
		}
		writeError(w, http.StatusInternalServerError, "failed to look up integration")
		return nil, false
	}
	return prov, true
}

func queryParamValue(r *http.Request, names ...string) string {
	for _, name := range names {
		if value := r.URL.Query().Get(name); value != "" {
			return value
		}
	}
	return ""
}

func (s *Server) requireOAuthHandler(w http.ResponseWriter, integration, connection string) (bootstrap.OAuthHandler, bool) {
	if s.connectionAuth == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("integration %q has no OAuth connections configured", integration))
		return nil, false
	}
	connMap := s.connectionAuth()[integration]
	if connMap == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("integration %q has no OAuth connections configured", integration))
		return nil, false
	}
	handler, ok := connMap[connection]
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("connection %q on integration %q does not support OAuth", connection, integration))
		return nil, false
	}
	return handler, true
}

func (s *Server) resolveRequestedConnection(w http.ResponseWriter, integration, requested string) (string, bool) {
	if requested != "" {
		if !safeParamValue.MatchString(requested) {
			writeError(w, http.StatusBadRequest, "connection name contains invalid characters")
			return "", false
		}
		return config.ResolveConnectionAlias(requested), true
	}

	connection := s.defaultConnection[integration]
	if connection == "" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("integration %q requires an explicit connection", integration))
		return "", false
	}
	return connection, true
}

func resolveRequestedInstance(w http.ResponseWriter, requested string) (string, bool) {
	instance := requested
	if instance == "" {
		instance = defaultTokenInstance
	}
	if !safeInstanceValue.MatchString(instance) {
		writeError(w, http.StatusBadRequest, "instance name contains invalid characters")
		return "", false
	}
	return instance, true
}

func resolveConnectionParams(w http.ResponseWriter, prov core.Provider, provided map[string]string) (map[string]string, bool) {
	connParams, err := validateConnectionParams(prov.ConnectionParamDefs(), provided)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	return connParams, true
}

func (s *Server) listOperations(w http.ResponseWriter, r *http.Request) {
	const operation = "operations.list"

	name := chi.URLParam(r, "name")
	prov, ok := s.getProvider(w, name)
	if !ok {
		return
	}
	p := PrincipalFromContext(r.Context())
	if !s.allowProvider(p, name) {
		s.auditHTTPEvent(r.Context(), p, name, operation, false, errOperationAccess)
		writeError(w, http.StatusForbidden, errOperationAccess.Error())
		return
	}
	selectors, ok := s.resolveRequestedSelectors(
		w,
		name,
		r.URL.Query().Get(httpConnectionParam),
		r.URL.Query().Get(httpInstanceParam),
	)
	if !ok {
		return
	}
	if err := rejectWorkloadSelectors(w, p, selectors.Connection, selectors.Instance); err != nil {
		s.auditHTTPEvent(r.Context(), p, name, operation, false, err)
		return
	}
	var resolver invocation.TokenResolver
	if tr, ok := s.invoker.(invocation.TokenResolver); ok {
		resolver = tr
	}
	recordDiscoveryMetrics := false
	discoveryStartedAt := time.Time{}
	discoveryConnectionMode := ""
	discoveryFailed := false
	if core.SupportsSessionCatalog(prov) && resolver != nil && p != nil && prov.ConnectionMode() != core.ConnectionModeNone {
		recordDiscoveryMetrics = true
		discoveryStartedAt = time.Now()
		discoveryConnectionMode = metricutil.NormalizeConnectionMode(prov.ConnectionMode())
	}
	resolveCatalog := invocation.ResolveCatalogWithMetadata
	plan := s.planSessionCatalogResolution(p, name, selectors)
	strictCatalog := plan.requiresStrictCatalog(prov)
	if strictCatalog {
		resolveCatalog = invocation.ResolveCatalogStrictWithMetadata
	}
	ctx := invocation.WithAccessContext(r.Context(), s.providerAccessContext(p, name))
	var cat *catalog.Catalog
	if strictCatalog && plan.shouldTryAllTargets(p) {
		var firstErr error
		for _, target := range plan.Targets {
			var (
				err      error
				metadata invocation.CatalogResolutionMetadata
			)
			cat, metadata, err = resolveCatalog(ctx, prov, name, resolver, p, target.Connection, target.Instance)
			if err == nil {
				discoveryFailed = metadata.SessionFailed
				break
			}
			if firstErr == nil {
				firstErr = err
			}
			cat = nil
		}
		if cat == nil && firstErr != nil {
			discoveryFailed = true
			if recordDiscoveryMetrics {
				metricutil.RecordDiscoveryMetrics(r.Context(), discoveryStartedAt, name, "list_operations", discoveryConnectionMode, discoveryFailed)
			}
			s.writeInvocationError(w, r, name, "", firstErr)
			return
		}
	} else {
		target := plan.Targets[0]
		var (
			err      error
			metadata invocation.CatalogResolutionMetadata
		)
		cat, metadata, err = resolveCatalog(ctx, prov, name, resolver, p, target.Connection, target.Instance)
		discoveryFailed = metadata.SessionFailed || err != nil
		if err != nil {
			if recordDiscoveryMetrics {
				metricutil.RecordDiscoveryMetrics(r.Context(), discoveryStartedAt, name, "list_operations", discoveryConnectionMode, discoveryFailed)
			}
			s.writeInvocationError(w, r, name, "", err)
			return
		}
	}
	if recordDiscoveryMetrics {
		metricutil.RecordDiscoveryMetrics(r.Context(), discoveryStartedAt, name, "list_operations", discoveryConnectionMode, discoveryFailed)
	}
	cat = invocation.FilterCatalogForPrincipal(cat, name, p, s.authorizer)
	ops := httpVisibleCatalogOperations(cat.Operations)
	sort.Slice(ops, func(i, j int) bool {
		return ops[i].ID < ops[j].ID
	})
	writeJSON(w, http.StatusOK, ops)
}

func (s *Server) executeOperation(w http.ResponseWriter, r *http.Request) {
	providerName := chi.URLParam(r, "integration")
	operationName := chi.URLParam(r, "operation")

	p := PrincipalFromContext(r.Context())
	prov, ok := s.getProvider(w, providerName)
	if !ok {
		return
	}
	access := s.providerAccessContext(p, providerName)
	providerAllowed := s.allowProvider(p, providerName)
	operationAllowed := s.allowOperation(p, providerName, operationName)
	if !providerAllowed || !operationAllowed {
		authz := auditAuthorization{
			Policy: access.Policy,
			Role:   access.Role,
		}
		if !providerAllowed {
			authz.Decision = auditDecisionProviderAccessDenied
		} else {
			authz.Decision = auditDecisionOperationBindingDenied
		}
		s.auditHTTPAuthorizationEvent(r.Context(), p, providerName, operationName, false, errOperationAccess, authz)
		writeError(w, http.StatusForbidden, errOperationAccess.Error())
		return
	}

	querySelectors, ok := s.resolveRequestedSelectors(
		w,
		providerName,
		r.URL.Query().Get(httpConnectionParam),
		r.URL.Query().Get(httpInstanceParam),
	)
	if !ok {
		return
	}
	if err := rejectWorkloadSelectors(w, p, querySelectors.Connection, querySelectors.Instance); err != nil {
		s.auditHTTPEvent(r.Context(), p, providerName, operationName, false, err)
		return
	}

	params := make(map[string]any)
	if r.Method == http.MethodPost {
		if r.Body != nil {
			defer func() { _ = r.Body.Close() }()
			if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON body")
				return
			}
		}
	} else {
		for key, values := range r.URL.Query() {
			if len(values) > 0 {
				params[key] = values[0]
			}
		}
	}

	bodyInstance, _ := params[httpInstanceParam].(string)
	delete(params, httpInstanceParam)
	bodyConnection, _ := params[httpConnectionParam].(string)
	delete(params, httpConnectionParam)

	bodySelectors, ok := s.resolveRequestedSelectors(w, providerName, bodyConnection, bodyInstance)
	if !ok {
		return
	}
	if err := rejectWorkloadSelectors(w, p, bodySelectors.Connection, bodySelectors.Instance); err != nil {
		s.auditHTTPEvent(r.Context(), p, providerName, operationName, false, err)
		return
	}
	selectors, ok := mergeResolvedSelectors(w, querySelectors, bodySelectors)
	if !ok {
		return
	}
	ctx := r.Context()
	ctx = invocation.WithAccessContext(ctx, access)

	var resolver invocation.TokenResolver
	if tr, ok := s.invoker.(invocation.TokenResolver); ok {
		resolver = tr
	}
	opMeta, ok := invocation.CatalogOperation(prov.Catalog(), operationName)
	sessionOpFound := false
	plan := s.planSessionCatalogResolution(p, providerName, selectors)
	if core.SupportsSessionCatalog(prov) {
		var firstSessionErr error
		sessionCatalogResolved := false
		for _, target := range plan.Targets {
			sessionCat, err := invocation.ResolveSessionCatalog(ctx, prov, providerName, resolver, p, target.Connection, target.Instance)
			if err != nil {
				if firstSessionErr == nil {
					firstSessionErr = err
				}
				if plan.ExplicitConnection {
					s.writeInvocationError(w, r, providerName, operationName, err)
					return
				}
				continue
			}
			sessionCatalogResolved = true
			if sessionOp, sessionOK := invocation.CatalogOperation(sessionCat, operationName); sessionOK {
				opMeta, ok = sessionOp, true
				sessionOpFound = true
				if !plan.ExplicitConnection {
					selectors.Connection = target.Connection
				}
				break
			}
		}
		if firstSessionErr != nil && !sessionCatalogResolved {
			s.writeInvocationError(w, r, providerName, operationName, firstSessionErr)
			return
		}
		if plan.ExplicitInstance && !sessionOpFound {
			s.writeInvocationError(w, r, providerName, operationName, fmt.Errorf("%w: %q on provider %q", invocation.ErrOperationNotFound, operationName, providerName))
			return
		}
	}
	if !ok {
		s.writeInvocationError(w, r, providerName, operationName, fmt.Errorf("%w: %q on provider %q", invocation.ErrOperationNotFound, operationName, providerName))
		return
	}
	if s.authorizer != nil && !s.authorizer.AllowCatalogOperation(p, providerName, opMeta) {
		s.auditHTTPAuthorizationEvent(ctx, p, providerName, operationName, false, errOperationAccess, auditAuthorization{
			Policy:   access.Policy,
			Role:     access.Role,
			Decision: auditDecisionCatalogRoleDenied,
		})
		writeError(w, http.StatusForbidden, "operation access denied")
		return
	}
	ctx = invocation.WithCatalogOperation(ctx, providerName, opMeta)
	if selectors.Connection != "" {
		if !safeParamValue.MatchString(selectors.Connection) {
			writeError(w, http.StatusBadRequest, "connection name contains invalid characters")
			return
		}
		selectors.Connection = config.ResolveConnectionAlias(selectors.Connection)
		ctx = invocation.WithConnection(ctx, selectors.Connection)
	}
	ctx = invocation.WithInvocationSurface(ctx, invocation.InvocationSurfaceHTTP)

	result, err := s.invoker.Invoke(ctx, p, providerName, selectors.Instance, operationName, params)
	if err != nil {
		s.writeInvocationError(w, r, providerName, operationName, err)
		return
	}

	writeOperationResult(w, result)
}

func (s *Server) writeInvocationError(w http.ResponseWriter, r *http.Request, providerName, operationName string, err error) {
	var upstreamErr *apiexec.UpstreamHTTPError
	switch {
	case errors.Is(err, invocation.ErrProviderNotFound):
		writeError(w, http.StatusNotFound, fmt.Sprintf("integration %q not found", providerName))
	case errors.Is(err, invocation.ErrOperationNotFound):
		writeError(w, http.StatusNotFound, fmt.Sprintf("operation %q not found on integration %q", operationName, providerName))
	case errors.Is(err, invocation.ErrNotAuthenticated):
		writeError(w, http.StatusUnauthorized, "not authenticated")
	case errors.Is(err, invocation.ErrAuthorizationDenied):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, invocation.ErrScopeDenied):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, invocation.ErrNoToken):
		writeError(w, http.StatusPreconditionFailed, fmt.Sprintf("no token stored for integration %q; connect via OAuth first", providerName))
	case errors.Is(err, invocation.ErrAmbiguousInstance):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, invocation.ErrUserResolution):
		writeError(w, http.StatusInternalServerError, "failed to resolve user")
	case errors.Is(err, invocation.ErrInternal):
		writeError(w, http.StatusInternalServerError, "internal error")
	case errors.Is(err, core.ErrMCPOnly):
		writeError(w, http.StatusBadRequest, "this integration is accessible only via MCP")
	case errors.Is(err, apiexec.ErrMissingPathParam):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.As(err, &upstreamErr):
		writeOperationResult(w, &core.OperationResult{
			Status:  upstreamErr.Status,
			Headers: upstreamErr.Headers,
			Body:    upstreamErr.Body,
		})
	default:
		if message, ok := safeOperationErrorMessage(err); ok {
			slog.WarnContext(
				r.Context(),
				"operation failed with user-facing error",
				"provider",
				providerName,
				"operation",
				operationName,
				"error",
				err,
			)
			writeError(w, http.StatusBadGateway, message)
			return
		}
		slog.ErrorContext(r.Context(), "operation failed", "provider", providerName, "operation", operationName, "error", err)
		writeError(w, http.StatusBadGateway, "operation failed")
	}
}

func (s *Server) catalogLookupConnection(providerName, explicit string) string {
	if explicit != "" {
		return config.ResolveConnectionAlias(explicit)
	}
	if conn := s.catalogConnection[providerName]; conn != "" {
		return conn
	}
	if broker, ok := s.invoker.(interface{ MCPConnection(string) string }); ok {
		if conn := broker.MCPConnection(providerName); conn != "" {
			return conn
		}
	}
	return s.defaultConnection[providerName]
}

func (s *Server) sessionCatalogConnections(providerName string, p *principal.Principal, explicit string) []string {
	if explicit != "" {
		return []string{config.ResolveConnectionAlias(explicit)}
	}
	if s.authorizer != nil && s.authorizer.IsWorkload(p) {
		return []string{""}
	}

	connections := make([]string, 0, 2)
	if conn := s.catalogLookupConnection(providerName, ""); conn != "" {
		connections = append(connections, conn)
	}
	if conn := s.defaultConnection[providerName]; conn != "" && (len(connections) == 0 || connections[0] != conn) && s.catalogConnection[providerName] == "" {
		connections = append(connections, conn)
	}
	if len(connections) == 0 {
		return []string{""}
	}
	return connections
}

func httpVisibleCatalogOperations(ops []catalog.CatalogOperation) []catalog.CatalogOperation {
	filtered := make([]catalog.CatalogOperation, 0, len(ops))
	for i := range ops {
		op := ops[i]
		if catalogOperationTransport(op) == catalog.TransportMCPPassthrough {
			continue
		}
		filtered = append(filtered, op)
	}
	return filtered
}

func catalogOperationTransport(op catalog.CatalogOperation) string {
	transport := strings.TrimSpace(op.Transport)
	if transport == "" && strings.TrimSpace(op.Method) != "" {
		return catalog.TransportREST
	}
	return transport
}

func safeOperationErrorMessage(err error) (string, bool) {
	if errors.Is(err, apiexec.ErrUpstreamTimedOut) {
		return "upstream service timed out", true
	}

	if errors.Is(err, apiexec.ErrUpstreamUnavailable) {
		return "failed to reach upstream service", true
	}

	if errors.Is(err, apiexec.ErrUpstreamResponseRead) {
		return "failed to read upstream response", true
	}

	if errors.Is(err, apiexec.ErrUpstreamInvalidResponse) {
		return "upstream service returned an invalid response", true
	}

	var operationErr *apiexec.UpstreamOperationError
	if errors.As(err, &operationErr) {
		return operationErr.Error(), true
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return "operation timed out", true
	}

	status, ok := grpcstatus.FromError(err)
	if !ok {
		return "", false
	}

	switch status.Code() {
	case codes.DeadlineExceeded:
		return "operation timed out", true
	case codes.Unavailable:
		return "integration unavailable", true
	default:
		return "", false
	}
}
