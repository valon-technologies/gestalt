package config

import "net/http"

// NormalizeHeaders canonicalizes header names so overrides are deterministic
// regardless of the input casing.
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

// MergeHeaders overlays override on top of base using case-insensitive header
// names. Values from override win.
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
