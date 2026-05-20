package workflows

import (
	"context"
	"testing"

	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWorkflowManagerPlanDeploymentRequiresTokenResolver(t *testing.T) {
	t.Parallel()

	server := NewManagerServer("caller", nil, nil)
	_, err := server.PlanDeployment(context.Background(), &proto.WorkflowManagerPlanDeploymentRequest{})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("PlanDeployment error = %v, want FailedPrecondition", err)
	}
}
