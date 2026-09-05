package providergateway

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestEnforceAuthorizationPublicAccessRecordsAppScopedAuthFailure(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)
	ctx = publicrpc.WithPublicOrigin(ctx, proto.Authorization_AddRelationship_FullMethodName)

	provider := &appScopedAuthorizationProvider{
		stubAuthorizationProvider: &stubAuthorizationProvider{
			allowedActions: map[string]bool{},
		},
		appAdmin: map[string]bool{},
	}
	transport := &ProviderGatewayTransport{authorization: provider}

	req := &proto.AddRelationshipRequest{
		Relationship: &proto.Relationship{
			Tuple: &proto.RelationshipTuple{
				Resource: &proto.Resource{Type: "app", Id: "roadmap"},
				Relation: "viewer",
				Target: &proto.RelationshipTarget{
					Kind: &proto.RelationshipTarget_Subject{
						Subject: &proto.Subject{Type: "subject", Id: "user:viewer@example.com"},
					},
				},
			},
		},
	}
	_, err := transport.enforceAuthorizationPublicAccess(
		ctx,
		"user:outsider@example.com",
		proto.Authorization_AddRelationship_FullMethodName,
		req,
	)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("error = %v, want permission denied", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{
		"gestaltd.app_admin.app":                 "roadmap",
		"gestaltd.app_admin.operation":           "members_grant_add",
		"gestaltd.app_admin.outcome":             "failure",
		"gestaltd.app_admin.failure_category":    "auth_failure",
		"gestaltd.subject.kind":                  "user",
		"gestaltd.app_admin.target_subject.kind": "user",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.count", 1, attrs)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.error_count", 1, attrs)
}

func TestWithAppScopedRelationshipMutationAuth(t *testing.T) {
	t.Parallel()

	ctx := WithAppScopedRelationshipMutationAuth(context.Background(), "roadmap", "grant_remove", &proto.RelationshipTuple{
		Resource: &proto.Resource{Type: "app", Id: "roadmap"},
		Relation: "viewer",
		Target: &proto.RelationshipTarget{
			Kind: &proto.RelationshipTarget_Subject{
				Subject: &proto.Subject{Type: "subject", Id: "user:viewer@example.com"},
			},
		},
	})
	appID, action, targetKind, targetID, ok := AppScopedRelationshipMutationFromContext(ctx)
	if !ok || appID != "roadmap" || action != "grant_remove" || targetKind != "user" || targetID != "viewer@example.com" {
		t.Fatalf("context marker = (%q, %q, %q, %q, %v), want (roadmap, grant_remove, user, viewer@example.com, true)", appID, action, targetKind, targetID, ok)
	}
}
