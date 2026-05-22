package declarative

import "strings"

const MaxDescriptionLen = 200

func TruncateDescription(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	runes := []rune(s)
	if len(runes) <= MaxDescriptionLen {
		return s
	}
	truncated := string(runes[:MaxDescriptionLen])
	if i := strings.LastIndexByte(truncated, ' '); i > len(truncated)/2 {
		truncated = truncated[:i]
	}
	return truncated + "..."
}
