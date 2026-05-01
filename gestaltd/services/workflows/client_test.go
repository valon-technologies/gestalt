package workflows

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

func TestNewExecutableWorkflowForwardsDefaultEgressAction(t *testing.T) {
	t.Parallel()

	originalStart := startWorkflowProviderProcess
	defer func() { startWorkflowProviderProcess = originalStart }()

	wantErr := errors.New("boom")
	var got runtimehost.ProcessConfig
	startWorkflowProviderProcess = func(_ context.Context, cfg runtimehost.ProcessConfig) (*runtimehost.PluginProcess, error) {
		got = cfg
		return nil, wantErr
	}

	_, err := NewExecutable(context.Background(), ExecConfig{
		Command: "/bin/true",
		Egress:  egress.Policy{DefaultAction: egress.PolicyDeny},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("NewExecutable error = %v, want %v", err, wantErr)
	}
	if got.Egress.DefaultAction != egress.PolicyDeny {
		t.Fatalf("default action = %q, want %q", got.Egress.DefaultAction, egress.PolicyDeny)
	}
}
