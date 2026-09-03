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
	if got := MergeAllowedOperationsWithOverlay(static, nil, nil); got == nil || len(got) != 1 {
		t.Fatalf("got = %#v, want static baseline preserved", got)
	}
}
