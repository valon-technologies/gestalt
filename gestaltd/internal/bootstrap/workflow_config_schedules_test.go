package bootstrap

import (
	"testing"

	"go.yaml.in/yaml/v4"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestWorkflowConfigStepWhenPreservesEqualsPresence(t *testing.T) {
	t.Parallel()

	var withNullEquals config.WorkflowStepWhenConfig
	if err := yaml.Unmarshal([]byte("value:\n  literal: ready\nequals: null\n"), &withNullEquals); err != nil {
		t.Fatalf("unmarshal when with null equals: %v", err)
	}
	if got := config.WorkflowStepWhenToCore(&withNullEquals); got == nil || !got.EqualsSet {
		t.Fatalf("WorkflowStepWhenToCore with null equals = %#v, want EqualsSet", got)
	}

	var withoutEquals config.WorkflowStepWhenConfig
	if err := yaml.Unmarshal([]byte("value:\n  literal: ready\n"), &withoutEquals); err != nil {
		t.Fatalf("unmarshal when without equals: %v", err)
	}
	if got := config.WorkflowStepWhenToCore(&withoutEquals); got == nil || got.EqualsSet {
		t.Fatalf("WorkflowStepWhenToCore without equals = %#v, want EqualsSet=false", got)
	}
}
