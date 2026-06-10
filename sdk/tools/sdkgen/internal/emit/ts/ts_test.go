package ts_test

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/ts"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/pipeline"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

// TestEmitSpikeSurface renders the real provider schema and asserts the shape
// of the emitted TypeScript: native types with presence, oneof unions,
// frame-level streaming clients, and the canonical error model. Requires the
// pinned buf; skips when unavailable, exactly like the pipeline test.
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
	set, err := ts.New().Emit(schema)
	if err != nil {
		t.Fatal(err)
	}

	files := map[string]string{}
	for _, f := range set.Files() {
		files[f.Path] = string(f.Content)
	}
	for _, want := range []string{"rpc_support.ts", "cache_client.ts", "s3_client.ts", "datastore_client.ts"} {
		if _, ok := files[want]; !ok {
			t.Fatalf("missing generated file %s (have %v)", want, keys(files))
		}
	}
	if len(files) != 14 {
		t.Errorf("generated files = %d, want 14: %v", len(files), keys(files))
	}

	assertContains(t, files, "rpc_support.ts",
		"export class GestaltError extends Error",
		"export type DurationMs = number;",
		"export interface RpcStatus",
	)
	assertContains(t, files, "cache_client.ts",
		"export class CacheClient {",
		"constructor(transport: Transport)",
		"ttl?: DurationMs;",
		"deleted: bigint;",
		"async get(request: CacheGetRequest): Promise<CacheGetResponse>",
		"async set(request: CacheSetRequest): Promise<void>",
	)
	assertContains(t, files, "s3_client.ts",
		"readObject(request: ReadObjectRequest): AsyncIterable<ReadObjectChunk>",
		"writeObject(requests: AsyncIterable<WriteObjectRequest>): Promise<WriteObjectResponse>",
		"lastModified?: Date;",
	)
	assertContains(t, files, "datastore_client.ts",
		"export type TypedValueKind =",
		`| { case: "nullValue"; value: null }`,
		`| { case: "jsonValue"; value: JsonValue }`,
		"openCursor(requests: AsyncIterable<CursorClientMessage>): AsyncIterable<CursorResponse>",
		"transaction(requests: AsyncIterable<TransactionClientMessage>): AsyncIterable<TransactionServerMessage>",
		"error?: RpcStatus;",
		"range?: KeyRange;",
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
