package access

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNotAuthenticated = errors.New("not authenticated")
	ErrDenied           = errors.New("authorization denied")
	ErrScopeDenied      = errors.New("token scope denied")
)

type cause string

const (
	causeNotAuthenticated  cause = "not_authenticated"
	causeScopeProvider     cause = "scope_provider"
	causeScopeOperation    cause = "scope_operation"
	causePolicyDenied      cause = "policy_denied"
	causePolicyUnavailable cause = "policy_unavailable"
)

type accessError struct {
	cause     cause
	provider  string
	operation string
	resource  string
	action    string
	err       error
}

func (e *accessError) Error() string {
	if e == nil {
		return ""
	}
	base := "access error"
	switch e.cause {
	case causeNotAuthenticated:
		base = ErrNotAuthenticated.Error()
	case causeScopeProvider, causeScopeOperation:
		base = ErrScopeDenied.Error()
	case causePolicyDenied:
		base = ErrDenied.Error()
	case causePolicyUnavailable:
		base = "authorization policy unavailable"
	}
	details := e.details()
	if e.err != nil && e.cause != causeNotAuthenticated {
		if details != "" {
			return fmt.Sprintf("%s: %s: %v", base, details, e.err)
		}
		return fmt.Sprintf("%s: %v", base, e.err)
	}
	if details != "" {
		return fmt.Sprintf("%s: %s", base, details)
	}
	return base
}

func (e *accessError) Unwrap() error {
	if e == nil {
		return nil
	}
	switch e.cause {
	case causeNotAuthenticated:
		return ErrNotAuthenticated
	case causeScopeProvider, causeScopeOperation:
		return ErrScopeDenied
	case causePolicyDenied:
		return ErrDenied
	default:
		return e.err
	}
}

func (e *accessError) details() string {
	if e == nil {
		return ""
	}
	provider := strings.TrimSpace(e.provider)
	operation := strings.TrimSpace(e.operation)
	if provider != "" && operation != "" {
		return provider + "." + operation
	}
	if provider != "" {
		return provider
	}
	resource := strings.TrimSpace(e.resource)
	action := strings.TrimSpace(e.action)
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

func IsOperationScopeDenied(err error) bool {
	var accessErr *accessError
	return errors.As(err, &accessErr) && accessErr != nil && accessErr.cause == causeScopeOperation
}

func IsPolicyUnavailable(err error) bool {
	var accessErr *accessError
	return errors.As(err, &accessErr) && accessErr != nil && accessErr.cause == causePolicyUnavailable
}
