package golang

import _ "embed"

// supportFile is the shared public file: the canonical error model. It is
// emitted once as rpc_support.go.
//
//go:embed rpc_support.go.tmpl
var supportFile string

// codecSupportFile is the shared codec runtime: the nil-safe wire converters
// for the well-known types used by the generated codec files. It is emitted
// once as support_codec.go.
//
//go:embed support_codec.go.tmpl
var codecSupportFile string
