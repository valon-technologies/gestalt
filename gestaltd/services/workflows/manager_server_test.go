package workflows

import (
	"context"
	"testing"

	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWorkflowManagerApplyDefinitionRequiresTokenResolver(t *testing.T) {
	t.Parallel()

	server := NewManagerServer("caller", nil, nil)
	_, err := server.ApplyDefinition(context.Background(), &proto.WorkflowManagerApplyDefinitionRequest{})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("ApplyDefinition error = %v, want FailedPrecondition", err)
	}
}
