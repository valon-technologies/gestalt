package principal

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
)

func TestPermissionsForAPITokenExplicitEmptyDeniesAll(t *testing.T) {
	t.Parallel()

	perms, ok := permissionsForAPIToken(&core.APIToken{
		Permissions: []core.AccessPermission{},
	})
	if !ok {
		t.Fatal("permissionsForAPIToken did not treat explicit empty permissions as scoped")
	}
	if perms == nil || len(perms) != 0 {
		t.Fatalf("permissions = %#v, want explicit empty set", perms)
	}
}
