package gestalt

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ProviderServer adapts a [Provider] implementation to the gRPC
// AppProvider service. Most integration-provider authors should use
// [ServeProvider] instead of constructing this directly.
type ProviderServer struct {
	proto.UnimplementedAppProviderServer
	provider   Provider
	executeFn  func(ctx context.Context, operation string, params map[string]any, token string) (*OperationResult, error)
	sessionCat func() (SessionCatalogProvider, bool)
}

// NewProviderServer adapts provider plus router into the gRPC integration
// surface used by gestaltd.
func NewProviderServer[P any, PP interface {
	*P
	Provider
}](provider PP, router *Router[P]) *ProviderServer {
	return &ProviderServer{
		provider: provider,
		executeFn: func(ctx context.Context, operation string, params map[string]any, token string) (*OperationResult, error) {
			if router == nil {
				return nil, fmt.Errorf("router is nil")
			}
			return router.Execute(ctx, (*P)(provider), operation, params, token)
		},
		sessionCat: func() (SessionCatalogProvider, bool) {
			scp, ok := any(provider).(SessionCatalogProvider)
			return scp, ok
		},
	}
}

func (s *ProviderServer) StartProvider(ctx context.Context, req *proto.StartProviderRequest) (*proto.StartProviderResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := validateProtocolVersion(req.GetProtocolVersion()); err != nil {
		return nil, err
	}
	config := req.GetConfig().AsMap()
	if config == nil {
		config = map[string]any{}
	}
	if err := s.provider.Configure(ctx, req.GetName(), config); err != nil {
		return nil, status.Errorf(codes.Unknown, "configure provider: %v", err)
	}
	return &proto.StartProviderResponse{
		ProtocolVersion: proto.CurrentProtocolVersion,
	}, nil
}

func (s *ProviderServer) GetMetadata(_ context.Context, _ *emptypb.Empty) (*proto.ProviderMetadata, error) {
	_, ok := s.sessionCat()
	return &proto.ProviderMetadata{
		SupportsSessionCatalog: ok,
		MinProtocolVersion:     proto.CurrentProtocolVersion,
		MaxProtocolVersion:     proto.CurrentProtocolVersion,
	}, nil
}

func (s *ProviderServer) Execute(ctx context.Context, req *proto.ExecuteRequest) (*proto.OperationResult, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	ctx = withRequestContext(ctx, req.GetContext())
	ctx = WithIdempotencyKey(ctx, req.GetIdempotencyKey())
	if len(req.GetConnectionParams()) > 0 {
		ctx = WithConnectionParams(ctx, req.GetConnectionParams())
	}
	result, err := s.executeFn(ctx, req.GetOperation(), req.GetParams().AsMap(), req.GetToken())
	if err != nil {
		return operationResultProto(operationResultFromError(err)), nil
	}
	if result == nil {
		return operationResultProto(operationResult(http.StatusInternalServerError, nilResultMessage)), nil
	}
	return operationResultProto(result), nil
}

func (s *ProviderServer) ResolveHTTPSubject(ctx context.Context, req *proto.ResolveHTTPSubjectRequest) (resp *proto.ResolveHTTPSubjectResponse, err error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	ctx = withRequestContext(ctx, req.GetContext())
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = recoveredOperationResult("ResolveHTTPSubject", recovered)
			err = status.Error(codes.Internal, internalErrorMessage)
		}
	}()

	resolver, ok := s.provider.(HTTPSubjectResolver)
	if !ok {
		return &proto.ResolveHTTPSubjectResponse{}, nil
	}

	subject, err := resolver.ResolveHTTPSubject(ctx, httpSubjectRequestFromProto(req.GetRequest()))
	if err != nil {
		var opErr *operationError
		if errors.As(err, &opErr) {
			return &proto.ResolveHTTPSubjectResponse{
				RejectStatus:  int32(opErr.status),
				RejectMessage: opErr.message,
			}, nil
		}
		return nil, providerRPCError("resolve http subject", err)
	}
	if subject == nil {
		return &proto.ResolveHTTPSubjectResponse{}, nil
	}

	return &proto.ResolveHTTPSubjectResponse{
		Subject: &proto.SubjectContext{
			Id:                  subject.ID,
			CredentialSubjectId: subject.CredentialSubjectID,
			Email:               subject.Email,
			DisplayName:         subject.DisplayName,
		},
	}, nil
}

func httpSubjectRequestFromProto(req *proto.HTTPSubjectRequest) HTTPSubjectRequest {
	if req == nil {
		return HTTPSubjectRequest{}
	}
	return HTTPSubjectRequest{
		Binding:         req.GetBinding(),
		Method:          req.GetMethod(),
		Path:            req.GetPath(),
		ContentType:     req.GetContentType(),
		Headers:         httpHeaderFromProto(req.GetHeaders()),
		Query:           urlValuesFromProto(req.GetQuery()),
		Params:          req.GetParams().AsMap(),
		RawBody:         append([]byte(nil), req.GetRawBody()...),
		SecurityScheme:  req.GetSecurityScheme(),
		VerifiedSubject: req.GetVerifiedSubject(),
		VerifiedClaims:  cloneVerifiedClaims(req.GetVerifiedClaims()),
	}
}

func httpHeaderFromProto(values map[string]*proto.StringList) http.Header {
	if len(values) == 0 {
		return nil
	}
	out := make(http.Header, len(values))
	for key, value := range values {
		for _, item := range value.GetValues() {
			out.Add(key, item)
		}
	}
	return out
}

func httpHeaderToProto(values http.Header) map[string]*proto.StringList {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]*proto.StringList, len(values))
	for key, value := range values {
		out[key] = &proto.StringList{Values: append([]string(nil), value...)}
	}
	return out
}

func urlValuesFromProto(values map[string]*proto.StringList) url.Values {
	if len(values) == 0 {
		return nil
	}
	out := make(url.Values, len(values))
	for key, value := range values {
		out[key] = append([]string(nil), value.GetValues()...)
	}
	return out
}

func cloneVerifiedClaims(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func (s *ProviderServer) GetSessionCatalog(ctx context.Context, req *proto.GetSessionCatalogRequest) (*proto.GetSessionCatalogResponse, error) {
	scp, ok := s.sessionCat()
	if !ok {
		return nil, status.Error(codes.Unimplemented, "provider does not support session catalogs")
	}
	ctx = withRequestContext(ctx, req.GetContext())
	if len(req.GetConnectionParams()) > 0 {
		ctx = WithConnectionParams(ctx, req.GetConnectionParams())
	}
	cat, err := scp.CatalogForRequest(ctx, req.GetToken())
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "session catalog: %v", err)
	}
	pbCat, err := catalogToProto(cat)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "session catalog: %v", err)
	}
	return &proto.GetSessionCatalogResponse{Catalog: pbCat}, nil
}

func operationResultProto(result *OperationResult) *proto.OperationResult {
	if result == nil {
		return nil
	}
	return &proto.OperationResult{
		Status:  int32(result.Status),
		Headers: httpHeaderToProto(result.Headers),
		Body:    result.Body,
	}
}

func withRequestContext(ctx context.Context, reqCtx *proto.RequestContext) context.Context {
	if reqCtx == nil {
		return ctx
	}
	ctx = context.WithValue(ctx, requestContextKey{}, reqCtx)
	if subject := reqCtx.GetSubject(); subject != nil {
		ctx = WithSubject(ctx, Subject{
			ID:                  subject.GetId(),
			CredentialSubjectID: subject.GetCredentialSubjectId(),
			Email:               subject.GetEmail(),
			DisplayName:         subject.GetDisplayName(),
			Scopes:              cloneStrings(subject.GetScopes()),
			Permissions:         subjectPermissionsFromProto(subject.GetPermissions()),
		})
	}
	if subject := reqCtx.GetAgentSubject(); subject != nil {
		ctx = WithAgentSubject(ctx, Subject{
			ID:                  subject.GetId(),
			CredentialSubjectID: subject.GetCredentialSubjectId(),
			Email:               subject.GetEmail(),
			DisplayName:         subject.GetDisplayName(),
			Scopes:              cloneStrings(subject.GetScopes()),
			Permissions:         subjectPermissionsFromProto(subject.GetPermissions()),
		})
	}
	if credential := reqCtx.GetCredential(); credential != nil {
		ctx = WithCredential(ctx, Credential{
			Mode:       credential.GetMode(),
			SubjectID:  credential.GetSubjectId(),
			Connection: credential.GetConnection(),
			Instance:   credential.GetInstance(),
		})
	}
	if access := reqCtx.GetAccess(); access != nil {
		ctx = WithAccess(ctx, Access{
			Policy: access.GetPolicy(),
			Role:   access.GetRole(),
		})
	}
	if host := reqCtx.GetHost(); host != nil {
		ctx = WithHostContext(ctx, Host{
			PublicBaseURL: host.GetPublicBaseUrl(),
		})
	}
	if workflow := reqCtx.GetWorkflow(); workflow != nil {
		ctx = WithWorkflowContext(ctx, workflow.AsMap())
	}
	if reqCtx.GetToolRefsSet() {
		ctx = WithToolRefsContext(ctx, agentToolRefsFromProto(reqCtx.GetToolRefs()))
	}
	return ctx
}

type requestContextKey struct{}

func requestContextFromContext(ctx context.Context) *proto.RequestContext {
	reqCtx, _ := ctx.Value(requestContextKey{}).(*proto.RequestContext)
	return reqCtx
}
