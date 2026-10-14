package maputil

import "maps"

// CloneNonEmpty returns a shallow copy of values, or nil when values is empty.
func CloneNonEmpty[M ~map[K]V, K comparable, V any](values M) M {
	if len(values) == 0 {
		return nil
	}
	return maps.Clone(values)
}
