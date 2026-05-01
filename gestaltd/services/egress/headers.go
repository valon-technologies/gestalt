package egress

import "net/http"

// HeaderAction describes how a header mutation should be applied.
type HeaderAction string

const (
	HeaderActionSet     HeaderAction = "set"
	HeaderActionRemove  HeaderAction = "remove"
	HeaderActionReplace HeaderAction = "replace"
)

// HeaderMutation applies a single case-insensitive header operation.
type HeaderMutation struct {
	Action HeaderAction
	Name   string
	Value  string
}

// ApplyHeaderMutations returns a new header map with the mutations applied.
func ApplyHeaderMutations(headers map[string]string, mutations []HeaderMutation) map[string]string {
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		out[http.CanonicalHeaderKey(k)] = v
	}

	for _, mutation := range mutations {
		name := http.CanonicalHeaderKey(mutation.Name)
		switch mutation.Action {
		case HeaderActionRemove:
			delete(out, name)
		case HeaderActionReplace:
			if _, ok := out[name]; ok {
				out[name] = mutation.Value
			}
		default:
			out[name] = mutation.Value
		}
	}

	return out
}
