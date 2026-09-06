package config

import "strings"

// ScimManagedGroupIDs returns group resource ids provisioned by Rippling SCIM
// plus any bootstrap SCIM groups wired through authorization relationships.
func ScimManagedGroupIDs(cfg *Config) map[string]struct{} {
	ids := map[string]struct{}{}
	if cfg == nil {
		return ids
	}
	for _, client := range cfg.Server.SCIM.Clients {
		for _, projection := range client.ActiveUserRelationships {
			if strings.TrimSpace(projection.Resource.Type) != "group" {
				continue
			}
			id := strings.TrimSpace(projection.Resource.ID)
			if id != "" {
				ids[id] = struct{}{}
			}
		}
	}
	for _, relationship := range cfg.Authorization.Relationships {
		if !hasAuthorizationRelationshipTarget(relationship.Target) {
			continue
		}
		subjectSet := relationship.Target.SubjectSet
		if subjectSet == nil {
			continue
		}
		if strings.TrimSpace(subjectSet.Resource.Type) != "group" {
			continue
		}
		id := strings.TrimSpace(subjectSet.Resource.ID)
		if id != "" {
			ids[id] = struct{}{}
		}
	}
	return ids
}
