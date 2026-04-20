package authorization

import (
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
)

const (
	ProviderResourceTypePolicyStatic      = "policy_static"
	ProviderResourceTypePluginStatic      = "plugin_static"
	ProviderResourceTypePluginDynamic     = "plugin_dynamic"
	ProviderResourceTypeAdminPolicyStatic = "admin_policy_static"
	ProviderResourceTypeAdminDynamic      = "admin_dynamic"
	ProviderResourceIDAdminDynamicGlobal  = "global"

	ProviderSubjectTypeSubject = "subject"
	ProviderSubjectTypeUser    = "user"
)

const (
	resourceTypePolicyStatic      = ProviderResourceTypePolicyStatic
	resourceTypePluginStatic      = ProviderResourceTypePluginStatic
	resourceTypePluginDynamic     = ProviderResourceTypePluginDynamic
	resourceTypeAdminPolicyStatic = ProviderResourceTypeAdminPolicyStatic
	resourceTypeAdminDynamic      = ProviderResourceTypeAdminDynamic
	resourceIDAdminDynamicGlobal  = ProviderResourceIDAdminDynamicGlobal

	subjectTypeSubject = ProviderSubjectTypeSubject
	subjectTypeUser    = ProviderSubjectTypeUser
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

func buildProviderAuthorizationModel(state providerBackedRoleState) *core.AuthorizationModel {
	model := &core.AuthorizationModel{Version: 1}

	policyRoles := unionRoleLists(state.policyStaticRoles)
	policyRelations := map[string][]string{}
	for _, role := range policyRoles {
		policyRelations[role] = []string{subjectTypeSubject}
	}
	model.ResourceTypes = appendIfModelResourceType(model.ResourceTypes,
		buildProviderAuthorizationResourceType(resourceTypePolicyStatic, policyRelations, policyRoles),
	)

	model.ResourceTypes = appendIfModelResourceType(model.ResourceTypes,
		buildProviderAuthorizationResourceType(
			resourceTypePluginStatic,
			resourceTypesForRoles(unionRoleLists(state.pluginStaticRoles), subjectTypeSubject),
			unionRoleLists(state.pluginStaticRoles),
		),
	)
	model.ResourceTypes = appendIfModelResourceType(model.ResourceTypes,
		buildProviderAuthorizationResourceType(
			resourceTypePluginDynamic,
			resourceTypesForRoles(unionRoleLists(state.pluginDynamicRoles), subjectTypeUser),
			unionRoleLists(state.pluginDynamicRoles),
		),
	)
	model.ResourceTypes = appendIfModelResourceType(model.ResourceTypes,
		buildProviderAuthorizationResourceType(
			resourceTypeAdminPolicyStatic,
			resourceTypesForRoles(policyRoles, subjectTypeSubject),
			policyRoles,
		),
	)
	model.ResourceTypes = appendIfModelResourceType(model.ResourceTypes,
		buildProviderAuthorizationResourceType(
			resourceTypeAdminDynamic,
			resourceTypesForRoles(state.adminDynamicRoles, subjectTypeUser),
			state.adminDynamicRoles,
		),
	)

	slices.SortFunc(model.ResourceTypes, func(left, right *core.AuthorizationModelResourceType) int {
		return strings.Compare(left.GetName(), right.GetName())
	})
	return model
}

func appendIfModelResourceType(target []*core.AuthorizationModelResourceType, resourceType *core.AuthorizationModelResourceType) []*core.AuthorizationModelResourceType {
	if resourceType == nil {
		return target
	}
	return append(target, resourceType)
}

func buildProviderAuthorizationResourceType(name string, relations map[string][]string, actionNames []string) *core.AuthorizationModelResourceType {
	if len(relations) == 0 {
		return nil
	}
	resourceType := &core.AuthorizationModelResourceType{Name: name}
	relationNames := make([]string, 0, len(relations))
	for relation := range relations {
		relationNames = append(relationNames, relation)
	}
	slices.SortFunc(relationNames, strings.Compare)
	for _, relation := range relationNames {
		resourceType.Relations = append(resourceType.Relations, &core.AuthorizationModelRelation{
			Name:         relation,
			SubjectTypes: append([]string(nil), relations[relation]...),
		})
	}
	for _, action := range actionNames {
		action = strings.TrimSpace(action)
		if action == "" {
			continue
		}
		resourceType.Actions = append(resourceType.Actions, &core.AuthorizationModelAction{
			Name:      action,
			Relations: []string{action},
		})
	}
	return resourceType
}

func resourceTypesForRoles(roles []string, subjectTypes ...string) map[string][]string {
	if len(roles) == 0 {
		return nil
	}
	allowedSubjects := append([]string(nil), subjectTypes...)
	out := make(map[string][]string, len(roles))
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		out[role] = allowedSubjects
	}
	return out
}

func unionRoleLists(grouped map[string][]string) []string {
	if len(grouped) == 0 {
		return nil
	}
	out := map[string]struct{}{}
	for _, roles := range grouped {
		for _, role := range roles {
			role = strings.TrimSpace(role)
			if role == "" {
				continue
			}
			out[role] = struct{}{}
		}
	}
	return normalizeRoleList(out)
}
