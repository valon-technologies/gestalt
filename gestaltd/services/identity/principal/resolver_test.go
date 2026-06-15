package principal

import "testing"

func TestPermissionSetFromScopesExplicitEmptyDeniesAll(t *testing.T) {
	t.Parallel()

	perms := PermissionSetFromScopes(nil)
	if perms != nil {
		t.Fatalf("permissions = %#v, want nil for empty scopes", perms)
	}
}
