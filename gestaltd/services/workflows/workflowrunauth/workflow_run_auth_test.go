package workflowrunauth

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"google.golang.org/protobuf/types/known/structpb"
)

type recordingResolver struct {
	providerName string
	runID        string
	run          *coreworkflow.Run
}

func (r *recordingResolver) ResolveWorkflowRun(_ context.Context, providerName, runID string) (*coreworkflow.Run, error) {
	r.providerName = providerName
	r.runID = runID
	return r.run, nil
}

func TestResolveInvocationFromWorkflowRunUsesPersistedRunAs(t *testing.T) {
	t.Parallel()

	workflow, err := structpb.NewStruct(map[string]any{
		"providerName": "workflow-provider",
		"runId":        "run-1",
		"runAs": map[string]any{
			"id": "service_account:forged",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolver := &recordingResolver{run: &coreworkflow.Run{
		RunAs: &core.RunAsSubject{
			SubjectID: "service_account:persisted",
		},
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
			App: &coreworkflow.AppCall{Name: "ledger", Operation: "post"},
		}}},
	}}

	resolved, err := ResolveInvocationFromWorkflowRun(context.Background(), resolver, workflow)
	if err != nil {
		t.Fatal(err)
	}
	if resolver.providerName != "workflow-provider" || resolver.runID != "run-1" {
		t.Fatalf("resolver called with (%q, %q), want (workflow-provider, run-1)", resolver.providerName, resolver.runID)
	}
	if got := resolved.RunAs.SubjectID; got != "service_account:persisted" {
		t.Fatalf("resolved runAs subject ID = %q, want persisted subject", got)
	}
	runAs, ok := resolved.Workflow["runAs"].(map[string]any)
	if !ok {
		t.Fatalf("resolved workflow runAs = %#v, want map", resolved.Workflow["runAs"])
	}
	if got := runAs["id"]; got != "service_account:persisted" {
		t.Fatalf("workflow runAs id = %#v, want persisted subject", got)
	}
}
