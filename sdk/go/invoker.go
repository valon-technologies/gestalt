package gestalt

import (
	"context"
	"fmt"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
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
}

// InvocationGrant describes access granted to an exchanged invocation token.
type InvocationGrant struct {
	// App is the app name the child token may invoke.
	App string
	// Operations are the specific operation ids allowed by the child token.
	Operations []string
	// Surfaces are the surface names allowed by the child token.
	Surfaces []string
	// AllOperations allows every operation on Plugin.
	AllOperations bool
}

// InvokerClient invokes sibling app operations through the host.
type InvokerClient struct {
	client          proto.AppInvokerClient
	invocationToken string
}

var sharedInvokerTransport sharedManagerTransport[proto.AppInvokerClient]

// Invoker returns a client that attaches invocationToken to every request.
func Invoker(invocationToken string) (*InvokerClient, error) {
	if strings.TrimSpace(invocationToken) == "" {
		return nil, fmt.Errorf("plugin invoker: invocation token is not available")
	}
	target, token, err := hostServiceTarget("plugin invoker")
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := managerTransportClient(ctx, "plugin invoker", target, token, &sharedInvokerTransport, proto.NewAppInvokerClient)
	if err != nil {
		return nil, err
	}

	return &InvokerClient{
		client:          client,
		invocationToken: strings.TrimSpace(invocationToken),
	}, nil
}

// InvokerFromContext returns an Invoker using the context invocation token.
func InvokerFromContext(ctx context.Context) (*InvokerClient, error) {
	return Invoker(InvocationTokenFromContext(ctx))
}

// Close is a no-op compatibility method because this client uses shared transport.
func (c *InvokerClient) Close() error {
	return nil
}

// Invoke calls one operation on another app.
func (c *InvokerClient) Invoke(ctx context.Context, app, operation string, params any, opts *InvokeOptions) (*OperationResult, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("plugin invoker: client is not initialized")
	}
	if params == nil {
		params = map[string]any{}
	}
	msg, err := structFromAny(params)
	if err != nil {
		return nil, fmt.Errorf("plugin invoker: encode params: %w", err)
	}
	if msg == nil {
		msg = &structpb.Struct{}
	}

	req := &proto.AppInvokeRequest{
		InvocationToken: c.invocationToken,
		App:             app,
		Operation:       operation,
		Params:          msg,
	}
	if opts != nil {
		req.Connection = opts.Connection
		req.Instance = opts.Instance
		req.IdempotencyKey = strings.TrimSpace(opts.IdempotencyKey)
	}

	resp, err := c.client.Invoke(ctx, req)
	if err != nil {
		return nil, err
	}
	return &OperationResult{
		Status: int(resp.GetStatus()),
		Body:   resp.GetBody(),
	}, nil
}

// InvokeGraphQL calls another plugin's GraphQL surface.
func (c *InvokerClient) InvokeGraphQL(ctx context.Context, app, document string, variables any, opts *InvokeOptions) (*OperationResult, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("plugin invoker: client is not initialized")
	}
	document = strings.TrimSpace(document)
	if document == "" {
		return nil, fmt.Errorf("plugin invoker: graphql document is required")
	}

	var msg *structpb.Struct
	var err error
	if variables != nil {
		msg, err = structFromAny(variables)
		if err != nil {
			return nil, fmt.Errorf("plugin invoker: encode variables: %w", err)
		}
		if msg != nil && len(msg.GetFields()) == 0 {
			msg = nil
		}
	}

	req := &proto.AppInvokeGraphQLRequest{
		InvocationToken: c.invocationToken,
		App:             app,
		Document:        document,
		Variables:       msg,
	}
	if opts != nil {
		req.Connection = opts.Connection
		req.Instance = opts.Instance
		req.IdempotencyKey = strings.TrimSpace(opts.IdempotencyKey)
	}

	resp, err := c.client.InvokeGraphQL(ctx, req)
	if err != nil {
		return nil, err
	}
	return &OperationResult{
		Status: int(resp.GetStatus()),
		Body:   resp.GetBody(),
	}, nil
}

// ExchangeInvocationToken exchanges this invocation token for a narrower child token.
func (c *InvokerClient) ExchangeInvocationToken(ctx context.Context, grants []InvocationGrant, ttl time.Duration) (string, error) {
	if c == nil || c.client == nil {
		return "", fmt.Errorf("plugin invoker: client is not initialized")
	}

	req := &proto.ExchangeInvocationTokenRequest{
		ParentInvocationToken: c.invocationToken,
		Grants:                encodeInvocationGrants(grants),
	}
	if ttl > 0 {
		req.TtlSeconds = int64(ttl / time.Second)
		if req.TtlSeconds == 0 {
			req.TtlSeconds = 1
		}
	}

	resp, err := c.client.ExchangeInvocationToken(ctx, req)
	if err != nil {
		return "", err
	}
	return resp.GetInvocationToken(), nil
}

func encodeInvocationGrants(grants []InvocationGrant) []*proto.AppInvocationGrant {
	if len(grants) == 0 {
		return nil
	}
	out := make([]*proto.AppInvocationGrant, 0, len(grants))
	for _, grant := range grants {
		app := strings.TrimSpace(grant.App)
		if app == "" {
			continue
		}
		ops := make([]string, 0, len(grant.Operations))
		for _, operation := range grant.Operations {
			operation = strings.TrimSpace(operation)
			if operation == "" {
				continue
			}
			ops = append(ops, operation)
		}
		out = append(out, &proto.AppInvocationGrant{
			App:           app,
			Operations:    ops,
			Surfaces:      grant.Surfaces,
			AllOperations: grant.AllOperations,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
