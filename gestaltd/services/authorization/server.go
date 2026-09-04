package authorization

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type providerServer struct {
	proto.UnimplementedAuthorizationServer
	provider core.AuthorizationProvider
}

func NewProviderServer(provider core.AuthorizationProvider) proto.AuthorizationServer {
	return &providerServer{provider: provider}
}

func (s *providerServer) CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.CheckAccess(s.gatewayContext(ctx), req)
}

func (s *providerServer) CheckAccessMany(ctx context.Context, req *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.CheckAccessMany(s.gatewayContext(ctx), req)
}

func (s *providerServer) ListRelationships(ctx context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.ListRelationships(s.gatewayContext(ctx), req)
}

func (s *providerServer) WriteRelationships(ctx context.Context, req *proto.WriteRelationshipsRequest) (*proto.WriteRelationshipsResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.WriteRelationships(s.gatewayContext(ctx), req)
}

func (s *providerServer) AddRelationship(ctx context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	startedAt := time.Now()
	resp, err := s.provider.AddRelationship(s.gatewayContext(ctx), req)
	recordAppScopedRelationshipMutation(ctx, startedAt, err)
	return resp, err
}

func (s *providerServer) DeleteRelationship(ctx context.Context, req *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	startedAt := time.Now()
	resp, err := s.provider.DeleteRelationship(s.gatewayContext(ctx), req)
	recordAppScopedRelationshipMutation(ctx, startedAt, err)
	return resp, err
}

func (s *providerServer) SetAuthorizationState(ctx context.Context, req *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.SetAuthorizationState(s.gatewayContext(ctx), req)
}

func (s *providerServer) GetActiveModelRef(ctx context.Context, _ *emptypb.Empty) (*proto.GetActiveModelRefResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.GetActiveModelRef(s.gatewayContext(ctx))
}

func (s *providerServer) SetActiveModel(ctx context.Context, req *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.SetActiveModel(s.gatewayContext(ctx), req)
}

func (s *providerServer) ListActiveModelResourceTypes(ctx context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.ListActiveModelResourceTypes(s.gatewayContext(ctx), req)
}

func (s *providerServer) gatewayContext(ctx context.Context) context.Context {
	return invocation.WithEntry(ctx, invocation.EntryGRPC)
}

func (s *providerServer) requireProvider() error {
	if s == nil || s.provider == nil {
		return status.Error(codes.FailedPrecondition, "authorization provider is not configured")
	}
	return nil
}

func recordAppScopedRelationshipMutation(ctx context.Context, startedAt time.Time, err error) {
	if _, ok := publicrpc.PublicOriginFromContext(ctx); !ok {
		return
	}
	appID, action, ok := providergateway.AppScopedRelationshipMutationFromContext(ctx)
	if !ok {
		return
	}
	failed := err != nil
	outcome := "success"
	if failed {
		outcome = "failure"
	}
	attrs := []attribute.KeyValue{
		observability.AttrAppAdminUIApp.String(appID),
		observability.AttrAppAdminUISurface.String("members"),
		observability.AttrAppAdminUIAction.String(action),
		observability.AttrAppAdminUIOutcome.String(outcome),
	}
	if failed {
		failureCategory := appAdminUIFailureCategoryFromRPC(err)
		attrs = append(attrs, observability.AttrAppAdminUIFailureCategory.String(failureCategory))
	}
	observability.RecordAppAdminUI(ctx, startedAt, failed, attrs...)
	if failed {
		logAppScopedRelationshipMutationFailure(ctx, appID, action, appAdminUIFailureCategoryFromRPC(err), err)
	}
}

func appAdminUIFailureCategoryFromRPC(err error) string {
	if err == nil {
		return ""
	}
	switch status.Code(err) {
	case codes.Unauthenticated, codes.PermissionDenied:
		return "auth_failure"
	case codes.InvalidArgument:
		return "validation"
	case codes.Internal, codes.Unavailable:
		return "server"
	default:
		return "other"
	}
}

func logAppScopedRelationshipMutationFailure(ctx context.Context, appID, action, failureCategory string, err error) {
	attrs := []any{
		slog.String("event", "app_admin.ui"),
		slog.String("app", appID),
		slog.String("surface", "members"),
		slog.String("action", action),
		slog.String("failure_category", failureCategory),
	}
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	if meta := invocation.MetaFromContext(ctx); meta != nil {
		if requestID := strings.TrimSpace(meta.RequestID); requestID != "" {
			attrs = append(attrs, slog.String("request_id", requestID))
		}
	}
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		attrs = append(attrs, slog.String("trace_id", spanCtx.TraceID().String()))
	}
	slog.WarnContext(ctx, "app admin ui interaction failed", attrs...)
}
