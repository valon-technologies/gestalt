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
	for _, want := range []string{
		"rpc_support.rs",
		"agent_client.rs",
		"app_client.rs",
		"authentication_client.rs",
		"authorization_client.rs",
		"cache_client.rs",
		"datastore_client.rs",
		"external_credential_client.rs",
		"runtime_client.rs",
		"runtime_provider_client.rs",
		"s3_client.rs",
		"secrets_client.rs",
		"test_client.rs",
		"workflow_client.rs",
	} {
		if _, ok := files[want]; !ok {
			t.Fatalf("missing generated file %s (have %v)", want, keys(files))
		}
	}
	if len(files) != 14 {
		t.Errorf("generated files = %d, want 14: %v", len(files), keys(files))
	}

	assertContains(t, files, "rpc_support.rs",
		"pub struct GestaltError {",
		"impl From<tonic::Status> for GestaltError",
		"pub struct RpcStatus {",
		"pub fn from_wire_duration(value: prost_types::Duration) -> Duration",
	)
	// Wire references must use prost's heck idents even when the proto name
	// diverges; native types keep the raw proto local name.
	assertContains(t, files, "app_client.rs",
		"pub fn to_wire_http_subject_request(value: HTTPSubjectRequest) -> v1::HttpSubjectRequest {",
		"pub fn from_wire_app_invoke_graph_ql_request(value: v1::AppInvokeGraphQlRequest) -> AppInvokeGraphQLRequest {",
		"self.inner.invoke_graph_ql(",
	)
	assertContains(t, files, "s3_client.rs",
		"inner: v1::s3_object_access_client::S3ObjectAccessClient<tonic::transport::Channel>,",
	)
	assertContains(t, files, "cache_client.rs",
		"pub struct CacheClient {",
		"pub fn new(channel: tonic::transport::Channel) -> Self",
		"pub ttl: Option<std::time::Duration>,",
		"pub deleted: i64,",
		"pub async fn get(&mut self, request: CacheGetRequest) -> Result<CacheGetResponse, GestaltError>",
		"pub async fn set(&mut self, request: CacheSetRequest) -> Result<(), GestaltError>",
	)
	assertContains(t, files, "s3_client.rs",
		"pub async fn read_object(&mut self, request: ReadObjectRequest) -> Result<S3ReadObjectStream, GestaltError>",
		"pub async fn recv(&mut self) -> Result<Option<ReadObjectChunk>, GestaltError>",
		"requests: impl tokio_stream::Stream<Item = WriteObjectRequest> + Send + 'static,",
		") -> Result<WriteObjectResponse, GestaltError>",
		"pub last_modified: Option<std::time::SystemTime>,",
	)
	assertContains(t, files, "datastore_client.rs",
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

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
