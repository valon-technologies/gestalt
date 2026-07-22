package appregistry_test

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func TestWorkflowsFromDefinitionSpecs(t *testing.T) {
	t.Parallel()

	workflows, err := appregistry.WorkflowsFromDefinitionSpecs([]*proto.WorkflowDefinitionSpec{
		{
			Id:    "slack_v2_smoke_test",
			RunAs: "service_account:slack-v2-smoke-test",
			Target: &proto.BoundWorkflowTarget{
				Steps: []*proto.WorkflowStep{{
					Id: "handle_slack_event",
					Action: &proto.WorkflowStep_App{App: &proto.WorkflowStepAppCall{
						Name:      "gIssues",
						Operation: "handle_slack_event",
					}},
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("WorkflowsFromDefinitionSpecs: %v", err)
	}
	if len(workflows.Definitions) != 1 {
		t.Fatalf("definitions = %#v", workflows.Definitions)
	}
	if workflows.Definitions[0].ID != "slack_v2_smoke_test" {
		t.Fatalf("definition id = %q", workflows.Definitions[0].ID)
	}
	if len(workflows.Definitions[0].Steps) != 1 {
		t.Fatalf("steps = %#v", workflows.Definitions[0].Steps)
	}
	if workflows.Definitions[0].Steps[0].App != "gIssues" || workflows.Definitions[0].Steps[0].Operation != "handle_slack_event" {
		t.Fatalf("step = %#v", workflows.Definitions[0].Steps[0])
	}
}
