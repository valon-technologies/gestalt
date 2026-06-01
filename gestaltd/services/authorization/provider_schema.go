package authorization

import (
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

const (
	ProviderResourceTypePolicyStatic                   = "policy_static"
	ProviderResourceTypeAppStatic                      = "app_static"
	ProviderResourceTypeAppDynamic                     = "app_dynamic"
	ProviderResourceTypeAdminPolicyStatic              = "admin_policy_static"
	ProviderResourceTypeAdminDynamic                   = "admin_dynamic"
	ProviderResourceTypeExternalIdentity               = "external_identity"
	ProviderResourceTypeManagedSubject                 = "managed_subject"
	ProviderResourceTypeEveryone                       = "everyone"
	ProviderResourceTypeTeam                           = "team"
	ProviderResourceTypeSlackChannel                   = "slack_channel"
	ProviderResourceIDAdminDynamicGlobal               = "global"
	ProviderResourceIDEveryoneGlobal                   = "global"
	ProviderExternalIdentityRelationAssume             = "assume"
	ProviderRelationMember                             = "member"
	ProviderManagedSubjectRelationViewer               = "viewer"
	ProviderManagedSubjectRelationEditor               = "editor"
	ProviderManagedSubjectRelationAdmin                = "admin"
	ProviderManagedSubjectActionView                   = "view"
	ProviderManagedSubjectActionManage                 = "manage"
	ProviderManagedSubjectActionCreateToken            = "create_token"
	ProviderManagedSubjectActionGrant                  = "grant"
	ProviderManagedSubjectActionConnect                = "connect"
	ProviderManagedSubjectActionManageExternalIdentity = "manage_external_identity"

	ProviderSubjectTypeSubject = "subject"
	ProviderSubjectTypeUser    = "user"
)

const (
	resourceTypePolicyStatic       = ProviderResourceTypePolicyStatic
	resourceTypeAppStatic          = ProviderResourceTypeAppStatic
	resourceTypeAppDynamic         = ProviderResourceTypeAppDynamic
	resourceTypeAdminPolicyStatic  = ProviderResourceTypeAdminPolicyStatic
	resourceTypeAdminDynamic       = ProviderResourceTypeAdminDynamic
	resourceTypeExternalIdentity   = ProviderResourceTypeExternalIdentity
	resourceTypeManagedSubject     = ProviderResourceTypeManagedSubject
	resourceTypeEveryone           = ProviderResourceTypeEveryone
	resourceTypeTeam               = ProviderResourceTypeTeam
	resourceTypeSlackChannel       = ProviderResourceTypeSlackChannel
	resourceIDAdminDynamicGlobal   = ProviderResourceIDAdminDynamicGlobal
	resourceIDEveryoneGlobal       = ProviderResourceIDEveryoneGlobal
	relationExternalIdentityAssume = ProviderExternalIdentityRelationAssume
	relationMember                 = ProviderRelationMember
	relationManagedSubjectViewer   = ProviderManagedSubjectRelationViewer
	relationManagedSubjectEditor   = ProviderManagedSubjectRelationEditor
	relationManagedSubjectAdmin    = ProviderManagedSubjectRelationAdmin

	subjectTypeSubject = ProviderSubjectTypeSubject
	subjectTypeUser    = ProviderSubjectTypeUser
)

var providerAuthorizationResourceTypes = []string{
	ProviderResourceTypePolicyStatic,
	ProviderResourceTypeAppStatic,
	ProviderResourceTypeAppDynamic,
	ProviderResourceTypeAdminPolicyStatic,
	ProviderResourceTypeAdminDynamic,
	ProviderResourceTypeExternalIdentity,
	ProviderResourceTypeManagedSubject,
	ProviderResourceTypeEveryone,
	ProviderResourceTypeTeam,
	ProviderResourceTypeSlackChannel,
}

func IsManagedProviderRelationship(rel *core.Relationship) bool {
	if rel == nil || relationshipResource(rel) == nil {
		return false
	}
	resourceType := strings.TrimSpace(relationshipResource(rel).GetType())
	for _, managedResourceType := range providerAuthorizationResourceTypes {
		if resourceType == managedResourceType {
			return true
		}
	}
	return false
}

func ProviderAuthorizationModelForRoles(policyRoles, appStaticRoles, appDynamicRoles, adminDynamicRoles []string) *core.AuthorizationModel {
	return buildProviderAuthorizationModel(providerBackedRoleState{
		policyStaticRoles: map[string][]string{
			"": append([]string(nil), policyRoles...),
		},
		appStaticRoles: map[string][]string{
			"": append([]string(nil), appStaticRoles...),
		},
		appDynamicRoles: map[string][]string{
			"": append([]string(nil), appDynamicRoles...),
		},
		adminDynamicRoles: append([]string(nil), adminDynamicRoles...),
	})
}

func buildProviderAuthorizationModel(state providerBackedRoleState) *core.AuthorizationModel {
	model := &core.AuthorizationModel{Version: "1"}

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
			resourceTypeAppStatic,
			resourceTypesForRoles(unionRoleLists(state.appStaticRoles), subjectTypeSubject),
			unionRoleLists(state.appStaticRoles),
		),
	)
	model.ResourceTypes = appendIfModelResourceType(model.ResourceTypes,
		buildProviderAuthorizationResourceType(
			resourceTypeAppDynamic,
			resourceTypesForRoles(unionRoleLists(state.appDynamicRoles), subjectTypeSubject),
			unionRoleLists(state.appDynamicRoles),
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
			resourceTypesForRoles(state.adminDynamicRoles, subjectTypeSubject),
			state.adminDynamicRoles,
		),
	)
	model.ResourceTypes = appendIfModelResourceType(model.ResourceTypes,
		&core.AuthorizationModelResourceType{
			Name: resourceTypeExternalIdentity,
			Relations: []*core.AuthorizationModelRelation{{
				Name:           relationExternalIdentityAssume,
				AllowedTargets: allowedSubjectTypeTargets(subjectTypeSubject, subjectTypeUser),
			}},
			Actions: []*core.AuthorizationModelAction{{
				Name:      relationExternalIdentityAssume,
				Relations: []string{relationExternalIdentityAssume},
			}},
		},
	)
	model.ResourceTypes = appendIfModelResourceType(model.ResourceTypes,
		&core.AuthorizationModelResourceType{
			Name: resourceTypeManagedSubject,
			Relations: []*core.AuthorizationModelRelation{
				{Name: relationManagedSubjectViewer, AllowedTargets: allowedSubjectTypeTargets(subjectTypeSubject)},
				{Name: relationManagedSubjectEditor, AllowedTargets: allowedSubjectTypeTargets(subjectTypeSubject)},
				{Name: relationManagedSubjectAdmin, AllowedTargets: allowedSubjectTypeTargets(subjectTypeSubject)},
			},
			Actions: []*core.AuthorizationModelAction{
				{Name: relationManagedSubjectViewer, Relations: []string{relationManagedSubjectViewer, relationManagedSubjectEditor, relationManagedSubjectAdmin}},
				{Name: relationManagedSubjectEditor, Relations: []string{relationManagedSubjectEditor, relationManagedSubjectAdmin}},
				{Name: relationManagedSubjectAdmin, Relations: []string{relationManagedSubjectAdmin}},
				{Name: ProviderManagedSubjectActionView, Relations: []string{relationManagedSubjectViewer, relationManagedSubjectEditor, relationManagedSubjectAdmin}},
				{Name: ProviderManagedSubjectActionManage, Relations: []string{relationManagedSubjectAdmin}},
				{Name: ProviderManagedSubjectActionCreateToken, Relations: []string{relationManagedSubjectAdmin}},
				{Name: ProviderManagedSubjectActionGrant, Relations: []string{relationManagedSubjectAdmin}},
				{Name: ProviderManagedSubjectActionConnect, Relations: []string{relationManagedSubjectEditor, relationManagedSubjectAdmin}},
				{Name: ProviderManagedSubjectActionManageExternalIdentity, Relations: []string{relationManagedSubjectAdmin}},
			},
		},
	)
	model.ResourceTypes = appendIfModelResourceType(model.ResourceTypes,
		membershipResourceType(resourceTypeEveryone),
	)
	model.ResourceTypes = appendIfModelResourceType(model.ResourceTypes,
		membershipResourceType(resourceTypeTeam),
	)
	model.ResourceTypes = appendIfModelResourceType(model.ResourceTypes,
		membershipResourceType(resourceTypeSlackChannel),
	)

	slices.SortFunc(model.ResourceTypes, func(left, right *core.AuthorizationModelResourceType) int {
		return strings.Compare(left.GetName(), right.GetName())
	})
	return model
}

func membershipResourceType(name string) *core.AuthorizationModelResourceType {
	return &core.AuthorizationModelResourceType{
		Name: name,
		Relations: []*core.AuthorizationModelRelation{{
			Name:           relationMember,
			AllowedTargets: allowedSubjectTypeTargets(subjectTypeSubject),
		}},
		Actions: []*core.AuthorizationModelAction{{
			Name:      relationMember,
			Relations: []string{relationMember},
		}},
	}
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
			Name:           relation,
			AllowedTargets: allowedSubjectTypeTargets(relations[relation]...),
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

func allowedSubjectTypeTargets(subjectTypes ...string) []*core.AuthorizationModelAllowedTarget {
	out := make([]*core.AuthorizationModelAllowedTarget, 0, len(subjectTypes))
	for _, subjectType := range subjectTypes {
		subjectType = strings.TrimSpace(subjectType)
		if subjectType == "" {
			continue
		}
		out = append(out, &core.AuthorizationModelAllowedTarget{
			Kind: &proto.AuthorizationModelAllowedTarget_SubjectType{SubjectType: subjectType},
		})
	}
	return out
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
