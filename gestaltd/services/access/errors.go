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

type Cause string

const (
	CauseUnknown           Cause = ""
	CauseNotAuthenticated  Cause = "not_authenticated"
	CauseScopeProvider     Cause = "scope_provider"
	CauseScopeOperation    Cause = "scope_operation"
	CausePolicyDenied      Cause = "policy_denied"
	CausePolicyUnavailable Cause = "policy_unavailable"
)

type Error struct {
	Cause     Cause
	Provider  string
	Operation string
	Resource  string
	Action    string
	Err       error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	base := "access error"
	switch e.Cause {
	case CauseNotAuthenticated:
		base = ErrNotAuthenticated.Error()
	case CauseScopeProvider, CauseScopeOperation:
		base = ErrScopeDenied.Error()
	case CausePolicyDenied:
		base = ErrDenied.Error()
	case CausePolicyUnavailable:
		base = "authorization policy unavailable"
	}
	details := e.details()
	if e.Err != nil && e.Cause != CauseNotAuthenticated {
		if details != "" {
			return fmt.Sprintf("%s: %s: %v", base, details, e.Err)
		}
		return fmt.Sprintf("%s: %v", base, e.Err)
	}
	if details != "" {
		return fmt.Sprintf("%s: %s", base, details)
	}
	return base
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	switch e.Cause {
	case CauseNotAuthenticated:
		return ErrNotAuthenticated
	case CauseScopeProvider, CauseScopeOperation:
		return ErrScopeDenied
	case CausePolicyDenied:
		return ErrDenied
	default:
		return e.Err
	}
}

func (e *Error) details() string {
	if e == nil {
		return ""
	}
	provider := strings.TrimSpace(e.Provider)
	operation := strings.TrimSpace(e.Operation)
	if provider != "" && operation != "" {
		return provider + "." + operation
	}
	if provider != "" {
		return provider
	}
	resource := strings.TrimSpace(e.Resource)
	action := strings.TrimSpace(e.Action)
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

func ErrorCause(err error) Cause {
	var accessErr *Error
	if errors.As(err, &accessErr) && accessErr != nil {
		return accessErr.Cause
	}
	switch {
	case errors.Is(err, ErrNotAuthenticated):
		return CauseNotAuthenticated
	case errors.Is(err, ErrScopeDenied):
		return CauseScopeOperation
	case errors.Is(err, ErrDenied):
		return CausePolicyDenied
	default:
		return CauseUnknown
	}
}

func IsOperationScopeDenied(err error) bool {
	var accessErr *Error
	return errors.As(err, &accessErr) && accessErr != nil && accessErr.Cause == CauseScopeOperation
}

func IsPolicyDenied(err error) bool {
	var accessErr *Error
	return errors.As(err, &accessErr) && accessErr != nil && accessErr.Cause == CausePolicyDenied
}

func IsPolicyUnavailable(err error) bool {
	var accessErr *Error
	return errors.As(err, &accessErr) && accessErr != nil && accessErr.Cause == CausePolicyUnavailable
}
