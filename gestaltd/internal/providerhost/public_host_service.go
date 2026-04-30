package providerhost

import "github.com/valon-technologies/gestalt/server/services/runtimehost"

type PublicHostServiceSessionVerifier = runtimehost.PublicHostServiceSessionVerifier
type PublicHostService = runtimehost.PublicHostService
type PublicHostServiceRegistry = runtimehost.PublicHostServiceRegistry

func NewPublicHostServiceRegistry() *PublicHostServiceRegistry {
	return runtimehost.NewPublicHostServiceRegistry()
}
