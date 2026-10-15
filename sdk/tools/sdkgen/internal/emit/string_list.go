package emit

import (
	"fmt"
	"strings"
)

// StringList renders quoted strings with the target language's list delimiters.
func StringList(values []string, empty, prefix, suffix string) string {
	if len(values) == 0 {
		return empty
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprintf("%q", v)
	}
	return prefix + strings.Join(parts, ", ") + suffix
}
