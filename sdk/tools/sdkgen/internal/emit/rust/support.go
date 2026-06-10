package rust

import _ "embed"

// supportFile is the shared generated runtime: the canonical error model,
// native representations for well-known types, and the conversions every
// generated client uses. It is emitted once as rpc_support.rs.
//
//go:embed rpc_support.rs
var supportFile string
