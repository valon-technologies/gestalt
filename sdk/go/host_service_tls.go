package gestalt

import (
	"github.com/valon-technologies/gestalt/sdk/go/internal/host"
)

// Environment variables carrying the host service TLS trust anchors.
const (
	EnvHostServiceTLSCAFile = host.EnvHostServiceTLSCAFile
	EnvHostServiceTLSCAPEM  = host.EnvHostServiceTLSCAPEM
)
