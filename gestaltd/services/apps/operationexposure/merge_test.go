package operationexposure

import "testing"

func TestMergeAllowedOperationsWithOverlay(t *testing.T) {
	t.Parallel()

	static := map[string]*OperationOverride{
		"get_item": {AllowedRoles: []string{"viewer"}},
		"delete":   {AllowedRoles: []string{"admin"}},
	}
	overlay := map[string]*OperationOverride{
		"get_item": {AllowedRoles: []string{"admin"}},
		"create":   {AllowedRoles: []string{"editor"}},
	}

	merged := MergeAllowedOperationsWithOverlay(static, overlay, []string{"delete"})
	if len(merged) != 2 {
		t.Fatalf("merged = %#v, want 2 operations", merged)
	}
	if roles := merged["get_item"].AllowedRoles; len(roles) != 1 || roles[0] != "admin" {
		t.Fatalf("get_item roles = %#v, want [admin]", roles)
	}
	if merged["create"] == nil {
		t.Fatal("expected runtime create operation")
	}
	if merged["delete"] != nil {
		t.Fatal("expected delete to be removed")
	}
}

func TestMergeAllowedOperationsWithOverlayReturnsStaticWhenEmptyOverlay(t *testing.T) {
	t.Parallel()

	static := map[string]*OperationOverride{
		"get_item": {AllowedRoles: []string{"viewer"}},
	}
	got := MergeAllowedOperationsWithOverlay(static, nil, nil)
	if len(got) != 1 || got["get_item"].AllowedRoles[0] != "viewer" {
		t.Fatalf("got = %#v, want cloned static baseline", got)
	}
	got["get_item"].AllowedRoles[0] = "admin"
	if static["get_item"].AllowedRoles[0] != "viewer" {
		t.Fatal("expected static baseline to remain unchanged")
	}
}

func TestMergeOverlayPatchAppliesDeltaWithoutDroppingExistingOverrides(t *testing.T) {
	t.Parallel()

	current := map[string]*OperationOverride{
		"create": {AllowedRoles: []string{"admin"}},
	}
	ops, removed := MergeOverlayPatch(current, nil, map[string]*OperationOverride{
		"get_item": {AllowedRoles: []string{"viewer"}},
	}, []string{"delete"})
	if len(ops) != 2 || ops["create"] == nil || ops["get_item"] == nil {
		t.Fatalf("ops = %#v, want create and get_item", ops)
	}
	if len(removed) != 1 || removed[0] != "delete" {
		t.Fatalf("removed = %#v, want [delete]", removed)
	}
}
