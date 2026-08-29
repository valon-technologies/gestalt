package metricutil

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/semvervalidate"
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
	if !semvervalidate.Valid(strings.TrimPrefix(raw, "v")) {
		return ClientVersionUnknown
	}
	return raw
}
