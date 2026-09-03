package server

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

const (
	connectionStatusReady                  = "ready"
	connectionStatusDegraded               = "degraded"
	connectionStatusNeedsUserConnection    = "needs_user_connection"
	connectionStatusNeedsInstanceSelection = "needs_instance_selection"
	connectionStatusUnavailable            = "unavailable"
	connectionStatusUnknown                = "unknown"

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

	credentialModeNone    = "none"
	credentialModeSubject = "subject"

	ownerKindNone           = "none"
	ownerKindCurrentUser    = "current_user"
	ownerKindServiceAccount = "service_account"
	ownerKindUnknown        = "unknown"
)

func (s *Server) applyIntegrationConnectionStatus(info *integrationInfo, prov core.Provider, instances []instanceInfo, authTypes []string, p *principal.Principal) {
	status := s.defaultIntegrationStatus(info, prov, instances, authTypes, p)
	info.Status = status.Status
	info.CredentialState = status.CredentialState
	info.HealthState = status.HealthState
	info.Actions = status.Actions
	info.Connected = status.Connected
}

func (s *Server) defaultIntegrationStatus(info *integrationInfo, prov core.Provider, instances []instanceInfo, authTypes []string, p *principal.Principal) connectionStatusInfo {
	if info == nil {
		return unknownConnectionStatus()
	}
	var status connectionStatusInfo
	if recon, ok := summarizeReconnectRequiredConnectionStatuses(info.Connections); ok {
		status = recon
	} else if conn, ok := info.connectionStatusForDefaultTarget(s.defaultConnectionName(info.Name)); ok {
		status = statusFromConnectionInfo(conn)
	} else if conn, ok := info.singleConnectionStatus(); ok {
		status = statusFromConnectionInfo(conn)
	} else if len(info.Connections) == 0 {
		status = s.implicitIntegrationStatus(info.Name, prov, instances, authTypes, p)
	} else {
		status = summarizeConnectionStatuses(info.Connections)
	}
	if len(info.Connections) > 0 {
		status.Connected = subjectProductConnected(info.Connections)
	}
	return status
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
	if prov == nil {
		return unknownConnectionStatus()
	}
	mode := core.NormalizeConnectionMode(prov.ConnectionMode())
	switch mode {
	case core.ConnectionModeNone:
		return connectionStatusInfo{
			Status:          connectionStatusReady,
			CredentialState: credentialStateNotRequired,
			HealthState:     healthStateNotApplicable,
			Actions:         []string{},
			Connected:       false,
		}
	default:
		return subjectConnectionStatus(groupInstancesForConnection(instances, ""), len(authTypes) > 0, ownerKindForPrincipal(p), "")
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
		Connected:       conn.Connected,
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
	if status, ok := summarizeReconnectRequiredConnectionStatuses(connections); ok {
		return status
	}
	for i := range connections {
		conn := &connections[i]
		if conn.Status == connectionStatusNeedsInstanceSelection {
			return statusFromConnectionInfo(conn)
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
		status.Connected = subjectProductConnected(connections)
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
		if conn.Status == connectionStatusUnavailable {
			return statusFromConnectionInfo(conn)
		}
	}
	return unknownConnectionStatus()
}

func summarizeReconnectRequiredConnectionStatuses(connections []connectionDefInfo) (connectionStatusInfo, bool) {
	if len(connections) == 0 {
		return connectionStatusInfo{}, false
	}
	var (
		invalidStatus *connectionDefInfo
		hasConnected  bool
	)
	for i := range connections {
		conn := &connections[i]
		if conn.Status == connectionStatusDegraded {
			return statusFromConnectionInfo(conn), true
		}
		if conn.CredentialState == credentialStateInvalid || conn.StatusCode == "reconnect_required" {
			if invalidStatus == nil {
				invalidStatus = conn
			}
			continue
		}
		if connectionHasSubjectIdentity(conn) && conn.Connected {
			hasConnected = true
		}
	}
	if invalidStatus == nil {
		return connectionStatusInfo{}, false
	}
	if hasConnected {
		return connectionStatusInfo{
			Status:          connectionStatusDegraded,
			CredentialState: credentialStateInvalid,
			HealthState:     healthStateUnhealthy,
			Actions:         []string{},
			CredentialMode:  invalidStatus.CredentialMode,
			OwnerKind:       invalidStatus.OwnerKind,
			Disconnectable:  invalidStatus.disconnectable,
			Connected:       true,
			StatusCode:      "reconnect_required",
			StatusReason:    "one or more stored credentials are expired and refresh has failed; reconnect them",
		}, true
	}
	return statusFromConnectionInfo(invalidStatus), true
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
		Connected:       false,
	}
}

// subjectProductConnected is true when this subject has a chosen identity on
// any credential-bearing connection. No-auth / mode-none rows are never that.
func subjectProductConnected(connections []connectionDefInfo) bool {
	for i := range connections {
		if connectionHasSubjectIdentity(&connections[i]) && connections[i].Connected {
			return true
		}
	}
	return false
}

func connectionHasSubjectIdentity(conn *connectionDefInfo) bool {
	if conn == nil {
		return false
	}
	return conn.CredentialMode != credentialModeNone
}

func subjectConnectionStatus(instances []instanceInfo, connectable bool, ownerKind string, preferredInstance string) connectionStatusInfo {
	status := connectionStatusInfo{
		CredentialMode: credentialModeSubject,
		OwnerKind:      ownerKind,
		HealthState:    healthStateNotApplicable,
		Actions:        []string{},
		Connected:      false,
	}
	invalidCount := invalidInstanceCount(instances)
	validCount := len(instances) - invalidCount
	// Product invariant: connected iff a chosen account exists.
	// Chosen = valid preferred, or exactly one valid instance (implicitly chosen).
	chosen := preferredInstanceValid(instances, preferredInstance) || validCount == 1
	switch len(instances) {
	case 0:
		status.Status = connectionStatusNeedsUserConnection
		status.CredentialState = credentialStateMissing
		if connectable {
			status.Actions = []string{actionConnect}
		}
	default:
		switch {
		case validCount == 0:
			status.Status = connectionStatusNeedsUserConnection
			status.CredentialState = credentialStateInvalid
			status.HealthState = healthStateUnhealthy
			status.Disconnectable = true
			status.Connected = false
			status.Actions = reconnectStatusActions(instances, connectable, true, false)
			status.StatusCode = "reconnect_required"
			status.StatusReason = "stored credential is expired and refresh has failed; reconnect it"
		case !chosen:
			// Accounts exist as credential material, but none is chosen — not connected.
			status.Status = connectionStatusNeedsInstanceSelection
			status.CredentialState = credentialStateConnected
			status.HealthState = healthStateNotChecked
			status.Disconnectable = true
			status.Connected = false
			status.Actions = subjectConnectionActions(true, connectable, true)
			status.StatusCode = "instance_selection_required"
			status.StatusReason = "multiple accounts are available; choose which account this workspace should use"
		case invalidCount == 0:
			status.Status = connectionStatusReady
			status.CredentialState = credentialStateConnected
			status.HealthState = healthStateNotChecked
			status.Disconnectable = true
			status.Connected = true
			status.Actions = subjectConnectionActions(true, connectable, false)
		default:
			status.Status = connectionStatusDegraded
			status.CredentialState = credentialStateInvalid
			status.HealthState = healthStateUnhealthy
			status.Disconnectable = true
			status.Connected = true
			status.Actions = reconnectStatusActions(instances, connectable, true, connectable)
			status.StatusCode = "reconnect_required"
			status.StatusReason = "one or more stored credentials are expired and refresh has failed; reconnect them"
		}
	}
	return status
}

func preferredInstanceValid(instances []instanceInfo, preferredInstance string) bool {
	preferredInstance = strings.TrimSpace(preferredInstance)
	if preferredInstance == "" {
		return false
	}
	for _, instance := range instances {
		if instance.Name == preferredInstance && !instance.credentialInvalid {
			return true
		}
	}
	return false
}

func markPreferredInstances(instances []instanceInfo, preferredInstance string) []instanceInfo {
	preferredInstance = strings.TrimSpace(preferredInstance)
	if preferredInstance == "" || len(instances) == 0 {
		return instances
	}
	found := false
	for _, instance := range instances {
		if instance.Name == preferredInstance {
			found = true
			break
		}
	}
	if !found {
		return instances
	}
	out := make([]instanceInfo, len(instances))
	for i, instance := range instances {
		out[i] = instance
		if instance.Name == preferredInstance {
			out[i].Preferred = true
		}
	}
	return out
}

func invalidInstanceCount(instances []instanceInfo) int {
	count := 0
	for _, instance := range instances {
		if instance.credentialInvalid {
			count++
		}
	}
	return count
}

func reconnectStatusActions(instances []instanceInfo, reconnectable, disconnectable, addInstance bool) []string {
	var actions []string
	if len(instances) > 1 {
		actions = append(actions, actionSelectInstance)
	}
	if reconnectable && reconnectTargetsDefaultInstance(instances) {
		actions = append(actions, actionReconnect)
	}
	if disconnectable {
		actions = append(actions, actionDisconnect)
	}
	if addInstance {
		actions = append(actions, actionAddInstance)
	}
	return actions
}

func reconnectTargetsDefaultInstance(instances []instanceInfo) bool {
	return len(instances) == 1 && instances[0].Name == defaultTokenInstance
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
	canon := principal.Canonicalized(p)
	if canon == nil {
		return ownerKindUnknown
	}
	subjectID := strings.TrimSpace(canon.SubjectID)
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

func groupInstancesForConnection(instances []instanceInfo, connection string, preferred ...string) []instanceInfo {
	connection = userFacingConnectionName(config.ResolveConnectionAlias(connection))
	filtered := make([]instanceInfo, 0, len(instances))
	for _, instance := range instances {
		if connection != "" && config.ResolveConnectionAlias(instance.Connection) != config.ResolveConnectionAlias(connection) {
			continue
		}
		filtered = append(filtered, instance)
	}
	return dedupeInstancesByAccount(filtered, firstString(preferred))
}

func dedupeInstancesByAccount(instances []instanceInfo, preferred string) []instanceInfo {
	if len(instances) < 2 {
		return instances
	}
	// Keep records without a canonical key distinct. They are legacy or
	// provider credentials that cannot safely be proven to represent the same
	// account.
	seen := make(map[string]int, len(instances))
	out := make([]instanceInfo, 0, len(instances))
	for _, instance := range instances {
		if instance.AccountKey == "" {
			out = append(out, instance)
			continue
		}
		key := instance.AccountKey
		idx, ok := seen[key]
		if !ok {
			seen[key] = len(out)
			out = append(out, instance)
			continue
		}
		if shouldPreferInstance(instance, out[idx], preferred) {
			out[idx] = instance
		}
	}
	return out
}

func shouldPreferInstance(candidate, current instanceInfo, preferred string) bool {
	candidatePreferred := candidate.Name == preferred
	currentPreferred := current.Name == preferred
	if candidatePreferred != currentPreferred {
		return candidatePreferred
	}
	if candidate.credentialCreated.IsZero() != current.credentialCreated.IsZero() {
		return !candidate.credentialCreated.IsZero()
	}
	if !candidate.credentialCreated.Equal(current.credentialCreated) {
		return candidate.credentialCreated.Before(current.credentialCreated)
	}
	if candidate.credentialID != current.credentialID {
		return candidate.credentialID < current.credentialID
	}
	return candidate.Name < current.Name
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}
