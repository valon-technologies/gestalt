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

// contextSupportFile is the client option machinery, emitted as
// support_context.go when any service carries a request context. The fmt verb
// is the native request context type.
const contextSupportFile = `package client

// ClientOption configures a generated client constructor.
type ClientOption func(*clientOptions)

type clientOptions struct {
	requestContext %[1]s
}

// WithRequestContext sets the client's default request context: outgoing
// requests whose context field is unset carry it.
func WithRequestContext(context %[1]s) ClientOption {
	return func(o *clientOptions) { o.requestContext = context }
}

func applyClientOptions(opts []ClientOption) clientOptions {
	var options clientOptions
	for _, opt := range opts {
		opt(&options)
	}
	return options
}
`
