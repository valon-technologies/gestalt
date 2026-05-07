package workflows

import (
	"context"
	"testing"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowgrants"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestManagerServerEmptyWorkflowGrantsDenyWorkflowManagerMethods(t *testing.T) {
	t.Parallel()

	tokens, err := NewInvocationTokenManager([]byte("workflow-manager-token-test-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	token, err := tokens.MintRootTokenWithWorkflowGrants(
		principal.WithPrincipal(context.Background(), &principal.Principal{
			SubjectID: "user:user-123",
			UserID:    "user-123",
			Kind:      principal.KindUser,
			Source:    principal.SourceSession,
		}),
		"caller",
		nil,
		workflowgrants.Grants{},
	)
	if err != nil {
		t.Fatalf("MintRootTokenWithWorkflowGrants: %v", err)
	}

	server := NewManagerServer("caller", nil, tokens)
	_, err = server.CreateSchedule(context.Background(), &proto.WorkflowManagerCreateScheduleRequest{
		InvocationToken: token,
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("CreateSchedule error = %v, want PermissionDenied", err)
	}
}
