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

	ProviderModelSentinelRelation   = "managed"
	ProviderModelSentinelSubjectID  = "gestalt:authorization"
	ProviderModelSentinelResourceID = "__gestalt_authorization_model__"

	ProviderLegacyHumanImportSentinelRelation   = "managed"
	ProviderLegacyHumanImportSentinelSubjectID  = "gestalt:authorization"
	ProviderLegacyHumanImportSentinelResourceID = "__gestalt_legacy_human_import_v1__"
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

func ProviderModelSentinelRelationship() *core.Relationship {
	return &core.Relationship{
		Subject: &core.SubjectRef{
			Type: ProviderSubjectTypeSubject,
			Id:   ProviderModelSentinelSubjectID,
		},
		Relation: ProviderModelSentinelRelation,
		Resource: &core.ResourceRef{
			Type: ProviderResourceTypePolicyStatic,
			Id:   ProviderModelSentinelResourceID,
		},
	}
}

func IsProviderModelSentinelRelationship(rel *core.Relationship) bool {
	if rel == nil || rel.GetSubject() == nil || rel.GetResource() == nil {
		return false
	}
	return rel.GetSubject().GetType() == ProviderSubjectTypeSubject &&
		rel.GetSubject().GetId() == ProviderModelSentinelSubjectID &&
		rel.GetRelation() == ProviderModelSentinelRelation &&
		rel.GetResource().GetType() == ProviderResourceTypePolicyStatic &&
		rel.GetResource().GetId() == ProviderModelSentinelResourceID
}

func ProviderLegacyHumanImportSentinelRelationship() *core.Relationship {
	return &core.Relationship{
		Subject: &core.SubjectRef{
			Type: ProviderSubjectTypeSubject,
			Id:   ProviderLegacyHumanImportSentinelSubjectID,
		},
		Relation: ProviderLegacyHumanImportSentinelRelation,
		Resource: &core.ResourceRef{
			Type: ProviderResourceTypePolicyStatic,
			Id:   ProviderLegacyHumanImportSentinelResourceID,
		},
	}
}
