package config

import "testing"

func TestScimManagedGroupIDsUsesActiveUserRelationshipsOnly(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Server: ServerConfig{
			SCIM: ServerSCIMConfig{
				Clients: map[string]SCIMClientConfig{
					"rippling": {
						ActiveUserRelationships: []SCIMRelationshipConfig{{
							Relation: "member",
							Resource: AuthorizationResourceDef{Type: "group", ID: "valon-employees"},
						}},
					},
				},
			},
		},
		Authorization: AuthorizationConfig{
			Relationships: []AuthorizationRelationshipDef{
				{
					Relation: "admin",
					Resource: AuthorizationResourceDef{Type: "gestalt", ID: "gestalt"},
					Target: AuthorizationRelationshipTargetDef{
						SubjectSet: &AuthorizationSubjectSetDef{
							Resource: AuthorizationResourceDef{Type: "group", ID: "gestalt-team"},
							Relation: "member",
						},
					},
				},
				{
					Relation: "viewer",
					Resource: AuthorizationResourceDef{Type: "app", ID: "home"},
					Target: AuthorizationRelationshipTargetDef{
						SubjectSet: &AuthorizationSubjectSetDef{
							Resource: AuthorizationResourceDef{Type: "group", ID: "servicemacusa-employees"},
							Relation: "member",
						},
					},
				},
			},
		},
	}

	ids := ScimManagedGroupIDs(cfg)
	if _, ok := ids["valon-employees"]; !ok {
		t.Fatalf("ids = %#v, want valon-employees", ids)
	}
	if _, ok := ids["gestalt-team"]; !ok {
		t.Fatalf("ids = %#v, want gestalt-team", ids)
	}
	if _, ok := ids["servicemacusa-employees"]; ok {
		t.Fatalf("ids = %#v, app subject-set group must not be SCIM-managed", ids)
	}
}

func TestGroupDisplayNameUsesConfigThenKnownThenSlug(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Server: ServerConfig{
			SCIM: ServerSCIMConfig{
				Clients: map[string]SCIMClientConfig{
					"rippling": {
						ActiveUserRelationships: []SCIMRelationshipConfig{{
							Relation: "member",
							Resource: AuthorizationResourceDef{
								Type:       "group",
								ID:         "e7dce358-8291-431f-baf0-fdb8a10b4252",
								Properties: map[string]string{"displayName": "Valon Employees"},
							},
						}},
					},
				},
			},
		},
	}
	if got := GroupDisplayName(cfg, "e7dce358-8291-431f-baf0-fdb8a10b4252"); got != "Valon Employees" {
		t.Fatalf("config name = %q", got)
	}
	if got := GroupDisplayName(nil, "servicemacusa-employees"); got != "ServiceMac employees" {
		t.Fatalf("known name = %q", got)
	}
	if got := GroupDisplayName(nil, "partner-ops"); got != "Partner Ops" {
		t.Fatalf("slug name = %q", got)
	}
}
