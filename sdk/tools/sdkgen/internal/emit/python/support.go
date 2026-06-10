package python

import _ "embed"

// supportFile is the shared generated runtime: the canonical error model,
// native representations for well-known types, and the stream/unary call
// helpers every generated client uses. It is emitted once as rpc_support.py.
//
//go:embed rpc_support.py
var supportFile string
