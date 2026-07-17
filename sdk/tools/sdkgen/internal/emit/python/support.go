package python

import (
	_ "embed"
	"strings"
)

// supportFile is the shared error model emitted as rpc_support.py.
//
//go:embed rpc_support.py
var supportFile string

// providerRPCSupportFile is appended for provider clients.
//
//go:embed rpc_support_provider.py
var providerRPCSupportFile string

var providerSupportFile = strings.TrimRight(supportFile, "\n") + "\n\n" + strings.TrimLeft(providerRPCSupportFile, "\n")

// runtimeFile is the internal shared runtime for generated codec modules.
//
//go:embed codec_support.py
var runtimeFile string

// invokeSupportFile is the JSON operation-envelope decode runtime.
//
//go:embed invoke_support.py
var invokeSupportFile string

// transportKernelFile is the schema-derived public REST transport kernel.
//
//go:embed transport_kernel.py
var transportKernelFile string

// codecInit makes the generated _codec directory importable.
const codecInit = `"""Internal wire codec for the generated Gestalt SDK modules."""
`
