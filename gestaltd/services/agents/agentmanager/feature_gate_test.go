package agentmanager

import (
	"context"
	"testing"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/featureflags"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFeatureGateRejectsEveryAgentOperation(t *testing.T) {
	gate := NewFeatureGate(false, New(Config{}))
	if gate.Available() {
		t.Fatal("disabled gate reported available")
	}

	tests := []struct {
		name string
		call func() error
	}{
		{"resolve tool", func() error { _, err := gate.ResolveTool(context.Background(), nil, coreagent.ToolRef{}); return err }},
		{"resolve tools", func() error {
			_, err := gate.ResolveTools(context.Background(), nil, coreagent.ResolveToolsRequest{})
			return err
		}},
		{"list tools", func() error {
			_, err := gate.ListTools(context.Background(), nil, coreagent.ListToolsRequest{})
			return err
		}},
		{"create session", func() error { _, err := gate.CreateSession(context.Background(), nil, nil); return err }},
		{"get session", func() error { _, err := gate.GetSession(context.Background(), nil, nil); return err }},
		{"list sessions", func() error { _, err := gate.ListSessions(context.Background(), nil, nil); return err }},
		{"update session", func() error { _, err := gate.UpdateSession(context.Background(), nil, nil); return err }},
		{"create turn", func() error { _, err := gate.CreateTurn(context.Background(), nil, nil); return err }},
		{"get turn", func() error { _, err := gate.GetTurn(context.Background(), nil, nil); return err }},
		{"list turns", func() error { _, err := gate.ListTurns(context.Background(), nil, nil); return err }},
		{"cancel turn", func() error { _, err := gate.CancelTurn(context.Background(), nil, nil); return err }},
		{"list turn events", func() error { _, err := gate.ListTurnEvents(context.Background(), nil, nil); return err }},
		{"list interactions", func() error { _, err := gate.ListInteractions(context.Background(), nil, nil); return err }},
		{"resolve interaction", func() error { _, err := gate.ResolveInteraction(context.Background(), nil, nil); return err }},
		{"authorize app invocation", func() error {
			_, err := gate.AuthorizeAppInvocation(context.Background(), invocation.AgentAppAuthorizationRequest{})
			return err
		}},
		{"authorize workflow invocation", func() error {
			_, err := gate.AuthorizeWorkflowInvocation(context.Background(), invocation.AgentWorkflowAuthorizationRequest{})
			return err
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.call()
			if !featureflags.IsDisabled(err, featureflags.Agent) {
				t.Fatalf("error = %v, want disabled Agent feature", err)
			}
			if got := status.Code(err); got != codes.FailedPrecondition {
				t.Fatalf("status code = %v, want %v", got, codes.FailedPrecondition)
			}
		})
	}
}
