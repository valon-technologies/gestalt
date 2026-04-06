package pluginhost

import (
	"encoding/json"

	"github.com/valon-technologies/gestalt/server/core/catalog"

	"google.golang.org/protobuf/types/known/structpb"
)

func structFromMap(values map[string]any) (*structpb.Struct, error) {
	if len(values) == 0 {
		return nil, nil
	}
	return structpb.NewStruct(values)
}

func mapFromStruct(s *structpb.Struct) map[string]any {
	if s == nil {
		return nil
	}
	return s.AsMap()
}

func catalogToJSON(cat *catalog.Catalog) (string, error) {
	if cat == nil {
		return "", nil
	}
	data, err := json.Marshal(cat)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func catalogFromJSON(raw string) (*catalog.Catalog, error) {
	if raw == "" {
		return nil, nil
	}
	var cat catalog.Catalog
	if err := json.Unmarshal([]byte(raw), &cat); err != nil {
		return nil, err
	}
	if err := cat.Validate(); err != nil {
		return nil, err
	}
	return &cat, nil
}
