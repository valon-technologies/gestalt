package ptrutil

// Clone returns a pointer to a shallow copy of value, preserving nil.
func Clone[T any](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
