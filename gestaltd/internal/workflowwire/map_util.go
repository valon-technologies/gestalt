package workflowwire

import (
	"fmt"
	"strings"
)

func asMap(value any) (map[string]any, bool) {
	m, ok := value.(map[string]any)
	return m, ok
}

func asArray(value any) ([]any, bool) {
	if value == nil {
		return nil, true
	}
	switch typed := value.(type) {
	case []any:
		return typed, true
	case []map[string]any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = typed[i]
		}
		return out, true
	default:
		return nil, false
	}
}

func stringArg(args map[string]any, key string) string {
	if value, ok := args[key]; ok {
		if s, ok := value.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func intArg(args map[string]any, key string) int {
	value, ok := args[key]
	if !ok || value == nil {
		return 0
	}
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func rejectUnknownKeys(m map[string]any, path string, allowed ...string) error {
	if len(m) == 0 {
		return nil
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for key := range m {
		if _, ok := allowedSet[key]; !ok {
			return fmt.Errorf("%w: %s has unknown field %q", ErrInvalid, path, key)
		}
	}
	return nil
}

func objectArg(args map[string]any, key, path string) (map[string]any, error) {
	value, ok := args[key]
	if !ok || value == nil {
		return nil, nil
	}
	out, ok := asMap(value)
	if !ok {
		return nil, fmt.Errorf("%w: %s must be an object", ErrInvalid, path+"."+key)
	}
	return mapDeepClone(out), nil
}

func deepClone(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = deepClone(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, deepClone(item))
		}
		return out
	default:
		return typed
	}
}

func mapDeepClone(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = deepClone(value)
	}
	return out
}
