package config

import (
	"context"
	"fmt"
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// ValidateRuntimeSCIMConfig applies the same validation rules as
// validateSCIMConfig, but obtains the active authorization model from the
// configured provider instead of relying on static config-only definitions.
// Empty credentials are allowed here because runtime storage keeps their
// ciphertext separately and injects plaintext only when building the runtime
// service.
func ValidateRuntimeSCIMConfig(ctx context.Context, cfg ServerSCIMConfig, listModelTypes ListActiveAuthorizationModelTypes) error {
	if len(cfg.Clients) == 0 {
		return nil
	}
	resourceTypes, err := authorizationResourceTypesFromProvider(ctx, listModelTypes)
	if err != nil {
		return fmt.Errorf("SCIM validation: %w", err)
	}
	return validateSCIMClients(cfg.Clients, resourceTypes)
}

// ListActiveAuthorizationModelTypes is the minimal provider surface needed for
// runtime SCIM validation. It mirrors AuthorizationProvider.ListActiveModelResourceTypes.
type ListActiveAuthorizationModelTypes interface {
	ListActiveModelResourceTypes(ctx context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error)
}

func authorizationResourceTypesFromProvider(ctx context.Context, provider ListActiveAuthorizationModelTypes) (map[string]AuthorizationResourceTypeDef, error) {
	if provider == nil {
		return nil, fmt.Errorf("authorization provider is required")
	}
	out := map[string]AuthorizationResourceTypeDef{}
	var pageToken string
	for {
		response, err := provider.ListActiveModelResourceTypes(ctx, &proto.ListActiveModelResourceTypesRequest{
			PageSize:  200,
			PageToken: pageToken,
		})
		if err != nil {
			return nil, fmt.Errorf("list active model resource types: %w", err)
		}
		for _, resourceType := range response.GetResourceTypes() {
			name := strings.TrimSpace(resourceType.GetName())
			if name == "" {
				continue
			}
			if _, exists := out[name]; exists {
				return nil, fmt.Errorf("active model returned duplicate resource type %q", name)
			}
			out[name] = authorizationResourceTypeDefFromProto(resourceType)
		}
		nextToken := strings.TrimSpace(response.GetNextPageToken())
		if nextToken == "" || nextToken == pageToken {
			if nextToken != "" && nextToken == pageToken {
				return nil, fmt.Errorf("active model resource types pagination token did not advance")
			}
			return out, nil
		}
		pageToken = nextToken
	}
}

func authorizationResourceTypeDefFromProto(resourceType *proto.AuthorizationModelResourceType) AuthorizationResourceTypeDef {
	def := AuthorizationResourceTypeDef{
		DefaultRole: strings.TrimSpace(resourceType.GetDefaultRole()),
		Relations:   make(map[string]AuthorizationRelationDef, len(resourceType.GetRelations())),
		Actions:     make(map[string]AuthorizationActionDef, len(resourceType.GetActions())),
	}
	for _, relation := range resourceType.GetRelations() {
		relationDef := AuthorizationRelationDef{}
		for _, target := range relation.GetAllowedTargets() {
			targetDef := AuthorizationAllowedTargetDef{
				SubjectType:  strings.TrimSpace(target.GetSubjectType()),
				ResourceType: strings.TrimSpace(target.GetResourceType()),
			}
			if subjectSet := target.GetSubjectSetType(); subjectSet != nil {
				targetDef.SubjectSet = &AuthorizationSubjectSetTargetDef{
					ResourceType: strings.TrimSpace(subjectSet.GetResourceType()),
					Relation:     strings.TrimSpace(subjectSet.GetRelation()),
				}
			}
			relationDef.AllowedTargets = append(relationDef.AllowedTargets, targetDef)
		}
		def.Relations[strings.TrimSpace(relation.GetName())] = relationDef
	}
	for _, action := range resourceType.GetActions() {
		relations := make([]string, 0, len(action.GetRelations()))
		for _, relation := range action.GetRelations() {
			relations = append(relations, strings.TrimSpace(relation))
		}
		def.Actions[strings.TrimSpace(action.GetName())] = AuthorizationActionDef{Relations: relations}
	}
	// Runtime models intentionally allow SCIM relationship writes; the
	// config-only dynamic flag is not represented on the provider wire.
	def.Dynamic.AllowAdditionalRelationships = true
	return def
}

func validateSCIMClients(clients map[string]SCIMClientConfig, resourceTypes map[string]AuthorizationResourceTypeDef) error {
	groupType, hasGroup := resourceTypes["group"]
	if !hasGroup {
		return fmt.Errorf("SCIM requires authorization resource type %q", "group")
	}
	memberRelation, hasMember := groupType.Relations["member"]
	if !hasMember {
		return fmt.Errorf("SCIM requires group.member authorization relation")
	}
	hasUserSubject, hasGroupSubjectSet := false, false
	for _, subjectType := range memberRelation.SubjectTypes {
		if strings.TrimSpace(subjectType) == "subject" {
			hasUserSubject = true
		}
	}
	for _, target := range memberRelation.AllowedTargets {
		if target.SubjectType == "subject" {
			hasUserSubject = true
		}
		if target.SubjectSet != nil && target.SubjectSet.ResourceType == "group" && target.SubjectSet.Relation == "member" {
			hasGroupSubjectSet = true
		}
	}
	if !hasUserSubject || !hasGroupSubjectSet {
		return fmt.Errorf("SCIM requires group.member to permit both subject and group#member targets")
	}

	domainOwners := map[string]string{}
	projectionOwners := map[string]string{}
	for clientID, client := range clients {
		path := "server.scim.clients." + clientID
		if strings.TrimSpace(clientID) == "" || strings.TrimSpace(clientID) != clientID {
			return fmt.Errorf("server.scim.clients keys must be non-empty and trimmed")
		}
		if len(client.Credentials) > 2 {
			return fmt.Errorf("%s.credentials must contain at most two credentials", path)
		}
		credentialIDs := map[string]struct{}{}
		for i, credential := range client.Credentials {
			credentialPath := fmt.Sprintf("%s.credentials[%d]", path, i)
			id := strings.TrimSpace(credential.ID)
			if id == "" {
				return fmt.Errorf("%s.id is required", credentialPath)
			}
			if _, duplicate := credentialIDs[id]; duplicate {
				return fmt.Errorf("%s.id duplicates another credential in this client", credentialPath)
			}
			credentialIDs[id] = struct{}{}
		}
		if len(client.ActiveUserRelationships) == 0 {
			return fmt.Errorf("%s.activeUserRelationships must contain at least one relationship", path)
		}
		for i, rawDomain := range client.AuthoritativeUserDomains {
			domain := strings.ToLower(strings.TrimSpace(rawDomain))
			if domain == "" || domain != rawDomain || strings.Contains(domain, "@") {
				return fmt.Errorf("%s.authoritativeUserDomains[%d] must be a normalized domain", path, i)
			}
			if previous, ok := domainOwners[domain]; ok {
				return fmt.Errorf("%s.authoritativeUserDomains[%d] overlaps client %q", path, i, previous)
			}
			domainOwners[domain] = clientID
		}
		for i, projection := range client.ActiveUserRelationships {
			projectionPath := fmt.Sprintf("%s.activeUserRelationships[%d]", path, i)
			resourceTypeName := strings.TrimSpace(projection.Resource.Type)
			resourceType, ok := resourceTypes[resourceTypeName]
			if !ok {
				return fmt.Errorf("%s.resource.type references unknown resource type %q", projectionPath, resourceTypeName)
			}
			if strings.TrimSpace(projection.Resource.ID) == "" {
				return fmt.Errorf("%s.resource.id is required", projectionPath)
			}
			relationName := strings.TrimSpace(projection.Relation)
			relation, ok := resourceType.Relations[relationName]
			if !ok {
				return fmt.Errorf("%s.relation references unknown relation %q for resource type %q", projectionPath, relationName, resourceTypeName)
			}
			projectionKey := resourceTypeName + "\x00" + strings.TrimSpace(projection.Resource.ID) + "\x00" + relationName
			if previous, ok := projectionOwners[projectionKey]; ok {
				return fmt.Errorf("%s duplicates active projection configured by client %q", projectionPath, previous)
			}
			projectionOwners[projectionKey] = clientID
			hasSubjectTarget := false
			for _, subjectType := range relation.SubjectTypes {
				if strings.TrimSpace(subjectType) == "subject" {
					hasSubjectTarget = true
				}
			}
			for _, target := range relation.AllowedTargets {
				if target.SubjectType == "subject" {
					hasSubjectTarget = true
					break
				}
			}
			if !hasSubjectTarget {
				return fmt.Errorf("%s relation must permit direct subject targets for SCIM users", projectionPath)
			}
		}
	}
	return nil
}
