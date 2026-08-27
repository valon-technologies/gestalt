package scim

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type filterClause struct {
	attribute string
	value     string
}

var filterClausePattern = regexp.MustCompile(`(?i)^\s*(externalId|userName|emails\.value|emails\[\s*type\s+eq\s+"work"\s*\]\.value)\s+eq\s+("(?:\\.|[^"\\])*")\s*$`)

func parseFilter(raw string) ([]filterClause, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts, err := splitFilterConjunctions(raw)
	if err != nil {
		return nil, err
	}
	clauses := make([]filterClause, 0, len(parts))
	for _, part := range parts {
		matches := filterClausePattern.FindStringSubmatch(part)
		if matches == nil {
			return nil, fmt.Errorf("unsupported filter clause %q", strings.TrimSpace(part))
		}
		var value string
		if err := json.Unmarshal([]byte(matches[2]), &value); err != nil {
			return nil, fmt.Errorf("invalid filter value: %w", err)
		}
		clauses = append(clauses, filterClause{attribute: strings.ToLower(matches[1]), value: normalize(value)})
	}
	return clauses, nil
}

func splitFilterConjunctions(raw string) ([]string, error) {
	var parts []string
	start, bracketDepth := 0, 0
	inString, escaped := false, false
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '\\':
			if inString {
				escaped = !escaped
			}
		case '"':
			if !escaped {
				inString = !inString
			}
			escaped = false
		case '[':
			if !inString {
				bracketDepth++
			}
		case ']':
			if !inString {
				bracketDepth--
				if bracketDepth < 0 {
					return nil, fmt.Errorf("unbalanced filter brackets")
				}
			}
		default:
			escaped = false
		}
		if inString || bracketDepth != 0 || i+5 > len(raw) {
			continue
		}
		if strings.EqualFold(raw[i:i+5], " and ") {
			parts = append(parts, raw[start:i])
			start = i + 5
			i += 4
		}
	}
	if inString || bracketDepth != 0 {
		return nil, fmt.Errorf("unterminated filter expression")
	}
	parts = append(parts, raw[start:])
	return parts, nil
}

func matchesFilter(user persistedUser, clauses []filterClause) bool {
	for _, clause := range clauses {
		matched := false
		switch clause.attribute {
		case "externalid":
			matched = normalize(user.ExternalID) == clause.value
		case "username":
			matched = normalize(user.UserName) == clause.value
		case "emails.value":
			for _, email := range user.Emails {
				matched = matched || normalize(email.Value) == clause.value
			}
		default:
			for _, email := range user.Emails {
				matched = matched || strings.EqualFold(strings.TrimSpace(email.Type), "work") && normalize(email.Value) == clause.value
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
