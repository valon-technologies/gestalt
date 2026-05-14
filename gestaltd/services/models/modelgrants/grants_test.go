package modelgrants

import (
	"reflect"
	"testing"
)

func TestGrantsEncodeDecodeAndSupport(t *testing.T) {
	t.Parallel()

	if !IsSupportedOperation(" model.generate ") {
		t.Fatal("model.generate should be supported")
	}
	if IsSupportedOperation("model.unknown") {
		t.Fatal("model.unknown should not be supported")
	}
	if got := SupportedOperations(); !reflect.DeepEqual(got, []string{OperationGenerate}) {
		t.Fatalf("SupportedOperations = %#v, want model.generate", got)
	}
	if got := EncodeClaims(Grants{"z": {}, OperationGenerate: {}}); !reflect.DeepEqual(got, []string{OperationGenerate, "z"}) {
		t.Fatalf("EncodeClaims = %#v, want sorted operations", got)
	}
	decoded := DecodeClaims([]string{" ", " model.generate "})
	if !decoded.Allows("model.generate") || !decoded.Allows(" model.generate ") {
		t.Fatalf("decoded grants = %#v, want model.generate allowed", decoded)
	}
	if decoded.Allows("model.unknown") {
		t.Fatal("decoded grants should not allow unknown operation")
	}
}
