package workflowgrants

import "testing"

func TestClaimsRoundTripPreservesNilAllowAllAndEmptyDenyAll(t *testing.T) {
	t.Parallel()

	if got := DecodeClaims(EncodeClaims(nil)); got != nil {
		t.Fatalf("nil grants round trip = %#v, want nil allow-all grants", got)
	}

	got := DecodeClaims(EncodeClaims(Grants{}))
	if got == nil {
		t.Fatal("empty grants round trip = nil, want explicit empty deny-all grants")
	}
	if got.Allows(OperationEventsPublish) {
		t.Fatalf("empty grants allow %q, want denied", OperationEventsPublish)
	}
}
