package paraminterp

import (
	"regexp"
	"strings"
)

var re = regexp.MustCompile(`\{([^}]+)\}`)

// Interpolate replaces {key} placeholders in s with values from params.
// Unmatched placeholders are left unchanged.
func Interpolate(s string, params map[string]string) string {
	if len(params) == 0 || !strings.Contains(s, "{") {
		return s
	}
	return re.ReplaceAllStringFunc(s, func(match string) string {
		key := match[1 : len(match)-1]
		if val, ok := params[key]; ok {
			return val
		}
		return match
	})
}
