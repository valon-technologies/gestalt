package server

import "testing"

func TestPlatformAdminResourceNames(t *testing.T) {
	t.Parallel()

	if got := platformAdminResourceNames(""); len(got) != 2 || got[0] != "gestalt" || got[1] != "gestaltAdmin" {
		t.Fatalf("default names = %#v", got)
	}
	if got := platformAdminResourceNames("gestalt"); len(got) != 2 {
		t.Fatalf("gestalt names = %#v", got)
	}
	if got := platformAdminResourceNames("customPolicy"); len(got) != 1 || got[0] != "customPolicy" {
		t.Fatalf("custom names = %#v", got)
	}
}
