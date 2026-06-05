package appaccess

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/agentwire"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type ProviderRequestContext struct {
	callerName         string
	callerKind         invocation.ProviderKind
	principal          *principal.Principal
	agentSubject       *core.RunAsSubject
	requestMeta        invocation.RequestMeta
	credential         invocation.CredentialContext
	invocation         *invocation.InvocationMeta
	surface            invocation.InvocationSurface
	internalConnection bool
	connection         string
	workflow           map[string]any
	access             invocation.AccessContext
	host               invocation.HostContext
	toolRefs           []coreagent.ToolRef
	toolRefsSet        bool
}

func RequestContextProto(ctx context.Context, publicBaseURL string, caller invocation.CallerProvider) (*proto.RequestContext, error) {
	var out proto.RequestContext

	if p := principal.FromContext(ctx); p != nil {
		out.Subject = &proto.SubjectContext{
			Id:                  subjectIDForPrincipal(p),
			CredentialSubjectId: strings.TrimSpace(principal.EffectiveCredentialSubjectID(p)),
			Email:               subjectEmail(p),
			Scopes:              cloneStrings(p.Scopes),
			Permissions:         permissionSetToSubjectPermissionContext(p.TokenPermissions),
		}
	}

	if cred := invocation.CredentialContextFromContext(ctx); cred.Mode != "" || cred.SubjectID != "" || cred.Connection != "" || cred.Instance != "" {
		out.Credential = &proto.CredentialContext{
			Mode:       string(cred.Mode),
			SubjectId:  cred.SubjectID,
			Connection: cred.Connection,
			Instance:   cred.Instance,
		}
	}

	if access := invocation.AccessContextFromContext(ctx); access.Policy != "" || access.Role != "" {
		out.Access = &proto.AccessContext{
			Policy: access.Policy,
			Role:   access.Role,
		}
	}
	if workflow := invocation.WorkflowContextFromContext(ctx); workflow != nil {
		value, err := protoutil.StructFromMap(workflow)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "workflow request context: %v", err)
		}
		out.Workflow = value
	}
	if toolRefs := invocation.ToolRefsContextFromContext(ctx); toolRefs.Set {
		out.ToolRefs = agentwire.ToolRefsToProto(toolRefs.Refs)
		out.ToolRefsSet = true
	}

	if publicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/"); publicBaseURL == "" {
		publicBaseURL = strings.TrimRight(strings.TrimSpace(invocation.HostContextFromContext(ctx).PublicBaseURL), "/")
	}
	if publicBaseURL != "" {
		out.Host = &proto.HostContext{PublicBaseUrl: publicBaseURL}
	}
	if caller.Kind == "" && caller.Name == "" {
		caller = invocation.CallerProviderFromContext(ctx)
	}
	if caller.Kind != "" && caller.Name != "" {
		out.Caller = &proto.ProviderContext{
			Kind: string(caller.Kind),
			Name: caller.Name,
		}
	}
	if meta := invocation.MetaFromContext(ctx); meta != nil {
		out.Invocation = &proto.InvocationContext{
			RequestId: meta.RequestID,
			Depth:     int32(meta.Depth),
			CallChain: append([]string(nil), meta.CallChain...),
		}
	}
	if reqMeta := invocation.RequestMetaFromContext(ctx); reqMeta != (invocation.RequestMeta{}) {
		out.RequestMeta = &proto.RequestMetaContext{
			ClientIp:   reqMeta.ClientIP,
			RemoteAddr: reqMeta.RemoteAddr,
			UserAgent:  reqMeta.UserAgent,
		}
	}
	if out.Invocation == nil {
		out.Invocation = &proto.InvocationContext{}
	}
	out.Invocation.Surface = string(invocation.InvocationSurfaceFromContext(ctx))
	out.Invocation.InternalConnectionAccess = invocation.InternalConnectionAccessFromContext(ctx)
	out.Invocation.Connection = invocation.ConnectionFromContext(ctx)
	if audit := invocation.RunAsAuditFromContext(ctx); audit.AgentSubject != nil {
		out.AgentSubject = agentwire.RunAsSubjectToProto(audit.AgentSubject)
	}

	if out.Invocation != nil && out.Invocation.RequestId == "" && out.Invocation.Depth == 0 && len(out.Invocation.CallChain) == 0 && out.Invocation.Surface == "" && !out.Invocation.InternalConnectionAccess && out.Invocation.Connection == "" {
		out.Invocation = nil
	}
	if out.Subject == nil && out.Credential == nil && out.Access == nil && out.Workflow == nil && !out.ToolRefsSet && len(out.ToolRefs) == 0 && out.Host == nil && out.AgentSubject == nil && out.Caller == nil && out.Invocation == nil && out.RequestMeta == nil {
		return nil, nil
	}
	return &out, nil
}

func MergeRequestContext(existing, fallback *proto.RequestContext) *proto.RequestContext {
	if existing == nil {
		if fallback == nil {
			return nil
		}
		return gproto.Clone(fallback).(*proto.RequestContext)
	}
	if fallback == nil {
		return gproto.Clone(existing).(*proto.RequestContext)
	}
	merged := gproto.Clone(existing).(*proto.RequestContext)
	if merged.GetSubject().GetId() == "" && fallback.GetSubject() != nil {
		merged.Subject = fallback.GetSubject()
	}
	if caller := merged.GetCaller(); caller == nil || strings.TrimSpace(caller.GetKind()) == "" || strings.TrimSpace(caller.GetName()) == "" {
		if fallback.GetCaller() != nil {
			merged.Caller = fallback.GetCaller()
		}
	}
	if merged.GetCredential() == nil && fallback.GetCredential() != nil {
		merged.Credential = fallback.GetCredential()
	}
	if merged.GetAccess() == nil && fallback.GetAccess() != nil {
		merged.Access = fallback.GetAccess()
	}
	if merged.GetWorkflow() == nil && fallback.GetWorkflow() != nil {
		merged.Workflow = fallback.GetWorkflow()
	}
	if !merged.GetToolRefsSet() && fallback.GetToolRefsSet() {
		merged.ToolRefs = append([]*proto.AgentToolRef(nil), fallback.GetToolRefs()...)
		merged.ToolRefsSet = true
	}
	if merged.GetHost() == nil && fallback.GetHost() != nil {
		merged.Host = fallback.GetHost()
	}
	if merged.GetAgentSubject() == nil && fallback.GetAgentSubject() != nil {
		merged.AgentSubject = fallback.GetAgentSubject()
	}
	if merged.GetInvocation() == nil && fallback.GetInvocation() != nil {
		merged.Invocation = fallback.GetInvocation()
	}
	if merged.GetRequestMeta() == nil && fallback.GetRequestMeta() != nil {
		merged.RequestMeta = fallback.GetRequestMeta()
	}
	return merged
}

func ProviderRequestContextFromProto(reqCtx *proto.RequestContext, expectedKind invocation.ProviderKind, expectedName string) (ProviderRequestContext, error) {
	if reqCtx == nil {
		return ProviderRequestContext{}, status.Error(codes.FailedPrecondition, "request context is required")
	}
	caller := reqCtx.GetCaller()
	callerKind := invocation.ProviderKind(strings.TrimSpace(caller.GetKind()))
	callerName := strings.TrimSpace(caller.GetName())
	if expectedKind != "" && callerKind != expectedKind {
		return ProviderRequestContext{}, status.Errorf(codes.FailedPrecondition, "%s caller context is required", expectedKind)
	}
	if callerName == "" {
		return ProviderRequestContext{}, status.Error(codes.FailedPrecondition, "provider caller context is required")
	}
	if expected := strings.TrimSpace(expectedName); expected != "" && callerName != expected {
		return ProviderRequestContext{}, status.Errorf(codes.PermissionDenied, "provider caller context %q does not match host service caller %q", callerName, expected)
	}

	out := ProviderRequestContext{
		callerName:   callerName,
		callerKind:   callerKind,
		principal:    PrincipalFromSubjectContext(reqCtx.GetSubject()),
		agentSubject: agentwire.RunAsSubjectFromProto(reqCtx.GetAgentSubject()),
		requestMeta:  requestMetaFromProviderRequestContext(reqCtx.GetRequestMeta()),
		credential: invocation.CredentialContext{
			Mode:       core.ConnectionMode(reqCtx.GetCredential().GetMode()),
			SubjectID:  reqCtx.GetCredential().GetSubjectId(),
			Connection: reqCtx.GetCredential().GetConnection(),
			Instance:   reqCtx.GetCredential().GetInstance(),
		},
		workflow: workflowFromProviderRequestContext(reqCtx.GetWorkflow()),
		access: invocation.AccessContext{
			Policy: reqCtx.GetAccess().GetPolicy(),
			Role:   reqCtx.GetAccess().GetRole(),
		},
		host: invocation.HostContext{
			PublicBaseURL: reqCtx.GetHost().GetPublicBaseUrl(),
		},
	}
	if inv := reqCtx.GetInvocation(); inv != nil {
		out.invocation = &invocation.InvocationMeta{
			RequestID: strings.TrimSpace(inv.GetRequestId()),
			Depth:     int(inv.GetDepth()),
			CallChain: append([]string(nil), inv.GetCallChain()...),
		}
		out.surface = invocation.InvocationSurface(strings.TrimSpace(inv.GetSurface()))
		out.internalConnection = inv.GetInternalConnectionAccess()
		out.connection = strings.TrimSpace(inv.GetConnection())
	}
	if reqCtx.GetToolRefsSet() {
		out.toolRefs = agentwire.ToolRefsFromProto(reqCtx.GetToolRefs())
		out.toolRefsSet = true
	}
	return out, nil
}

func (c ProviderRequestContext) CallerName() string {
	return strings.TrimSpace(c.callerName)
}

func (c ProviderRequestContext) CallerKind() invocation.ProviderKind {
	return invocation.ProviderKind(strings.TrimSpace(string(c.callerKind)))
}

func (c ProviderRequestContext) Principal() *principal.Principal {
	return principal.Canonicalized(c.principal)
}

func (c ProviderRequestContext) Credential() invocation.CredentialContext {
	return c.credential
}

func (c ProviderRequestContext) Workflow() map[string]any {
	return invocation.CloneWorkflowContext(c.workflow)
}

func (c ProviderRequestContext) WithWorkflow(workflow *structpb.Struct) ProviderRequestContext {
	if workflow == nil {
		return c
	}
	c.workflow = invocation.CloneWorkflowContext(workflow.AsMap())
	return c
}

func (c ProviderRequestContext) Restore(ctx context.Context, connectionOverride string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.principal != nil {
		ctx = principal.WithPrincipal(ctx, principal.Canonicalized(c.principal))
	}
	if c.agentSubject != nil {
		ctx = invocation.WithRunAsAudit(ctx, c.agentSubject, agentwire.RunAsSubjectFromProto(subjectContextFromPrincipal(c.principal)))
	}
	if c.callerName != "" && c.callerKind != "" {
		ctx = invocation.WithCallerProvider(ctx, c.callerKind, c.callerName)
	}
	if c.invocation != nil {
		ctx = invocation.ContextWithMeta(ctx, &invocation.InvocationMeta{
			RequestID: c.invocation.RequestID,
			Depth:     c.invocation.Depth,
			CallChain: append([]string(nil), c.invocation.CallChain...),
		})
	}
	if c.requestMeta != (invocation.RequestMeta{}) {
		ctx = invocation.WithRequestMeta(ctx, c.requestMeta)
	}
	if c.credential != (invocation.CredentialContext{}) {
		ctx = invocation.WithCredentialContext(ctx, c.credential)
	}
	if c.surface != "" {
		ctx = invocation.WithInvocationSurface(ctx, c.surface)
	}
	if c.internalConnection {
		ctx = invocation.WithInternalConnectionAccess(ctx)
	}
	if c.workflow != nil {
		ctx = invocation.WithWorkflowContext(ctx, invocation.CloneWorkflowContext(c.workflow))
	}
	if c.access != (invocation.AccessContext{}) {
		ctx = invocation.WithAccessContext(ctx, c.access)
	}
	if c.host != (invocation.HostContext{}) {
		ctx = invocation.WithHostContext(ctx, c.host)
	}
	if c.toolRefsSet {
		ctx = invocation.WithToolRefsContext(ctx, c.toolRefs)
	}

	connection := strings.TrimSpace(connectionOverride)
	if connection == "" {
		connection = strings.TrimSpace(c.connection)
	}
	if connection == "" {
		connection = strings.TrimSpace(c.credential.Connection)
	}
	if connection != "" {
		ctx = invocation.WithConnection(ctx, connection)
	}
	return ctx
}

// PrincipalFromSubjectContext reconstructs the trusted caller principal carried
// on provider request context.
func PrincipalFromSubjectContext(subject *proto.SubjectContext) *principal.Principal {
	if subject == nil {
		return nil
	}
	p := &principal.Principal{
		SubjectID:           strings.TrimSpace(subject.GetId()),
		CredentialSubjectID: strings.TrimSpace(subject.GetCredentialSubjectId()),
		Scopes:              cloneStrings(subject.GetScopes()),
		TokenPermissions:    subjectPermissionContextToPermissionSet(subject.GetPermissions()),
	}
	if kind, _, ok := core.ParseSubjectID(p.SubjectID); ok {
		p.Kind = principal.Kind(kind)
	}
	p.UserID = principal.UserIDFromSubjectID(p.SubjectID)
	p = principal.Canonicalized(p)
	if p.Kind == principal.KindUser && strings.TrimSpace(subject.GetEmail()) != "" {
		p.Identity = &core.UserIdentity{Email: strings.TrimSpace(subject.GetEmail())}
	}
	if p.UserID == "" && p.SubjectID == "" && p.Kind == "" && p.Identity == nil && p.Source == principal.SourceUnknown {
		return &principal.Principal{}
	}
	return p
}

func subjectContextFromPrincipal(p *principal.Principal) *proto.SubjectContext {
	p = principal.Canonicalized(p)
	if p == nil {
		return nil
	}
	return &proto.SubjectContext{
		Id:                  p.SubjectID,
		CredentialSubjectId: p.CredentialSubjectID,
		Email:               subjectEmail(p),
		Scopes:              cloneStrings(p.Scopes),
		Permissions:         permissionSetToSubjectPermissionContext(p.TokenPermissions),
	}
}

func subjectIDForPrincipal(p *principal.Principal) string {
	p = principal.Canonicalized(p)
	if p == nil {
		return ""
	}
	return p.SubjectID
}

func subjectEmail(p *principal.Principal) string {
	if p == nil || p.Identity == nil {
		return ""
	}
	return strings.TrimSpace(p.Identity.Email)
}

func requestMetaFromProviderRequestContext(meta *proto.RequestMetaContext) invocation.RequestMeta {
	if meta == nil {
		return invocation.RequestMeta{}
	}
	return invocation.RequestMeta{
		ClientIP:   meta.GetClientIp(),
		RemoteAddr: meta.GetRemoteAddr(),
		UserAgent:  meta.GetUserAgent(),
	}
}

func workflowFromProviderRequestContext(workflow *structpb.Struct) map[string]any {
	if workflow == nil {
		return nil
	}
	return invocation.CloneWorkflowContext(workflow.AsMap())
}

func permissionSetToSubjectPermissionContext(set principal.PermissionSet) []*proto.SubjectPermissionContext {
	perms := principal.PermissionsToAccessPermissions(set)
	if len(perms) == 0 {
		return nil
	}
	out := make([]*proto.SubjectPermissionContext, 0, len(perms))
	for _, perm := range perms {
		app := strings.TrimSpace(perm.App)
		if app == "" {
			continue
		}
		ctx := &proto.SubjectPermissionContext{App: app}
		if len(perm.Operations) == 0 {
			ctx.AllOperations = true
		} else {
			ctx.Operations = cloneStrings(perm.Operations)
		}
		out = append(out, ctx)
	}
	return out
}

func subjectPermissionContextToPermissionSet(values []*proto.SubjectPermissionContext) principal.PermissionSet {
	if len(values) == 0 {
		return nil
	}
	perms := make([]core.AccessPermission, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		app := strings.TrimSpace(value.GetApp())
		if app == "" {
			continue
		}
		perm := core.AccessPermission{App: app}
		if !value.GetAllOperations() {
			perm.Operations = cloneStrings(value.GetOperations())
		}
		perms = append(perms, perm)
	}
	return principal.CompilePermissions(perms)
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}
