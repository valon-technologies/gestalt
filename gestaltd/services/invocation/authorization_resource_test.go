package invocation

import (
	"testing"
)

func TestAuthorizationResource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		kinds map[string]ProviderKind
		want  struct{ typ, id string }
	}{
		{
			name:  "category a app",
			kinds: map[string]ProviderKind{"slack": ProviderKindApp},
			want:  struct{ typ, id string }{"app", "slack"},
		},
		{
			name:  "dedicated resource type fallback",
			kinds: map[string]ProviderKind{"slack": ProviderKindApp},
			want:  struct{ typ, id string }{"legacyApp", "legacyApp"},
		},
		{
			name:  "workflow provider",
			kinds: map[string]ProviderKind{"nightly": ProviderKindWorkflow},
			want:  struct{ typ, id string }{"workflow", "nightly"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resourceName := "slack"
			if tc.name == "dedicated resource type fallback" {
				resourceName = "legacyApp"
			}
			if tc.name == "workflow provider" {
				resourceName = "nightly"
			}
			got := AuthorizationResource(resourceName, tc.kinds)
			if got.GetType() != tc.want.typ || got.GetId() != tc.want.id {
				t.Fatalf("AuthorizationResource(%q) = {%q, %q}, want {%q, %q}", resourceName, got.GetType(), got.GetId(), tc.want.typ, tc.want.id)
			}
		})
	}
}

func TestAuthorizationResourceMapperResolvesPolicyAliasFirst(t *testing.T) {
	t.Parallel()

	mapper := NewAuthorizationResourceMapper(
		map[string]ProviderKind{"traffic-cop": ProviderKindApp},
		map[string]string{"traffic-cop": "trafficCop"},
	)

	if got := mapper.Policy("traffic-cop"); got != "trafficCop" {
		t.Fatalf("Policy = %q, want trafficCop", got)
	}
	resource := mapper.Resource("traffic-cop")
	if resource.GetType() != "trafficCop" || resource.GetId() != "trafficCop" {
		t.Fatalf("Resource = %v, want trafficCop/trafficCop", resource)
	}
}

func TestAuthorizationResourceMapperFallsBackToProviderKind(t *testing.T) {
	t.Parallel()

	mapper := NewAuthorizationResourceMapper(map[string]ProviderKind{"slack": ProviderKindApp}, nil)

	if got := mapper.Policy("slack"); got != "slack" {
		t.Fatalf("Policy = %q, want slack", got)
	}
	resource := mapper.Resource(" slack ")
	if resource.GetType() != "app" || resource.GetId() != "slack" {
		t.Fatalf("Resource = %v, want app/slack", resource)
	}
}

func TestAuthorizationResourceMapperZeroValueIsUsable(t *testing.T) {
	t.Parallel()

	var mapper AuthorizationResourceMapper
	resource := mapper.Resource("unknown")
	if resource.GetType() != "unknown" || resource.GetId() != "unknown" {
		t.Fatalf("Resource = %v, want unknown/unknown", resource)
	}
}
