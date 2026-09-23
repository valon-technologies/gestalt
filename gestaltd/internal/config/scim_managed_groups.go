package config

import "strings"

// ManagedGroupIDs returns the group ids managed by SCIM user projections.
func ManagedGroupIDs(cfg ServerSCIMConfig) map[string]struct{} {
	ids := map[string]struct{}{}
	for _, client := range cfg.Clients {
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
	return ids
}

// ScimManagedGroupIDs returns Rippling-managed group ids: SCIM user
// projections plus platform subject-set grants (gestalt/authorization).
// App subject-set grants stay editable so local roster groups are not locked.
func ScimManagedGroupIDs(cfg *Config) map[string]struct{} {
	if cfg == nil {
		return map[string]struct{}{}
	}
	ids := ManagedGroupIDs(cfg.Server.SCIM)
	for i := range cfg.Authorization.Relationships {
		relationship := &cfg.Authorization.Relationships[i]
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
