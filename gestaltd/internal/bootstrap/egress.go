package bootstrap

import (
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/egress"
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

func (d EgressDeps) Policy(allowedHosts []string) egress.Policy {
	return egress.Policy{
		AllowedHosts:  allowedHosts,
		DefaultAction: d.DefaultAction,
	}
}

func (d EgressDeps) ProviderPolicy(entry *config.ProviderEntry) egress.Policy {
	if entry == nil {
		return d.Policy(nil)
	}
	return d.Policy(entry.EffectiveAllowedHosts())
}

// CheckFunc returns an egress check function scoped to a particular
// provider's allowed host list.
func (d EgressDeps) CheckFunc(allowedHosts []string) func(string) error {
	policy := d.Policy(allowedHosts)
	return policy.CheckHost
}
