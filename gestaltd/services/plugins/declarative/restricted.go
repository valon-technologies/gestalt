package declarative

import (
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/plugins/operationexposure"
)

type Restricted = operationexposure.Restricted
type RestrictedOption = operationexposure.RestrictedOption

func WithDescriptions(descs map[string]string) RestrictedOption {
	return operationexposure.WithDescriptions(descs)
}

func WithAllowedRoles(roles map[string][]string) RestrictedOption {
	return operationexposure.WithAllowedRoles(roles)
}

func WithTags(tags map[string][]string) RestrictedOption {
	return operationexposure.WithTags(tags)
}

func NewRestricted(inner core.Provider, ops map[string]string, opts ...RestrictedOption) core.Provider {
	return operationexposure.NewRestricted(inner, ops, opts...)
}
