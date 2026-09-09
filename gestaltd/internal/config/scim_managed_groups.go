package config

import "strings"

// KnownGroupDisplayNames are production group ids that predate persisted
// displayName properties. Keep in sync with home group-display-name.ts.
var KnownGroupDisplayNames = map[string]string{
	"e7dce358-8291-431f-baf0-fdb8a10b4252": "Valon Employees",
	"9a283acd-f348-40f0-b22c-b8a05ea7b574": "Gestalt Team",
	"187a1a6e-685f-407c-b919-138c68b98b4e": "Carrington",
	"servicemacusa-employees":              "ServiceMac employees",
	"4a78ccd4-d32c-405a-b42d-ed862e19eea4": "Unnamed group",
}

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

// GroupDisplayName returns a stored config label, then a known production
// name, then a hyphenated slug. UUID ids with no label stay as the id.
func GroupDisplayName(cfg *Config, groupID string) string {
	id := strings.TrimSpace(groupID)
	if id == "" {
		return ""
	}
	if name := configuredGroupDisplayName(cfg, id); name != "" {
		return name
	}
	if name := strings.TrimSpace(KnownGroupDisplayNames[id]); name != "" {
		return name
	}
	if !looksLikeUUID(id) {
		return humanizeGroupSlug(id)
	}
	return id
}

func configuredGroupDisplayName(cfg *Config, groupID string) string {
	if cfg == nil {
		return ""
	}
	for _, client := range cfg.Server.SCIM.Clients {
		for _, projection := range client.ActiveUserRelationships {
			if strings.TrimSpace(projection.Resource.Type) != "group" {
				continue
			}
			if strings.TrimSpace(projection.Resource.ID) != groupID {
				continue
			}
			if name := strings.TrimSpace(projection.Resource.Properties["displayName"]); name != "" {
				return name
			}
		}
	}
	for i := range cfg.Authorization.Relationships {
		relationship := &cfg.Authorization.Relationships[i]
		if name := resourceDisplayName(relationship.Resource, groupID); name != "" {
			return name
		}
		if relationship.Target.SubjectSet != nil {
			if name := resourceDisplayName(relationship.Target.SubjectSet.Resource, groupID); name != "" {
				return name
			}
		}
	}
	return ""
}

func resourceDisplayName(resource AuthorizationResourceDef, groupID string) string {
	if strings.TrimSpace(resource.Type) != "group" {
		return ""
	}
	if strings.TrimSpace(resource.ID) != groupID {
		return ""
	}
	return strings.TrimSpace(resource.Properties["displayName"])
}

func looksLikeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if r != '-' {
				return false
			}
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func humanizeGroupSlug(groupID string) string {
	parts := strings.FieldsFunc(groupID, func(r rune) bool {
		return r == '-' || r == '_'
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}
