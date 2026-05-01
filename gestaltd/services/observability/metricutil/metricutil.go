package metricutil

import "strings"

// UnknownAttrValue is recorded when a metric attribute is blank or missing.
const UnknownAttrValue = "unknown"

// AttrValue normalizes blank metric attribute values.
func AttrValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return UnknownAttrValue
	}
	return value
}

// ResultValue returns the standard success or error label for a metric result.
func ResultValue(failed bool) string {
	if failed {
		return "error"
	}
	return "success"
}
