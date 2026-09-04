package authorization

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAddRelationshipRecordsAppScopedMutationMetrics(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)
	ctx = publicrpc.WithPublicOrigin(ctx, proto.Authorization_AddRelationship_FullMethodName)
	ctx = providergateway.WithAppScopedRelationshipMutationAuth(ctx, "roadmap", "grant_add")

	provider := &stubAuthorizationProvider{}
	server := NewProviderServer(provider)

	_, err := server.AddRelationship(ctx, &proto.AddRelationshipRequest{
		Relationship: &proto.Relationship{
			Tuple: &proto.RelationshipTuple{
				Resource: &proto.Resource{Type: "app", Id: "roadmap"},
				Relation: "viewer",
			},
		},
	})
	if err != nil {
		t.Fatalf("AddRelationship: %v", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{
		"gestaltd.app_admin.ui.app":     "roadmap",
		"gestaltd.app_admin.ui.surface": "members",
		"gestaltd.app_admin.ui.action":  "grant_add",
		"gestaltd.app_admin.ui.outcome": "success",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.ui.count", 1, attrs)
	metrictest.RequireNoInt64Sum(t, rm, "gestaltd.app_admin.ui.error_count", attrs)
}

func TestAddRelationshipSkipsMetricsWithoutAppScopedMarker(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)
	ctx = publicrpc.WithPublicOrigin(ctx, proto.Authorization_AddRelationship_FullMethodName)

	provider := &stubAuthorizationProvider{}
	server := NewProviderServer(provider)

	_, err := server.AddRelationship(ctx, &proto.AddRelationshipRequest{})
	if err != nil {
		t.Fatalf("AddRelationship: %v", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	metrictest.RequireNoMetric(t, rm, "gestaltd.app_admin.ui.count")
}

func TestDeleteRelationshipRecordsAppScopedMutationFailure(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)
	ctx = publicrpc.WithPublicOrigin(ctx, proto.Authorization_DeleteRelationship_FullMethodName)
	ctx = providergateway.WithAppScopedRelationshipMutationAuth(ctx, "roadmap", "grant_remove")

	provider := &stubAuthorizationProvider{
		deleteErr: status.Error(codes.InvalidArgument, "invalid tuple"),
	}
	server := NewProviderServer(provider)

	_, err := server.DeleteRelationship(ctx, &proto.DeleteRelationshipRequest{})
	if err == nil {
		t.Fatal("DeleteRelationship error = nil, want error")
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{
		"gestaltd.app_admin.ui.app":              "roadmap",
		"gestaltd.app_admin.ui.surface":          "members",
		"gestaltd.app_admin.ui.action":           "grant_remove",
		"gestaltd.app_admin.ui.outcome":          "failure",
		"gestaltd.app_admin.ui.failure_category": "validation",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.ui.count", 1, attrs)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.ui.error_count", 1, attrs)
}

type stubAuthorizationProvider struct {
	core.AuthorizationProvider
	deleteErr error
}

func (p *stubAuthorizationProvider) AddRelationship(context.Context, *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	return &proto.AddRelationshipResponse{}, nil
}

func (p *stubAuthorizationProvider) DeleteRelationship(context.Context, *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	if p.deleteErr != nil {
		return nil, p.deleteErr
	}
	return &proto.DeleteRelationshipResponse{}, nil
}

func TestAppAdminUIFailureCategoryFromRPC(t *testing.T) {
	t.Parallel()

	if got := observability.AppAdminUIFailureCategoryGRPC(status.Error(codes.PermissionDenied, "denied")); got != "auth_failure" {
		t.Fatalf("category = %q, want auth_failure", got)
	}
	if got := observability.AppAdminUIFailureCategoryGRPC(status.Error(codes.InvalidArgument, "bad")); got != "validation" {
		t.Fatalf("category = %q, want validation", got)
	}
	if got := observability.AppAdminUIFailureCategoryGRPC(status.Error(codes.Internal, "boom")); got != "server" {
		t.Fatalf("category = %q, want server", got)
	}
}
