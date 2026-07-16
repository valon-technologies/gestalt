package coredata

import (
	"encoding/json"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

func recString(rec idb.Record, key string) string {
	v, ok := rec[key]
	if !ok || v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return ""
	}
}

func recTime(rec idb.Record, key string) time.Time {
	v, ok := rec[key]
	if !ok || v == nil {
		return time.Time{}
	}
	switch t := v.(type) {
	case time.Time:
		return t
	case *time.Time:
		if t == nil {
			return time.Time{}
		}
		return *t
	case string:
		parsed, err := time.Parse(time.RFC3339, t)
		if err != nil {
			parsed, _ = time.Parse("2006-01-02 15:04:05", t)
		}
		return parsed
	default:
		return time.Time{}
	}
}

func recAnyMap(rec idb.Record, key string) map[string]any {
	raw := recJSON(rec, key)
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]any)
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func recJSON(rec idb.Record, key string) []byte {
	v, ok := rec[key]
	if !ok || v == nil {
		return nil
	}
	switch raw := v.(type) {
	case []byte:
		return raw
	case string:
		return []byte(raw)
	case json.RawMessage:
		return raw
	case map[string]any:
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		return encoded
	default:
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		return encoded
	}
}

func jsonValue(v any) any {
	if v == nil {
		return nil
	}
	switch value := v.(type) {
	case map[string]string:
		if len(value) == 0 {
			return nil
		}
		out := make(map[string]any, len(value))
		for k, v := range value {
			out[k] = v
		}
		return out
	case map[string]any:
		if len(value) == 0 {
			return nil
		}
		out := make(map[string]any, len(value))
		for k, v := range value {
			out[k] = jsonValue(v)
		}
		return out
	default:
		return v
	}
}

func recTimePtr(rec idb.Record, key string) *time.Time {
	v, ok := rec[key]
	if !ok || v == nil {
		return nil
	}
	switch t := v.(type) {
	case time.Time:
		if t.IsZero() {
			return nil
		}
		return &t
	case *time.Time:
		return t
	case string:
		if t == "" {
			return nil
		}
		parsed, err := time.Parse(time.RFC3339, t)
		if err != nil {
			parsed, _ = time.Parse("2006-01-02 15:04:05", t)
		}
		if parsed.IsZero() {
			return nil
		}
		return &parsed
	default:
		return nil
	}
}
