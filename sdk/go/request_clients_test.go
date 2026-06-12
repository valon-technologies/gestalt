package gestalt

import (
	"reflect"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/go/client"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestClientRequestContextNil(t *testing.T) {
	if got := clientRequestContext(nil); got != nil {
		t.Fatalf("clientRequestContext(nil) = %#v, want nil", got)
	}
}

func TestClientRequestContextFullFidelity(t *testing.T) {
	workflow, err := structpb.NewStruct(map[string]any{
		"providerName": "temporal",
		"runId":        "run-1",
		"step":         map[string]any{"id": "review"},
	})
	if err != nil {
		t.Fatalf("structpb.NewStruct: %v", err)
	}

	reqCtx := &proto.RequestContext{
		Subject: &proto.SubjectContext{
			Id:                  "user:alice",
			CredentialSubjectId: "user:alice-credential",
			Email:               "alice@example.test",
			DisplayName:         "Alice",
			Scopes:              []string{"read", "write"},
			Permissions: []*proto.SubjectPermissionContext{{
				App:           "github",
				Operations:    []string{"issues.get"},
				AllOperations: false,
			}, {
				App:           "slack",
				AllOperations: true,
			}},
		},
		Credential: &proto.CredentialContext{
			Mode:       "subject",
			SubjectId:  "user:alice-credential",
			Connection: "github:default",
			Instance:   "primary",
		},
		Access:   &proto.AccessContext{Policy: "allow", Role: "admin"},
		Workflow: workflow,
		Host:     &proto.HostContext{PublicBaseUrl: "https://gestalt.example.test"},
		AgentSubject: &proto.SubjectContext{
			Id: "agent:tool-runner",
		},
		Caller: &proto.ProviderContext{Kind: "workflow", Name: "temporal"},
		Invocation: &proto.InvocationContext{
			RequestId:                "req-1",
			Depth:                    2,
			CallChain:                []string{"app-a", "app-b"},
			Surface:                  "graphql",
			InternalConnectionAccess: true,
			Connection:               "github:default",
		},
		ToolRefs: []*proto.AgentToolRef{{
			App:            "github",
			Operation:      "issues.get",
			Connection:     "github:default",
			Instance:       "primary",
			Title:          "Get issue",
			Description:    "Fetch one issue",
			CredentialMode: "subject",
			System:         "catalog",
			RunAs:          &proto.SubjectContext{Id: "service_account:tools"},
		}},
		ToolRefsSet: true,
		RequestMeta: &proto.RequestMetaContext{
			ClientIp:   "203.0.113.7",
			RemoteAddr: "203.0.113.7:51324",
			UserAgent:  "gestalt-test/1.0",
		},
		Agent: &proto.AgentInvocationContext{
			ProviderName: "claude",
			SessionId:    "session-1",
			TurnId:       "turn-1",
		},
	}

	got := clientRequestContext(reqCtx)
	if got == nil {
		t.Fatal("clientRequestContext returned nil for populated context")
	}

	want := &client.RequestContext{
		Subject: &client.SubjectContext{
			ID:                  "user:alice",
			CredentialSubjectID: "user:alice-credential",
			Email:               "alice@example.test",
			DisplayName:         "Alice",
			Scopes:              []string{"read", "write"},
			Permissions: []*client.SubjectPermissionContext{{
				App:        "github",
				Operations: []string{"issues.get"},
			}, {
				App:           "slack",
				AllOperations: true,
			}},
		},
		Credential: &client.CredentialContext{
			Mode:       "subject",
			SubjectID:  "user:alice-credential",
			Connection: "github:default",
			Instance:   "primary",
		},
		Access: &client.AccessContext{Policy: "allow", Role: "admin"},
		Workflow: map[string]any{
			"providerName": "temporal",
			"runId":        "run-1",
			"step":         map[string]any{"id": "review"},
		},
		Host: &client.HostContext{PublicBaseURL: "https://gestalt.example.test"},
		AgentSubject: &client.SubjectContext{
			ID: "agent:tool-runner",
		},
		Caller: &client.ProviderContext{Kind: "workflow", Name: "temporal"},
		Invocation: &client.InvocationContext{
			RequestID:                "req-1",
			Depth:                    2,
			CallChain:                []string{"app-a", "app-b"},
			Surface:                  "graphql",
			InternalConnectionAccess: true,
			Connection:               "github:default",
		},
		ToolRefs: []*client.AgentToolRef{{
			App:            "github",
			Operation:      "issues.get",
			Connection:     "github:default",
			Instance:       "primary",
			Title:          "Get issue",
			Description:    "Fetch one issue",
			CredentialMode: "subject",
			System:         "catalog",
			RunAs:          &client.SubjectContext{ID: "service_account:tools"},
		}},
		ToolRefsSet: true,
		RequestMeta: &client.RequestMetaContext{
			ClientIp:   "203.0.113.7",
			RemoteAddr: "203.0.113.7:51324",
			UserAgent:  "gestalt-test/1.0",
		},
		Agent: &client.AgentInvocationContext{
			ProviderName: "claude",
			SessionID:    "session-1",
			TurnID:       "turn-1",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("clientRequestContext mismatch:\ngot  %#v\nwant %#v", got, want)
	}

	// Sparse contexts stay sparse: absent nested messages convert to nil.
	sparse := clientRequestContext(&proto.RequestContext{})
	if sparse == nil {
		t.Fatal("clientRequestContext(empty) = nil, want non-nil empty context")
	}
	if sparse.Subject != nil || sparse.Credential != nil || sparse.Access != nil ||
		sparse.Workflow != nil || sparse.Host != nil || sparse.AgentSubject != nil ||
		sparse.Caller != nil || sparse.Invocation != nil || sparse.ToolRefs != nil ||
		sparse.ToolRefsSet || sparse.RequestMeta != nil || sparse.Agent != nil {
		t.Fatalf("clientRequestContext(empty) = %#v, want all-nil fields", sparse)
	}
}
