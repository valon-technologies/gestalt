package access

import (
	"errors"
	"strings"
)

var (
	ErrNotAuthenticated = errors.New("not authenticated")
	ErrDenied           = errors.New("authorization denied")
	ErrScopeDenied      = errors.New("token scope denied")

	errPolicyUnavailable = errors.New("authorization policy unavailable")
)

func accessDetails(resource, action string) string {
	resource = strings.TrimSpace(resource)
	action = strings.TrimSpace(action)
	switch {
	case resource != "" && action != "":
		return resource + " " + action
	case resource != "":
		return resource
	case action != "":
		return action
	default:
		return ""
	}
}

func IsPolicyUnavailable(err error) bool {
	return errors.Is(err, errPolicyUnavailable)
}
