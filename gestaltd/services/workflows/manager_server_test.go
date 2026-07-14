package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

func TestManagerServerMissingOrDeniedAuthorizationDenyWorkflowManagerMethods(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		auth core.AuthorizationProvider
		code codes.Code
	}{
		{name: "missing authorization", code: codes.FailedPrecondition},
		{name: "denied authorization", auth: &managerServerAuthorizationProvider{}, code: codes.PermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := NewProviderServer("caller", nil, tc.auth)
			_, err := server.ApplyDefinition(context.Background(), &proto.ApplyWorkflowProviderDefinitionRequest{
				Provider: "local",
				Context:  managerServerRequestContext("caller"),
			})
			if status.Code(err) != tc.code {
				t.Fatalf("ApplyDefinition error = %v, want %s", err, tc.code)
			}
		})
	}
}

func TestManagerServerRejectsCallerSuppliedDefinitionRunAs(t *testing.T) {
	t.Parallel()

	server := NewProviderServer("caller", nil, &managerServerAuthorizationProvider{allowed: true})
	runAs := "service_account:ada"

	_, err := server.ApplyDefinition(context.Background(), &proto.ApplyWorkflowProviderDefinitionRequest{
		Provider: "selected",
		Context:  managerServerRequestContext("caller"),
		Spec:     &proto.WorkflowDefinitionSpec{RunAs: runAs},
	})
	if status.Code(err) != codes.PermissionDenied && status.Code(err) != codes.InvalidArgument && status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("status = %s, want permission denied, invalid argument, or failed precondition (err=%v)", status.Code(err), err)
	}
}

func TestManagerServerPublicApplyRequiresServiceAccountManagement(t *testing.T) {
	t.Parallel()

	server := NewProviderServer("gestaltd", nil, &managerServerAuthorizationProvider{})
	ctx := publicrpc.WithPublicOrigin(context.Background(), proto.Workflow_ApplyDefinition_FullMethodName)
	_, err := server.ApplyDefinition(ctx, &proto.ApplyWorkflowProviderDefinitionRequest{
		Provider: "selected",
		Context:  managerServerRequestContext("gestaltd"),
		Spec:     &proto.WorkflowDefinitionSpec{RunAs: "service_account:ada"},
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("status = %s, want PermissionDenied (err=%v)", status.Code(err), err)
	}
}

func TestManagerServerDeliverEventThreadsCallerAppToSelectedProvider(t *testing.T) {
	t.Parallel()

	selected := &recordingWorkflowProvider{deliveredID: "provider-event"}
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
	authz := &managerServerAuthorizationProvider{allowed: true}
	server := NewProviderServer("sourceApp", manager, authz)

	delivered, err := server.DeliverEvent(context.Background(), &proto.DeliverWorkflowProviderEventRequest{
		Provider: "selected",
		Context:  managerServerRequestContext("sourceApp"),
		Event:    &proto.WorkflowEvent{Type: "example.event", Source: "sourceApp"},
	})
	if err != nil {
		t.Fatalf("DeliverEvent: %v", err)
	}
	if got := delivered.GetId(); got != "provider-event" {
		t.Fatalf("delivered event id = %q, want provider-event", got)
	}
	if len(selected.deliverReqs) != 1 {
		t.Fatalf("selected deliver requests = %d, want 1", len(selected.deliverReqs))
	}
	if got := selected.deliverReqs[0].GetEvent().GetSource(); got != "sourceApp" {
		t.Fatalf("selected deliver source = %q, want sourceApp", got)
	}
	if got := selected.deliverReqs[0].GetProvider(); got != "selected" {
		t.Fatalf("selected deliver provider = %q, want selected", got)
	}
	if len(other.deliverReqs) != 0 {
		t.Fatalf("other deliver requests = %d, want 0", len(other.deliverReqs))
	}
	authzRequests := authz.Requests()
	if len(authzRequests) != 1 {
		t.Fatalf("authorization checks = %d, want 1", len(authzRequests))
	}
	if got := authzRequests[0].GetResource().GetId(); got != "selected" {
		t.Fatalf("authorization resource = %q, want selected workflow resource", got)
	}
	assertManagerServerWorkflowAudit(t, auditBuf.String(), map[string]any{
		"level":          "INFO",
		"log.type":       "audit",
		"source":         "workflow_manager",
		"provider":       "selected",
		"operation":      "workflow.event.deliver",
		"target_kind":    "workflow_event",
		"target_name":    "example.event",
		"caller_app":     "sourceApp",
		"subject_id":     "user:user-123",
		"created_by":     "user:user-123",
		"request_id_set": true,
		"allowed":        true,
	})
}

func TestManagerServerDeliverEventIgnoresSpoofedAppNameOnInternalPath(t *testing.T) {
	t.Parallel()

	selected := &recordingWorkflowProvider{deliveredID: "provider-event"}
	manager := workflowmanager.New(workflowmanager.Config{
		Workflow: managerServerWorkflowControl{
			defaultName: "selected",
			names:       []string{"selected"},
			providers: map[string]coreworkflow.Provider{
				"selected": selected,
			},
		},
	})
	server := NewProviderServer("sourceApp", manager, &managerServerAuthorizationProvider{allowed: true})

	_, err := server.DeliverEvent(context.Background(), &proto.DeliverWorkflowProviderEventRequest{
		Provider: "selected",
		Context:  managerServerRequestContext("sourceApp"),
		Event:    &proto.WorkflowEvent{Type: "example.event", Source: "sourceApp"},
	})
	if err != nil {
		t.Fatalf("DeliverEvent: %v", err)
	}
	if len(selected.deliverReqs) != 1 {
		t.Fatalf("selected deliver requests = %d, want 1", len(selected.deliverReqs))
	}
}

func TestWorkflowManagerDeliverEventSelectedProviderRequiresCallerApp(t *testing.T) {
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

	_, err := manager.DeliverEvent(context.Background(), testWorkflowPrincipal(), workflowmanager.EventDeliver{
		ProviderName: "selected",
		AppName:      "   ",
		Event:        coreworkflow.Event{Type: "example.event"},
	})
	if !errors.Is(err, workflowmanager.ErrWorkflowEventSourceRequired) {
		t.Fatalf("DeliverEvent error = %v, want ErrWorkflowEventSourceRequired", err)
	}
	if len(selected.deliverReqs) != 0 {
		t.Fatalf("selected deliver requests = %d, want 0", len(selected.deliverReqs))
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

func testWorkflowPrincipal() *principal.Principal {
	return &principal.Principal{
		SubjectID: "user:user-123",
		UserID:    "user-123",
		Kind:      principal.KindUser,
		Source:    principal.SourceBearer,
	}
}

func managerServerRequestContext(callerApp string) *proto.RequestContext {
	return &proto.RequestContext{
		Caller: &proto.ProviderContext{
			Kind: string(invocation.ProviderKindApp),
			Name: callerApp,
		},
		Subject: &proto.SubjectContext{
			Id: "user:user-123",
		},
	}
}

type managerServerAuthorizationProvider struct {
	core.AuthorizationProvider

	mu       sync.Mutex
	allowed  bool
	requests []*proto.CheckAccessRequest
}

func (p *managerServerAuthorizationProvider) CheckAccess(_ context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, gproto.Clone(req).(*proto.CheckAccessRequest))
	return &proto.CheckAccessResponse{Allowed: p.allowed}, nil
}

func (p *managerServerAuthorizationProvider) Requests() []*proto.CheckAccessRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*proto.CheckAccessRequest(nil), p.requests...)
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
	deliverReqs []*proto.DeliverWorkflowProviderEventRequest
	deliveredID string
}

func (p *recordingWorkflowProvider) DeliverEvent(_ context.Context, req *proto.DeliverWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	p.deliverReqs = append(p.deliverReqs, gproto.Clone(req).(*proto.DeliverWorkflowProviderEventRequest))
	event := &proto.WorkflowEvent{}
	if req.GetEvent() != nil {
		event = gproto.Clone(req.GetEvent()).(*proto.WorkflowEvent)
	}
	if p.deliveredID != "" {
		event.Id = p.deliveredID
	}
	return event, nil
}

func (p *recordingWorkflowProvider) GetDefinition(context.Context, *proto.GetWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	target, err := workflowwire.TargetToProto(coreworkflow.Target{Steps: []coreworkflow.Step{{
		ID:  "run",
		App: &coreworkflow.AppCall{Name: "slack", Operation: "chat.postMessage"},
	}}})
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowDefinition{Id: "definition-1", Target: target}, nil
}
