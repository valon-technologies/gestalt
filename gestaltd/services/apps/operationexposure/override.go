package operationexposure

import providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"

// OperationOverride holds deployer-owned allowed-operation metadata from config.
type OperationOverride struct {
	Alias        string                                       `yaml:"alias,omitempty" json:"alias,omitempty"`
	Description  string                                       `yaml:"description,omitempty" json:"description,omitempty"`
	AllowedRoles []string                                     `yaml:"allowedRoles,omitempty" json:"allowedRoles,omitempty"`
	Tags         []string                                     `yaml:"tags,omitempty" json:"tags,omitempty"`
	Paginate     bool                                         `yaml:"paginate,omitempty" json:"paginate,omitempty"`
	Pagination   *providermanifestv1.ManifestPaginationConfig `yaml:"pagination,omitempty" json:"pagination,omitempty"`
	GraphQL      *providermanifestv1.ManifestGraphQLOperation `yaml:"graphql,omitempty" json:"graphql,omitempty"`
}

// OverridesFromManifest converts provider-authored operation exposure metadata into
// deployer override values without roles.
func OverridesFromManifest(allowed map[string]*providermanifestv1.ManifestOperationOverride) map[string]*OperationOverride {
	if allowed == nil {
		return nil
	}
	if len(allowed) == 0 {
		return map[string]*OperationOverride{}
	}
	out := make(map[string]*OperationOverride, len(allowed))
	for name, override := range allowed {
		if override == nil {
			out[name] = nil
			continue
		}
		out[name] = &OperationOverride{
			Alias:       override.Alias,
			Description: override.Description,
			Tags:        override.Tags,
			Paginate:    override.Paginate,
			Pagination:  override.Pagination,
			GraphQL:     override.GraphQL,
		}
	}
	return out
}
