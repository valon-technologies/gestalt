package metricutil

import (
	"strings"
	"unicode"
)

const (
	HeaderGestaltClientVersion = "X-Gestalt-Client-Version"

	ClientVersionUnknown = "unknown"

	maxCLIVersionHeaderLen = 64
	maxKnownCLIVersions    = 20
)

// knownCLIVersions is the server-owned allowlist of CLI releases that may be
// emitted as gestaltd.client.version. When full, add the next release and drop
// the oldest entry before deploying gestaltd and publishing the CLI.
var knownCLIVersions = []string{
	"0.0.2-alpha.17",
}

// ClassifyKnownCLIVersion returns the canonical allowlisted semver for raw or
// ClientVersionUnknown when the header is missing, malformed, oversized, or
// unrecognized.
func ClassifyKnownCLIVersion(raw string) string {
	return classifyVersionAgainstAllowlist(knownCLIVersions, raw)
}

func classifyVersionAgainstAllowlist(allowlist []string, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxCLIVersionHeaderLen || !validCLIVersionFormat(raw) {
		return ClientVersionUnknown
	}
	for _, known := range allowlist {
		if raw == known {
			return known
		}
	}
	return ClientVersionUnknown
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

func validCLIVersionFormat(version string) bool {
	parts := strings.SplitN(version, "-", 2)
	core := parts[0]
	segments := strings.Split(core, ".")
	if len(segments) != 3 {
		return false
	}
	for _, segment := range segments {
		if segment == "" || !isNumericSegment(segment) {
			return false
		}
	}
	if len(parts) == 1 {
		return true
	}
	prerelease := parts[1]
	if prerelease == "" {
		return false
	}
	for _, r := range prerelease {
		switch {
		case unicode.IsDigit(r):
		case unicode.IsLetter(r):
		case r == '.' || r == '-':
		default:
			return false
		}
	}
	return true
}

func isNumericSegment(segment string) bool {
	for _, r := range segment {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
