package rust

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

func TestSyncUnaryTransportTraitEmitted(t *testing.T) {
	t.Parallel()

	// The publicUnaryTransportFile constant is the template for
	// generated/unary_transport.rs. Verify it contains the sync trait.
	if !strings.Contains(publicUnaryTransportFile, "pub trait SyncUnaryTransport") {
		t.Fatal("publicUnaryTransportFile missing SyncUnaryTransport trait")
	}
	if !strings.Contains(publicUnaryTransportFile, "fn unary<Req, Resp>") {
		t.Fatal("publicUnaryTransportFile missing sync unary method")
	}
	// The sync trait must NOT return a Future.
	if strings.Contains(publicUnaryTransportFile, "impl Future") {
		// impl Future should only appear in the async trait — check it's
		// associated with UnaryTransport, not SyncUnaryTransport.
		idx := strings.Index(publicUnaryTransportFile, "pub trait SyncUnaryTransport")
		end := strings.Index(publicUnaryTransportFile[idx:], "}")
		syncTrait := publicUnaryTransportFile[idx : idx+end]
		if strings.Contains(syncTrait, "impl Future") {
			t.Fatal("SyncUnaryTransport trait must not return impl Future")
		}
	}
}

func TestSyncMethodsGeneratedForREST(t *testing.T) {
	t.Parallel()

	svc := &model.Service{
		FullName: "gestalt.provider.v1.Authorization",
		Name:     "Authorization",
		Methods: []*model.Method{
			{
				Name: "CheckAccess",
				HTTP: &model.HTTPRule{Verb: "POST", Path: "/api/v2/authorization/access:check"},
				Input: &model.Message{
					FullName:  "gestalt.provider.v1.CheckAccessRequest",
					Name:      "CheckAccessRequest",
					ProtoFile: "sdk/proto/v1/authorization.proto",
				},
				Output: &model.Message{
					FullName:  "gestalt.provider.v1.CheckAccessResponse",
					Name:      "CheckAccessResponse",
					ProtoFile: "sdk/proto/v1/authorization.proto",
				},
			},
		},
	}

	r := newRenderer(&index{}, "app_client", "app", modulePublic, true)
	r.renderAppClient(svc)
	out := r.assembleGenerated()

	// Async method exists
	if !strings.Contains(out, "pub async fn check_access(") {
		t.Fatal("missing async check_access method")
	}
	// Sync method exists
	if !strings.Contains(out, "pub fn check_access_sync(") {
		t.Fatal("missing sync check_access_sync method")
	}
	// Async impl block bound on UnaryTransport
	if !strings.Contains(out, "impl<T: UnaryTransport> AuthorizationClient<T>") {
		t.Fatal("missing async impl block with UnaryTransport bound")
	}
	// Sync impl block bound on SyncUnaryTransport
	if !strings.Contains(out, "impl<T: crate::public::generated::unary_transport::SyncUnaryTransport> AuthorizationClient<T>") {
		t.Fatal("missing sync impl block with SyncUnaryTransport bound")
	}
}

func TestNoSyncMethodsForGRPCOnly(t *testing.T) {
	t.Parallel()

	svc := &model.Service{
		FullName: "gestalt.provider.v1.ExternalCredentials",
		Name:     "ExternalCredentials",
		Methods: []*model.Method{
			{
				Name: "CreateCredential",
				// No HTTP rule → gRPC-only
				Input: &model.Message{
					FullName:  "gestalt.provider.v1.CreateCredentialRequest",
					Name:      "CreateCredentialRequest",
					ProtoFile: "sdk/proto/v1/external_credential.proto",
				},
				Output: &model.Message{
					FullName:  "gestalt.provider.v1.Credential",
					Name:      "Credential",
					ProtoFile: "sdk/proto/v1/external_credential.proto",
				},
			},
		},
	}

	r := newRenderer(&index{}, "app_client", "app", modulePublic, true)
	r.renderAppClient(svc)
	out := r.assembleGenerated()

	// Async method exists (in GrpcCapable block)
	if !strings.Contains(out, "pub async fn create_credential(") {
		t.Fatal("missing async create_credential method")
	}
	// No sync method — gRPC-only methods don't get sync variants
	if strings.Contains(out, "create_credential_sync") {
		t.Fatal("gRPC-only method should not have a sync variant")
	}
	// No SyncUnaryTransport impl block
	if strings.Contains(out, "SyncUnaryTransport") {
		t.Fatal("gRPC-only service should not have a SyncUnaryTransport impl block")
	}
}

func TestSyncMethodBodiesHaveNoAwait(t *testing.T) {
	t.Parallel()

	svc := &model.Service{
		FullName: "gestalt.provider.v1.Authorization",
		Name:     "Authorization",
		Methods: []*model.Method{
			{
				Name: "CheckAccess",
				HTTP: &model.HTTPRule{Verb: "POST", Path: "/api/v2/authorization/access:check"},
				Input: &model.Message{
					FullName:  "gestalt.provider.v1.CheckAccessRequest",
					Name:      "CheckAccessRequest",
					ProtoFile: "sdk/proto/v1/authorization.proto",
				},
				Output: &model.Message{
					FullName:  "gestalt.provider.v1.CheckAccessResponse",
					Name:      "CheckAccessResponse",
					ProtoFile: "sdk/proto/v1/authorization.proto",
				},
			},
		},
	}

	r := newRenderer(&index{}, "app_client", "app", modulePublic, true)
	r.renderAppClient(svc)
	out := r.assembleGenerated()

	// Find the sync impl block and verify no .await inside it
	idx := strings.Index(out, "impl<T: crate::public::generated::unary_transport::SyncUnaryTransport>")
	if idx < 0 {
		t.Fatal("missing sync impl block")
	}
	syncBlock := out[idx:]
	// The block ends at the next "\n}\n\n" after the impl
	endIdx := strings.Index(syncBlock, "\n}\n\n")
	if endIdx < 0 {
		// might be at the very end
		endIdx = len(syncBlock)
	}
	syncBlock = syncBlock[:endIdx]
	if strings.Contains(syncBlock, ".await") {
		t.Fatalf("sync method body contains .await:\n%s", syncBlock)
	}
	if strings.Contains(syncBlock, "async") {
		t.Fatalf("sync method body contains 'async':\n%s", syncBlock)
	}
}
