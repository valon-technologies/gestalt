package coredata

import (
	"time"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

func recString(rec indexeddb.Record, key string) string {
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

func recTime(rec indexeddb.Record, key string) time.Time {
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

func recTimePtr(rec indexeddb.Record, key string) *time.Time {
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
