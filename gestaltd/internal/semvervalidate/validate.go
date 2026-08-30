package semvervalidate

import (
	"strings"

	"golang.org/x/mod/semver"
)

// Valid reports whether version is a canonical semver without a leading v prefix.
func Valid(version string) bool {
	version = strings.TrimSpace(version)
	if version == "" || strings.HasPrefix(version, "v") {
		return false
	}

	canonical := "v" + version
	if !semver.IsValid(canonical) {
		return false
	}

	base := canonical
	if i := strings.IndexByte(base, '+'); i != -1 {
		base = base[:i]
	}
	return semver.Canonical(canonical) == base
}
