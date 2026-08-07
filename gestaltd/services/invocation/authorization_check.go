package invocation

import (
	"context"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func SubjectAccessRequest(subjectID, action string, resource *proto.Resource) *proto.CheckAccessRequest {
	return &proto.CheckAccessRequest{
		Subject:  &proto.Subject{Type: "subject", Id: strings.TrimSpace(subjectID)},
		Action:   &proto.Action{Name: strings.TrimSpace(action)},
		Resource: resource,
	}
}

func CheckSubjectAccess(ctx context.Context, authorization core.AuthorizationProvider, req *proto.CheckAccessRequest) (bool, error) {
	resp, err := authorization.CheckAccess(ctx, req)
	if err != nil || resp == nil {
		return false, err
	}
	return resp.GetAllowed(), nil
}

// ResolveSubjectRole returns the effective role to expose to a provider after
// host authorization succeeds. Explicit relationships take precedence over the
// resource type's default role.
func ResolveSubjectRole(
	ctx context.Context,
	authorization core.AuthorizationProvider,
	subjectID string,
	resource *proto.Resource,
	allowedRoles []string,
) (string, error) {
	roles := make(map[string]struct{})
	pageToken := ""
	for {
		resp, err := authorization.ListRelationships(ctx, &proto.ListRelationshipsRequest{
			Filter: &proto.RelationshipFilter{
				Target: &proto.RelationshipTarget{
					Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{
						Type: "subject",
						Id:   strings.TrimSpace(subjectID),
					}},
				},
				Resource: resource,
			},
			PageSize:  500,
			PageToken: pageToken,
		})
		if err != nil {
			return "", err
		}
		for _, relationship := range resp.GetRelationships() {
			role := strings.TrimSpace(relationship.GetTuple().GetRelation())
			if role != "" {
				roles[role] = struct{}{}
			}
		}
		pageToken = strings.TrimSpace(resp.GetNextPageToken())
		if pageToken == "" {
			break
		}
	}

	if len(allowedRoles) > 0 {
		for _, allowedRole := range allowedRoles {
			role := strings.TrimSpace(allowedRole)
			if _, ok := roles[role]; ok {
				return role, nil
			}
		}
	} else if len(roles) > 0 {
		sortedRoles := make([]string, 0, len(roles))
		for role := range roles {
			sortedRoles = append(sortedRoles, role)
		}
		sort.Strings(sortedRoles)
		return sortedRoles[0], nil
	}

	defaultRole, err := resourceDefaultRole(ctx, authorization, resource.GetType())
	if err != nil {
		return "", err
	}
	if len(allowedRoles) == 0 || roleAllowed(defaultRole, allowedRoles) {
		return defaultRole, nil
	}
	return "", nil
}

func resourceDefaultRole(ctx context.Context, authorization core.AuthorizationProvider, resourceType string) (string, error) {
	resourceType = strings.TrimSpace(resourceType)
	resp, err := authorization.ListActiveModelResourceTypes(ctx, &proto.ListActiveModelResourceTypesRequest{
		Filter:   &proto.AuthorizationModelResourceTypeFilter{Name: resourceType},
		PageSize: 1,
	})
	if err != nil {
		return "", err
	}
	for _, candidate := range resp.GetResourceTypes() {
		if strings.TrimSpace(candidate.GetName()) == resourceType {
			return strings.TrimSpace(candidate.GetDefaultRole()), nil
		}
	}
	return "", nil
}

func roleAllowed(role string, allowedRoles []string) bool {
	role = strings.TrimSpace(role)
	if role == "" {
		return false
	}
	for _, allowedRole := range allowedRoles {
		if strings.TrimSpace(allowedRole) == role {
			return true
		}
	}
	return false
}
