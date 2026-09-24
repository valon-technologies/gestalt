package config

import "strings"

// ScimManagedGroupIDs returns platform-managed group ids from subject-set
// grants (gestalt/authorization). Runtime SCIM projection groups are added
// separately from the active runtime snapshot. App subject-set grants stay
// editable so local roster groups are not locked.
func ScimManagedGroupIDs(cfg *Config) map[string]struct{} {
	if cfg == nil {
		return map[string]struct{}{}
	}
	ids := map[string]struct{}{}
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
