package gestalt

import (
	"crypto/tls"

	"github.com/valon-technologies/gestalt/sdk/go/internal/host"
)

// Environment variables carrying the host service TLS trust anchors.
const (
	EnvHostServiceTLSCAFile = host.EnvHostServiceTLSCAFile
	EnvHostServiceTLSCAPEM  = host.EnvHostServiceTLSCAPEM
)

func hostServiceTLSConfig(serviceName, serverName string) (*tls.Config, error) {
	return host.TLSConfig(serviceName, serverName)
}
