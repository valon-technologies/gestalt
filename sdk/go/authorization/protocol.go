package authorization

import (
	"encoding/json"
	"fmt"
	"math"

	"google.golang.org/protobuf/types/known/structpb"
)

func structFromMap(value map[string]any) (*structpb.Struct, error) {
	if value == nil {
		return nil, nil
	}
	normalized, err := normalizeJSON(value, "struct")
	if err != nil {
		return nil, err
	}
	object, ok := normalized.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("struct must be a JSON object")
	}
	return structpb.NewStruct(object)
}

func mapFromStruct(value *structpb.Struct) map[string]any {
	if value == nil {
		return nil
	}
	return value.AsMap()
}

func normalizeJSON(value any, path string) (any, error) {
	switch v := value.(type) {
	case nil, bool, string:
		return v, nil
	case int:
		return v, nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		return uint64(v), nil
	case uint8:
		return uint64(v), nil
	case uint16:
		return uint64(v), nil
	case uint32:
		return uint64(v), nil
	case uint64:
		return v, nil
	case float32:
		f := float64(v)
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return nil, fmt.Errorf("%s must be a finite number", path)
		}
		return f, nil
	case float64:
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return nil, fmt.Errorf("%s must be a finite number", path)
		}
		return v, nil
	case []any:
		out := make([]any, 0, len(v))
		for i, item := range v {
			normalized, err := normalizeJSON(item, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			out = append(out, normalized)
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			normalized, err := normalizeJSON(item, path+"."+key)
			if err != nil {
				return nil, err
			}
			out[key] = normalized
		}
		return out, nil
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i, nil
		}
		f, err := v.Float64()
		if err != nil {
			return nil, fmt.Errorf("%s must be a JSON number: %w", path, err)
		}
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return nil, fmt.Errorf("%s must be a finite number", path)
		}
		return f, nil
	default:
		return nil, fmt.Errorf("%s must be JSON-compatible, got %T", path, value)
	}
}
