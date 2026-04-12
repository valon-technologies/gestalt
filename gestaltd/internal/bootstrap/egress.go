package bootstrap

import (
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/egress"
)

// EgressDeps holds the server-wide egress configuration derived from the
// config file. Individual providers combine this with their own allowedHosts.
type EgressDeps struct {
	DefaultAction egress.PolicyAction
}

func newEgressDeps(cfg *config.Config) EgressDeps {
	action := egress.PolicyAction(cfg.Server.Egress.DefaultAction)
	if action == "" {
		action = egress.PolicyAllow
	}
	return EgressDeps{DefaultAction: action}
}

// CheckFunc returns an egress check function scoped to a particular
// provider's allowed host list.
func (d EgressDeps) CheckFunc(allowedHosts []string) func(string) error {
	return func(host string) error {
		return egress.CheckHost(allowedHosts, host, d.DefaultAction)
	}
}
