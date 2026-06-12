package gestalt

import "github.com/valon-technologies/gestalt/sdk/go/internal/host"

// Environment variables through which the daemon advertises its host service endpoint.
const (
	EnvHostServiceSocket       = host.EnvHostServiceSocket
	EnvHostServiceToken        = host.EnvHostServiceToken
	HostServiceBindingMetadata = host.BindingMetadata
)
