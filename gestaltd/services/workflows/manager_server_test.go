package workflows

import (
	"context"
	"errors"
	"strings"
	"testing"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowgrants"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
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

func TestWorkflowManagerTargetOrDefinitionAllowsDefinitionOnlyRequests(t *testing.T) {
	t.Parallel()

	for name, target := range map[string]*proto.BoundWorkflowTarget{
		"nil target":   nil,
		"empty target": {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := workflowManagerTargetOrDefinition(target, "workflow_definition:abc")
			if err != nil {
				t.Fatalf("workflowManagerTargetOrDefinition: %v", err)
			}
			if got.Plugin != nil || got.Agent != nil {
				t.Fatalf("target = %#v, want empty target for definition-only request", got)
			}
		})
	}
}

func TestManagerServerPublishEventThreadsCallerPluginToSelectedProvider(t *testing.T) {
	t.Parallel()

	tokens, err := NewInvocationTokenManager([]byte("workflow-manager-publish-selected-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	token := mintPublishEventToken(t, tokens, "valonSats")
	selected := &recordingWorkflowProvider{}
	other := &recordingWorkflowProvider{}
	manager := workflowmanager.New(workflowmanager.Config{
		Workflow: managerServerWorkflowControl{
			defaultName: "selected",
			names:       []string{"selected", "other"},
			providers: map[string]coreworkflow.Provider{
				"selected": selected,
				"other":    other,
			},
		},
	})
	server := NewManagerServer("valonSats", manager, tokens)

	_, err = server.PublishEvent(context.Background(), &proto.WorkflowManagerPublishEventRequest{
		ProviderName:    "selected",
		InvocationToken: token,
		Event:           &proto.WorkflowEvent{Type: "valon_sats.attempt.submitted"},
	})
	if err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}
	if len(selected.publishReqs) != 1 {
		t.Fatalf("selected publish requests = %d, want 1", len(selected.publishReqs))
	}
	if got := selected.publishReqs[0].PluginName; got != "valonSats" {
		t.Fatalf("selected publish plugin = %q, want valonSats", got)
	}
	if len(other.publishReqs) != 0 {
		t.Fatalf("other publish requests = %d, want 0", len(other.publishReqs))
	}
}

func TestManagerServerPublishEventThreadsCallerPluginToFanoutProviders(t *testing.T) {
	t.Parallel()

	tokens, err := NewInvocationTokenManager([]byte("workflow-manager-publish-fanout-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	token := mintPublishEventToken(t, tokens, "valonSats")
	first := &recordingWorkflowProvider{}
	second := &recordingWorkflowProvider{}
	manager := workflowmanager.New(workflowmanager.Config{
		Workflow: managerServerWorkflowControl{
			defaultName: "first",
			names:       []string{"first", "second"},
			providers: map[string]coreworkflow.Provider{
				"first":  first,
				"second": second,
			},
		},
	})
	server := NewManagerServer(" valonSats ", manager, tokens)

	_, err = server.PublishEvent(context.Background(), &proto.WorkflowManagerPublishEventRequest{
		InvocationToken: token,
		Event:           &proto.WorkflowEvent{Type: "valon_sats.attempt.submitted"},
	})
	if err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}
	for name, provider := range map[string]*recordingWorkflowProvider{
		"first":  first,
		"second": second,
	} {
		if len(provider.publishReqs) != 1 {
			t.Fatalf("%s publish requests = %d, want 1", name, len(provider.publishReqs))
		}
		if got := provider.publishReqs[0].PluginName; got != "valonSats" {
			t.Fatalf("%s publish plugin = %q, want valonSats", name, got)
		}
	}
}

func TestWorkflowManagerPublishEventSelectedProviderPreservesBlankPlugin(t *testing.T) {
	t.Parallel()

	selected := &recordingWorkflowProvider{}
	manager := workflowmanager.New(workflowmanager.Config{
		Workflow: managerServerWorkflowControl{
			defaultName: "selected",
			names:       []string{"selected"},
			providers: map[string]coreworkflow.Provider{
				"selected": selected,
			},
		},
	})

	_, err := manager.PublishEvent(context.Background(), publishEventPrincipal(), workflowmanager.EventPublish{
		ProviderName: "selected",
		PluginName:   "   ",
		Event:        coreworkflow.Event{Type: "valon_sats.attempt.submitted"},
	})
	if err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}
	if len(selected.publishReqs) != 1 {
		t.Fatalf("selected publish requests = %d, want 1", len(selected.publishReqs))
	}
	if got := selected.publishReqs[0].PluginName; got != "" {
		t.Fatalf("selected publish plugin = %q, want empty", got)
	}
}

func mintPublishEventToken(t *testing.T, tokens *InvocationTokenManager, callerPlugin string) string {
	t.Helper()
	token, err := tokens.MintRootTokenWithWorkflowGrants(
		principal.WithPrincipal(context.Background(), publishEventPrincipal()),
		callerPlugin,
		nil,
		workflowgrants.Grants{workflowgrants.OperationEventsPublish: {}},
	)
	if err != nil {
		t.Fatalf("MintRootTokenWithWorkflowGrants: %v", err)
	}
	return token
}

func publishEventPrincipal() *principal.Principal {
	return &principal.Principal{
		SubjectID: "user:user-123",
		UserID:    "user-123",
		Kind:      principal.KindUser,
		Source:    principal.SourceSession,
	}
}

type managerServerWorkflowControl struct {
	defaultName string
	names       []string
	providers   map[string]coreworkflow.Provider
}

func (c managerServerWorkflowControl) ResolveProvider(name string) (coreworkflow.Provider, error) {
	provider := c.providers[strings.TrimSpace(name)]
	if provider == nil {
		return nil, errors.New("provider not found")
	}
	return provider, nil
}

func (c managerServerWorkflowControl) ResolveProviderSelection(name string) (string, coreworkflow.Provider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = c.defaultName
	}
	provider, err := c.ResolveProvider(name)
	if err != nil {
		return "", nil, err
	}
	return name, provider, nil
}

func (c managerServerWorkflowControl) ProviderNames() []string {
	return append([]string(nil), c.names...)
}

type recordingWorkflowProvider struct {
	coreworkflow.Provider
	publishReqs []coreworkflow.PublishEventRequest
}

func (p *recordingWorkflowProvider) PublishEvent(_ context.Context, req coreworkflow.PublishEventRequest) error {
	p.publishReqs = append(p.publishReqs, req)
	return nil
}
