package golang

import _ "embed"

// supportFile is the shared generated runtime: the canonical error model and
// native representations for well-known types. It is emitted once as
// rpc_support.go.
//
//go:embed rpc_support.go.tmpl
var supportFile string
