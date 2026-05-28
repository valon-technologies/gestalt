package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowgrants"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

func TestManagerServerMissingOrEmptyWorkflowGrantsDenyWorkflowManagerMethods(t *testing.T) {
	t.Parallel()

	for name, grants := range map[string]workflowgrants.Grants{
		"missing grants": nil,
		"empty grants":   {},
	} {
		t.Run(name, func(t *testing.T) {
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
				grants,
			)
			if err != nil {
				t.Fatalf("MintRootTokenWithWorkflowGrants: %v", err)
			}

			server := NewProviderServer("caller", nil, tokens)
			_, err = server.UpsertSchedule(context.Background(), &proto.UpsertWorkflowProviderScheduleRequest{
				InvocationToken: token,
			})
			if status.Code(err) != codes.PermissionDenied {
				t.Fatalf("CreateSchedule error = %v, want PermissionDenied", err)
			}
		})
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
			if len(got.Steps) != 0 {
				t.Fatalf("target = %#v, want empty target for definition-only request", got)
			}
		})
	}
}

func TestManagerServerPublishEventThreadsCallerAppToSelectedProvider(t *testing.T) {
	t.Parallel()

	tokens, err := NewInvocationTokenManager([]byte("workflow-manager-publish-selected-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	token := mintPublishEventToken(t, tokens, "valonSats")
	selected := &recordingWorkflowProvider{publishedID: "provider-event"}
	other := &recordingWorkflowProvider{}
	var auditBuf bytes.Buffer
	manager := workflowmanager.New(workflowmanager.Config{
		Workflow: managerServerWorkflowControl{
			defaultName: "selected",
			names:       []string{"selected", "other"},
			providers: map[string]coreworkflow.Provider{
				"selected": selected,
				"other":    other,
			},
		},
		Audit: invocation.NewSlogAuditSink(&auditBuf),
	})
	server := NewProviderServer("valonSats", manager, tokens)

	published, err := server.PublishEvent(context.Background(), &proto.PublishWorkflowProviderEventRequest{
		ProviderName:    "selected",
		InvocationToken: token,
		Event:           &proto.WorkflowEvent{Type: "valon_sats.attempt.submitted"},
	})
	if err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}
	if got := published.GetId(); got != "provider-event" {
		t.Fatalf("published event id = %q, want provider-event", got)
	}
	if len(selected.publishReqs) != 1 {
		t.Fatalf("selected publish requests = %d, want 1", len(selected.publishReqs))
	}
	if got := selected.publishReqs[0].GetAppName(); got != "valonSats" {
		t.Fatalf("selected publish app = %q, want valonSats", got)
	}
	if len(other.publishReqs) != 0 {
		t.Fatalf("other publish requests = %d, want 0", len(other.publishReqs))
	}
	assertManagerServerWorkflowAudit(t, auditBuf.String(), map[string]any{
		"level":          "INFO",
		"log.type":       "audit",
		"source":         "workflow_manager",
		"provider":       "selected",
		"operation":      "workflow.event.publish",
		"target_kind":    "workflow_event",
		"target_name":    "valon_sats.attempt.submitted",
		"caller_app":     "valonSats",
		"subject_id":     "user:user-123",
		"subject_kind":   "user",
		"request_id_set": true,
		"allowed":        true,
	})
}

func TestManagerServerPublishEventThreadsCallerAppToFanoutProviders(t *testing.T) {
	t.Parallel()

	tokens, err := NewInvocationTokenManager([]byte("workflow-manager-publish-fanout-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	token := mintPublishEventToken(t, tokens, "valonSats")
	first := &recordingWorkflowProvider{}
	second := &recordingWorkflowProvider{}
	var auditBuf bytes.Buffer
	manager := workflowmanager.New(workflowmanager.Config{
		Workflow: managerServerWorkflowControl{
			defaultName: "first",
			names:       []string{"first", "second"},
			providers: map[string]coreworkflow.Provider{
				"first":  first,
				"second": second,
			},
		},
		Audit: invocation.NewSlogAuditSink(&auditBuf),
	})
	server := NewProviderServer(" valonSats ", manager, tokens)

	_, err = server.PublishEvent(context.Background(), &proto.PublishWorkflowProviderEventRequest{
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
		if got := provider.publishReqs[0].GetAppName(); got != "valonSats" {
			t.Fatalf("%s publish app = %q, want valonSats", name, got)
		}
	}
	for _, providerName := range []string{"first", "second"} {
		assertManagerServerWorkflowAudit(t, auditBuf.String(), map[string]any{
			"level":          "INFO",
			"log.type":       "audit",
			"source":         "workflow_manager",
			"provider":       providerName,
			"operation":      "workflow.event.publish",
			"target_kind":    "workflow_event",
			"target_name":    "valon_sats.attempt.submitted",
			"caller_app":     "valonSats",
			"subject_id":     "user:user-123",
			"subject_kind":   "user",
			"request_id_set": true,
			"allowed":        true,
		})
	}
}

func TestWorkflowManagerPublishEventSelectedProviderPreservesBlankApp(t *testing.T) {
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
		AppName:      "   ",
		Event:        coreworkflow.Event{Type: "valon_sats.attempt.submitted"},
	})
	if err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}
	if len(selected.publishReqs) != 1 {
		t.Fatalf("selected publish requests = %d, want 1", len(selected.publishReqs))
	}
	if got := selected.publishReqs[0].GetAppName(); got != "" {
		t.Fatalf("selected publish app = %q, want empty", got)
	}
}

func assertManagerServerWorkflowAudit(t *testing.T, output string, want map[string]any) {
	t.Helper()

	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("audit line is not valid JSON: %q: %v", line, err)
		}
		if record["log.type"] != "audit" {
			continue
		}
		matches := true
		for key, value := range want {
			if key == "request_id_set" {
				if value == true && !managerServerAuditStringPresent(record, "request_id") {
					matches = false
					break
				}
				continue
			}
			if got := record[key]; got != value {
				matches = false
				break
			}
		}
		if matches {
			return
		}
	}
	t.Fatalf("workflow audit record not found in %q", output)
}

func managerServerAuditStringPresent(record map[string]any, key string) bool {
	value, ok := record[key].(string)
	return ok && value != ""
}

func mintPublishEventToken(t *testing.T, tokens *InvocationTokenManager, callerApp string) string {
	t.Helper()
	token, err := tokens.MintRootTokenWithWorkflowGrants(
		principal.WithPrincipal(context.Background(), publishEventPrincipal()),
		callerApp,
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

func (c managerServerWorkflowControl) ResolveProvider(_ context.Context, name string) (string, coreworkflow.Provider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = c.defaultName
	}
	provider := c.providers[name]
	if provider == nil {
		return "", nil, errors.New("provider not found")
	}
	return name, provider, nil
}

func (c managerServerWorkflowControl) ProviderNames() []string {
	return append([]string(nil), c.names...)
}

type recordingWorkflowProvider struct {
	coreworkflow.Provider
	publishReqs []*proto.PublishWorkflowProviderEventRequest
	publishedID string
}

func (p *recordingWorkflowProvider) PublishEvent(_ context.Context, req *proto.PublishWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	p.publishReqs = append(p.publishReqs, gproto.Clone(req).(*proto.PublishWorkflowProviderEventRequest))
	event := &proto.WorkflowEvent{}
	if req.GetEvent() != nil {
		event = gproto.Clone(req.GetEvent()).(*proto.WorkflowEvent)
	}
	if p.publishedID != "" {
		event.Id = p.publishedID
	}
	return event, nil
}
