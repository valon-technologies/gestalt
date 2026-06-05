package access

import "testing"

func TestRequestBuilders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		req          Request
		resourceType string
		resourceID   string
		action       string
	}{
		{name: "app operation", req: AppOperation("example", "read"), resourceType: "example", resourceID: "example", action: "read"},
		{name: "provider", req: Provider("example"), resourceType: "example", resourceID: "example", action: ProviderAccessAction},
		{name: "ui role", req: UIRole("gestaltAdmin", "admin"), resourceType: "gestaltAdmin", resourceID: "gestaltAdmin", action: "admin"},
		{name: "authorization mutation", req: AuthorizationMutation("SetAuthorizationState"), resourceType: "AuthorizationProvider", resourceID: "authorization", action: "SetAuthorizationState"},
		{name: "workflow platform", req: WorkflowPlatform("workflow.run.start"), resourceType: "gestalt", resourceID: "gestalt", action: "workflow.run.start"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.req.Resource.Type != tt.resourceType || tt.req.Resource.Id != tt.resourceID {
				t.Fatalf("resource = %#v, want type %q id %q", tt.req.Resource, tt.resourceType, tt.resourceID)
			}
			if tt.req.Action.Name != tt.action {
				t.Fatalf("action = %q, want %q", tt.req.Action.Name, tt.action)
			}
		})
	}
}
