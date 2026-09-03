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
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/apiexec"
	"github.com/valon-technologies/gestalt/server/services/apps/mcphttp"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const defaultTokenInstance = "default"
const httpInstanceParam = "_instance"
const httpConnectionParam = "_connection"

const cliStatePrefix = "cli:"
const maxPort = 65535

const sessionCookieName = "session_token"
const defaultSessionCookieTTL = 24 * time.Hour

var (
	errNotAuthenticated = errors.New("not authenticated")
	errResolveUser      = errors.New("failed to resolve user")
	errUserRequired     = errors.New("user caller is required on this route")
)

var (
	safeParamValue         = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	safeTokenResponseValue = regexp.MustCompile(`^[a-zA-Z0-9._:/-]+$`)
)

type instanceInfo struct {
	Name       string           `json:"name"`
	Connection string           `json:"connection,omitempty"`
	Preferred  bool             `json:"preferred,omitempty"`
	Identity   *accountIdentity `json:"identity,omitempty"`
	AccountKey string           `json:"accountKey,omitempty"`

	credentialInvalid bool
	credentialID      string
	credentialCreated time.Time
}

type credentialFieldInfo struct {
	Name        string `json:"name"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
}

type connectionDefInfo struct {
	DisplayName       string                         `json:"displayName,omitempty"`
	Name              string                         `json:"name"`
	Mode              string                         `json:"mode,omitempty"`
	AuthTypes         []string                       `json:"authTypes"`
	ConnectionParams  map[string]connectionParamInfo `json:"connectionParams,omitempty"`
	CredentialFields  []credentialFieldInfo          `json:"credentialFields"`
	Status            string                         `json:"status"`
	CredentialState   string                         `json:"credentialState"`
	HealthState       string                         `json:"healthState"`
	Actions           []string                       `json:"actions"`
	CredentialMode    string                         `json:"credentialMode"`
	OwnerKind         string                         `json:"ownerKind"`
	Instances         []instanceInfo                 `json:"instances"`
	PreferredInstance string                         `json:"preferredInstance,omitempty"`
	StatusCode        string                         `json:"statusCode,omitempty"`
	StatusReason      string                         `json:"statusReason,omitempty"`
	// Connected is true only when a chosen account exists for this connection
	// (valid preferred instance, or a single valid instance). Stored credentials
	// without a chosen account leave Connected false.
	Connected bool `json:"connected"`

	connectable    bool
	disconnectable bool
}

type integrationInfo struct {
	Name            string              `json:"name"`
	DisplayName     string              `json:"displayName,omitempty"`
	Description     string              `json:"description,omitempty"`
	IconSVG         string              `json:"iconSvg,omitempty"`
	MountedPath     string              `json:"mountedPath,omitempty"`
	ManagementPath  string              `json:"managementPath,omitempty"`
	Prompts         []appPromptInfo     `json:"prompts,omitempty"`
	SourceTreeURL   string              `json:"sourceTreeUrl,omitempty"`
	Connections     []connectionDefInfo `json:"connections"`
	Status          string              `json:"status"`
	CredentialState string              `json:"credentialState"`
	HealthState     string              `json:"healthState"`
	Actions         []string            `json:"actions"`
	// Connected is true only when this subject has a chosen account.
	Connected bool `json:"connected"`
}

type appPromptInfo struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type connectionParamInfo struct {
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
	Default     string `json:"default,omitempty"`
}

func (s *Server) resolveUserID(w http.ResponseWriter, r *http.Request) (string, error) {
	if err := requireUserCaller(w, PrincipalFromContext(r.Context())); err != nil {
		return "", errUserRequired
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

func (s *Server) resolveCredentialSubjectID(w http.ResponseWriter, r *http.Request, serviceAccountID string) (string, error) {
	if strings.TrimSpace(serviceAccountID) != "" {
		return s.resolveServiceAccountCredentialSubjectID(w, r, serviceAccountID)
	}
	p := PrincipalFromContext(r.Context())
	if principal.IsNonUserPrincipal(p) {
		subjectID, err := principal.ResolveCredentialSubjectID(r.Context(), s.users, p)
		if err != nil {
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

func (s *Server) resolveServiceAccountCredentialSubjectID(w http.ResponseWriter, r *http.Request, serviceAccountID string) (string, error) {
	subjectID, err := canonicalServiceAccountCredentialSubjectID(serviceAccountID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", err
	}
	if err := s.authorizeServiceAccountCredentialManagement(r.Context(), PrincipalFromContext(r.Context()), subjectID); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return "", err
	}
	return subjectID, nil
}

func canonicalServiceAccountCredentialSubjectID(serviceAccountID string) (string, error) {
	value := strings.TrimSpace(serviceAccountID)
	if !strings.Contains(value, ":") {
		value = "service_account:" + value
	}
	subjectID, err := canonicalServiceAccountSubjectID(value)
	if err != nil {
		return "", fmt.Errorf("serviceAccountId must be a service account id or canonical service_account:<id> subject ID")
	}
	return subjectID, nil
}

func (s *Server) authorizeServiceAccountCredentialManagement(ctx context.Context, p *principal.Principal, serviceAccountSubjectID string) error {
	if s == nil || s.authorization == nil {
		return fmt.Errorf("authorization provider is required")
	}
	subjectID, err := principal.ResolveCredentialSubjectID(ctx, s.users, p)
	if err != nil {
		return fmt.Errorf("not authenticated")
	}
	allowed, err := invocation.CheckSubjectAccess(ctx, s.authorization, invocation.SubjectAccessRequest(subjectID, "manages", &proto.Resource{
		Type: "service_account",
		Id:   serviceAccountSubjectID,
	}))
	if err != nil {
		return fmt.Errorf("service account credential management denied: %w", err)
	}
	if !allowed {
		return fmt.Errorf("service account credential management denied")
	}
	return nil
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
	dir, err := s.assembleAppDirectory(r)
	if err != nil {
		writeAppListingError(w, r, err)
		return
	}
	out, err := s.projectComposedAppListing(r, dir)
	if err != nil {
		writeAppListingError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) integrationManagementPath(ctx context.Context, p *principal.Principal, appName string) string {
	if s == nil || s.authorization == nil || p == nil || principal.IsNonUserPrincipal(p) {
		return ""
	}
	if _, ok := s.registryApp(appName); !ok {
		return ""
	}
	subjectID, err := principal.ResolveAuthorizationSubjectID(ctx, s.credentialUserResolver(), p)
	if err != nil || strings.TrimSpace(subjectID) == "" {
		return ""
	}
	allowed, err := s.hasExplicitAppAdmin(ctx, strings.TrimSpace(subjectID), appName)
	if err != nil || !allowed {
		return ""
	}
	return "/apps/" + appName + "/admin"
}

func (s *Server) subjectConnectedIntegrations(r *http.Request) (map[string][]instanceInfo, error) {
	p := PrincipalFromContext(r.Context())
	// Ingress already canonicalized p; read the subject directly to avoid
	// re-querying the user store and surfacing duplicate-email errors.
	if canon := principal.Canonicalized(p); canon != nil {
		if subjectID := strings.TrimSpace(canon.SubjectID); subjectID != "" {
			return s.connectedIntegrationsForSubject(r.Context(), subjectID)
		}
	}
	if principal.IsNonUserPrincipal(p) {
		return nil, nil
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
	tokens, err := s.externalCredentials.ListCredentials(ctx, subjectID, "")
	if err != nil {
		return nil, fmt.Errorf("listing external credentials: %w", err)
	}
	now := time.Now()
	m := make(map[string][]instanceInfo, len(tokens))
	for _, tok := range tokens {
		if tok == nil {
			continue
		}
		credentialInvalid := credentialNeedsReconnect(tok, now)
		for _, binding := range s.pluginConnectionBindingsForCredentialID(tok.Audience) {
			m[binding.App] = append(m[binding.App], instanceInfo{
				Name:              tok.Qualifier,
				Connection:        userFacingConnectionName(binding.Connection),
				Identity:          identityFromMetadataJSON(tok.MetadataJSON),
				AccountKey:        accountKeyFromMetadataJSON(tok.MetadataJSON),
				credentialInvalid: credentialInvalid,
				credentialID:      tok.ID,
				credentialCreated: tok.CreatedAt,
			})
		}
	}
	return m, nil
}

func credentialNeedsReconnect(credential *core.ExternalCredential, now time.Time) bool {
	if credential == nil || credential.Grant == nil || credential.Grant.ExpiresAt == nil || credential.Grant.RefreshErrorCount <= 0 {
		return false
	}
	return !credential.Grant.ExpiresAt.After(now)
}

type pluginConnectionBinding struct {
	App        string
	Connection string
}

func (s *Server) pluginConnectionBindingsForCredentialID(connectionID string) []pluginConnectionBinding {
	connectionID = strings.TrimSpace(connectionID)
	if connectionID == "" || len(s.pluginDefs) == 0 {
		return nil
	}
	bindings := make([]pluginConnectionBinding, 0, 1)
	for pluginName, entry := range s.pluginDefs {
		if entry == nil {
			continue
		}
		var manifestSpec *providermanifestv1.Spec
		if entry.ResolvedManifest != nil {
			manifestSpec = entry.ResolvedManifest.Spec
		}
		plan, err := config.BuildStaticConnectionPlan(entry, manifestSpec)
		if err != nil {
			continue
		}
		add := func(connection string, conn config.ConnectionDef) {
			if serverCredentialConnectionID(pluginName, connection, conn) != connectionID {
				return
			}
			bindings = append(bindings, pluginConnectionBinding{App: pluginName, Connection: connection})
		}
		add(config.AppConnectionName, plan.AppConnection())
		for _, connection := range plan.NamedConnectionNames() {
			conn, ok := plan.NamedConnectionDef(connection)
			if ok {
				add(connection, conn)
			}
		}
	}
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].App != bindings[j].App {
			return bindings[i].App < bindings[j].App
		}
		return bindings[i].Connection < bindings[j].Connection
	})
	return bindings
}

func serverCredentialConnectionID(pluginName, connection string, conn config.ConnectionDef) string {
	if connectionID := strings.TrimSpace(conn.ConnectionID); connectionID != "" {
		return connectionID
	}
	connection = strings.TrimSpace(connection)
	if connection == "" {
		connection = config.AppConnectionName
	}
	return strings.TrimSpace(pluginName) + ":" + connection
}

func (s *Server) disconnectIntegration(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	auditAllowed := false
	auditErr := errors.New("connection disconnect failed")
	auditTarget := auditTarget{}
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), name, "connection.disconnect", auditAllowed, auditErr, auditTarget)
	}()

	subjectID, err := s.resolveCredentialSubjectID(w, r, "")
	if err != nil {
		auditErr = err
		return
	}

	if _, ok := s.getProvider(r.Context(), w, name); !ok {
		auditErr = errors.New("integration not found")
		return
	}
	query := r.URL.Query()
	for param := range query {
		switch param {
		case httpConnectionParam, httpInstanceParam:
		default:
			auditErr = fmt.Errorf("unsupported query parameter %q", param)
			writeError(w, http.StatusBadRequest, auditErr.Error())
			return
		}
	}

	requestedInstance := query.Get(httpInstanceParam)
	if requestedInstance != "" {
		var ok bool
		requestedInstance, ok = resolveRequestedInstance(w, requestedInstance)
		if !ok {
			auditErr = errors.New("invalid instance parameter")
			return
		}
	}
	requestedConnection := query.Get(httpConnectionParam)
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

	tokens, err := s.externalCredentials.ListCredentials(r.Context(), subjectID, "")
	if err != nil {
		auditErr = errors.New("failed to list external credentials")
		writeError(w, http.StatusInternalServerError, "failed to list external credentials")
		return
	}

	type matchedCredential struct {
		credential *core.ExternalCredential
		connection string
	}
	var matched []matchedCredential
	for _, tok := range tokens {
		if tok == nil {
			continue
		}
		for _, binding := range s.pluginConnectionBindingsForCredentialID(tok.Audience) {
			if binding.App != name {
				continue
			}
			if requestedConnection != "" && binding.Connection != requestedConnection {
				continue
			}
			matched = append(matched, matchedCredential{credential: tok, connection: binding.Connection})
		}
	}

	if len(matched) == 0 {
		auditErr = errors.New("connection not found")
		writeError(w, http.StatusNotFound, fmt.Sprintf("no connection found for integration %q", name))
		return
	}

	if requestedInstance != "" {
		var instanceMatched []matchedCredential
		for _, tok := range matched {
			if tok.credential.Qualifier == requestedInstance {
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
			labels[i] = fmt.Sprintf("%s/%s", t.connection, t.credential.Qualifier)
		}
		hint := "?" + httpInstanceParam + "=NAME"
		if requestedInstance != "" {
			hint = "?" + httpConnectionParam + "=NAME"
		}
		writeError(w, http.StatusConflict, fmt.Sprintf("multiple connections exist for %q (%v); specify %s", name, labels, hint))
		return
	}

	tokenID := matched[0].credential.ID
	auditTarget = connectionAuditTarget(name, matched[0].connection, matched[0].credential.Qualifier)
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

	disconnected := matched[0]
	if s.connectionInstancePreferences != nil {
		connectionID := serverCredentialConnectionID(name, disconnected.connection, s.effectiveConnectionDefOrEmpty(name, disconnected.connection))
		if pref, err := s.connectionInstancePreferences.Get(r.Context(), subjectID, connectionID); err == nil && pref != nil && pref.Instance == disconnected.credential.Qualifier {
			if err := s.connectionInstancePreferences.Delete(r.Context(), subjectID, connectionID); err != nil {
				slog.WarnContext(r.Context(), "failed to clear preferred instance after disconnect", "integration", name, "connection", disconnected.connection, "error", err)
			}
		}
	}

	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

func (s *Server) getProvider(ctx context.Context, w http.ResponseWriter, name string) (core.Provider, bool) {
	prov, err := s.providers.GetWithContext(ctx, name)
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

func resolveConnectionParams(w http.ResponseWriter, defs map[string]core.ConnectionParamDef, provided map[string]string) (map[string]string, bool) {
	connParams, err := validateConnectionParams(defs, provided)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	return connParams, true
}

func (s *Server) listOperations(w http.ResponseWriter, r *http.Request) {
	const operation = "operations.list"

	name := chi.URLParam(r, "name")
	prov, ok := s.getProvider(r.Context(), w, name)
	if !ok {
		return
	}
	metricutil.AddHTTPServerMetricDims(r.Context(), metricutil.HTTPMetricDims{
		ProviderName:   name,
		OperationName:  operation,
		ConnectionMode: metricutil.NormalizeConnectionMode(prov.ConnectionMode()),
		Surface:        metricutil.InvocationSurfaceHTTP,
	})
	p := PrincipalFromContext(r.Context())
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
	ctx := core.WithCatalogSurface(r.Context(), core.CatalogSurfaceAPI)
	cat, metadata, err := invocation.ResolveCatalogForTargetsWithMetadata(
		ctx,
		prov,
		name,
		resolver,
		p,
		s.catalogSelectorConfig().APICatalogTargets(name, requestedConnection, requestedInstance),
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
	cat, err = invocation.FilterCatalogForPrincipal(ctx, cat, name, p, s.operationAccess)
	if err != nil {
		// Never answer "no operations" because the evaluator was unreachable.
		slog.ErrorContext(ctx, "listing operations", "app", name, "error", err)
		writeError(w, http.StatusServiceUnavailable, "failed to authorize app access")
		return
	}
	ops := s.publicHTTPOperations(name, prov, cat.Operations)
	sort.Slice(ops, func(i, j int) bool {
		return ops[i].ID < ops[j].ID
	})
	writeJSON(w, http.StatusOK, ops)
}

func (s *Server) executeOperation(w http.ResponseWriter, r *http.Request) {
	providerName := chi.URLParam(r, "integration")
	operationName := chi.URLParam(r, "operation")

	p := PrincipalFromContext(r.Context())
	prov, ok := s.getProvider(r.Context(), w, providerName)
	if !ok {
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

	var resolver invocation.TokenResolver
	if tr, ok := s.invoker.(invocation.TokenResolver); ok {
		resolver = tr
	}
	if visible, ok := staticCatalogOperationVisibleByDefault(prov, operationName); ok && !visible {
		s.writeInvocationError(w, r, providerName, operationName, invocation.ErrOperationNotFound)
		return
	}
	sessionConnections := s.catalogSelectorConfig().SessionCatalogConnections(providerName, connection)
	opMeta, transport, resolvedConnection, err := invocation.ResolveOperation(ctx, prov, providerName, resolver, p, operationName, sessionConnections, instance)
	if err != nil {
		s.writeInvocationError(w, r, providerName, operationName, err)
		return
	}
	if !catalog.OperationVisibleByDefault(opMeta) {
		s.writeInvocationError(w, r, providerName, operationName, invocation.ErrOperationNotFound)
		return
	}
	if err := mcphttp.ValidateHTTPInvocation(transport, opMeta, r.Method); err != nil {
		if mcphttp.IsMethodNotAllowed(err) {
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, http.StatusMethodNotAllowed, err.Error())
			return
		}
		s.writeInvocationError(w, r, providerName, operationName, invocation.ErrOperationNotFound)
		return
	}
	if err := s.validatePublicOperationInvocation(providerName, prov, opMeta, params, connection); err != nil {
		s.writeInvocationError(w, r, providerName, operationName, err)
		return
	}
	operationConnection := resolvedConnection
	if operationConnection == "" {
		operationConnection, err = invocation.ResolveOperationConnection(prov, opMeta.ID, params)
		if err != nil {
			s.writeInvocationError(w, r, providerName, operationName, err)
			return
		}
	}
	explicitConnection := connectionInput
	if explicitConnection != "" {
		if !safeParamValue.MatchString(explicitConnection) {
			writeError(w, http.StatusBadRequest, "connection name contains invalid characters")
			return
		}
		if operationConnection := config.ResolveConnectionAlias(operationConnection); operationConnection != "" && operationConnection != connection && !invocation.OperationConnectionOverrideAllowed(prov, opMeta.ID, params) {
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
		connection = operationConnection
	}
	if connection != "" {
		if !safeParamValue.MatchString(connection) {
			writeError(w, http.StatusBadRequest, "connection name contains invalid characters")
			return
		}
		connection = config.ResolveConnectionAlias(connection)
		ctx = invocation.WithConnection(ctx, connection)
	}
	metricutil.AddHTTPServerMetricDims(r.Context(), metricutil.HTTPMetricDims{
		ProviderName:  providerName,
		OperationName: opMeta.ID,
	})
	ctx = invocation.WithInvocationSurface(ctx, invocation.InvocationSurfaceHTTP)
	ctx = invocation.WithEntry(ctx, invocation.EntryHTTP)

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
		s.persistReconnectRequiredGrant(r, providerName, err)
		writeTypedError(
			w,
			http.StatusPreconditionFailed,
			"reconnect_required",
			providerName,
			fmt.Sprintf("OAuth token for integration %q expired or was revoked; reconnect it", providerName),
		)
	case errors.Is(err, invocation.ErrAmbiguousInstance):
		writeTypedError(
			w,
			http.StatusConflict,
			"instance_selection_required",
			providerName,
			err.Error(),
		)
	case errors.Is(err, invocation.ErrUserResolution):
		writeError(w, http.StatusInternalServerError, "failed to resolve user")
	case errors.Is(err, invocation.ErrInternal):
		writeError(w, http.StatusInternalServerError, "internal error")
	case errors.Is(err, invocation.ErrInvalidInvocation):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, core.ErrProviderActivating):
		writeTypedError(
			w,
			http.StatusServiceUnavailable,
			"provider_activating",
			providerName,
			fmt.Sprintf("integration %q is still activating; retry shortly", providerName),
		)
	case errors.Is(err, core.ErrMCPOnly):
		writeError(w, http.StatusBadRequest, "this integration is accessible only via MCP")
	case errors.Is(err, apiexec.ErrMissingPathParam):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.As(err, &upstreamErr):
		if upstreamErr.Status == http.StatusUnauthorized {
			s.persistReconnectRequiredGrant(r, providerName, err)
			writeTypedError(
				w,
				http.StatusPreconditionFailed,
				"reconnect_required",
				providerName,
				fmt.Sprintf("stored credential for integration %q was rejected by upstream; reconnect it", providerName),
			)
			return
		}
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
