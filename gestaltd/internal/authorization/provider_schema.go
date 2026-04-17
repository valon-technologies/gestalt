package authorization

import "github.com/valon-technologies/gestalt/server/core"

const (
	ProviderAuthzSchema = `version: gestalt.authorization.v1
resources:
  - policy_static
  - plugin_static
  - plugin_dynamic
  - admin_policy_static
  - admin_dynamic
subjects:
  - subject
  - user
  - email
`

	ProviderResourceTypePolicyStatic      = "policy_static"
	ProviderResourceTypePluginStatic      = "plugin_static"
	ProviderResourceTypePluginDynamic     = "plugin_dynamic"
	ProviderResourceTypeAdminPolicyStatic = "admin_policy_static"
	ProviderResourceTypeAdminDynamic      = "admin_dynamic"
	ProviderResourceIDAdminDynamicGlobal  = "global"

	ProviderSubjectTypeSubject = "subject"
	ProviderSubjectTypeUser    = "user"
	ProviderSubjectTypeEmail   = "email"
)

const (
	providerAuthzSchema = ProviderAuthzSchema

	resourceTypePolicyStatic      = ProviderResourceTypePolicyStatic
	resourceTypePluginStatic      = ProviderResourceTypePluginStatic
	resourceTypePluginDynamic     = ProviderResourceTypePluginDynamic
	resourceTypeAdminPolicyStatic = ProviderResourceTypeAdminPolicyStatic
	resourceTypeAdminDynamic      = ProviderResourceTypeAdminDynamic
	resourceIDAdminDynamicGlobal  = ProviderResourceIDAdminDynamicGlobal

	subjectTypeSubject = ProviderSubjectTypeSubject
	subjectTypeUser    = ProviderSubjectTypeUser
	subjectTypeEmail   = ProviderSubjectTypeEmail
)

func IsManagedProviderRelationship(rel *core.Relationship) bool {
	if rel == nil || rel.GetResource() == nil {
		return false
	}
	switch rel.GetResource().GetType() {
	case ProviderResourceTypePolicyStatic,
		ProviderResourceTypePluginStatic,
		ProviderResourceTypePluginDynamic,
		ProviderResourceTypeAdminPolicyStatic,
		ProviderResourceTypeAdminDynamic:
		return true
	default:
		return false
	}
}
