package gestalt

import (
	"context"
	"fmt"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// InvokeOptions selects a target connection for an app invocation.
type InvokeOptions struct {
	// Connection is the connected account id or name to invoke against.
	Connection string
	// Instance is the provider instance id or name to invoke against.
	Instance string
	// IdempotencyKey is forwarded to the target operation.
	IdempotencyKey string
	// CredentialMode selects the credential mode for this operation when the caller declared one.
	CredentialMode string
	// WorkflowContext attaches workflow metadata to the downstream operation request.
	WorkflowContext map[string]any
	// Timeout bounds the host-service RPC when greater than zero.
	Timeout time.Duration
}

// InvokeGraphQLOptions selects a target connection for a GraphQL surface invocation.
type InvokeGraphQLOptions struct {
	// Connection is the connected account id or name to invoke against.
	Connection string
	// Instance is the provider instance id or name to invoke against.
	Instance string
	// IdempotencyKey is forwarded to the target operation.
	IdempotencyKey string
	// Timeout bounds the host-service RPC when greater than zero.
	Timeout time.Duration
}

type appClient struct {
	client  proto.AppClient
	context *proto.RequestContext
}

// App is the fakeable contract for app invocation calls.
type App interface {
	Invoke(ctx context.Context, app string, operation string, params any, opts *InvokeOptions) (any, error)
	InvokeRaw(ctx context.Context, app string, operation string, params any, opts *InvokeOptions) (*OperationResult, error)
	InvokeGraphQL(ctx context.Context, app string, document string, variables any, opts *InvokeGraphQLOptions) (any, error)
	InvokeGraphQLRaw(ctx context.Context, app string, document string, variables any, opts *InvokeGraphQLOptions) (*OperationResult, error)
}

var sharedAppTransport sharedManagerTransport[proto.AppClient]

// NewApp returns an app invocation capability for the current provider request.
func NewApp(req Request) (App, error) {
	reqCtx, err := requestContextForRequest(req)
	if err != nil {
		return nil, err
	}
	return newApp(reqCtx)
}

// NewAppFromRequest returns an app invocation capability for the current
// provider request.
func NewAppFromRequest(req Request) (App, error) {
	return NewApp(req)
}

func newApp(reqCtx *proto.RequestContext) (App, error) {
	target, token, err := hostServiceTarget("app")
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := managerTransportClient(ctx, "app", target, token, &sharedAppTransport, proto.NewAppClient)
	if err != nil {
		return nil, err
	}

	return &appClient{
		client:  client,
		context: cloneRequestContext(reqCtx),
	}, nil
}

// AppFromContext returns an App using the current provider request context.
func AppFromContext(ctx context.Context) (App, error) {
	return newApp(requestContextFromContext(ctx))
}

// Close is a no-op because this capability uses shared transport.
func (c *appClient) Close() error {
	return nil
}

// Invoke calls one operation on another app.
func (c *appClient) Invoke(ctx context.Context, app, operation string, params any, opts *InvokeOptions) (any, error) {
	result, err := c.InvokeRaw(ctx, app, operation, params, opts)
	if err != nil {
		return nil, err
	}
	return decodeAppOperationResult(app, operation, result)
}

// InvokeRaw calls one operation on another app and returns the raw transport result.
func (c *appClient) InvokeRaw(ctx context.Context, app, operation string, params any, opts *InvokeOptions) (*OperationResult, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("app: client is not initialized")
	}
	if params == nil {
		params = map[string]any{}
	}
	msg, err := structFromAny(params)
	if err != nil {
		return nil, fmt.Errorf("app: encode params: %w", err)
	}
	if msg == nil {
		msg = &structpb.Struct{}
	}

	req := &proto.AppInvokeRequest{
		App:       app,
		Operation: operation,
		Params:    msg,
		Context:   cloneRequestContext(c.context),
	}
	if opts != nil {
		req.Connection = opts.Connection
		req.Instance = opts.Instance
		req.IdempotencyKey = strings.TrimSpace(opts.IdempotencyKey)
		req.CredentialMode = strings.TrimSpace(opts.CredentialMode)
		if opts.WorkflowContext != nil {
			workflow, err := structFromAny(opts.WorkflowContext)
			if err != nil {
				return nil, fmt.Errorf("app: encode workflow context: %w", err)
			}
			if req.Context == nil {
				req.Context = &proto.RequestContext{}
			}
			req.Context.Workflow = workflow
		}
		if opts.Timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
			defer cancel()
		}
	}

	resp, err := c.client.Invoke(ctx, req)
	if err != nil {
		return nil, err
	}
	return &OperationResult{
		Status:  int(resp.GetStatus()),
		Headers: httpHeaderFromProto(resp.GetHeaders()),
		Body:    resp.GetBody(),
	}, nil
}

// InvokeGraphQL calls another plugin's GraphQL surface.
func (c *appClient) InvokeGraphQL(ctx context.Context, app, document string, variables any, opts *InvokeGraphQLOptions) (any, error) {
	result, err := c.InvokeGraphQLRaw(ctx, app, document, variables, opts)
	if err != nil {
		return nil, err
	}
	return decodeAppGraphQLResult(app, result)
}

// InvokeGraphQLRaw calls another plugin's GraphQL surface and returns the raw transport result.
func (c *appClient) InvokeGraphQLRaw(ctx context.Context, app, document string, variables any, opts *InvokeGraphQLOptions) (*OperationResult, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("app: client is not initialized")
	}
	document = strings.TrimSpace(document)
	if document == "" {
		return nil, fmt.Errorf("app: graphql document is required")
	}

	var msg *structpb.Struct
	var err error
	if variables != nil {
		msg, err = structFromAny(variables)
		if err != nil {
			return nil, fmt.Errorf("app: encode variables: %w", err)
		}
		if msg != nil && len(msg.GetFields()) == 0 {
			msg = nil
		}
	}

	req := &proto.AppInvokeGraphQLRequest{
		App:       app,
		Document:  document,
		Variables: msg,
		Context:   cloneRequestContext(c.context),
	}
	if opts != nil {
		req.Connection = opts.Connection
		req.Instance = opts.Instance
		req.IdempotencyKey = strings.TrimSpace(opts.IdempotencyKey)
		if opts.Timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
			defer cancel()
		}
	}

	resp, err := c.client.InvokeGraphQL(ctx, req)
	if err != nil {
		return nil, err
	}
	return &OperationResult{
		Status:  int(resp.GetStatus()),
		Headers: httpHeaderFromProto(resp.GetHeaders()),
		Body:    resp.GetBody(),
	}, nil
}

func cloneRequestContext(reqCtx *proto.RequestContext) *proto.RequestContext {
	if reqCtx == nil {
		return nil
	}
	cloned, _ := gproto.Clone(reqCtx).(*proto.RequestContext)
	return cloned
}
