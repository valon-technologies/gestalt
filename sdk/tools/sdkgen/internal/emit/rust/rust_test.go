package rust_test

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/rust"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/pipeline"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

// TestEmitSpikeSurface renders the real provider schema and asserts the shape
// of the emitted Rust: native types with presence, oneof enums, frame-level
// streaming clients, and the canonical error model. Requires the pinned buf;
// skips when unavailable, exactly like the pipeline test.
func TestEmitSpikeSurface(t *testing.T) {
	t.Parallel()
	bufTool := toolchain.Buf()
	if err := bufTool.Verify(); err != nil {
		t.Skipf("skipping: %v", err)
	}
	root, err := pipeline.FindRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := pipeline.BuildSchema(bufTool, root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	set, err := rust.New().Emit(schema)
	if err != nil {
		t.Fatal(err)
	}

	files := map[string]string{}
	for _, f := range set.Files() {
		files[f.Path] = string(f.Content)
	}
	bases := []string{
		"agent",
		"app",
		"authentication",
		"authorization",
		"cache",
		"indexeddb",
		"external_credential",
		"runtime",
		"runtime_provider",
		"s3",
		"secrets",
		"test",
		"workflow",
	}
	want := []string{"rpc_support.rs", "codec.rs", "codec/support.rs", "codec/host_service.rs"}
	for _, base := range bases {
		want = append(want, base+".rs", "codec/"+base+".rs")
	}
	for _, path := range want {
		if _, ok := files[path]; !ok {
			t.Fatalf("missing generated file %s (have %v)", path, keys(files))
		}
	}
	if len(files) != len(want) {
		t.Errorf("generated files = %d, want %d: %v", len(files), len(want), keys(files))
	}

	// The wire seam stays out of the public modules: converters live only in
	// the crate-private codec tree.
	for _, base := range bases {
		assertNotContains(t, files, base+".rs", "fn to_wire", "fn from_wire")
		assertContains(t, files, "codec/"+base+".rs", "pub(crate) fn ")
	}

	assertContains(t, files, "rpc_support.rs",
		"pub struct GestaltError {",
		"impl From<tonic::Status> for GestaltError",
		"pub struct RpcStatus {",
	)
	assertNotContains(t, files, "rpc_support.rs",
		"pub(crate) fn ",
	)
	assertContains(t, files, "codec.rs",
		"pub(crate) mod agent;",
		"pub(crate) mod host_service;",
		"pub(crate) mod support;",
		"pub(crate) mod workflow;",
	)
	// The host-service transport is crate-private shared plumbing.
	assertContains(t, files, "codec/host_service.rs",
		"pub(crate) type HostServiceChannel",
		"pub(crate) async fn connect_host_service(",
		"pub(crate) fn plain_channel(",
	)
	// Only the well-known-type converters some generated module uses are
	// emitted, so every crate-private converter keeps a caller.
	assertContains(t, files, "codec/support.rs",
		"pub(crate) fn to_wire_duration(value: Duration) -> prost_types::Duration",
	)
	assertNotContains(t, files, "codec/support.rs",
		"fn from_wire_duration",
		"fn to_wire_status",
	)
	// Wire references must use prost's heck idents even when the proto name
	// diverges; native types keep the raw proto local name. Converters are
	// crate-private and only the needed direction is emitted.
	assertContains(t, files, "codec/app.rs",
		"use crate::app::{",
		"pub(crate) fn to_wire_http_subject_request(value: HTTPSubjectRequest) -> v1::HttpSubjectRequest {",
		"pub(crate) fn to_wire_app_invoke_graphql_request(value: AppInvokeGraphQLRequest) -> v1::AppInvokeGraphQlRequest {",
		"pub(crate) fn from_wire_resolve_http_subject_response(value: v1::ResolveHttpSubjectResponse) -> ResolveHTTPSubjectResponse {",
	)
	assertNotContains(t, files, "codec/app.rs",
		"pub fn to_wire_",
		"pub fn from_wire_",
		"fn from_wire_app_invoke_graph_ql_request",
	)
	// Public clients reach their converters through the codec tree; codec
	// modules convert nested cross-file messages through each other.
	assertContains(t, files, "app.rs",
		"use crate::codec::app::{",
		"self.inner.invoke_graph_ql(",
		// Contextful services take a client-level default request context,
		// injected into flattened request literals directly and into
		// request-object calls only when the caller left context unset.
		"    context: Option<RequestContext>,\n}",
		"    pub fn with_context(mut self, context: RequestContext) -> Self {",
		"context: self.context.clone() }",
		"        let mut request = request;\n        if request.context.is_none() {\n            request.context = self.context.clone();\n        }",
	)
	assertContains(t, files, "codec/workflow.rs",
		"use crate::codec::agent::{",
		"use crate::workflow::{",
	)
	assertContains(t, files, "s3.rs",
		"inner: v1::s3_object_access_client::S3ObjectAccessClient<tonic::transport::Channel>,",
	)
	// Ergonomic methods own the natural snake_case name; the faithful form
	// keeps a _raw suffix. Signature fields flatten to owned parameters with
	// presence fields trailing as Option, and annotated responses collapse.
	assertContains(t, files, "cache.rs",
		"pub struct Cache {",
		"pub fn new(channel: tonic::transport::Channel) -> Self",
		"pub ttl: Option<std::time::Duration>,",
		"pub deleted: i64,",
		"pub async fn get(&mut self, key: String) -> Result<Option<Vec<u8>>, GestaltError>",
		"pub async fn get_raw(&mut self, request: CacheGetRequest) -> Result<CacheGetResponse, GestaltError>",
		"pub async fn get_many(&mut self, keys: Vec<String>) -> Result<std::collections::BTreeMap<String, Vec<u8>>, GestaltError>",
		"pub async fn set(&mut self, key: String, value: Vec<u8>, ttl: Option<std::time::Duration>) -> Result<(), GestaltError>",
		"pub async fn set_raw(&mut self, request: CacheSetRequest) -> Result<(), GestaltError>",
		"pub async fn delete_many(&mut self, keys: Vec<String>) -> Result<i64, GestaltError>",
		"pub async fn touch(&mut self, key: String, ttl: Option<std::time::Duration>) -> Result<bool, GestaltError>",
	)
	// Host-bound services gain connect constructors; the binding string from
	// the host_binding annotation passes through verbatim.
	assertContains(t, files, "cache.rs",
		"use crate::codec::host_service::{HostServiceChannel, connect_host_service, plain_channel};",
		"inner: v1::cache_client::CacheClient<HostServiceChannel>,",
		"pub async fn connect() -> Result<Self, GestaltError>",
		"pub async fn connect_named(name: &str) -> Result<Self, GestaltError>",
		`connect_host_service("cache", name).await?`,
	)
	assertContains(t, files, "indexeddb.rs",
		`connect_host_service("IndexedDB", name).await?`,
	)
	assertContains(t, files, "runtime_provider.rs",
		`connect_host_service("runtime log host", name).await?`,
	)
	// Unannotated services keep the plain channel transport and no connect.
	assertContains(t, files, "authentication.rs",
		"inner: v1::authentication_client::AuthenticationClient<tonic::transport::Channel>,",
		"pub async fn validate_external_token(&mut self, token: String) -> Result<AuthenticatedUser, GestaltError>",
		"pub async fn validate_external_token_raw(&mut self, request: ValidateExternalTokenRequest) -> Result<AuthenticatedUser, GestaltError>",
	)
	assertNotContains(t, files, "authentication.rs",
		"pub async fn connect",
	)
	// Unwrap collapses to the field's native type; presence-bearing unwraps
	// stay optional. Empty-input ergonomic methods take only the receiver.
	assertContains(t, files, "secrets.rs",
		"pub async fn get_secret(&mut self, name: String) -> Result<String, GestaltError>",
		"pub async fn get_secret_raw(&mut self, request: GetSecretRequest) -> Result<GetSecretResponse, GestaltError>",
	)
	assertContains(t, files, "authorization.rs",
		"pub async fn get_active_model_ref(&mut self) -> Result<Option<AuthorizationModelRef>, GestaltError>",
		"pub async fn get_active_model_ref_raw(&mut self) -> Result<GetActiveModelRefResponse, GestaltError>",
	)
	// Proto leading comments render as rustdoc above the generic lines:
	// message docs above structs, field docs above fields, and service docs
	// above clients.
	assertContains(t, files, "cache.rs",
		"/// CacheSetRequest writes one cache key.\n///\n"+
			"/// Native message type for `gestalt.provider.v1.CacheSetRequest`.\n"+
			"#[derive(Clone, Debug, Default, PartialEq)]\npub struct CacheSetRequest {",
		"    /// ttl applies an optional expiration to the entry.\n    ///\n"+
			"    /// The `ttl` field; None when unset.\n"+
			"    pub ttl: Option<std::time::Duration>,",
		"/// Cache models the shared Gestalt cache-provider protocol.\n///\n"+
			"/// Client for the `gestalt.provider.v1.Cache` service.\npub struct Cache {",
	)
	// Framed streams collapse to a header value plus a payload stream; the
	// frame-level forms keep the _raw suffix.
	assertContains(t, files, "s3.rs",
		"pub async fn read_object(&mut self, request: ReadObjectRequest) -> Result<(S3ObjectMeta, S3ReadObjectData), GestaltError>",
		"pub async fn read_object_raw(&mut self, request: ReadObjectRequest) -> Result<S3ReadObjectStream, GestaltError>",
		"pub struct S3ReadObjectData {",
		"pub async fn recv(&mut self) -> Result<Option<Vec<u8>>, GestaltError>",
		"\"stream did not begin with the expected header frame\"",
		"\"unexpected frame in payload stream\"",
		"pub async fn recv(&mut self) -> Result<Option<ReadObjectChunk>, GestaltError>",
		"pub async fn write_object(\n        &mut self,\n        open: WriteObjectOpen,\n        data: impl tokio_stream::Stream<Item = Vec<u8>> + Send + 'static,\n    ) -> Result<WriteObjectResponse, GestaltError>",
		"pub async fn write_object_raw(",
		"requests: impl tokio_stream::Stream<Item = WriteObjectRequest> + Send + 'static,",
		") -> Result<WriteObjectResponse, GestaltError>",
		"pub last_modified: Option<std::time::SystemTime>,",
	)
	// Multi-line method docs render above both the client method and its
	// stream wrapper type.
	assertContains(t, files, "s3.rs",
		"    /// The first response frame carries object metadata. All subsequent frames\n"+
			"    /// carry byte chunks. Zero-byte objects therefore emit exactly one frame.\n    ///\n"+
			"    /// Calls `gestalt.provider.v1.S3.ReadObject`, returning a stream of converted frames.",
		"/// The first response frame carries object metadata. All subsequent frames\n"+
			"/// carry byte chunks. Zero-byte objects therefore emit exactly one frame.\n///\n"+
			"/// Stream of converted `ReadObjectChunk` frames; transport errors convert to GestaltError.\n"+
			"pub struct S3ReadObjectStream {",
	)
	assertContains(t, files, "indexeddb.rs",
		"pub enum TypedValueKind {",
		"    NullValue,",
		"    JsonValue(serde_json::Value),",
		"requests: impl tokio_stream::Stream<Item = CursorClientMessage> + Send + 'static,",
		") -> Result<IndexedDBOpenCursorStream, GestaltError>",
		"requests: impl tokio_stream::Stream<Item = TransactionClientMessage> + Send + 'static,",
		") -> Result<IndexedDBTransactionStream, GestaltError>",
		"pub error: Option<RpcStatus>,",
		"pub range: Option<KeyRange>,",
	)
}

func assertContains(t *testing.T, files map[string]string, path string, wants ...string) {
	t.Helper()
	content := files[path]
	for _, want := range wants {
		if !strings.Contains(content, want) {
			t.Errorf("%s missing %q", path, want)
		}
	}
}

func assertNotContains(t *testing.T, files map[string]string, path string, rejects ...string) {
	t.Helper()
	content := files[path]
	for _, reject := range rejects {
		if strings.Contains(content, reject) {
			t.Errorf("%s contains %q", path, reject)
		}
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
