package jsonyaml

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Decode parses either JSON or YAML into plain Go values using the JSON data model.
func Decode(data []byte) (any, error) {
	var doc any
	if err := json.Unmarshal(data, &doc); err == nil {
		return doc, nil
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return normalize(doc)
}

func normalize(v any) (any, error) {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			normalized, err := normalize(child)
			if err != nil {
				return nil, err
			}
			out[k] = normalized
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			key, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("yaml map key must be a string, got %T", k)
			}
			normalized, err := normalize(child)
			if err != nil {
				return nil, err
			}
			out[key] = normalized
		}
		return out, nil
	case []any:
		out := make([]any, len(val))
		for i, child := range val {
			normalized, err := normalize(child)
			if err != nil {
				return nil, err
			}
			out[i] = normalized
		}
		return out, nil
	default:
		return val, nil
	}
}
