package ts

import _ "embed"

// supportFile is the public shared module: the canonical error model and the
// native representations for well-known types. It is emitted once as
// rpc_support.ts.
//
//go:embed rpc_support.ts
var supportFile string

// runtimeFile is the internal shared runtime: stream/unary call helpers and
// well-known-type converters used by generated clients and codec modules. It
// is emitted once as internal/codec/support.ts; "runtime" would collide with
// the codec module generated for runtime.proto.
//
//go:embed codec_support.ts
var runtimeFile string

// invokeSupportFile is the JSON operation-envelope decode runtime, emitted as
// invoke_support.ts when any method carries the json_result annotation.
//
//go:embed invoke_support.ts
var invokeSupportFile string
