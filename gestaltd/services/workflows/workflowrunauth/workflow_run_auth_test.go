package workflowrunauth

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
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
			SubjectID:   "service_account:persisted",
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
	if _, ok := resolved.Auth.Operations["ledger"]["post"]; !ok {
		t.Fatalf("resolved auth operations = %#v, want ledger.post", resolved.Auth.Operations)
	}
}

func TestTargetInvocationAuthIncludesAppAndAgentToolRefs(t *testing.T) {
	t.Parallel()

	auth := TargetInvocationAuth(coreworkflow.Target{Steps: []coreworkflow.Step{
		{
			App: &coreworkflow.AppCall{
				Name:           "ledger",
				Operation:      "post",
				CredentialMode: core.ConnectionModeNone,
			},
		},
		{
			Agent: &coreworkflow.AgentTurn{
				ProviderName: "assistant",
				ToolRefs: []coreagent.ToolRef{{
					App:       "docs",
					Operation: "search",
				}},
			},
		},
	}})

	if got := auth.Operations["ledger"]["post"]; got != core.ConnectionModeNone {
		t.Fatalf("ledger.post credential mode = %q, want %q", got, core.ConnectionModeNone)
	}
	if _, ok := auth.Operations["docs"]["search"]; !ok {
		t.Fatalf("auth operations = %#v, want docs.search", auth.Operations)
	}
	if _, ok := auth.Permissions["ledger"]["post"]; !ok {
		t.Fatalf("auth permissions = %#v, want ledger.post", auth.Permissions)
	}
	if auth.Permissions["assistant"] != nil {
		t.Fatalf("assistant permissions = %#v, want provider-wide grant", auth.Permissions["assistant"])
	}
	if _, ok := auth.Permissions["docs"]["search"]; !ok {
		t.Fatalf("auth permissions = %#v, want docs.search", auth.Permissions)
	}
}
