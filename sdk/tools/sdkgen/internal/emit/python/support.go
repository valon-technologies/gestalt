package python

import _ "embed"

// supportFile is the public shared module: the canonical error model and the
// native representations for well-known types that appear in generated
// signatures. It is emitted once as rpc_support.py.
//
//go:embed rpc_support.py
var supportFile string

// invokeSupportFile is the JSON operation-envelope decode runtime, emitted as
// invoke_support.py when any method carries the json_result annotation.
//
//go:embed invoke_support.py
var invokeSupportFile string

// runtimeFile is the internal shared runtime: stream/unary call helpers and
// well-known-type converters used by generated clients and codec modules. It
// is emitted once as _codec/support.py.
//
//go:embed codec_support.py
var runtimeFile string

// indexedDBPaginationFile is emitted as indexeddb_pagination.py when the schema
// includes the IndexedDB service.
//
//go:embed indexeddb_pagination.py
var indexedDBPaginationFile string

// codecInit makes the generated _codec directory an importable package. The
// underscore-prefixed package name keeps every codec module internal by the
// SDK's naming convention (like _gen), so the converters inside carry no
// leading underscore.
const codecInit = `"""Internal wire codec for the generated Gestalt SDK modules."""
`
