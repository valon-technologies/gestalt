package config

import (
	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/authorization"
	"google.golang.org/protobuf/types/known/structpb"
)

// AuthorizationStaticConfig adapts parsed config into the service-owned
// authorization static policy model.
func AuthorizationStaticConfig(cfg AuthorizationConfig, pluginDefs map[string]*ProviderEntry) authorization.StaticConfig {
	out := authorization.StaticConfig{
		Policies:                make(map[string]authorization.StaticSubjectPolicy, len(cfg.Policies)),
		ProviderPolicies:        make(map[string]string, len(pluginDefs)),
		ModelFragments:          authorizationModelFragments(cfg),
		Relationships:           authorizationRelationships(cfg),
		ResourceDynamicPolicies: authorizationResourceDynamicPolicies(cfg),
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

func authorizationResourceDynamicPolicies(cfg AuthorizationConfig) map[string]authorization.StaticResourceDynamicPolicy {
	out := map[string]authorization.StaticResourceDynamicPolicy{}
	for name, resourceType := range cfg.ResourceTypes {
		if resourceType.Dynamic.AllowAdditionalRelationships {
			out[name] = authorization.StaticResourceDynamicPolicy{AllowAdditionalRelationships: true}
		}
	}
	for _, model := range cfg.Models {
		for name, resourceType := range model.ResourceTypes {
			if resourceType.Dynamic.AllowAdditionalRelationships {
				out[name] = authorization.StaticResourceDynamicPolicy{AllowAdditionalRelationships: true}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func authorizationModelFragments(cfg AuthorizationConfig) []*core.AuthorizationModelResourceType {
	var out []*core.AuthorizationModelResourceType
	for _, model := range cfg.Models {
		for name, resourceType := range model.ResourceTypes {
			out = append(out, authorizationModelResourceType(name, resourceType))
		}
	}
	return out
}

func authorizationModelResourceType(name string, def AuthorizationResourceTypeDef) *core.AuthorizationModelResourceType {
	resourceType := &core.AuthorizationModelResourceType{Name: name}
	for relationName, relation := range def.Relations {
		resourceType.Relations = append(resourceType.Relations, &core.AuthorizationModelRelation{
			Name:           relationName,
			SubjectTypes:   append([]string(nil), relation.SubjectTypes...),
			AllowedTargets: authorizationAllowedTargets(relation.AllowedTargets),
		})
	}
	for actionName, action := range def.Actions {
		resourceType.Actions = append(resourceType.Actions, &core.AuthorizationModelAction{
			Name:      actionName,
			Relations: append([]string(nil), action.Relations...),
		})
	}
	return resourceType
}

func authorizationAllowedTargets(targets []AuthorizationAllowedTargetDef) []*core.AuthorizationModelAllowedTarget {
	out := make([]*core.AuthorizationModelAllowedTarget, 0, len(targets))
	for _, target := range targets {
		switch {
		case target.SubjectType != "":
			out = append(out, &core.AuthorizationModelAllowedTarget{
				Kind: &proto.AuthorizationModelAllowedTarget_SubjectType{SubjectType: target.SubjectType},
			})
		case target.ResourceType != "":
			out = append(out, &core.AuthorizationModelAllowedTarget{
				Kind: &proto.AuthorizationModelAllowedTarget_ResourceType{ResourceType: target.ResourceType},
			})
		case target.SubjectSet != nil:
			out = append(out, &core.AuthorizationModelAllowedTarget{
				Kind: &proto.AuthorizationModelAllowedTarget_SubjectSet{SubjectSet: &core.AuthorizationModelSubjectSetTarget{
					ResourceType: target.SubjectSet.ResourceType,
					Relation:     target.SubjectSet.Relation,
				}},
			})
		}
	}
	return out
}

func authorizationRelationships(cfg AuthorizationConfig) []*core.Relationship {
	out := make([]*core.Relationship, 0, len(cfg.Relationships))
	for i := range cfg.Relationships {
		relationship := &cfg.Relationships[i]
		out = append(out, &core.Relationship{
			Subject:    authorizationSubject(relationship.Subject),
			Relation:   relationship.Relation,
			Resource:   authorizationResource(relationship.Resource),
			Target:     authorizationRelationshipTarget(relationship.Target),
			Properties: authorizationStringProperties(relationship.Properties),
		})
	}
	return out
}

func authorizationSubject(def AuthorizationSubjectDef) *core.SubjectRef {
	if def.Type == "" && def.ID == "" {
		return nil
	}
	return &core.SubjectRef{Type: def.Type, Id: def.ID}
}

func authorizationResource(def AuthorizationResourceDef) *core.ResourceRef {
	if def.Type == "" && def.ID == "" {
		return nil
	}
	return &core.ResourceRef{Type: def.Type, Id: def.ID}
}

func authorizationRelationshipTarget(def AuthorizationRelationshipTargetDef) *core.RelationshipTargetRef {
	switch {
	case def.Subject != nil:
		return &core.RelationshipTargetRef{Kind: &proto.RelationshipTarget_Subject{Subject: authorizationSubject(*def.Subject)}}
	case def.Resource != nil:
		return &core.RelationshipTargetRef{Kind: &proto.RelationshipTarget_Resource{Resource: authorizationResource(*def.Resource)}}
	case def.SubjectSet != nil:
		return &core.RelationshipTargetRef{Kind: &proto.RelationshipTarget_SubjectSet{SubjectSet: &core.SubjectSetRef{
			Resource: authorizationResource(def.SubjectSet.Resource),
			Relation: def.SubjectSet.Relation,
		}}}
	default:
		return nil
	}
}

func authorizationStringProperties(properties map[string]string) *structpb.Struct {
	if len(properties) == 0 {
		return nil
	}
	values := make(map[string]any, len(properties))
	for key, value := range properties {
		values[key] = value
	}
	out, _ := structpb.NewStruct(values)
	return out
}
