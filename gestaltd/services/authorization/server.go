package authorization

import (
	"context"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
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
	return s.provider.WriteRelationships(s.gatewayContext(ctx), stampPublicRuntimeWriteRelationships(ctx, req))
}

func (s *providerServer) AddRelationship(ctx context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	startedAt := time.Now()
	resp, err := s.provider.AddRelationship(s.gatewayContext(ctx), stampPublicRuntimeAddRelationship(ctx, req))
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
	appID, action, targetSubjectKind, targetSubjectID, ok := providergateway.AppScopedRelationshipMutationFromContext(ctx)
	if !ok {
		return
	}
	failed := err != nil
	principalKind, principalID := principal.MetricAuthorizationSubject(principal.FromContext(ctx))
	interaction := observability.AppAdminUIInteraction{
		App:                  appID,
		Surface:              observability.AppAdminUISurfaceMembers,
		Action:               action,
		Failed:               failed,
		Err:                  err,
		TargetSubjectKind:    targetSubjectKind,
		TargetSubjectID:      targetSubjectID,
		PrincipalSubjectKind: principalKind,
		PrincipalSubjectID:   principalID,
	}
	if failed {
		interaction.FailureCategory = observability.AppAdminUIFailureCategoryGRPC(err)
	}
	if meta := invocation.MetaFromContext(ctx); meta != nil {
		interaction.RequestID = strings.TrimSpace(meta.RequestID)
	}
	observability.RecordAppAdminUIInteraction(ctx, startedAt, interaction)
}

func relationshipWithPublicRuntimeSourceLayer(ctx context.Context, relationship *proto.Relationship) *proto.Relationship {
	if _, ok := publicrpc.PublicOriginFromContext(ctx); !ok {
		return relationship
	}
	if relationship == nil || relationship.GetSourceLayer() != proto.SourceLayer_SOURCE_LAYER_UNSPECIFIED {
		return relationship
	}
	cloned := gproto.Clone(relationship).(*proto.Relationship)
	cloned.SourceLayer = proto.SourceLayer_SOURCE_LAYER_RUNTIME
	return cloned
}

func stampPublicRuntimeAddRelationship(ctx context.Context, req *proto.AddRelationshipRequest) *proto.AddRelationshipRequest {
	stamped := relationshipWithPublicRuntimeSourceLayer(ctx, req.GetRelationship())
	if stamped == req.GetRelationship() {
		return req
	}
	cloned := gproto.Clone(req).(*proto.AddRelationshipRequest)
	cloned.Relationship = stamped
	return cloned
}

func stampPublicRuntimeWriteRelationships(ctx context.Context, req *proto.WriteRelationshipsRequest) *proto.WriteRelationshipsRequest {
	if _, ok := publicrpc.PublicOriginFromContext(ctx); !ok {
		return req
	}
	cloned := req
	for i, update := range req.GetUpdates() {
		if update.GetOperation() != proto.RelationshipUpdate_OPERATION_TOUCH {
			continue
		}
		stamped := relationshipWithPublicRuntimeSourceLayer(ctx, update.GetRelationship())
		if stamped == update.GetRelationship() {
			continue
		}
		if cloned == req {
			cloned = gproto.Clone(req).(*proto.WriteRelationshipsRequest)
		}
		cloned.Updates[i].Relationship = stamped
	}
	return cloned
}
