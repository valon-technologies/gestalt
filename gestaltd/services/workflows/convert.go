package workflows

import (
	"github.com/valon-technologies/gestalt/server/services/internal/protoutil"
	"google.golang.org/protobuf/types/known/structpb"
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
