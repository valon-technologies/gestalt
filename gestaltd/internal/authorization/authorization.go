package authorization

import (
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	authorizationservice "github.com/valon-technologies/gestalt/server/services/authorization"
)

const (
	ProviderResourceTypePolicyStatic        = authorizationservice.ProviderResourceTypePolicyStatic
	ProviderResourceTypePluginStatic        = authorizationservice.ProviderResourceTypePluginStatic
	ProviderResourceTypePluginDynamic       = authorizationservice.ProviderResourceTypePluginDynamic
	ProviderResourceTypeAdminPolicyStatic   = authorizationservice.ProviderResourceTypeAdminPolicyStatic
	ProviderResourceTypeAdminDynamic        = authorizationservice.ProviderResourceTypeAdminDynamic
	ProviderResourceTypeExternalIdentity    = authorizationservice.ProviderResourceTypeExternalIdentity
	ProviderResourceTypeManagedSubject      = authorizationservice.ProviderResourceTypeManagedSubject
	ProviderResourceIDAdminDynamicGlobal    = authorizationservice.ProviderResourceIDAdminDynamicGlobal
	ProviderExternalIdentityRelationAssume  = authorizationservice.ProviderExternalIdentityRelationAssume
	ProviderManagedSubjectRelationViewer    = authorizationservice.ProviderManagedSubjectRelationViewer
	ProviderManagedSubjectRelationEditor    = authorizationservice.ProviderManagedSubjectRelationEditor
	ProviderManagedSubjectRelationAdmin     = authorizationservice.ProviderManagedSubjectRelationAdmin
	ProviderManagedSubjectActionView        = authorizationservice.ProviderManagedSubjectActionView
	ProviderManagedSubjectActionManage      = authorizationservice.ProviderManagedSubjectActionManage
	ProviderManagedSubjectActionCreateToken = authorizationservice.ProviderManagedSubjectActionCreateToken
	ProviderManagedSubjectActionGrant       = authorizationservice.ProviderManagedSubjectActionGrant
	ProviderManagedSubjectActionConnect     = authorizationservice.ProviderManagedSubjectActionConnect
	ProviderSubjectTypeSubject              = authorizationservice.ProviderSubjectTypeSubject
	ProviderSubjectTypeUser                 = authorizationservice.ProviderSubjectTypeUser
)

type AccessContext = authorizationservice.AccessContext
type SubjectPolicy = authorizationservice.SubjectPolicy
type StaticSubjectMember = authorizationservice.StaticSubjectMember
type StaticConfig = authorizationservice.StaticConfig
type StaticSubjectPolicy = authorizationservice.StaticSubjectPolicy
type Authorizer = authorizationservice.Authorizer
type ProviderBackedAuthorizer = authorizationservice.ProviderBackedAuthorizer
type RuntimeAuthorizer = authorizationservice.RuntimeAuthorizer
type ProviderActionAuthorizer = authorizationservice.ProviderActionAuthorizer
type ManagedAuthorizationModelResolver = authorizationservice.ManagedAuthorizationModelResolver

func New(cfg config.AuthorizationConfig, pluginDefs map[string]*config.ProviderEntry) (*Authorizer, error) {
	return authorizationservice.New(staticConfigFromConfig(cfg, pluginDefs))
}

func NewProviderBacked(base *Authorizer, provider core.AuthorizationProvider) (*ProviderBackedAuthorizer, error) {
	return authorizationservice.NewProviderBacked(base, provider)
}

func IsManagedProviderRelationship(rel *core.Relationship) bool {
	return authorizationservice.IsManagedProviderRelationship(rel)
}

func ProviderAuthorizationModelForRoles(policyRoles, pluginStaticRoles, pluginDynamicRoles, adminDynamicRoles []string) *core.AuthorizationModel {
	return authorizationservice.ProviderAuthorizationModelForRoles(policyRoles, pluginStaticRoles, pluginDynamicRoles, adminDynamicRoles)
}

func staticConfigFromConfig(cfg config.AuthorizationConfig, pluginDefs map[string]*config.ProviderEntry) StaticConfig {
	out := StaticConfig{
		Policies:         make(map[string]StaticSubjectPolicy, len(cfg.Policies)),
		ProviderPolicies: make(map[string]string, len(pluginDefs)),
	}
	for policyID, def := range cfg.Policies {
		policy := StaticSubjectPolicy{
			Default: def.Default,
			Members: make([]StaticSubjectMember, 0, len(def.Members)),
		}
		for _, member := range def.Members {
			policy.Members = append(policy.Members, StaticSubjectMember{
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
