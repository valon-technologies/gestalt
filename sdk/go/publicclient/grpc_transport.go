package publicclient

import (
	"context"

	"github.com/valon-technologies/gestalt/sdk/go/publicclient/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// GRPCTransport attaches bearer credentials to outgoing public gRPC calls.
type GRPCTransport struct {
	Conn grpc.ClientConnInterface
	Auth Auth
}

func (t *GRPCTransport) context(ctx context.Context) context.Context {
	if t == nil || t.Auth == nil {
		return ctx
	}
	meta := &Request{Headers: map[string]string{}}
	_ = t.Auth.Apply(ctx, meta)
	if token := meta.Headers["Authorization"]; token != "" {
		return metadata.AppendToOutgoingContext(ctx, "authorization", token)
	}
	return ctx
}

// AppGRPC returns the generated App gRPC client.
func (t *GRPCTransport) AppGRPC() *generated.AppGRPC {
	if t == nil || t.Conn == nil {
		return nil
	}
	return generated.NewAppGRPC(t.Conn)
}

// AgentGRPC returns the generated Agent gRPC client.
func (t *GRPCTransport) AgentGRPC() *generated.AgentGRPC {
	if t == nil || t.Conn == nil {
		return nil
	}
	return generated.NewAgentGRPC(t.Conn)
}

// WorkflowGRPC returns the generated Workflow gRPC client.
func (t *GRPCTransport) WorkflowGRPC() *generated.WorkflowGRPC {
	if t == nil || t.Conn == nil {
		return nil
	}
	return generated.NewWorkflowGRPC(t.Conn)
}

// IdentityGRPC returns the generated Identity gRPC client.
func (t *GRPCTransport) IdentityGRPC() *generated.IdentityGRPC {
	if t == nil || t.Conn == nil {
		return nil
	}
	return generated.NewIdentityGRPC(t.Conn)
}

// AuthorizationGRPC returns the generated Authorization gRPC client.
func (t *GRPCTransport) AuthorizationGRPC() *generated.AuthorizationGRPC {
	if t == nil || t.Conn == nil {
		return nil
	}
	return generated.NewAuthorizationGRPC(t.Conn)
}

// IndexedDBGRPC returns the generated IndexedDB gRPC client.
func (t *GRPCTransport) IndexedDBGRPC() *generated.IndexedDBGRPC {
	if t == nil || t.Conn == nil {
		return nil
	}
	return generated.NewIndexedDBGRPC(t.Conn)
}

// ExternalCredentialsGRPC returns the generated ExternalCredentials gRPC client.
func (t *GRPCTransport) ExternalCredentialsGRPC() *generated.ExternalCredentialsGRPC {
	if t == nil || t.Conn == nil {
		return nil
	}
	return generated.NewExternalCredentialsGRPC(t.Conn)
}

// WithAuthContext returns ctx with auth metadata applied.
func (t *GRPCTransport) WithAuthContext(ctx context.Context) context.Context {
	return t.context(ctx)
}
