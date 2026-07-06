package packageio

import (
	"fmt"
	"strings"
)

func NormalizeUIAllowedRoles(label string, allowedRoles []string) ([]string, error) {
	if len(allowedRoles) == 0 {
		return nil, fmt.Errorf("%s must not be empty", label)
	}
	roles := allowedRoles[:0]
	seenRoles := make(map[string]struct{}, len(allowedRoles))
	for i, role := range allowedRoles {
		role = strings.TrimSpace(role)
		if role == "" {
			return nil, fmt.Errorf("%s[%d] is required", label, i)
		}
		if _, exists := seenRoles[role]; exists {
			continue
		}
		seenRoles[role] = struct{}{}
		roles = append(roles, role)
	}
	return roles, nil
}
