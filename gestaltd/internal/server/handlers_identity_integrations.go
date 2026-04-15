package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
)

func (s *Server) listManagedIdentityIntegrations(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveManagedIdentityActor(w, r, managedIdentityRoleViewer)
	if !ok {
		return
	}

	connected, err := s.connectedIntegrationsByOwner(r.Context(), core.IntegrationTokenOwnerKindManagedIdentity, actor.Identity.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check integration status")
		return
	}

	viewer := PrincipalFromContext(r.Context())
	names := s.providers.List()
	out := make([]integrationInfo, 0, len(names))
	for _, name := range names {
		if !s.allowProvider(viewer, name) {
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

		instances := connected[name]
		info.Connected = len(instances) > 0
		if prov.ConnectionMode() == core.ConnectionModeNone {
			info.Connected = true
		}
		visibleInstances := make([]instanceInfo, 0, len(instances))
		for _, instance := range instances {
			if !s.managedIdentityConnectionVisible(name, instance.Connection) {
				continue
			}
			visibleInstances = append(visibleInstances, instance)
		}
		info.Instances = append(make([]instanceInfo, 0, len(visibleInstances)), visibleInstances...)
		s.populateIntegrationSettings(&info, prov)
		definedConnections := len(info.Connections) > 0
		info.Connections = s.filterManagedIdentityConnectableConnections(name, info.Connections)
		info.Connections = managedIdentityConnectableConnections(info.Connections)
		info.AuthTypes = s.managedIdentitySupportedAuthTypes(name, prov, definedConnections, info.Connections)
		info.CredentialFields = credentialFieldInfosFromProvider(prov, info.AuthTypes)
		if info.AuthTypes == nil {
			info.AuthTypes = []string{}
		}
		info.MountedPath = s.integrationMountedPathForPrincipal(viewer, name, info.MountedPath)
		if !s.integrationHasUsableSurface(viewer, name, prov, info) {
			continue
		}
		out = append(out, info)
	}

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) connectedIntegrationsByOwner(ctx context.Context, ownerKind, ownerID string) (map[string][]instanceInfo, error) {
	tokens, err := s.tokens.ListTokensByOwner(ctx, ownerKind, ownerID)
	if err != nil {
		return nil, fmt.Errorf("listing tokens: %w", err)
	}
	connected := make(map[string][]instanceInfo, len(tokens))
	for _, tok := range tokens {
		connection := tok.Connection
		if connection == "" {
			connection = config.PluginConnectionName
		}
		connected[tok.Integration] = append(connected[tok.Integration], instanceInfo{
			Name:       tok.Instance,
			Connection: userFacingConnectionName(connection),
		})
	}
	return connected, nil
}

func (s *Server) filterManagedIdentityConnectableConnections(integration string, connections []connectionDefInfo) []connectionDefInfo {
	if len(connections) == 0 {
		return nil
	}
	visible := make([]connectionDefInfo, 0, len(connections))
	for _, connection := range connections {
		if !s.managedIdentityConnectionVisible(integration, connection.Name) {
			continue
		}
		visible = append(visible, connection)
	}
	return visible
}

func (s *Server) managedIdentitySupportedAuthTypes(integration string, prov core.Provider, definedConnections bool, connections []connectionDefInfo) []string {
	if len(connections) > 0 {
		return authTypesFromConnectionInfos(connections)
	}
	if definedConnections {
		return nil
	}
	entry := s.pluginDefs[integration]
	defaultConnection := s.defaultConnection[integration]
	if defaultConnection == "" {
		defaultConnection = config.PluginConnectionName
	}
	if !s.managedIdentityConnectionVisible(integration, defaultConnection) {
		if entry != nil {
			return nil
		}
		defaultConnection = config.PluginConnectionName
	}
	baseAuthTypes := userFacingAuthTypes(prov.AuthTypes())
	authTypes := connectionAuthTypes(s.effectiveConnectionAuth(integration, defaultConnection), baseAuthTypes)
	authTypes = s.supportedConnectionAuthTypes(integration, defaultConnection, authTypes)
	if authTypes = managedIdentityConnectableAuthTypes(authTypes); len(authTypes) > 0 {
		return authTypes
	}
	if mp, ok := prov.(interface{ SupportsManualAuth() bool }); ok && mp.SupportsManualAuth() {
		return []string{"manual"}
	}
	return nil
}

func authTypesFromConnectionInfos(connections []connectionDefInfo) []string {
	if len(connections) == 0 {
		return nil
	}
	out := make([]string, 0, 2)
	for _, connection := range connections {
		for _, authType := range connection.AuthTypes {
			if authTypesContain(out, authType) {
				continue
			}
			out = append(out, authType)
		}
	}
	return out
}

func managedIdentityConnectableAuthTypes(authTypes []string) []string {
	if len(authTypes) == 0 {
		return nil
	}
	out := make([]string, 0, len(authTypes))
	for _, authType := range authTypes {
		if (authType != "manual" && authType != "oauth") || authTypesContain(out, authType) {
			continue
		}
		out = append(out, authType)
	}
	return out
}

func managedIdentityConnectableConnections(connections []connectionDefInfo) []connectionDefInfo {
	if len(connections) == 0 {
		return nil
	}
	out := make([]connectionDefInfo, 0, len(connections))
	for _, connection := range connections {
		connection.AuthTypes = managedIdentityConnectableAuthTypes(connection.AuthTypes)
		if len(connection.AuthTypes) == 0 {
			continue
		}
		out = append(out, connection)
	}
	return out
}

func (s *Server) connectManagedIdentityManual(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	auditAllowed := false
	auditErr := errors.New("identity manual connection failed")
	auditTarget := auditTarget{Kind: auditTargetKindConnection}
	providerName := ""
	metricProviderName := metricutil.UnknownAttrValue
	connectionMode := metricutil.UnknownAttrValue
	defer func() {
		metricutil.RecordConnectionAuthMetrics(r.Context(), startedAt, metricProviderName, "manual", "complete", connectionMode, auditErr != nil)
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), providerName, "identity.connection.manual.connect", auditAllowed, auditErr, auditTarget)
	}()

	actor, ok := s.resolveManagedIdentityActor(w, r, managedIdentityRoleEditor)
	if !ok {
		return
	}

	var req connectManualRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auditErr = errors.New("invalid JSON body")
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	providerName = req.Integration
	if req.Integration == "" {
		auditErr = errors.New("integration is required")
		writeError(w, http.StatusBadRequest, "integration is required")
		return
	}

	viewer := PrincipalFromContext(r.Context())
	if !s.managedIdentityGrantPluginVisible(req.Integration, viewer) {
		auditErr = errors.New("integration not found")
		writeError(w, http.StatusNotFound, fmt.Sprintf("integration %q not found", req.Integration))
		return
	}

	prov, ok := s.getProvider(w, req.Integration)
	if !ok {
		auditErr = errors.New("integration not found")
		return
	}
	metricProviderName = req.Integration
	connectionMode = metricutil.NormalizeConnectionMode(prov.ConnectionMode())

	manualConnection, ok := s.resolveRequestedConnection(w, req.Integration, req.Connection)
	if !ok {
		auditErr = errors.New("invalid connection")
		return
	}

	auth := s.effectiveConnectionAuth(req.Integration, manualConnection)
	if !manualConnectionAllowed(prov, auth) {
		auditErr = errors.New("integration does not support manual auth")
		writeError(w, http.StatusBadRequest, fmt.Sprintf("integration %q does not support manual auth; use OAuth connect instead", req.Integration))
		return
	}

	manualInstance, ok := resolveRequestedInstance(w, req.Instance)
	if !ok {
		auditErr = errors.New("invalid instance")
		return
	}
	auditTarget = connectionAuditTarget(req.Integration, manualConnection, manualInstance)

	effectiveCredential, credErr := buildEffectiveManualCredential(req, auth)
	if credErr != nil {
		auditErr = credErr
		writeError(w, http.StatusBadRequest, credErr.Error())
		return
	}
	if effectiveCredential == "" {
		auditErr = errors.New("credential is required")
		writeError(w, http.StatusBadRequest, "credential is required")
		return
	}

	connParams, ok := resolveConnectionParams(w, prov, req.ConnectionParams)
	if !ok {
		auditErr = errors.New("invalid connection parameters")
		return
	}

	manualMeta, metaErr := buildConnectionMetadata(prov, connParams, nil)
	if metaErr != nil {
		auditErr = errors.New(metaErr.Error())
		writeError(w, http.StatusBadRequest, metaErr.Error())
		return
	}

	authSource := ""
	if viewer != nil {
		authSource = viewer.AuthSource()
	}
	viewerScopes, viewerPerms := viewerCeilingForConnectionState(viewer)
	tm := tokenMaterial{
		OwnerKind:       core.IntegrationTokenOwnerKindManagedIdentity,
		OwnerID:         actor.Identity.ID,
		InitiatorUserID: actor.UserID,
		AuthSource:      authSource,
		ViewerScopes:    viewerScopes,
		ViewerPerms:     viewerPerms,
		Integration:     req.Integration,
		Connection:      manualConnection,
		Instance:        manualInstance,
		AccessToken:     effectiveCredential,
		MetadataJSON:    manualMeta,
	}

	if err := s.validateManagedIdentityConnectionWrite(r.Context(), tm); err != nil {
		if s.writeManagedIdentityConnectionWriteError(w, req.Integration, err) {
			auditErr = err
			return
		}
		auditErr = err
		writeError(w, http.StatusForbidden, err.Error())
		return
	}

	result, err := s.runPostConnect(r.Context(), prov, tm)
	if err != nil {
		if s.writeManagedIdentityConnectionWriteError(w, req.Integration, err) {
			auditErr = err
			return
		}
		auditErr = errors.New("connection setup failed")
		writeError(w, http.StatusBadGateway, "connection setup failed")
		return
	}

	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) disconnectManagedIdentityIntegration(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	auditAllowed := false
	auditErr := errors.New("identity connection disconnect failed")
	auditTarget := auditTarget{}
	defer func() {
		s.auditHTTPEventWithTarget(r.Context(), PrincipalFromContext(r.Context()), name, "identity.connection.disconnect", auditAllowed, auditErr, auditTarget)
	}()

	actor, ok := s.resolveManagedIdentityActor(w, r, managedIdentityRoleEditor)
	if !ok {
		return
	}

	viewer := PrincipalFromContext(r.Context())
	if !s.managedIdentityGrantPluginVisible(name, viewer) {
		auditErr = errors.New("integration not found")
		writeError(w, http.StatusNotFound, fmt.Sprintf("integration %q not found", name))
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

	tokens, err := s.tokens.ListTokensForIntegrationByOwner(r.Context(), core.IntegrationTokenOwnerKindManagedIdentity, actor.Identity.ID, name)
	if err != nil {
		auditErr = errors.New("failed to list tokens")
		writeError(w, http.StatusInternalServerError, "failed to list tokens")
		return
	}

	var matched []*core.IntegrationToken
	for _, tok := range tokens {
		connection := tok.Connection
		if connection == "" {
			connection = config.PluginConnectionName
		}
		if requestedConnection != "" && connection != requestedConnection {
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
