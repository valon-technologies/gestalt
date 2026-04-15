package providerhost

import (
	"context"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ProviderServer struct {
	proto.UnimplementedIntegrationProviderServer
	provider core.Provider
}

type requestHandleCtxKey struct{}

func NewProviderServer(provider core.Provider) *ProviderServer {
	return &ProviderServer{provider: provider}
}

func (s *ProviderServer) GetMetadata(_ context.Context, _ *emptypb.Empty) (*proto.ProviderMetadata, error) {
	return &proto.ProviderMetadata{
		SupportsSessionCatalog: supportsSessionCatalog(s.provider),
	}, nil
}

func (s *ProviderServer) Execute(ctx context.Context, req *proto.ExecuteRequest) (*proto.OperationResult, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	ctx = applyRequestContext(ctx, req.GetContext())
	ctx = WithRequestHandle(ctx, req.GetRequestHandle())
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
	scp, ok := s.provider.(core.SessionCatalogProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "provider does not support session catalogs")
	}
	ctx = applyRequestContext(ctx, req.GetContext())
	if len(req.GetConnectionParams()) > 0 {
		ctx = core.WithConnectionParams(ctx, req.GetConnectionParams())
	}
	cat, err := scp.CatalogForRequest(ctx, req.GetToken())
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "session catalog: %v", err)
	}
	return &proto.GetSessionCatalogResponse{Catalog: catalogToProto(cat)}, nil
}

func supportsSessionCatalog(prov core.Provider) bool {
	_, ok := prov.(core.SessionCatalogProvider)
	return ok
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
	return ctx
}

func WithRequestHandle(ctx context.Context, handle string) context.Context {
	return context.WithValue(ctx, requestHandleCtxKey{}, handle)
}

func RequestHandleFromContext(ctx context.Context) string {
	handle, _ := ctx.Value(requestHandleCtxKey{}).(string)
	return handle
}

func principalFromProto(subject *proto.SubjectContext) *principal.Principal {
	if subject == nil {
		return nil
	}
	p := &principal.Principal{
		SubjectID:   subject.GetId(),
		DisplayName: subject.GetDisplayName(),
		Source:      sourceFromString(subject.GetAuthSource()),
	}
	switch subject.GetKind() {
	case string(principal.KindUser):
		p.Kind = principal.KindUser
	case string(principal.KindWorkload):
		p.Kind = principal.KindWorkload
	}
	if strings.HasPrefix(subject.GetId(), "user:") {
		p.UserID = strings.TrimPrefix(subject.GetId(), "user:")
		if p.Kind == "" {
			p.Kind = principal.KindUser
		}
	} else if strings.HasPrefix(subject.GetId(), "workload:") && p.Kind == "" {
		p.Kind = principal.KindWorkload
	}
	if p.Kind == principal.KindUser && subject.GetDisplayName() != "" {
		p.Identity = &core.UserIdentity{
			DisplayName: subject.GetDisplayName(),
		}
	}
	if p.UserID == "" && p.SubjectID == "" && p.Kind == "" && p.DisplayName == "" && p.Identity == nil && p.Source == principal.SourceUnknown {
		return &principal.Principal{}
	}
	return p
}

func sourceFromString(raw string) principal.Source {
	switch raw {
	case principal.SourceSession.String():
		return principal.SourceSession
	case principal.SourceAPIToken.String():
		return principal.SourceAPIToken
	case principal.SourceWorkloadToken.String():
		return principal.SourceWorkloadToken
	case principal.SourceEnv.String():
		return principal.SourceEnv
	default:
		return principal.SourceUnknown
	}
}
