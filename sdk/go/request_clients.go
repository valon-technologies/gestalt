package gestalt

import (
	"context"

	"github.com/valon-technologies/gestalt/sdk/go/client"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

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
	if reqCtx == nil {
		return nil
	}
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
}

func clientSubjectContext(subject *proto.SubjectContext) *client.SubjectContext {
	if subject == nil {
		return nil
	}
	out := &client.SubjectContext{
		ID:                  subject.GetId(),
		CredentialSubjectID: subject.GetCredentialSubjectId(),
		Email:               subject.GetEmail(),
		DisplayName:         subject.GetDisplayName(),
		Scopes:              append([]string(nil), subject.GetScopes()...),
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
}

func clientCredentialContext(credential *proto.CredentialContext) *client.CredentialContext {
	if credential == nil {
		return nil
	}
	return &client.CredentialContext{
		Mode:       credential.GetMode(),
		SubjectID:  credential.GetSubjectId(),
		Connection: credential.GetConnection(),
		Instance:   credential.GetInstance(),
	}
}

func clientAccessContext(access *proto.AccessContext) *client.AccessContext {
	if access == nil {
		return nil
	}
	return &client.AccessContext{Policy: access.GetPolicy(), Role: access.GetRole()}
}

func clientHostContext(host *proto.HostContext) *client.HostContext {
	if host == nil {
		return nil
	}
	return &client.HostContext{PublicBaseURL: host.GetPublicBaseUrl()}
}

func clientProviderContext(caller *proto.ProviderContext) *client.ProviderContext {
	if caller == nil {
		return nil
	}
	return &client.ProviderContext{Kind: caller.GetKind(), Name: caller.GetName()}
}

func clientInvocationContext(invocation *proto.InvocationContext) *client.InvocationContext {
	if invocation == nil {
		return nil
	}
	return &client.InvocationContext{
		RequestID:                invocation.GetRequestId(),
		Depth:                    invocation.GetDepth(),
		CallChain:                append([]string(nil), invocation.GetCallChain()...),
		Surface:                  invocation.GetSurface(),
		InternalConnectionAccess: invocation.GetInternalConnectionAccess(),
		Connection:               invocation.GetConnection(),
	}
}

func clientAgentToolRefs(refs []*proto.AgentToolRef) []*client.AgentToolRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]*client.AgentToolRef, 0, len(refs))
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		out = append(out, &client.AgentToolRef{
			App:            ref.GetApp(),
			Operation:      ref.GetOperation(),
			Connection:     ref.GetConnection(),
			Instance:       ref.GetInstance(),
			Title:          ref.GetTitle(),
			Description:    ref.GetDescription(),
			CredentialMode: ref.GetCredentialMode(),
			System:         ref.GetSystem(),
			RunAs:          clientSubjectContext(ref.GetRunAs()),
		})
	}
	return out
}

func clientRequestMetaContext(meta *proto.RequestMetaContext) *client.RequestMetaContext {
	if meta == nil {
		return nil
	}
	return &client.RequestMetaContext{
		ClientIp:   meta.GetClientIp(),
		RemoteAddr: meta.GetRemoteAddr(),
		UserAgent:  meta.GetUserAgent(),
	}
}

func clientAgentInvocationContext(agent *proto.AgentInvocationContext) *client.AgentInvocationContext {
	if agent == nil {
		return nil
	}
	return &client.AgentInvocationContext{
		ProviderName: agent.GetProviderName(),
		SessionID:    agent.GetSessionId(),
		TurnID:       agent.GetTurnId(),
	}
}
