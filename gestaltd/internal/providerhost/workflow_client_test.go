package providerhost

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/egress"
)

func TestNewExecutableWorkflowForwardsDefaultEgressAction(t *testing.T) {
	t.Parallel()

	originalStart := startWorkflowProviderProcess
	defer func() { startWorkflowProviderProcess = originalStart }()

	wantErr := errors.New("boom")
	var got ProcessConfig
	startWorkflowProviderProcess = func(_ context.Context, cfg ProcessConfig) (*providerProcess, error) {
		got = cfg
		return nil, wantErr
	}

	_, err := NewExecutableWorkflow(context.Background(), WorkflowExecConfig{
		Command: "/bin/true",
		Egress:  egress.Policy{DefaultAction: egress.PolicyDeny},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("NewExecutableWorkflow error = %v, want %v", err, wantErr)
	}
	if got.Egress.DefaultAction != egress.PolicyDeny {
		t.Fatalf("default action = %q, want %q", got.Egress.DefaultAction, egress.PolicyDeny)
	}
}
