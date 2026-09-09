package config

import "strings"

// ScimManagedGroupIDs returns Rippling-managed group ids: SCIM user
// projections plus platform subject-set grants (gestalt/authorization).
// App subject-set grants stay editable so local roster groups are not locked.
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
		if strings.TrimSpace(relationship.Resource.Type) == "app" {
			continue
		}
		subjectSet := relationship.Target.SubjectSet
		if subjectSet == nil || strings.TrimSpace(subjectSet.Resource.Type) != "group" {
			continue
		}
		id := strings.TrimSpace(subjectSet.Resource.ID)
		if id != "" {
			ids[id] = struct{}{}
		}
	}
	return ids
}
