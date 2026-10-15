package maputil

// ConvertNonEmpty converts each value, returning nil for an empty input map.
func ConvertNonEmpty[K comparable, T, R any](values map[K]T, convert func(T) R) map[K]R {
	if len(values) == 0 {
		return nil
	}
	return Convert(values, convert)
}

// Convert converts each value, returning an allocated map even for an empty input.
func Convert[K comparable, T, R any](values map[K]T, convert func(T) R) map[K]R {
	out := make(map[K]R, len(values))
	for key, value := range values {
		out[key] = convert(value)
	}
	return out
}
