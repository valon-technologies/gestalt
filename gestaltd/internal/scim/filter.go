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

type groupFilterClause struct {
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

var (
	groupFilterClausePattern = regexp.MustCompile(`(?i)^\s*(displayName|externalId)\s+eq\s+("(?:\\.|[^"\\])*")\s*$`)
	groupMemberFilterPattern = regexp.MustCompile(`(?i)^\s*members\[\s*value\s+eq\s+("(?:\\.|[^"\\])*")\s*\]\s*$`)
)

func parseGroupFilter(raw string) ([]groupFilterClause, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts, err := splitFilterConjunctions(raw)
	if err != nil {
		return nil, err
	}
	clauses := make([]groupFilterClause, 0, len(parts))
	for _, part := range parts {
		matches := groupFilterClausePattern.FindStringSubmatch(part)
		attribute := ""
		valueRaw := ""
		if matches != nil {
			attribute, valueRaw = strings.ToLower(strings.TrimSpace(matches[1])), matches[2]
		} else if value, ok := parseGroupMemberFilter(part); ok {
			clauses = append(clauses, groupFilterClause{attribute: "members.value", value: value})
			continue
		} else {
			return nil, fmt.Errorf("unsupported filter clause %q", strings.TrimSpace(part))
		}
		var value string
		if err := json.Unmarshal([]byte(valueRaw), &value); err != nil {
			return nil, fmt.Errorf("invalid filter value: %w", err)
		}
		clauses = append(clauses, groupFilterClause{attribute: attribute, value: normalize(value)})
	}
	return clauses, nil
}

func parseGroupMemberFilter(raw string) (string, bool) {
	matches := groupMemberFilterPattern.FindStringSubmatch(raw)
	if matches == nil {
		return "", false
	}
	var value string
	if err := json.Unmarshal([]byte(matches[1]), &value); err != nil {
		return "", false
	}
	return strings.TrimSpace(value), true
}

func matchesGroupFilter(group persistedGroup, clauses []groupFilterClause) bool {
	for _, clause := range clauses {
		matched := false
		switch clause.attribute {
		case "displayname":
			matched = normalize(group.DisplayName) == clause.value
		case "externalid":
			matched = normalize(group.ExternalID) == clause.value
		case "members.value":
			for _, member := range group.Members {
				if strings.TrimSpace(member.Value) == clause.value {
					matched = true
					break
				}
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
