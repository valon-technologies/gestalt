package headerutil

import (
	"net/http"
	"sort"
)

func NormalizeHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}

	out := make(map[string]string, len(headers))
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := headers[k]
		out[http.CanonicalHeaderKey(k)] = v
	}
	return out
}

func MergeHeaders(base, override map[string]string) map[string]string {
	out := NormalizeHeaders(base)
	if len(override) == 0 {
		return out
	}
	if out == nil {
		out = make(map[string]string, len(override))
	}
	for k, v := range override {
		out[http.CanonicalHeaderKey(k)] = v
	}
	return out
}
