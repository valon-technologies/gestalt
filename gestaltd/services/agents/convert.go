package agents

import (
	"time"

	"github.com/valon-technologies/gestalt/server/services/internal/protoutil"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func structFromMap(values map[string]any) (*structpb.Struct, error) {
	return protoutil.StructFromMap(values)
}

func mapFromStruct(s *structpb.Struct) map[string]any {
	return protoutil.MapFromStruct(s)
}

func protoValueToAny(v *structpb.Value) any {
	return protoutil.ValueToAny(v)
}

func protoValueFromAny(value any) (*structpb.Value, error) {
	return protoutil.ValueFromAny(value)
}

func timeToProto(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func timeFromProto(t *timestamppb.Timestamp) *time.Time {
	if t == nil {
		return nil
	}
	value := t.AsTime()
	return &value
}
