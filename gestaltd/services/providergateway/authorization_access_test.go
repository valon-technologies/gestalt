package providergateway

import "testing"

func TestAuthorizationPublicActions(t *testing.T) {
	t.Parallel()

	if got := authorizationPublicActions(true, false); len(got) != 3 || got[0] != "viewer" || got[1] != "admin" || got[2] != legacyAuthorizationAction {
		t.Fatalf("read actions = %#v", got)
	}
	if got := authorizationPublicActions(false, true); len(got) != 2 || got[0] != "admin" || got[1] != legacyAuthorizationAction {
		t.Fatalf("write actions = %#v", got)
	}
}
