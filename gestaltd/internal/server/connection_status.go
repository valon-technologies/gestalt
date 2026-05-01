package server

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

const (
	connectionStatusReady                   = "ready"
	connectionStatusDegraded                = "degraded"
	connectionStatusNeedsUserConnection     = "needs_user_connection"
	connectionStatusNeedsInstanceSelection  = "needs_instance_selection"
	connectionStatusNeedsAdminConfiguration = "needs_admin_configuration"
	connectionStatusUnavailable             = "unavailable"
	connectionStatusUnknown                 = "unknown"

	credentialStateNotRequired = "not_required"
	credentialStateConnected   = "connected"
	credentialStateConfigured  = "configured"
	credentialStateMissing     = "missing"
	credentialStateInvalid     = "invalid"
	credentialStateUnknown     = "unknown"

	healthStateHealthy       = "healthy"
	healthStateUnhealthy     = "unhealthy"
	healthStateNotChecked    = "not_checked"
	healthStateNotApplicable = "not_applicable"
	healthStateUnknown       = "unknown"

	actionConnect        = "connect"
	actionDisconnect     = "disconnect"
	actionAddInstance    = "add_instance"
	actionSelectInstance = "select_instance"
	actionReconnect      = "reconnect"
	actionAdminConfigure = "admin_configure"

	credentialModeNone     = "none"
	credentialModeSubject  = "subject"
	credentialModePlatform = "platform"

	ownerKindNone           = "none"
	ownerKindCurrentUser    = "current_user"
	ownerKindServiceAccount = "service_account"
	ownerKindPlatform       = "platform"
	ownerKindUnknown        = "unknown"
)

func (s *Server) applyIntegrationConnectionStatus(info *integrationInfo, prov core.Provider, instances []instanceInfo, authTypes []string, p *principal.Principal) {
	status := s.defaultIntegrationStatus(info, prov, instances, authTypes, p)
	info.Status = status.Status
	info.CredentialState = status.CredentialState
	info.HealthState = status.HealthState
	info.Actions = status.Actions
}

func (s *Server) defaultIntegrationStatus(info *integrationInfo, prov core.Provider, instances []instanceInfo, authTypes []string, p *principal.Principal) connectionStatusInfo {
	if info == nil {
		return unknownConnectionStatus()
	}
	if conn, ok := info.connectionStatusForDefaultTarget(s.defaultConnectionName(info.Name)); ok {
		return statusFromConnectionInfo(conn)
	}
	if conn, ok := info.singleConnectionStatus(); ok {
		return statusFromConnectionInfo(conn)
	}
	if len(info.Connections) == 0 {
		return s.implicitIntegrationStatus(info.Name, prov, instances, authTypes, p)
	}
	return summarizeConnectionStatuses(info.Connections)
}

func (info *integrationInfo) connectionStatusForDefaultTarget(connection string) (*connectionDefInfo, bool) {
	connection = userFacingConnectionName(config.ResolveConnectionAlias(connection))
	if connection == "" {
		return nil, false
	}
	for i := range info.Connections {
		conn := &info.Connections[i]
		if config.ResolveConnectionAlias(conn.Name) == config.ResolveConnectionAlias(connection) {
			return conn, true
		}
	}
	return nil, false
}

func (info *integrationInfo) singleConnectionStatus() (*connectionDefInfo, bool) {
	if len(info.Connections) != 1 {
		return nil, false
	}
	return &info.Connections[0], true
}

func (s *Server) defaultConnectionName(integration string) string {
	if s.defaultConnection != nil {
		if connection := strings.TrimSpace(s.defaultConnection[integration]); connection != "" {
			return connection
		}
	}
	entry := s.pluginDefs[integration]
	if entry == nil {
		return ""
	}
	plan, err := config.BuildStaticConnectionPlan(entry, entry.ManifestSpec())
	if err != nil {
		return ""
	}
	return plan.AuthDefaultConnection()
}

func (s *Server) implicitIntegrationStatus(integration string, prov core.Provider, instances []instanceInfo, authTypes []string, p *principal.Principal) connectionStatusInfo {
	mode := core.ConnectionModeUser
	if prov != nil {
		mode = core.NormalizeConnectionMode(prov.ConnectionMode())
	}
	switch mode {
	case core.ConnectionModeNone:
		return connectionStatusInfo{
			Status:          connectionStatusReady,
			CredentialState: credentialStateNotRequired,
			HealthState:     healthStateNotApplicable,
			Actions:         []string{},
			Connected:       true,
		}
	case core.ConnectionModePlatform:
		if s.hasConfiguredPlatformConnection(integration) {
			return connectionStatusInfo{
				Status:          connectionStatusReady,
				CredentialState: credentialStateConfigured,
				HealthState:     healthStateNotChecked,
				Actions:         []string{},
				Connected:       true,
			}
		}
		return connectionStatusInfo{
			Status:          connectionStatusNeedsAdminConfiguration,
			CredentialState: credentialStateMissing,
			HealthState:     healthStateUnknown,
			Actions:         []string{actionAdminConfigure},
			Connected:       false,
			StatusCode:      "admin_configuration_required",
			StatusReason:    "deployment/admin configuration is required",
		}
	default:
		return subjectConnectionStatus(groupInstancesForConnection(instances, ""), len(authTypes) > 0, ownerKindForPrincipal(p))
	}
}

type connectionStatusInfo struct {
	Status          string
	CredentialState string
	HealthState     string
	Actions         []string
	CredentialMode  string
	OwnerKind       string
	Disconnectable  bool
	Connected       bool
	StatusCode      string
	StatusReason    string
}

func statusFromConnectionInfo(conn *connectionDefInfo) connectionStatusInfo {
	return connectionStatusInfo{
		Status:          conn.Status,
		CredentialState: conn.CredentialState,
		HealthState:     conn.HealthState,
		Actions:         cloneStatusActions(conn.Actions),
		CredentialMode:  conn.CredentialMode,
		OwnerKind:       conn.OwnerKind,
		Disconnectable:  conn.disconnectable,
		Connected:       conn.connected,
		StatusCode:      conn.StatusCode,
		StatusReason:    conn.StatusReason,
	}
}

func cloneStatusActions(actions []string) []string {
	if len(actions) == 0 {
		return []string{}
	}
	return append([]string(nil), actions...)
}

func summarizeConnectionStatuses(connections []connectionDefInfo) connectionStatusInfo {
	if len(connections) == 0 {
		return unknownConnectionStatus()
	}
	for i := range connections {
		conn := &connections[i]
		if conn.Status == connectionStatusNeedsInstanceSelection {
			return statusFromConnectionInfo(conn)
		}
	}
	allPlatform := true
	for i := range connections {
		conn := &connections[i]
		if conn.CredentialMode != credentialModePlatform {
			allPlatform = false
			break
		}
	}
	if allPlatform {
		for i := range connections {
			conn := &connections[i]
			if conn.Status == connectionStatusNeedsAdminConfiguration {
				return statusFromConnectionInfo(conn)
			}
		}
	}
	allReady := true
	for i := range connections {
		conn := &connections[i]
		if conn.Status != connectionStatusReady {
			allReady = false
			break
		}
	}
	if allReady {
		status := statusFromConnectionInfo(&connections[0])
		status.Actions = []string{}
		status.Connected = true
		return status
	}
	for i := range connections {
		conn := &connections[i]
		if conn.Status == connectionStatusNeedsUserConnection {
			return statusFromConnectionInfo(conn)
		}
	}
	for i := range connections {
		conn := &connections[i]
		if conn.Status == connectionStatusNeedsAdminConfiguration {
			return statusFromConnectionInfo(conn)
		}
	}
	for i := range connections {
		conn := &connections[i]
		if conn.Status == connectionStatusUnavailable {
			return statusFromConnectionInfo(conn)
		}
	}
	return unknownConnectionStatus()
}

func unknownConnectionStatus() connectionStatusInfo {
	return connectionStatusInfo{
		Status:          connectionStatusUnknown,
		CredentialState: credentialStateUnknown,
		HealthState:     healthStateUnknown,
		Actions:         []string{},
		Connected:       false,
	}
}

func noAuthConnectionStatus() connectionStatusInfo {
	return connectionStatusInfo{
		Status:          connectionStatusReady,
		CredentialState: credentialStateNotRequired,
		HealthState:     healthStateNotApplicable,
		Actions:         []string{},
		CredentialMode:  credentialModeNone,
		OwnerKind:       ownerKindNone,
		Connected:       true,
	}
}

func (s *Server) platformConnectionStatus(integration, connection string, conn config.ConnectionDef) connectionStatusInfo {
	if _, err := bootstrap.StaticConnectionRuntimeInfo(integration, connection, conn); err != nil {
		return connectionStatusInfo{
			Status:          connectionStatusNeedsAdminConfiguration,
			CredentialState: credentialStateMissing,
			HealthState:     healthStateUnknown,
			Actions:         []string{actionAdminConfigure},
			CredentialMode:  credentialModePlatform,
			OwnerKind:       ownerKindPlatform,
			Connected:       false,
			StatusCode:      "admin_configuration_required",
			StatusReason:    err.Error(),
		}
	}
	return connectionStatusInfo{
		Status:          connectionStatusReady,
		CredentialState: credentialStateConfigured,
		HealthState:     healthStateNotChecked,
		Actions:         []string{},
		CredentialMode:  credentialModePlatform,
		OwnerKind:       ownerKindPlatform,
		Connected:       true,
	}
}

func subjectConnectionStatus(instances []instanceInfo, connectable bool, ownerKind string) connectionStatusInfo {
	status := connectionStatusInfo{
		CredentialMode: credentialModeSubject,
		OwnerKind:      ownerKind,
		HealthState:    healthStateNotApplicable,
		Actions:        []string{},
		Connected:      false,
	}
	switch len(instances) {
	case 0:
		status.Status = connectionStatusNeedsUserConnection
		status.CredentialState = credentialStateMissing
		if connectable {
			status.Actions = []string{actionConnect}
		}
	case 1:
		status.Status = connectionStatusReady
		status.CredentialState = credentialStateConnected
		status.HealthState = healthStateNotChecked
		status.Disconnectable = true
		status.Connected = true
		status.Actions = subjectConnectionActions(true, connectable, false)
	default:
		status.Status = connectionStatusNeedsInstanceSelection
		status.CredentialState = credentialStateConnected
		status.HealthState = healthStateNotChecked
		status.Disconnectable = true
		status.Connected = true
		status.Actions = subjectConnectionActions(true, connectable, true)
		status.StatusCode = "instance_selection_required"
		status.StatusReason = "multiple connected instances require explicit instance selection"
	}
	return status
}

func subjectConnectionActions(disconnectable, connectable, selectInstance bool) []string {
	var actions []string
	if selectInstance {
		actions = append(actions, actionSelectInstance)
	}
	if disconnectable {
		actions = append(actions, actionDisconnect)
	}
	if connectable {
		actions = append(actions, actionAddInstance)
	}
	return actions
}

func ownerKindForPrincipal(p *principal.Principal) string {
	subjectID := strings.TrimSpace(principal.EffectiveCredentialSubjectID(p))
	if subjectID == "" {
		return ownerKindUnknown
	}
	kind, _, ok := core.ParseSubjectID(subjectID)
	if !ok {
		return ownerKindUnknown
	}
	switch kind {
	case string(principal.KindUser):
		return ownerKindCurrentUser
	case ownerKindServiceAccount:
		return ownerKindServiceAccount
	default:
		return ownerKindUnknown
	}
}

func groupInstancesForConnection(instances []instanceInfo, connection string) []instanceInfo {
	connection = userFacingConnectionName(config.ResolveConnectionAlias(connection))
	out := make([]instanceInfo, 0, len(instances))
	for _, instance := range instances {
		if connection != "" && config.ResolveConnectionAlias(instance.Connection) != config.ResolveConnectionAlias(connection) {
			continue
		}
		out = append(out, instance)
	}
	return out
}
