package config

import "net/http"

func NormalizeHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}

	out := make(map[string]string, len(headers))
	for k, v := range headers {
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
