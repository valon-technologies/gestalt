package providerdrivers

import (
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

type AuthenticationDeps struct {
	DefaultCallbackURL string
	SessionKey         []byte
}

type WorkflowDeps struct {
	EgressDefaultAction egress.PolicyAction
}

type AgentDeps struct {
	EgressDefaultAction egress.PolicyAction
	Telemetry           runtimehost.TelemetryProviders
}
