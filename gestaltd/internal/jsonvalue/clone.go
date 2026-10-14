package jsonvalue

// Clone recursively copies JSON-shaped maps and slices. Other values are unchanged.
// Typed nil maps and slices become empty containers, matching non-nil empty inputs.
func Clone(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = Clone(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = Clone(typed[i])
		}
		return out
	default:
		return value
	}
}
