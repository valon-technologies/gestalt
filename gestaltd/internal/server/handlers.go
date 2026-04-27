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
)

var (
	safeParamValue         = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
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
	if err := rejectWorkloadCaller(w, PrincipalFromContext(r.Context())); err != nil {
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

func (s *Server) resolveCredentialSubjectID(w http.ResponseWriter, r *http.Request) (string, error) {
	p := PrincipalFromContext(r.Context())
	if principal.IsWorkloadPrincipal(p) {
		subjectID := strings.TrimSpace(principal.EffectiveCredentialSubjectID(p))
		if subjectID == "" {
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return "", errNotAuthenticated
		}
		return subjectID, nil
	}
	userID, err := s.resolveUserID(w, r)
	if err != nil {
		return "", err
	}
	return principal.UserSubjectID(userID), nil
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
	connected, err := s.subjectConnectedIntegrations(r)
	if err != nil {
		slog.ErrorContext(r.Context(), "listing integrations", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to check integration status")
		return
	}

	names := s.providers.List()
	out := make([]integrationInfo, 0, len(names))
	for _, name := range names {
		if !s.allowProviderContext(r.Context(), p, name) {
			continue
		}
		prov, err := s.providers.Get(name)
		if err != nil {
			continue
		}
		surfaceProv := prov
		if override, ok, err := s.providerOverrideForContext(r.Context(), p, name); err != nil {
			slog.ErrorContext(r.Context(), "resolving provider dev override", "provider", name, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to resolve provider dev override")
			return
		} else if ok {
			surfaceProv = override
		}
		info := integrationInfo{
			Name:             name,
			DisplayName:      surfaceProv.DisplayName(),
			Description:      surfaceProv.Description(),
			Connected:        false,
			Instances:        []instanceInfo{},
			AuthTypes:        []string{},
			ConnectionParams: map[string]connectionParamInfo{},
			Connections:      []connectionDefInfo{},
			CredentialFields: []credentialFieldInfo{},
		}
		if cat := surfaceProv.Catalog(); cat != nil {
			info.IconSVG = cat.IconSVG
		}
		if entry, ok := s.pluginDefs[name]; ok && entry != nil {
			info.MountedPath = strings.TrimSpace(entry.MountPath)
		}
		instances := connected[name]
		info.Connected = len(instances) > 0 || core.NormalizeConnectionMode(prov.ConnectionMode()) == core.ConnectionModeNone
		info.Instances = append(make([]instanceInfo, 0, len(instances)), instances...)
		s.populateIntegrationSettings(&info, prov)
		info.MountedPath = s.integrationMountedPathForPrincipalContext(r.Context(), p, name, info.MountedPath)
		if !s.integrationHasUsableSurfaceContext(r.Context(), p, name, surfaceProv, info) {
			continue
		}
		out = append(out, info)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) subjectConnectedIntegrations(r *http.Request) (map[string][]instanceInfo, error) {
	p := PrincipalFromContext(r.Context())
	if principal.IsWorkloadPrincipal(p) {
		subjectID := strings.TrimSpace(principal.EffectiveCredentialSubjectID(p))
		if subjectID == "" {
			return nil, nil
		}
		return s.connectedIntegrationsForSubject(r.Context(), subjectID)
	}
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
	return s.connectedIntegrationsForSubject(r.Context(), principal.UserSubjectID(userID))
}

func (s *Server) connectedIntegrationsForSubject(ctx context.Context, subjectID string) (map[string][]instanceInfo, error) {
	tokens, err := s.externalCredentials.ListCredentials(ctx, subjectID)
	if err != nil {
		return nil, fmt.Errorf("listing external credentials: %w", err)
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

	subjectID, err := s.resolveCredentialSubjectID(w, r)
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

	tokens, err := s.externalCredentials.ListCredentialsForProvider(r.Context(), subjectID, name)
	if err != nil {
		auditErr = errors.New("failed to list external credentials")
		writeError(w, http.StatusInternalServerError, "failed to list external credentials")
		return
	}

	var matched []*core.ExternalCredential
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
		var instanceMatched []*core.ExternalCredential
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
		auditErr = errors.New("connection credential is missing an ID")
		writeError(w, http.StatusNotFound, fmt.Sprintf("no connection found for integration %q", name))
		return
	}

	if err := s.externalCredentials.DeleteCredential(r.Context(), tokenID); err != nil {
		auditErr = errors.New("failed to disconnect integration")
		writeError(w, http.StatusInternalServerError, "failed to disconnect integration")
		return
	}

	if err := s.unlinkStoredCredentialAuthorization(r.Context(), matched[0]); err != nil {
		if restoreErr := s.externalCredentials.RestoreCredential(r.Context(), matched[0]); restoreErr != nil {
			slog.Error("restore disconnected integration after authz unlink failure", "integration", name, "connection", matched[0].Connection, "instance", matched[0].Instance, "token_id", matched[0].ID, "error", restoreErr)
		}
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
		if !config.SafeConnectionValue(requested) {
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
	if !config.SafeInstanceValue(instance) {
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
	metricutil.AddHTTPAttributes(r.Context(),
		metricutil.AttrProvider.String(metricutil.AttrValue(name)),
		metricutil.AttrOperation.String(operation),
		metricutil.AttrConnectionMode.String(metricutil.NormalizeConnectionMode(prov.ConnectionMode())),
		metricutil.AttrInvocationSurface.String("http"),
	)
	p := PrincipalFromContext(r.Context())
	if override, ok, err := s.providerOverrideForContext(r.Context(), p, name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if ok {
		prov = override
	}
	if !s.allowProviderContext(r.Context(), p, name) {
		s.auditHTTPEvent(r.Context(), p, name, operation, false, errOperationAccess)
		writeError(w, http.StatusForbidden, errOperationAccess.Error())
		return
	}
	requestedConnection := r.URL.Query().Get(httpConnectionParam)
	if requestedConnection != "" {
		var ok bool
		requestedConnection, ok = s.resolveRequestedConnection(w, name, requestedConnection)
		if !ok {
			return
		}
	}
	requestedInstance := r.URL.Query().Get(httpInstanceParam)
	if requestedInstance != "" {
		var ok bool
		requestedInstance, ok = resolveRequestedInstance(w, requestedInstance)
		if !ok {
			return
		}
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
	strictCatalog := false
	if requestedConnection != "" || requestedInstance != "" {
		strictCatalog = true
	} else if core.SupportsSessionCatalog(prov) {
		strictCatalog = true
	}
	ctx := invocation.WithAccessContext(r.Context(), s.providerAccessContextWithContext(r.Context(), p, name))
	cat, metadata, err := invocation.ResolveCatalogForTargetsWithMetadata(
		ctx,
		prov,
		name,
		resolver,
		p,
		s.catalogSelectorConfig().SessionCatalogTargets(name, requestedConnection, requestedInstance),
		strictCatalog,
	)
	discoveryFailed = metadata.SessionFailed
	if err != nil {
		if recordDiscoveryMetrics {
			metricutil.RecordDiscoveryMetrics(r.Context(), discoveryStartedAt, name, "list_operations", discoveryConnectionMode, discoveryFailed)
		}
		s.writeInvocationError(w, r, name, "", err)
		return
	}
	if recordDiscoveryMetrics {
		metricutil.RecordDiscoveryMetrics(r.Context(), discoveryStartedAt, name, "list_operations", discoveryConnectionMode, discoveryFailed)
	}
	cat = invocation.FilterCatalogForPrincipal(ctx, cat, name, p, s.authorizer)
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
	surfaceProv := prov
	if override, ok, err := s.providerOverrideForContext(r.Context(), p, providerName); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if ok {
		surfaceProv = override
	}
	access := s.providerAccessContextWithContext(r.Context(), p, providerName)
	providerAllowed := s.allowProviderContext(r.Context(), p, providerName)
	operationAllowed := s.authorizer == nil || s.authorizer.AllowOperation(r.Context(), p, providerName, operationName)
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

	requestedConnectionInput := r.URL.Query().Get(httpConnectionParam)
	requestedConnection := requestedConnectionInput
	if requestedConnection != "" {
		var ok bool
		requestedConnection, ok = s.resolveRequestedConnection(w, providerName, requestedConnection)
		if !ok {
			return
		}
	}
	requestedInstance := r.URL.Query().Get(httpInstanceParam)
	if requestedInstance != "" {
		var ok bool
		requestedInstance, ok = resolveRequestedInstance(w, requestedInstance)
		if !ok {
			return
		}
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
	bodyConnectionInput, _ := params[httpConnectionParam].(string)
	bodyConnection := bodyConnectionInput
	delete(params, httpConnectionParam)

	if bodyInstance != "" {
		var ok bool
		bodyInstance, ok = resolveRequestedInstance(w, bodyInstance)
		if !ok {
			return
		}
	}
	if requestedInstance != "" && bodyInstance != "" && requestedInstance != bodyInstance {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("conflicting instance parameter %q in query string and JSON body", httpInstanceParam))
		return
	}
	instance := bodyInstance
	if instance == "" {
		instance = requestedInstance
	}

	if bodyConnection != "" {
		var ok bool
		bodyConnection, ok = s.resolveRequestedConnection(w, providerName, bodyConnection)
		if !ok {
			return
		}
	}
	if requestedConnection != "" && bodyConnection != "" && requestedConnection != bodyConnection {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("conflicting connection parameter %q in query string and JSON body", httpConnectionParam))
		return
	}
	connection := bodyConnection
	connectionInput := bodyConnectionInput
	if connection == "" {
		connection = requestedConnection
		connectionInput = requestedConnectionInput
	}
	ctx := r.Context()
	ctx = invocation.WithAccessContext(ctx, access)

	var resolver invocation.TokenResolver
	if tr, ok := s.invoker.(invocation.TokenResolver); ok {
		resolver = tr
	}
	if visible, ok := staticCatalogOperationVisibleByDefault(surfaceProv, operationName); ok && !visible {
		s.writeInvocationError(w, r, providerName, operationName, invocation.ErrOperationNotFound)
		return
	}
	sessionConnections := s.catalogSelectorConfig().SessionCatalogConnections(providerName, connection)
	opMeta, _, resolvedConnection, err := invocation.ResolveOperation(ctx, surfaceProv, providerName, resolver, p, operationName, sessionConnections, instance)
	if err != nil {
		s.writeInvocationError(w, r, providerName, operationName, err)
		return
	}
	if !catalog.OperationVisibleByDefault(opMeta) {
		s.writeInvocationError(w, r, providerName, operationName, invocation.ErrOperationNotFound)
		return
	}
	metricutil.AddHTTPAttributes(r.Context(),
		metricutil.AttrProvider.String(metricutil.AttrValue(providerName)),
		metricutil.AttrOperation.String(metricutil.AttrValue(opMeta.ID)),
		metricutil.AttrConnectionMode.String(metricutil.NormalizeConnectionMode(prov.ConnectionMode())),
		metricutil.AttrInvocationSurface.String("http"),
	)
	if s.authorizer != nil && !s.authorizer.AllowCatalogOperation(ctx, p, providerName, opMeta) {
		s.auditHTTPAuthorizationEvent(ctx, p, providerName, operationName, false, errOperationAccess, auditAuthorization{
			Policy:   access.Policy,
			Role:     access.Role,
			Decision: auditDecisionCatalogRoleDenied,
		})
		writeError(w, http.StatusForbidden, "operation access denied")
		return
	}
	explicitConnection := connectionInput
	if explicitConnection != "" {
		if !safeParamValue.MatchString(explicitConnection) {
			writeError(w, http.StatusBadRequest, "connection name contains invalid characters")
			return
		}
		if operationConnection := config.ResolveConnectionAlias(prov.ConnectionForOperation(opMeta.ID)); operationConnection != "" && operationConnection != connection {
			writeError(
				w,
				http.StatusBadRequest,
				fmt.Sprintf(
					"operation %q on integration %q uses connection %q; omit the connection override or use that connection instead of %q",
					opMeta.ID,
					providerName,
					operationConnection,
					explicitConnection,
				),
			)
			return
		}
	}
	ctx = invocation.WithCatalogOperation(ctx, providerName, opMeta)
	if connection == "" {
		connection = resolvedConnection
	}
	if connection != "" {
		if !safeParamValue.MatchString(connection) {
			writeError(w, http.StatusBadRequest, "connection name contains invalid characters")
			return
		}
		connection = config.ResolveConnectionAlias(connection)
		ctx = invocation.WithConnection(ctx, connection)
	}
	ctx = invocation.WithInvocationSurface(ctx, invocation.InvocationSurfaceHTTP)

	result, err := s.invoker.Invoke(ctx, p, providerName, instance, operationName, params)
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
	case errors.Is(err, invocation.ErrNoCredential):
		writeTypedError(
			w,
			http.StatusPreconditionFailed,
			"not_connected",
			providerName,
			fmt.Sprintf("no external credential stored for integration %q; connect via OAuth first", providerName),
		)
	case errors.Is(err, invocation.ErrReconnectRequired):
		writeTypedError(
			w,
			http.StatusPreconditionFailed,
			"reconnect_required",
			providerName,
			fmt.Sprintf("OAuth token for integration %q expired or was revoked; reconnect it", providerName),
		)
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

func httpVisibleCatalogOperations(ops []catalog.CatalogOperation) []catalog.CatalogOperation {
	filtered := make([]catalog.CatalogOperation, 0, len(ops))
	for i := range ops {
		op := ops[i]
		if !catalog.OperationVisibleByDefault(op) {
			continue
		}
		if invocation.OperationTransport(op) == catalog.TransportMCPPassthrough {
			continue
		}
		filtered = append(filtered, op)
	}
	return filtered
}

func staticCatalogOperationVisibleByDefault(prov core.Provider, operation string) (bool, bool) {
	if prov == nil {
		return true, false
	}
	op, ok := invocation.CatalogOperation(prov.Catalog(), operation)
	if !ok {
		return true, false
	}
	return catalog.OperationVisibleByDefault(op), true
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
