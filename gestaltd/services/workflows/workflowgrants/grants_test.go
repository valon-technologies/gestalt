package workflowgrants

import "testing"

func TestClaimsRoundTripPreservesNilAndEmptyDenyAll(t *testing.T) {
	t.Parallel()

	if got := DecodeClaims(EncodeClaims(nil)); got != nil {
		t.Fatalf("nil grants round trip = %#v, want nil deny-all grants", got)
	} else if got.Allows(OperationEventsPublish) {
		t.Fatalf("nil grants allow %q, want denied", OperationEventsPublish)
	}

	got := DecodeClaims(EncodeClaims(Grants{}))
	if got == nil {
		t.Fatal("empty grants round trip = nil, want explicit empty deny-all grants")
	}
	if got.Allows(OperationEventsPublish) {
		t.Fatalf("empty grants allow %q, want denied", OperationEventsPublish)
	}
}
