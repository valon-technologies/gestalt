package metricutil

import (
	"strings"

	"golang.org/x/mod/semver"
)

const (
	HeaderGestaltClientVersion = "X-Gestalt-Client-Version"

	ClientVersionUnknown = "unknown"

	maxCLIVersionHeaderLen = 64
)

func NormalizeCLIVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxCLIVersionHeaderLen {
		return ClientVersionUnknown
	}

	canonical := raw
	if !strings.HasPrefix(canonical, "v") {
		canonical = "v" + canonical
	}
	if !semver.IsValid(canonical) {
		return ClientVersionUnknown
	}

	base := canonical
	if i := strings.IndexByte(base, '+'); i != -1 {
		base = base[:i]
	}
	if semver.Canonical(canonical) != base {
		return ClientVersionUnknown
	}

	return raw
}
