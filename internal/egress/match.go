package egress

import "strings"

// MatchPathPrefix reports whether path begins with the given prefix,
// respecting path-segment boundaries. A prefix of "/" matches every path.
// Trailing slashes on the prefix are tolerated: "/repos/" is equivalent
// to "/repos" and matches "/repos", "/repos/", and "/repos/foo".
func MatchPathPrefix(prefix, path string) bool {
	prefix = strings.TrimRight(prefix, "/")
	if prefix == "" {
		return true
	}
	if path == prefix {
		return true
	}
	return strings.HasPrefix(path, prefix+"/")
}
