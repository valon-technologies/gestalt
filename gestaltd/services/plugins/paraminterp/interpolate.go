package paraminterp

import (
	"fmt"
	"regexp"
	"strings"
)

var re = regexp.MustCompile(`\{([^}]+)\}`)
var safeURLTemplateValue = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

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

// ValidateURLTemplateParams rejects values that can inject URL authority or
// path delimiters when substituted into connection-scoped URL templates.
func ValidateURLTemplateParams(template string, params map[string]string) error {
	if len(params) == 0 || !strings.Contains(template, "{") {
		return nil
	}
	for _, match := range re.FindAllStringSubmatch(template, -1) {
		if len(match) != 2 {
			continue
		}
		key := match[1]
		value, ok := params[key]
		if !ok {
			continue
		}
		if !safeURLTemplateValue.MatchString(value) {
			return fmt.Errorf("connection parameter %q contains invalid URL template characters", key)
		}
	}
	return nil
}
