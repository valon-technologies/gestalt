package v1_test

import (
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

func TestWorkflowProtoReexports(t *testing.T) {
	t.Parallel()

	run := &proto.BoundWorkflowRun{
		Id:     "run-1",
		Status: proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING,
	}
	if run.GetStatus() != proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING {
		t.Fatalf("status = %v, want pending", run.GetStatus())
	}

	var _ proto.WorkflowHostServer = proto.UnimplementedWorkflowHostServer{}
	var _ proto.WorkflowProviderServer = proto.UnimplementedWorkflowProviderServer{}
}
