package authorization

import (
	"testing"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func TestNilPropertiesEncodeAsEmptyStruct(t *testing.T) {
	wire, err := SubjectToProto(&Subject{Type: "user", ID: "u1"})
	if err != nil {
		t.Fatalf("SubjectToProto: %v", err)
	}
	if wire.GetProperties() == nil {
		t.Fatal("properties = nil, want empty Struct")
	}
	if len(wire.GetProperties().GetFields()) != 0 {
		t.Fatalf("properties fields = %#v, want empty", wire.GetProperties().GetFields())
	}
}

func TestUnsetOneofsRoundTrip(t *testing.T) {
	if _, ok := relationshipTargetFromProto(&proto.RelationshipTarget{}).(RelationshipTargetUnset); !ok {
		t.Fatalf("relationship target unset did not convert to RelationshipTargetUnset")
	}
	relationshipWire, err := protoRelationshipTarget(RelationshipTargetUnset{})
	if err != nil {
		t.Fatalf("protoRelationshipTarget: %v", err)
	}
	if relationshipWire.GetKind() != nil {
		t.Fatalf("relationship target kind = %#v, want nil", relationshipWire.GetKind())
	}

	allowedTargets := sliceModelAllowedTargetFromProto([]*proto.ModelAllowedTarget{{}})
	if len(allowedTargets) != 1 {
		t.Fatalf("allowed targets len = %d, want 1", len(allowedTargets))
	}
	if _, ok := allowedTargets[0].(ModelAllowedTargetUnset); !ok {
		t.Fatalf("model allowed target unset did not convert to ModelAllowedTargetUnset")
	}
	allowedWire, err := protoModelAllowedTarget(ModelAllowedTargetUnset{})
	if err != nil {
		t.Fatalf("protoModelAllowedTarget: %v", err)
	}
	if allowedWire.GetKind() != nil {
		t.Fatalf("model allowed target kind = %#v, want nil", allowedWire.GetKind())
	}
}
