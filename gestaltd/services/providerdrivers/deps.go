package providerdrivers

import (
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

type IdentityDeps struct {
	DefaultCallbackURL string
	SessionKey         []byte
	HostServices       []runtimehost.HostService
}

type WorkflowDeps struct {
	EgressDefaultAction egress.PolicyAction
	Telemetry           runtimehost.TelemetryProviders
}

type AgentDeps struct {
	EgressDefaultAction egress.PolicyAction
	Telemetry           runtimehost.TelemetryProviders
}
