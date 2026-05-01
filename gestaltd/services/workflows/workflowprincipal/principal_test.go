package workflowprincipal

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestFromExecutionReferencePreservesNilPermissionsAsUnrestricted(t *testing.T) {
	t.Parallel()

	p := FromExecutionReference(&coreworkflow.ExecutionReference{
		SubjectID: "system:workflow:test",
	})
	if p == nil {
		t.Fatal("principal is nil")
	}
	if p.TokenPermissions != nil {
		t.Fatalf("TokenPermissions = %#v, want nil unrestricted permissions", p.TokenPermissions)
	}
	if !principal.AllowsProviderPermission(p, "github") {
		t.Fatal("nil execution ref permissions should allow provider access")
	}
}

func TestFromExecutionReferencePreservesEmptyPermissionsAsDenyAll(t *testing.T) {
	t.Parallel()

	p := FromExecutionReference(&coreworkflow.ExecutionReference{
		SubjectID:   "system:workflow:test",
		Permissions: []core.AccessPermission{},
	})
	if p == nil {
		t.Fatal("principal is nil")
	}
	if p.TokenPermissions == nil {
		t.Fatal("TokenPermissions is nil, want non-nil empty deny-all permissions")
	}
	if principal.AllowsProviderPermission(p, "github") {
		t.Fatal("empty execution ref permissions should deny provider access")
	}
}
