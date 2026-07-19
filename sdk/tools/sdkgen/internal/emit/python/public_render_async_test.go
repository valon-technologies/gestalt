package python

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// asyncTestIndex builds an index populated with the request and response
// messages referenced by the test services below.
func asyncTestIndex(msgs ...*model.Message) *index {
	idx := &index{
		messages: map[string]*model.Message{},
		enums:    map[string]*model.Enum{},
		taken:    map[string]bool{},
	}
	for _, m := range msgs {
		idx.messages[m.FullName] = m
	}
	return idx
}

func TestAsyncUnaryTransportProtocolEmitted(t *testing.T) {
	t.Parallel()

	if !strings.Contains(unaryTransportFile, "class AsyncUnaryTransport(Protocol)") {
		t.Fatal("unaryTransportFile missing AsyncUnaryTransport protocol")
	}
	if !strings.Contains(unaryTransportFile, "async def unary") {
		t.Fatal("unaryTransportFile missing async unary method")
	}
}

func TestAsyncClientGeneratedForREST(t *testing.T) {
	t.Parallel()

	req := &model.Message{
		FullName:  "gestalt.provider.v1.CheckAccessRequest",
		Name:      "CheckAccessRequest",
		ProtoFile: "sdk/proto/v1/authorization.proto",
	}
	resp := &model.Message{
		FullName:  "gestalt.provider.v1.CheckAccessResponse",
		Name:      "CheckAccessResponse",
		ProtoFile: "sdk/proto/v1/authorization.proto",
	}
	svc := &model.Service{
		FullName: "gestalt.provider.v1.Authorization",
		Name:     "Authorization",
		Methods: []*model.Method{
			{
				Name: "CheckAccess",
				HTTP: &model.HTTPRule{Verb: "POST", Path: "/api/v2/authorization/access:check"},
				Input:  req,
				Output: resp,
			},
		},
	}

	r := newRenderer(asyncTestIndex(req, resp), "authorization_client", "authorization", modulePublic)
	r.publicClient = true
	r.renderAppClient(svc)
	out := r.assembleGenerated()

	// Sync class and method exist (unchanged).
	if !strings.Contains(out, "class AuthorizationClient:") {
		t.Fatal("missing sync AuthorizationClient class")
	}
	if !strings.Contains(out, "def check_access(self, request: CheckAccessRequest)") {
		t.Fatal("missing sync check_access method")
	}
	// Async class and method exist.
	if !strings.Contains(out, "class AsyncAuthorizationClient:") {
		t.Fatal("missing AsyncAuthorizationClient class")
	}
	if !strings.Contains(out, "async def check_access(self, request: CheckAccessRequest)") {
		t.Fatal("missing async check_access method")
	}
	// Async transport import present.
	if !strings.Contains(out, "from .unary_transport import AsyncUnaryTransport") {
		t.Fatal("missing AsyncUnaryTransport import")
	}
	// Async method body must await the transport.
	if !strings.Contains(out, "await self._transport.unary(") {
		t.Fatal("async method body missing await on transport.unary")
	}
}

func TestAsyncClientGeneratedForGRPCOnly(t *testing.T) {
	t.Parallel()

	req := &model.Message{
		FullName:  "gestalt.provider.v1.CreateCredentialRequest",
		Name:      "CreateCredentialRequest",
		ProtoFile: "sdk/proto/v1/external_credential.proto",
	}
	resp := &model.Message{
		FullName:  "gestalt.provider.v1.Credential",
		Name:      "Credential",
		ProtoFile: "sdk/proto/v1/external_credential.proto",
	}
	svc := &model.Service{
		FullName: "gestalt.provider.v1.ExternalCredentials",
		Name:     "ExternalCredentials",
		Methods: []*model.Method{
			{
				Name:   "CreateCredential",
				Input:  req,
				Output: resp,
				// No HTTP rule -> gRPC-only.
			},
		},
	}

	r := newRenderer(asyncTestIndex(req, resp), "external_credentials_client", "external_credentials", modulePublic)
	r.publicClient = true
	r.renderAppClient(svc)
	out := r.assembleGenerated()

	// Async gRPC-only methods DO get an async variant (unlike Rust's sync PR,
	// where gRPC-only methods got no _sync sibling — here async gRPC is in scope).
	if !strings.Contains(out, "class AsyncExternalCredentialsClient:") {
		t.Fatal("missing AsyncExternalCredentialsClient class")
	}
	if !strings.Contains(out, "async def create_credential(") {
		t.Fatal("missing async create_credential method")
	}
}

func TestSyncClientUnchangedByAsyncAddition(t *testing.T) {
	t.Parallel()

	req := &model.Message{
		FullName:  "gestalt.provider.v1.CheckAccessRequest",
		Name:      "CheckAccessRequest",
		ProtoFile: "sdk/proto/v1/authorization.proto",
	}
	resp := &model.Message{
		FullName:  "gestalt.provider.v1.CheckAccessResponse",
		Name:      "CheckAccessResponse",
		ProtoFile: "sdk/proto/v1/authorization.proto",
	}
	svc := &model.Service{
		FullName: "gestalt.provider.v1.Authorization",
		Name:     "Authorization",
		Methods: []*model.Method{
			{
				Name: "CheckAccess",
				HTTP: &model.HTTPRule{Verb: "POST", Path: "/api/v2/authorization/access:check"},
				Input:  req,
				Output: resp,
			},
		},
	}

	r := newRenderer(asyncTestIndex(req, resp), "authorization_client", "authorization", modulePublic)
	r.publicClient = true
	r.renderAppClient(svc)
	out := r.assembleGenerated()

	// Sync method body must NOT contain await.
	idx := strings.Index(out, "def check_access(self, request:")
	end := strings.Index(out[idx:], "\n\n")
	syncMethod := out[idx : idx+end]
	if strings.Contains(syncMethod, "await") {
		t.Fatal("sync check_access method body must not contain await")
	}
	// Sync class constructor uses UnaryTransport, not AsyncUnaryTransport.
	if !strings.Contains(out, "def __init__(self, transport: UnaryTransport) -> None:") {
		t.Fatal("sync client constructor must use UnaryTransport")
	}
}

func TestAsyncRESTProtocolEmitted(t *testing.T) {
	t.Parallel()

	req := &model.Message{
		FullName:  "gestalt.provider.v1.CheckAccessRequest",
		Name:      "CheckAccessRequest",
		ProtoFile: "sdk/proto/v1/authorization.proto",
	}
	resp := &model.Message{
		FullName:  "gestalt.provider.v1.CheckAccessResponse",
		Name:      "CheckAccessResponse",
		ProtoFile: "sdk/proto/v1/authorization.proto",
	}
	svc := &model.Service{
		FullName: "gestalt.provider.v1.Authorization",
		Name:     "Authorization",
		Methods: []*model.Method{
			{
				Name: "CheckAccess",
				HTTP: &model.HTTPRule{Verb: "POST", Path: "/api/v2/authorization/access:check"},
				Input:  req,
				Output: resp,
			},
		},
	}

	r := newRenderer(asyncTestIndex(req, resp), "authorization_client", "authorization", modulePublic)
	r.publicClient = true
	r.renderAppClient(svc)
	out := r.assembleGenerated()

	if !strings.Contains(out, "class AsyncAuthorizationClientREST(Protocol):") {
		t.Fatal("missing AsyncAuthorizationClientREST protocol")
	}
	if !strings.Contains(out, "async def check_access(self, request: CheckAccessRequest) -> CheckAccessResponse: ...") {
		t.Fatal("missing async REST protocol method signature")
	}
}
