package config

import "github.com/valon-technologies/gestalt/server/services/authorization"

// AuthorizationStaticConfig adapts parsed config into the service-owned
// authorization static policy model.
func AuthorizationStaticConfig(cfg AuthorizationConfig, pluginDefs map[string]*ProviderEntry) authorization.StaticConfig {
	out := authorization.StaticConfig{
		Policies:         make(map[string]authorization.StaticSubjectPolicy, len(cfg.Policies)),
		ProviderPolicies: make(map[string]string, len(pluginDefs)),
	}
	for policyID, def := range cfg.Policies {
		policy := authorization.StaticSubjectPolicy{
			Default: def.Default,
			Members: make([]authorization.StaticSubjectMember, 0, len(def.Members)),
		}
		for _, member := range def.Members {
			policy.Members = append(policy.Members, authorization.StaticSubjectMember{
				SubjectID: member.SubjectID,
				Role:      member.Role,
			})
		}
		out.Policies[policyID] = policy
	}
	for providerName, entry := range pluginDefs {
		if entry != nil {
			out.ProviderPolicies[providerName] = entry.AuthorizationPolicy
		}
	}
	return out
}
