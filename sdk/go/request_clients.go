package gestalt

import (
	"context"

	"github.com/valon-technologies/gestalt/sdk/go/client"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// RequestContextFromContext returns the host-attached wire request context.
func RequestContextFromContext(ctx context.Context) *proto.RequestContext {
	return requestContextFromContext(ctx)
}

// AppFromContext returns the generated app-invocation client carrying the
// current provider request's context as the client default. It dials the
// "app" host service advertised through the GESTALT_HOST_SERVICE_SOCKET
// environment.
func AppFromContext(ctx context.Context) (*client.App, error) {
	return client.ConnectApp(ctx, "", requestContextClientOptions(ctx)...)
}

// AgentFromContext returns the generated agent client carrying the current
// provider request's context as the client default. It dials the "agent"
// host service advertised through the GESTALT_HOST_SERVICE_SOCKET
// environment.
func AgentFromContext(ctx context.Context) (*client.Agent, error) {
	return client.ConnectAgent(ctx, "", requestContextClientOptions(ctx)...)
}

// WorkflowFromContext returns the generated workflow client carrying the
// current provider request's context as the client default. It dials the
// "workflow" host service advertised through the GESTALT_HOST_SERVICE_SOCKET
// environment.
func WorkflowFromContext(ctx context.Context) (*client.Workflow, error) {
	return client.ConnectWorkflow(ctx, "", requestContextClientOptions(ctx)...)
}

func requestContextClientOptions(ctx context.Context) []client.ClientOption {
	reqCtx := clientRequestContext(requestContextFromContext(ctx))
	if reqCtx == nil {
		return nil
	}
	return []client.ClientOption{client.WithRequestContext(reqCtx)}
}

// clientRequestContext converts a wire-form request context into the
// generated client package's native form. The conversion is nil-safe and
// preserves every field of the request context.
func clientRequestContext(reqCtx *proto.RequestContext) *client.RequestContext {
	return convertProtoMessage(reqCtx, func(reqCtx *proto.RequestContext) *client.RequestContext {
		out := &client.RequestContext{
			Subject:      clientSubjectContext(reqCtx.GetSubject()),
			Credential:   clientCredentialContext(reqCtx.GetCredential()),
			Access:       clientAccessContext(reqCtx.GetAccess()),
			Host:         clientHostContext(reqCtx.GetHost()),
			AgentSubject: clientSubjectContext(reqCtx.GetAgentSubject()),
			Caller:       clientProviderContext(reqCtx.GetCaller()),
			Invocation:   clientInvocationContext(reqCtx.GetInvocation()),
			ToolRefs:     clientAgentToolRefs(reqCtx.GetToolRefs()),
			ToolRefsSet:  reqCtx.GetToolRefsSet(),
			RequestMeta:  clientRequestMetaContext(reqCtx.GetRequestMeta()),
			Agent:        clientAgentInvocationContext(reqCtx.GetAgent()),
		}
		if workflow := reqCtx.GetWorkflow(); workflow != nil {
			out.Workflow = workflow.AsMap()
		}
		return out
	})
}

func clientSubjectContext(subject *proto.SubjectContext) *client.SubjectContext {
	return convertProtoMessage(subject, func(subject *proto.SubjectContext) *client.SubjectContext {
		out := &client.SubjectContext{
			Id:          subject.GetId(),
			Email:       subject.GetEmail(),
			DisplayName: subject.GetDisplayName(),
			Scopes:      append([]string(nil), subject.GetScopes()...),
		}
		for _, permission := range subject.GetPermissions() {
			if permission == nil {
				continue
			}
			out.Permissions = append(out.Permissions, &client.SubjectPermissionContext{
				App:           permission.GetApp(),
				Operations:    append([]string(nil), permission.GetOperations()...),
				AllOperations: permission.GetAllOperations(),
			})
		}
		return out
	})
}

func clientCredentialContext(credential *proto.CredentialContext) *client.CredentialContext {
	return convertProtoMessage(credential, func(credential *proto.CredentialContext) *client.CredentialContext {
		return &client.CredentialContext{
			Mode:       credential.GetMode(),
			SubjectId:  credential.GetSubjectId(),
			Connection: credential.GetConnection(),
			Instance:   credential.GetInstance(),
		}
	})
}

func clientAccessContext(access *proto.AccessContext) *client.AccessContext {
	return convertProtoMessage(access, func(access *proto.AccessContext) *client.AccessContext {
		return &client.AccessContext{Policy: access.GetPolicy(), Role: access.GetRole()}
	})
}

func clientHostContext(host *proto.HostContext) *client.HostContext {
	return convertProtoMessage(host, func(host *proto.HostContext) *client.HostContext {
		return &client.HostContext{PublicBaseUrl: host.GetPublicBaseUrl()}
	})
}

func clientProviderContext(caller *proto.ProviderContext) *client.ProviderContext {
	return convertProtoMessage(caller, func(caller *proto.ProviderContext) *client.ProviderContext {
		return &client.ProviderContext{Kind: caller.GetKind(), Name: caller.GetName()}
	})
}

func clientInvocationContext(invocation *proto.InvocationContext) *client.InvocationContext {
	return convertProtoMessage(invocation, func(invocation *proto.InvocationContext) *client.InvocationContext {
		return &client.InvocationContext{
			RequestId:                invocation.GetRequestId(),
			Depth:                    invocation.GetDepth(),
			CallChain:                append([]string(nil), invocation.GetCallChain()...),
			Surface:                  invocation.GetSurface(),
			InternalConnectionAccess: invocation.GetInternalConnectionAccess(),
			Connection:               invocation.GetConnection(),
		}
	})
}

func clientAgentToolRefs(refs []*proto.AgentToolRef) []*client.AgentToolRef {
	return convertProtoNonNilSlice(refs, func(ref *proto.AgentToolRef) *client.AgentToolRef {
		return &client.AgentToolRef{
			App:            ref.GetApp(),
			Operation:      ref.GetOperation(),
			Connection:     ref.GetConnection(),
			Instance:       ref.GetInstance(),
			Title:          ref.GetTitle(),
			Description:    ref.GetDescription(),
			CredentialMode: ref.GetCredentialMode(),
			System:         ref.GetSystem(),
			RunAs:          clientSubjectContext(ref.GetRunAs()),
		}
	})
}

func clientRequestMetaContext(meta *proto.RequestMetaContext) *client.RequestMetaContext {
	return convertProtoMessage(meta, func(meta *proto.RequestMetaContext) *client.RequestMetaContext {
		return &client.RequestMetaContext{
			ClientIp:   meta.GetClientIp(),
			RemoteAddr: meta.GetRemoteAddr(),
			UserAgent:  meta.GetUserAgent(),
		}
	})
}

func clientAgentInvocationContext(agent *proto.AgentInvocationContext) *client.AgentInvocationContext {
	return convertProtoMessage(agent, func(agent *proto.AgentInvocationContext) *client.AgentInvocationContext {
		return &client.AgentInvocationContext{
			ProviderName: agent.GetProviderName(),
			SessionId:    agent.GetSessionId(),
			TurnId:       agent.GetTurnId(),
		}
	})
}
