package workflowmanager

import (
	"context"
	"testing"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/featureflags"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFeatureGateRejectsEveryWorkflowOperation(t *testing.T) {
	gate := NewFeatureGate(false, New(Config{}))
	tests := []struct {
		name string
		call func() error
	}{
		{"apply definition", func() error { _, err := gate.ApplyDefinition(context.Background(), nil, DefinitionApply{}); return err }},
		{"get definition", func() error { _, err := gate.GetDefinition(context.Background(), nil, "", ""); return err }},
		{"list definitions", func() error { _, err := gate.ListDefinitions(context.Background(), nil, ""); return err }},
		{"set definition paused", func() error { _, err := gate.SetDefinitionPaused(context.Background(), nil, "", "", false); return err }},
		{"set activation paused", func() error {
			_, err := gate.SetActivationPaused(context.Background(), nil, "", "", "", false)
			return err
		}},
		{"delete definition", func() error { return gate.DeleteDefinition(context.Background(), nil, "", "") }},
		{"list runs", func() error {
			_, err := gate.ListRuns(context.Background(), nil, "", coreworkflow.ListRunsRequest{})
			return err
		}},
		{"start run", func() error { _, err := gate.StartRun(context.Background(), nil, RunStart{}); return err }},
		{"get run", func() error { _, err := gate.GetRun(context.Background(), nil, "", ""); return err }},
		{"get run events", func() error { _, err := gate.GetRunEvents(context.Background(), nil, "", ""); return err }},
		{"get run output", func() error { _, err := gate.GetRunOutput(context.Background(), nil, "", ""); return err }},
		{"cancel run", func() error { _, err := gate.CancelRun(context.Background(), nil, "", "", ""); return err }},
		{"signal run", func() error { _, err := gate.SignalRun(context.Background(), nil, RunSignal{}); return err }},
		{"signal or start run", func() error {
			_, err := gate.SignalOrStartRun(context.Background(), nil, RunSignalOrStart{})
			return err
		}},
		{"deliver event", func() error { _, err := gate.DeliverEvent(context.Background(), nil, EventDeliver{}); return err }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.call()
			if !featureflags.IsDisabled(err, featureflags.Workflow) {
				t.Fatalf("error = %v, want disabled Workflow feature", err)
			}
			if got := status.Code(err); got != codes.FailedPrecondition {
				t.Fatalf("status code = %v, want %v", got, codes.FailedPrecondition)
			}
		})
	}
}
