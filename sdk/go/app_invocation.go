package gestalt

import (
	"context"
	"fmt"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
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
}

// InvokeGraphQLOptions selects a target connection for a GraphQL surface invocation.
type InvokeGraphQLOptions struct {
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
	// AllOperations allows every operation on App.
	AllOperations bool
}

type appClient struct {
	client          proto.AppClient
	invocationToken string
}

// App is the fakeable contract for app invocation calls.
type App interface {
	Invoke(ctx context.Context, app string, operation string, params any, opts *InvokeOptions) (*OperationResult, error)
	InvokeGraphQL(ctx context.Context, app string, document string, variables any, opts *InvokeGraphQLOptions) (*OperationResult, error)
	ExchangeInvocationToken(ctx context.Context, grants []InvocationGrant, ttl time.Duration) (string, error)
}

var sharedAppTransport sharedManagerTransport[proto.AppClient]

// NewApp returns a capability that attaches invocationToken to every request.
func NewApp(invocationToken string) (App, error) {
	if strings.TrimSpace(invocationToken) == "" {
		return nil, fmt.Errorf("app: invocation token is not available")
	}
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
		client:          client,
		invocationToken: strings.TrimSpace(invocationToken),
	}, nil
}

// AppFromContext returns an App using the context invocation token.
func AppFromContext(ctx context.Context) (App, error) {
	return NewApp(InvocationTokenFromContext(ctx))
}

// Close is a no-op because this capability uses shared transport.
func (c *appClient) Close() error {
	return nil
}

// Invoke calls one operation on another app.
func (c *appClient) Invoke(ctx context.Context, app, operation string, params any, opts *InvokeOptions) (*OperationResult, error) {
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
		InvocationToken: c.invocationToken,
		App:             app,
		Operation:       operation,
		Params:          msg,
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
			req.Workflow = workflow
		}
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
func (c *appClient) InvokeGraphQL(ctx context.Context, app, document string, variables any, opts *InvokeGraphQLOptions) (*OperationResult, error) {
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
func (c *appClient) ExchangeInvocationToken(ctx context.Context, grants []InvocationGrant, ttl time.Duration) (string, error) {
	if c == nil || c.client == nil {
		return "", fmt.Errorf("app: client is not initialized")
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
