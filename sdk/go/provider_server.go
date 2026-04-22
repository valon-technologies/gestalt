package gestalt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ProviderServer adapts a [Provider] implementation to the gRPC
// IntegrationProvider service. Most integration-provider authors should use
// [ServeProvider] instead of constructing this directly.
type ProviderServer struct {
	proto.UnimplementedIntegrationProviderServer
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
	ctx = withInvocationToken(ctx, req.GetInvocationToken())
	if len(req.GetConnectionParams()) > 0 {
		ctx = WithConnectionParams(ctx, req.GetConnectionParams())
	}
	if req.GetOperation() == proto.InternalResolveHTTPSubjectOperation {
		return s.resolveHTTPSubject(ctx, req.GetParams().AsMap()), nil
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

type httpSubjectExecuteRequest struct {
	Binding         string              `json:"binding"`
	Method          string              `json:"method"`
	Path            string              `json:"path"`
	ContentType     string              `json:"content_type"`
	Headers         map[string][]string `json:"headers"`
	Query           map[string][]string `json:"query"`
	Params          map[string]any      `json:"params"`
	RawBodyBase64   string              `json:"raw_body_base64"`
	SecurityScheme  string              `json:"security_scheme"`
	VerifiedSubject string              `json:"verified_subject"`
	VerifiedClaims  map[string]string   `json:"verified_claims"`
}

func (s *ProviderServer) resolveHTTPSubject(ctx context.Context, rawParams map[string]any) *proto.OperationResult {
	return operationResultProto(protectedOperationResult(proto.InternalResolveHTTPSubjectOperation, func() (*OperationResult, error) {
		resolver, ok := s.provider.(HTTPSubjectResolver)
		if !ok {
			return operationResult(http.StatusNotFound, unknownOperationMessage), nil
		}

		var payload httpSubjectExecuteRequest
		if err := decodeParams(rawParams, &payload); err != nil {
			return nil, newOperationError(http.StatusBadRequest, "decode http subject request", err)
		}

		var rawBody []byte
		if payload.RawBodyBase64 != "" {
			decoded, err := base64.StdEncoding.DecodeString(payload.RawBodyBase64)
			if err != nil {
				return nil, newOperationError(http.StatusBadRequest, "decode http subject raw body", err)
			}
			rawBody = decoded
		}

		subject, err := resolver.ResolveHTTPSubject(ctx, HTTPSubjectRequest{
			Binding:         payload.Binding,
			Method:          payload.Method,
			Path:            payload.Path,
			ContentType:     payload.ContentType,
			Headers:         http.Header(payload.Headers),
			Query:           url.Values(payload.Query),
			Params:          payload.Params,
			RawBody:         rawBody,
			SecurityScheme:  payload.SecurityScheme,
			VerifiedSubject: payload.VerifiedSubject,
			VerifiedClaims:  payload.VerifiedClaims,
		})
		if err != nil {
			return nil, err
		}
		if subject == nil {
			return &OperationResult{Status: http.StatusNoContent}, nil
		}

		body, err := json.Marshal(subject)
		if err != nil {
			return nil, newOperationError(http.StatusInternalServerError, "marshal http subject response", err)
		}
		return &OperationResult{
			Status: http.StatusOK,
			Body:   string(body),
		}, nil
	}))
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
	return &proto.GetSessionCatalogResponse{Catalog: cat}, nil
}

func operationResultProto(result *OperationResult) *proto.OperationResult {
	if result == nil {
		return nil
	}
	return &proto.OperationResult{
		Status: int32(result.Status),
		Body:   result.Body,
	}
}

func withRequestContext(ctx context.Context, reqCtx *proto.RequestContext) context.Context {
	if reqCtx == nil {
		return ctx
	}
	if subject := reqCtx.GetSubject(); subject != nil {
		ctx = WithSubject(ctx, Subject{
			ID:          subject.GetId(),
			Kind:        subject.GetKind(),
			DisplayName: subject.GetDisplayName(),
			AuthSource:  subject.GetAuthSource(),
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
	if workflow := reqCtx.GetWorkflow(); workflow != nil {
		ctx = WithWorkflowContext(ctx, workflow.AsMap())
	}
	return ctx
}
