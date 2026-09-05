package providergateway

import "testing"

func TestAuthorizationMethodAccessClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method string
		read   bool
		write  bool
		ok     bool
	}{
		{method: "GetActiveModelRef", read: true, ok: true},
		{method: "ListActiveModelResourceTypes", read: true, ok: true},
		{method: "ListRelationships", read: true, ok: true},
		{method: "CheckAccess", read: true, ok: true},
		{method: "CheckAccessMany", read: true, ok: true},
		{method: "SetActiveModel", write: true, ok: true},
		{method: "SetAuthorizationState", write: true, ok: true},
		{method: "AddRelationship", write: true, ok: true},
		{method: "DeleteRelationship", write: true, ok: true},
		{method: "WriteRelationships", write: true, ok: true},
		{method: "Ping", ok: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.method, func(t *testing.T) {
			t.Parallel()
			fullMethod := "/gestalt.provider.v1.Authorization/" + tc.method
			read, write, ok := authorizationMethodAccessClass(fullMethod)
			if read != tc.read || write != tc.write || ok != tc.ok {
				t.Fatalf("authorizationMethodAccessClass(%q) = (%v, %v, %v), want (%v, %v, %v)", fullMethod, read, write, ok, tc.read, tc.write, tc.ok)
			}
		})
	}
}

func TestAuthorizationPublicActions(t *testing.T) {
	t.Parallel()

	if got := authorizationPublicActions(true, false); len(got) != 3 || got[0] != "viewer" || got[1] != "admin" || got[2] != legacyAuthorizationAction {
		t.Fatalf("read actions = %#v", got)
	}
	if got := authorizationPublicActions(false, true); len(got) != 2 || got[0] != "admin" || got[1] != legacyAuthorizationAction {
		t.Fatalf("write actions = %#v", got)
	}
}
