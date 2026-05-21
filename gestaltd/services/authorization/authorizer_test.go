package authorization

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestDefaultAllowAppliesToUsersAndServiceAccounts(t *testing.T) {
	t.Parallel()

	authz, err := New(StaticConfig{
		Policies: map[string]StaticSubjectPolicy{
			"default_allow": {Default: "allow"},
		},
		ProviderPolicies: map[string]string{
			"svc": "default_allow",
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, tc := range []struct {
		name      string
		principal *principal.Principal
		wantAllow bool
	}{
		{
			name:      "user subject",
			principal: &principal.Principal{SubjectID: "user:u-123"},
			wantAllow: true,
		},
		{
			name:      "service account subject",
			principal: &principal.Principal{SubjectID: "service_account:triage-bot"},
			wantAllow: true,
		},
		{
			name:      "managed subject",
			principal: &principal.Principal{SubjectID: "subject:deploy-managed-bot"},
			wantAllow: false,
		},
		{
			name:      "system subject",
			principal: &principal.Principal{SubjectID: "system:workflow"},
			wantAllow: false,
		},
		{
			name:      "empty subject",
			principal: &principal.Principal{},
			wantAllow: false,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			access, allowed := authz.ResolvePolicyAccess(context.Background(), tc.principal, "default_allow")
			if allowed != tc.wantAllow {
				t.Fatalf("ResolveAccess allowed = %v, want %v", allowed, tc.wantAllow)
			}
			if tc.wantAllow && access.Role != defaultSubjectRole {
				t.Fatalf("ResolveAccess role = %q, want %q", access.Role, defaultSubjectRole)
			}
		})
	}
}
