package egress

import "strings"

type MatchCriteria struct {
	SubjectKind SubjectKind
	SubjectID   string
	Provider    string
	Operation   string
	Method      string
	Host        string
	PathPrefix  string
}

func (c *MatchCriteria) Matches(subject Subject, target Target) bool {
	if c.SubjectKind != "" && c.SubjectKind != subject.Kind {
		return false
	}
	if c.SubjectID != "" && c.SubjectID != subject.ID {
		return false
	}
	if c.Provider != "" && c.Provider != target.Provider {
		return false
	}
	if c.Operation != "" && c.Operation != target.Operation {
		return false
	}
	if c.Method != "" && c.Method != target.Method {
		return false
	}
	if c.Host != "" && c.Host != target.Host {
		return false
	}
	if c.PathPrefix != "" && !MatchPathPrefix(c.PathPrefix, target.Path) {
		return false
	}
	return true
}

// MatchPathPrefix reports whether path begins with the given prefix,
// respecting path-segment boundaries. A prefix of "/" matches every path.
// Trailing slashes on the prefix are tolerated: "/repos/" is equivalent
// to "/repos" and matches "/repos", "/repos/", and "/repos/foo".
func MatchPathPrefix(prefix, path string) bool {
	if prefix == "" {
		return false
	}
	prefix = strings.TrimRight(prefix, "/")
	if prefix == "" {
		return true
	}
	if path == prefix {
		return true
	}
	return strings.HasPrefix(path, prefix+"/")
}
