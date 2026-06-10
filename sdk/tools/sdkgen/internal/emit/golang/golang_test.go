package golang_test

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/golang"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/pipeline"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

// TestEmitSpikeSurface renders the real provider schema and asserts the shape
// of the emitted Go: native types with presence, oneof wrapper structs,
// frame-level streaming clients, and the canonical error model. Requires the
// pinned buf; skips when unavailable, exactly like the pipeline test. Output
// is asserted post-gofmt, matching the checked-in files byte for byte.
func TestEmitSpikeSurface(t *testing.T) {
	t.Parallel()
	bufTool := toolchain.Buf()
	if err := bufTool.Verify(); err != nil {
		t.Skipf("skipping: %v", err)
	}
	emitter := golang.New()
	if err := emitter.Formatter().Verify(); err != nil {
		t.Skipf("skipping: %v", err)
	}
	root, err := pipeline.FindRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	scratch := t.TempDir()
	schema, err := pipeline.BuildSchema(bufTool, root, scratch)
	if err != nil {
		t.Fatal(err)
	}
	set, err := pipeline.EmitFormatted(emitter, schema, scratch)
	if err != nil {
		t.Fatal(err)
	}

	files := map[string]string{}
	for _, f := range set.Files() {
		files[f.Path] = string(f.Content)
	}
	for _, want := range []string{"client/rpc_support.go", "client/cache.go", "client/s3.go", "client/datastore.go"} {
		if _, ok := files[want]; !ok {
			t.Fatalf("missing generated file %s (have %v)", want, keys(files))
		}
	}
	if len(files) != 14 {
		t.Errorf("generated files = %d, want 14: %v", len(files), keys(files))
	}

	assertContains(t, files, "client/rpc_support.go",
		"type GestaltError struct",
		"type GestaltErrorCode int32",
		"type RpcStatus struct",
		"func toGestaltError(err error) *GestaltError",
		"func (e *GestaltError) Unwrap() error",
	)
	assertContains(t, files, "client/cache.go",
		"type CacheClient struct {",
		"func NewCacheClient(conn grpc.ClientConnInterface) *CacheClient {",
		"Ttl   *time.Duration",
		"Deleted int64",
		"func (c *CacheClient) Get(ctx context.Context, request *CacheGetRequest) (*CacheGetResponse, error) {",
		"func (c *CacheClient) Set(ctx context.Context, request *CacheSetRequest) error {",
	)
	assertContains(t, files, "client/s3.go",
		"func (c *S3Client) ReadObject(ctx context.Context, request *ReadObjectRequest) (*S3ReadObjectStream, error) {",
		"func (s *S3ReadObjectStream) Recv() (*ReadObjectChunk, error) {",
		"func (c *S3Client) WriteObject(ctx context.Context) (*S3WriteObjectStream, error) {",
		"func (s *S3WriteObjectStream) Send(request *WriteObjectRequest) error {",
		"func (s *S3WriteObjectStream) CloseAndRecv() (*WriteObjectResponse, error) {",
		"LastModified *time.Time",
		"Start *int64",
		"type ByteRange struct",
		"PresignMethodGet         PresignMethod = 1",
	)
	assertContains(t, files, "client/datastore.go",
		"type TypedValueKind interface {",
		"isTypedValueKind()",
		"type TypedValueKindNullValue struct{}",
		"type TypedValueKindJsonValue struct {\n\tValue any\n}",
		"type TransactionOperationResponseResultEmpty struct{}",
		"func (c *IndexedDBClient) OpenCursor(ctx context.Context) (*IndexedDBOpenCursorStream, error) {",
		"func (s *IndexedDBOpenCursorStream) Send(request *CursorClientMessage) error {",
		"func (c *IndexedDBClient) Transaction(ctx context.Context) (*IndexedDBTransactionStream, error) {",
		"func (s *IndexedDBTransactionStream) Recv() (*TransactionServerMessage, error) {",
		"Error *RpcStatus",
		"Range *KeyRange",
		"CursorDirectionCursorNext       CursorDirection = 0",
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
