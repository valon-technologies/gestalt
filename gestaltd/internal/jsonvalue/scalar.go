package jsonvalue

import "encoding/json"

// IsScalar reports whether value is representable as a scalar JSON value.
func IsScalar(value any) bool {
	switch value.(type) {
	case nil, string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return true
	default:
		return false
	}
}
