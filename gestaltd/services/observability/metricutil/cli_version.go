package metricutil

import "strings"

const (
	HeaderGestaltClientVersion = "X-Gestalt-Client-Version"

	ClientVersionUnknown = "unknown"

	maxCLIVersionHeaderLen = 64
	maxKnownCLIVersions    = 20
)

var knownCLIVersions = []string{
	"0.0.2-alpha.17",
}

func ClassifyKnownCLIVersion(raw string) string {
	return classifyKnownCLIVersion(knownCLIVersions, raw)
}

func classifyKnownCLIVersion(versions []string, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxCLIVersionHeaderLen {
		return ClientVersionUnknown
	}
	for _, known := range effectiveKnownCLIVersions(versions) {
		if raw == known {
			return known
		}
	}
	return ClientVersionUnknown
}

func effectiveKnownCLIVersions(versions []string) []string {
	if len(versions) <= maxKnownCLIVersions {
		return versions
	}
	return versions[len(versions)-maxKnownCLIVersions:]
}

func appendKnownCLIVersion(versions []string, version string, max int) []string {
	version = strings.TrimSpace(version)
	if version == "" || max <= 0 {
		return versions
	}
	for _, known := range versions {
		if known == version {
			return versions
		}
	}
	if len(versions) < max {
		return append(versions, version)
	}
	return append(versions[1:], version)
}
