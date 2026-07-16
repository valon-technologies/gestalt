package emit

import "strings"

// PrefixScope returns a stale scope that owns only paths under prefix.
func PrefixScope(prefix string) func(rel string) bool {
	return func(rel string) bool {
		return strings.HasPrefix(rel, prefix)
	}
}

// ExcludePrefixScope returns a stale scope that owns every path except those
// under prefix.
func ExcludePrefixScope(prefix string) func(rel string) bool {
	return func(rel string) bool {
		return !strings.HasPrefix(rel, prefix)
	}
}
