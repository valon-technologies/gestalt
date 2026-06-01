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
		return nil, providerExecuteError(err)
	}
	return &proto.OperationResult{
		Status:  int32(result.Status),
		Headers: protoutil.StringSlicesToProto(result.Headers),
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

func httpSubjectRequestFromProto(req *proto.HTTPSubjectRequest) *core.HTTPSubjectResolveRequest {
	if req == nil {
		return nil
	}
	return &core.HTTPSubjectResolveRequest{
		Binding:         req.GetBinding(),
		Method:          req.GetMethod(),
		Path:            req.GetPath(),
		ContentType:     req.GetContentType(),
		Headers:         protoutil.StringListsFromProto(req.GetHeaders()),
		Query:           protoutil.StringListsFromProto(req.GetQuery()),
		Params:          protoutil.MapFromStruct(req.GetParams()),
		RawBody:         append([]byte(nil), req.GetRawBody()...),
		SecurityScheme:  req.GetSecurityScheme(),
		VerifiedSubject: req.GetVerifiedSubject(),
		VerifiedClaims:  cloneStringMap(req.GetVerifiedClaims()),
	}
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
