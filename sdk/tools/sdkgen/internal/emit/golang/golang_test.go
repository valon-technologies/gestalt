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
	for _, want := range []string{
		"client/rpc_support.go", "client/support_codec.go", "client/support_context.go",
		"client/cache.go", "client/cache_codec.go",
		"client/s3.go", "client/s3_codec.go",
		"client/indexeddb.go", "client/indexeddb_codec.go",
		"client/secrets.go", "client/authorization.go",
		"client/workflow_server.go", "client/support_server.go",
	} {
		if _, ok := files[want]; !ok {
			t.Fatalf("missing generated file %s (have %v)", want, keys(files))
		}
	}
	if len(files) != 32 {
		t.Errorf("generated files = %d, want 32: %v", len(files), keys(files))
	}

	// The wire seam stays out of the public files: every converter lives in
	// the _codec.go file beside its public file.
	for path, content := range files {
		isCodec := strings.HasSuffix(path, "_codec.go")
		hasConverters := strings.Contains(content, "func toWire") || strings.Contains(content, "func fromWire")
		if isCodec && !hasConverters {
			t.Errorf("%s is a codec file but defines no wire converters", path)
		}
		if !isCodec && hasConverters {
			t.Errorf("%s is a public file but defines wire converters", path)
		}
	}

	assertContains(t, files, "client/rpc_support.go",
		"type GestaltError struct",
		"type GestaltErrorCode int32",
		"type RpcStatus struct",
		"func toGestaltError(err error) *GestaltError",
		"func (e *GestaltError) Unwrap() error",
	)
	assertContains(t, files, "client/support_server.go",
		"func statusError(operation string, err error) error",
	)
	assertContains(t, files, "client/workflow_server.go",
		"type WorkflowProvider interface {",
		"ApplyDefinition(ctx context.Context, request *ApplyWorkflowProviderDefinitionRequest) (*WorkflowDefinition, error)",
		"DeleteDefinition(ctx context.Context, request *DeleteWorkflowProviderDefinitionRequest) error",
		"type UnimplementedWorkflowProvider struct{}",
		`Message: "workflow cancel run is not implemented"`,
		"func NewWorkflowProviderServer(provider WorkflowProvider) proto.WorkflowServer {",
		"s.provider.ApplyDefinition(ctx, fromWireApplyWorkflowProviderDefinitionRequest(request))",
		`statusError("workflow apply definition", err)`,
		"return toWireWorkflowDefinition(response), nil",
		"return &emptypb.Empty{}, nil",
	)
	assertContains(t, files, "client/support_codec.go",
		"func toWireTimestamp(value *time.Time) *timestamppb.Timestamp",
		"func fromWireDuration(value *durationpb.Duration) *time.Duration",
		"func toWireStruct(value map[string]any) *structpb.Struct",
		"func fromWireValue(value *structpb.Value) any",
		"func fromWireStatus(value *rpcstatus.Status) *RpcStatus",
	)
	assertContains(t, files, "client/support_context.go",
		// The option machinery is shared by every contextful client.
		"type ClientOption func(*clientOptions)",
		"func WithRequestContext(context *RequestContext) ClientOption",
	)
	assertContains(t, files, "client/support_invoke.go",
		// The JSON operation-envelope decode runtime is emitted because
		// App.Invoke carries the json_result annotation.
		"type InvokeError struct",
		"func DecodeAppResult(app, operation string, status int32, body []byte) (any, error)",
		"func DecodeGraphQLResult(app string, status int32, body []byte) (any, error)",
		"func JSONResultAs[T any](value any, err error) (T, error)",
		"func IsOK(status int32) bool",
		"func RequireOK(app, operation string, status int32, body []byte) error",
	)
	assertContains(t, files, "client/app.go",
		// json_result: the ergonomic method returns the decoded envelope and
		// the faithful Raw sibling keeps the full result. optional_signature
		// fields ride a trailing nilable options struct.
		"func (c *App) Invoke(ctx context.Context, app string, operation string, params map[string]any, opts *AppInvokeOptions) (any, error) {",
		"type AppInvokeOptions struct {\n\tConnection     string\n\tInstance       string\n\tIdempotencyKey string\n\tCredentialMode string\n}",
		"\tif opts == nil {\n\t\topts = &AppInvokeOptions{}\n\t}",
		"Params: params, Connection: opts.Connection, Instance: opts.Instance, IdempotencyKey: opts.IdempotencyKey, CredentialMode: opts.CredentialMode, Context: c.context}",
		"\treturn DecodeAppResult(request.App, request.Operation, out.Status, out.Body)",
		"func (c *App) InvokeRaw(ctx context.Context, request *AppInvokeRequest) (*OperationResult, error) {",
		// GraphQL stays faithful by design: decoding remains a helper.
		"func (c *App) InvokeGraphQL(ctx context.Context, app string, document string, opts *AppInvokeGraphQLOptions) (*OperationResult, error) {",
		"type AppInvokeGraphQLOptions struct {\n\tConnection     string\n\tInstance       string\n\tIdempotencyKey string\n\tVariables      map[string]any\n}",
	)
	assertContains(t, files, "client/agent.go",
		// Contextful services take client options: the default request context
		// fills flattened request literals directly and request-object calls
		// only when the caller left context unset (on a shallow copy, never
		// mutating the caller's request).
		"func NewAgent(conn grpc.ClientConnInterface, opts ...ClientOption) *Agent {",
		"func ConnectAgent(ctx context.Context, name string, opts ...ClientOption) (*Agent, error) {",
		"ProviderName: opts.ProviderName, Context: c.context}",
		"\tif request.Context == nil && c.context != nil {\n\t\tshallow := *request\n\t\tshallow.Context = c.context\n\t\trequest = &shallow\n\t}",
		// A method whose signature list is empty but whose optional_signature
		// is not still renders the ergonomic options-only form.
		"func (c *Agent) ListSessions(ctx context.Context, opts *AgentListSessionsOptions) ([]*AgentSession, error) {",
		"type AgentListSessionsOptions struct {",
	)
	assertContains(t, files, "client/cache.go",
		"type Cache struct {",
		"func NewCache(conn grpc.ClientConnInterface) *Cache {",
		"// CacheSetRequest writes one cache key.\ntype CacheSetRequest struct {",
		"\t// ttl applies an optional expiration to the entry.\n\tTtl *time.Duration",
		"Deleted int64",
		// The host_binding annotation generates a Connect constructor that
		// dials the named host service like the handwritten sdk/go clients.
		"func ConnectCache(ctx context.Context, name string) (*Cache, error) {",
		`host.Target("cache")`,
		// Ergonomic methods own the natural name: signature fields flatten
		// into parameters (presence fields as trailing pointers) and the
		// annotated responses collapse (optional_result to comma-ok, keyed to
		// a map, unwrap to the field's native type).
		"func (c *Cache) Get(ctx context.Context, key string) ([]byte, bool, error) {",
		"func (c *Cache) GetMany(ctx context.Context, keys []string) (map[string][]byte, error) {",
		"func (c *Cache) Set(ctx context.Context, key string, value []byte, ttl *time.Duration) error {",
		"func (c *Cache) Delete(ctx context.Context, key string) (bool, error) {",
		"func (c *Cache) DeleteMany(ctx context.Context, keys []string) (int64, error) {",
		"func (c *Cache) Touch(ctx context.Context, key string, ttl *time.Duration) (bool, error) {",
		// The faithful forms keep a Raw suffix.
		"func (c *Cache) GetRaw(ctx context.Context, request *CacheGetRequest) (*CacheGetResponse, error) {",
		"func (c *Cache) GetManyRaw(ctx context.Context, request *CacheGetManyRequest) (*CacheGetManyResponse, error) {",
		"func (c *Cache) SetRaw(ctx context.Context, request *CacheSetRequest) error {",
	)
	assertContains(t, files, "client/secrets.go",
		// signature + unwrap: flattened parameter, response unwrapped to the
		// value field's native type.
		"func (c *Secrets) GetSecret(ctx context.Context, name string) (string, error) {",
		"func (c *Secrets) GetSecretRaw(ctx context.Context, request *GetSecretRequest) (*GetSecretResponse, error) {",
	)
	assertContains(t, files, "client/authorization.go",
		// Empty-input ergonomic methods take ctx only.
		"func (c *Authorization) GetActiveModelRef(ctx context.Context) (*AuthorizationModelRef, error) {",
		"func (c *Authorization) GetActiveModelRefRaw(ctx context.Context) (*GetActiveModelRefResponse, error) {",
		"func ConnectAuthorization(ctx context.Context, name string) (*Authorization, error) {",
	)
	assertContains(t, files, "client/cache_codec.go",
		"func toWireCacheGetRequest(value *CacheGetRequest) *proto.CacheGetRequest {",
		"func fromWireCacheGetResponse(value *proto.CacheGetResponse) *CacheGetResponse {",
	)
	assertContains(t, files, "client/s3.go",
		// Framed streaming: the ergonomic forms consume/send the header frame
		// and expose typed payload streams; the frame-level forms keep Raw.
		"func (c *S3) ReadObject(ctx context.Context, request *ReadObjectRequest) (*S3ObjectMeta, *S3ReadObjectDataStream, error) {",
		"func (s *S3ReadObjectDataStream) Recv() ([]byte, error) {",
		// Byte payload streams buffer with an io.ReadAll-style helper.
		"func (s *S3ReadObjectDataStream) ReadAll() ([]byte, error) {",
		"func (c *S3) WriteObject(ctx context.Context, open *WriteObjectOpen) (*S3WriteObjectDataStream, error) {",
		"func (s *S3WriteObjectDataStream) Send(data []byte) error {",
		"func (s *S3WriteObjectDataStream) CloseAndRecv() (*WriteObjectResponse, error) {",
		"stream did not begin with the expected header frame",
		"unexpected frame in payload stream",
		"func ConnectS3(ctx context.Context, name string) (*S3, error) {",
		"func (c *S3) ReadObjectRaw(ctx context.Context, request *ReadObjectRequest) (*S3ReadObjectStream, error) {",
		"func (s *S3ReadObjectStream) Recv() (*ReadObjectChunk, error) {",
		"func (c *S3) WriteObjectRaw(ctx context.Context) (*S3WriteObjectStream, error) {",
		"func (s *S3WriteObjectStream) Send(request *WriteObjectRequest) error {",
		"func (s *S3WriteObjectStream) CloseAndRecv() (*WriteObjectResponse, error) {",
		"LastModified *time.Time",
		"Start *int64",
		"type ByteRange struct",
		"PresignMethodGet PresignMethod = 1",
		// optional_signature after a non-empty signature: positional
		// parameters first, then the trailing options struct.
		"func (c *S3) PresignObject(ctx context.Context, method PresignMethod, expiresSeconds int64, ref *S3ObjectRef, opts *S3PresignObjectOptions) (*PresignObjectResponse, error) {",
		"type S3PresignObjectOptions struct {\n\tContentType        string\n\tContentDisposition string\n}",
	)
	assertContains(t, files, "client/indexeddb.go",
		// The verbatim "indexeddb" binding string flows through unchanged.
		"func ConnectIndexedDB(ctx context.Context, name string) (*IndexedDB, error) {",
		`conn, err := connectIndexedDBConns.Conn(ctx, "indexeddb", target, token, name)`,
		"type TypedValueKind interface {",
		"isTypedValueKind()",
		"type TypedValueKindNullValue struct{}",
		"type TypedValueKindJsonValue struct {\n\tValue any\n}",
		"type TransactionOperationResponseResultEmpty struct{}",
		"func (c *IndexedDB) OpenCursor(ctx context.Context) (*IndexedDBOpenCursorStream, error) {",
		"func (s *IndexedDBOpenCursorStream) Send(request *CursorClientMessage) error {",
		"func (c *IndexedDB) Transaction(ctx context.Context) (*IndexedDBTransactionStream, error) {",
		"func (s *IndexedDBTransactionStream) Recv() (*TransactionServerMessage, error) {",
		"Error *RpcStatus",
		"Range *KeyRange",
		"CursorDirectionCursorNext CursorDirection = 0",
	)
	assertContains(t, files, "client/indexeddb_codec.go",
		"func toWireTypedValue(value *TypedValue) *proto.TypedValue {",
		"case *TypedValueKindNullValue:",
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
