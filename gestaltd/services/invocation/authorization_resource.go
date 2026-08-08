package invocation

import (
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func AuthorizationResource(name string, kinds map[string]ProviderKind) *proto.Resource {
	name = strings.TrimSpace(name)
	if kind, ok := kinds[name]; ok {
		return &proto.Resource{Type: string(kind), Id: name}
	}
	return &proto.Resource{Type: name, Id: name}
}

// AuthorizationResourceMapper is the single place that maps an app key to the
// authorization policy alias and to the resource type/ID pair the authorization
// evaluator is asked about. Invocation, mounted UI, and app-admin decisions all
// resolve through it so no surface derives its own resource mapping.
type AuthorizationResourceMapper struct {
	kinds    map[string]ProviderKind
	policies map[string]string
}

// NewAuthorizationResourceMapper builds a mapper over the configured provider
// kinds and the configured per-app authorization policy aliases.
func NewAuthorizationResourceMapper(kinds map[string]ProviderKind, policies map[string]string) AuthorizationResourceMapper {
	return AuthorizationResourceMapper{kinds: kinds, policies: policies}
}

// Policy returns the authorization policy alias for an app key, or the app key
// itself when no alias is configured.
func (m AuthorizationResourceMapper) Policy(appKey string) string {
	appKey = strings.TrimSpace(appKey)
	if policy := strings.TrimSpace(m.policies[appKey]); policy != "" {
		return policy
	}
	return appKey
}

// Resource returns the authorization resource for an app key. A configured
// policy alias becomes its own dedicated resource type; otherwise the provider
// kind supplies the resource type.
func (m AuthorizationResourceMapper) Resource(appKey string) *proto.Resource {
	appKey = strings.TrimSpace(appKey)
	if policy := strings.TrimSpace(m.policies[appKey]); policy != "" {
		return &proto.Resource{Type: policy, Id: policy}
	}
	return AuthorizationResource(appKey, m.kinds)
}
