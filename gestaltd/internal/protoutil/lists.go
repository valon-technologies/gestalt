package protoutil

// ConvertNonNilSlice preserves order while skipping nil entries. Empty input
// returns nil; nonempty input containing only nils returns an allocated empty slice.
func ConvertNonNilSlice[T, R any](values []*T, convert func(*T) R) []R {
	if len(values) == 0 {
		return nil
	}
	out := make([]R, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, convert(value))
	}
	return out
}
