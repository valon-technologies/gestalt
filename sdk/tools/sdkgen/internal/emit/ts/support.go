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

// gatewayErrorFile is the canonical REST gateway error parser, emitted as
// client/generated/gateway_error.ts with package-local rpc_support imports.
//
//go:embed gateway_error.ts
var gatewayErrorFile string

// restRequestMappingFile is the protobuf-JSON REST request mapper, emitted as
// client/generated/rest_request_mapping.ts.
//
//go:embed rest_request_mapping.ts
var restRequestMappingFile string

// transportSupportFile is the shared public transport helper runtime, emitted as
// client/generated/transport_support.ts with package-local rpc_support imports.
//
//go:embed transport_support.ts
var transportSupportFile string

// transportKernelFile is the schema-derived public REST transport kernel.
//
//go:embed transport_kernel.ts
var transportKernelFile string
