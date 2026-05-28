package plugins

import (
	"context"
	"errors"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/agentwire"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ProviderServer struct {
	proto.UnimplementedAppProviderServer
	provider core.Provider
}

func NewProviderServer(provider core.Provider) *ProviderServer {
	return &ProviderServer{provider: provider}
}

func NewServer(provider core.Provider) proto.AppProviderServer {
	return NewProviderServer(provider)
}

func (s *ProviderServer) GetMetadata(_ context.Context, _ *emptypb.Empty) (*proto.ProviderMetadata, error) {
	return &proto.ProviderMetadata{
		SupportsSessionCatalog: core.SupportsSessionCatalog(s.provider),
		SupportsPostConnect:    core.SupportsPostConnect(s.provider),
	}, nil
}

func (s *ProviderServer) Execute(ctx context.Context, req *proto.ExecuteRequest) (*proto.OperationResult, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	ctx = applyRequestContext(ctx, req.GetContext())
	ctx = appaccessservice.WithInvocationToken(ctx, req.GetInvocationToken())
	ctx = invocation.WithIdempotencyKey(ctx, req.GetIdempotencyKey())
	if len(req.GetConnectionParams()) > 0 {
		ctx = core.WithConnectionParams(ctx, req.GetConnectionParams())
	}
	result, err := s.provider.Execute(ctx, req.GetOperation(), protoutil.MapFromStruct(req.GetParams()), req.GetToken())
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "execute: %v", err)
	}
	return &proto.OperationResult{
		Status:  int32(result.Status),
		Headers: mapStringSlices(result.Headers),
		Body:    result.Body,
	}, nil
}

func (s *ProviderServer) GetSessionCatalog(ctx context.Context, req *proto.GetSessionCatalogRequest) (*proto.GetSessionCatalogResponse, error) {
	if !core.SupportsSessionCatalog(s.provider) {
		return nil, status.Error(codes.Unimplemented, "provider does not support session catalogs")
	}
	ctx = applyRequestContext(ctx, req.GetContext())
	if len(req.GetConnectionParams()) > 0 {
		ctx = core.WithConnectionParams(ctx, req.GetConnectionParams())
	}
	cat, _, err := core.CatalogForRequest(ctx, s.provider, req.GetToken())
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "session catalog: %v", err)
	}
	return &proto.GetSessionCatalogResponse{Catalog: catalogToProto(cat)}, nil
}

func (s *ProviderServer) ResolveHTTPSubject(ctx context.Context, req *proto.ResolveHTTPSubjectRequest) (*proto.ResolveHTTPSubjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	ctx = applyRequestContext(ctx, req.GetContext())
	subject, _, err := core.ResolveHTTPSubject(ctx, s.provider, httpSubjectRequestFromProto(req.GetRequest()))
	if err != nil {
		var resolveErr *core.HTTPSubjectResolveError
		if errors.As(err, &resolveErr) {
			return &proto.ResolveHTTPSubjectResponse{
				RejectStatus:  int32(resolveErr.Status),
				RejectMessage: resolveErr.Message,
			}, nil
		}
		return nil, status.Errorf(codes.Unknown, "resolve http subject: %v", err)
	}
	if subject == nil {
		return &proto.ResolveHTTPSubjectResponse{}, nil
	}
	return &proto.ResolveHTTPSubjectResponse{
		Subject: &proto.SubjectContext{
			Id:          subject.ID,
			Kind:        subject.Kind,
			DisplayName: subject.DisplayName,
			AuthSource:  subject.AuthSource,
		},
	}, nil
}

func (s *ProviderServer) PostConnect(ctx context.Context, req *proto.PostConnectRequest) (*proto.PostConnectResponse, error) {
	if !core.SupportsPostConnect(s.provider) {
		return nil, status.Error(codes.Unimplemented, "provider does not support post connect")
	}
	metadata, supported, err := core.PostConnect(ctx, s.provider, postConnectCredentialFromProto(req.GetToken()))
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "post connect: %v", err)
	}
	if !supported {
		return nil, status.Error(codes.Unimplemented, "provider does not support post connect for credential")
	}
	return &proto.PostConnectResponse{Metadata: metadata}, nil
}

func httpSubjectRequestFromProto(req *proto.HTTPSubjectRequest) *core.HTTPSubjectResolveRequest {
	if req == nil {
		return nil
	}
	return &core.HTTPSubjectResolveRequest{
		Binding:         req.GetBinding(),
		Method:          req.GetMethod(),
		Path:            req.GetPath(),
		ContentType:     req.GetContentType(),
		Headers:         mapStringLists(req.GetHeaders()),
		Query:           mapStringLists(req.GetQuery()),
		Params:          protoutil.MapFromStruct(req.GetParams()),
		RawBody:         append([]byte(nil), req.GetRawBody()...),
		SecurityScheme:  req.GetSecurityScheme(),
		VerifiedSubject: req.GetVerifiedSubject(),
		VerifiedClaims:  cloneStringMap(req.GetVerifiedClaims()),
	}
}

func mapStringLists[V ~map[string]*proto.StringList](values V) map[string][]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string][]string, len(values))
	for key, value := range values {
		out[key] = append([]string(nil), value.GetValues()...)
	}
	return out
}

func applyRequestContext(ctx context.Context, reqCtx *proto.RequestContext) context.Context {
	if reqCtx == nil {
		return ctx
	}
	if subject := reqCtx.GetSubject(); subject != nil {
		ctx = principal.WithPrincipal(ctx, principalFromProto(subject))
	}
	if agentSubject := reqCtx.GetAgentSubject(); agentSubject != nil {
		ctx = invocation.WithRunAsAudit(ctx, agentwire.RunAsSubjectFromProto(agentSubject), agentwire.RunAsSubjectFromProto(reqCtx.GetSubject()))
	}
	if identity := reqCtx.GetAgentExternalIdentity(); identity != nil {
		ctx = invocation.WithAgentExternalIdentityContext(ctx, invocation.ExternalIdentityContext{
			Type: identity.GetType(),
			ID:   identity.GetId(),
		})
	}
	if identity := reqCtx.GetExternalIdentity(); identity != nil {
		ctx = invocation.WithExternalIdentityContext(ctx, invocation.ExternalIdentityContext{
			Type: identity.GetType(),
			ID:   identity.GetId(),
		})
	}
	if credential := reqCtx.GetCredential(); credential != nil {
		ctx = invocation.WithCredentialContext(ctx, invocation.CredentialContext{
			Mode:       core.ConnectionMode(credential.GetMode()),
			SubjectID:  credential.GetSubjectId(),
			Connection: credential.GetConnection(),
			Instance:   credential.GetInstance(),
		})
	}
	if access := reqCtx.GetAccess(); access != nil {
		ctx = invocation.WithAccessContext(ctx, invocation.AccessContext{
			Policy: access.GetPolicy(),
			Role:   access.GetRole(),
		})
	}
	if host := reqCtx.GetHost(); host != nil {
		ctx = invocation.WithHostContext(ctx, invocation.HostContext{
			PublicBaseURL: host.GetPublicBaseUrl(),
		})
	}
	if workflow := reqCtx.GetWorkflow(); workflow != nil {
		ctx = invocation.WithWorkflowContext(ctx, workflow.AsMap())
	}
	if reqCtx.GetToolRefsSet() {
		ctx = invocation.WithToolRefsContext(ctx, agentwire.ToolRefsFromProto(reqCtx.GetToolRefs()))
	}
	return ctx
}

func principalFromProto(subject *proto.SubjectContext) *principal.Principal {
	if subject == nil {
		return nil
	}
	displayName := strings.TrimSpace(subject.GetDisplayName())
	email := strings.TrimSpace(subject.GetEmail())
	p := &principal.Principal{
		SubjectID:   subject.GetId(),
		DisplayName: displayName,
	}
	principal.SetAuthSource(p, subject.GetAuthSource())
	if kind := strings.TrimSpace(subject.GetKind()); kind != "" {
		p.Kind = principal.Kind(kind)
	}
	p.UserID = principal.UserIDFromSubjectID(p.SubjectID)
	p = principal.Canonicalized(p)
	if p.Kind == principal.KindUser && (displayName != "" || email != "") {
		p.Identity = &core.UserIdentity{
			Email:       email,
			DisplayName: displayName,
		}
	}
	if p.UserID == "" && p.SubjectID == "" && p.Kind == "" && p.DisplayName == "" && p.Identity == nil && p.Source == principal.SourceUnknown && p.AuthSourceOverride == "" {
		return &principal.Principal{}
	}
	return p
}

func postConnectCredentialFromProto(token *proto.PostConnectCredential) *core.ExternalCredential {
	if token == nil {
		return nil
	}
	out := &core.ExternalCredential{
		ID:                token.GetId(),
		SubjectID:         token.GetSubjectId(),
		Integration:       token.GetIntegration(),
		Connection:        token.GetConnection(),
		Instance:          token.GetInstance(),
		AccessToken:       token.GetAccessToken(),
		RefreshToken:      token.GetRefreshToken(),
		Scopes:            token.GetScopes(),
		RefreshErrorCount: int(token.GetRefreshErrorCount()),
		MetadataJSON:      token.GetMetadataJson(),
	}
	if ts := token.GetExpiresAt(); ts != nil {
		value := ts.AsTime()
		out.ExpiresAt = &value
	}
	if ts := token.GetLastRefreshedAt(); ts != nil {
		value := ts.AsTime()
		out.LastRefreshedAt = &value
	}
	if ts := token.GetCreatedAt(); ts != nil {
		out.CreatedAt = ts.AsTime()
	}
	if ts := token.GetUpdatedAt(); ts != nil {
		out.UpdatedAt = ts.AsTime()
	}
	return out
}
