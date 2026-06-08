package bootstrap

import (
	"testing"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
)

func TestWorkflowSystemToolsExposeApplyDefinition(t *testing.T) {
	t.Parallel()

	tool, err := workflowSystemToolFromRef(coreagent.ToolRef{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsApply})
	if err != nil {
		t.Fatalf("definitions.apply tool: %v", err)
	}
	if tool.Target.System != coreagent.SystemToolWorkflow || tool.Target.Operation != workflowSystemToolDefinitionsApply {
		t.Fatalf("tool target = %#v, want workflow definitions.apply", tool.Target)
	}
}
