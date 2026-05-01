package plugins

import (
	"context"
	"strings"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ProviderServer struct {
	proto.UnimplementedIntegrationProviderServer
	provider core.Provider
}

func NewProviderServer(provider core.Provider) *ProviderServer {
	return &ProviderServer{provider: provider}
}

func NewServer(provider core.Provider) proto.IntegrationProviderServer {
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
	ctx = plugininvokerservice.WithInvocationToken(ctx, req.GetInvocationToken())
	ctx = invocation.WithIdempotencyKey(ctx, req.GetIdempotencyKey())
	if len(req.GetConnectionParams()) > 0 {
		ctx = core.WithConnectionParams(ctx, req.GetConnectionParams())
	}
	result, err := s.provider.Execute(ctx, req.GetOperation(), mapFromStruct(req.GetParams()), req.GetToken())
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "execute: %v", err)
	}
	return &proto.OperationResult{
		Status: int32(result.Status),
		Body:   result.Body,
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

func (s *ProviderServer) PostConnect(ctx context.Context, req *proto.PostConnectRequest) (*proto.PostConnectResponse, error) {
	if !core.SupportsPostConnect(s.provider) {
		return nil, status.Error(codes.Unimplemented, "provider does not support post connect")
	}
	metadata, _, err := core.PostConnect(ctx, s.provider, postConnectCredentialFromProto(req.GetToken()))
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "post connect: %v", err)
	}
	return &proto.PostConnectResponse{Metadata: metadata}, nil
}

func applyRequestContext(ctx context.Context, reqCtx *proto.RequestContext) context.Context {
	if reqCtx == nil {
		return ctx
	}
	if subject := reqCtx.GetSubject(); subject != nil {
		ctx = principal.WithPrincipal(ctx, principalFromProto(subject))
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
	if workflow := reqCtx.GetWorkflow(); workflow != nil {
		ctx = invocation.WithWorkflowContext(ctx, workflow.AsMap())
	}
	return ctx
}

func principalFromProto(subject *proto.SubjectContext) *principal.Principal {
	if subject == nil {
		return nil
	}
	p := &principal.Principal{
		SubjectID:   subject.GetId(),
		DisplayName: subject.GetDisplayName(),
	}
	principal.SetAuthSource(p, subject.GetAuthSource())
	if kind := strings.TrimSpace(subject.GetKind()); kind != "" {
		p.Kind = principal.Kind(kind)
	}
	p.UserID = principal.UserIDFromSubjectID(p.SubjectID)
	p = principal.Canonicalized(p)
	if p.Kind == principal.KindUser && subject.GetDisplayName() != "" {
		p.Identity = &core.UserIdentity{
			DisplayName: subject.GetDisplayName(),
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
