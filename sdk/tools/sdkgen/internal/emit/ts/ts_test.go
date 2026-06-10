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
	for _, want := range []string{
		"rpc_support.ts", "invoke_support.ts", "internal/codec/support.ts",
		"cache.ts", "internal/codec/cache.ts",
		"s3.ts", "internal/codec/s3.ts",
		"indexeddb.ts", "internal/codec/indexeddb.ts",
	} {
		if _, ok := files[want]; !ok {
			t.Fatalf("missing generated file %s (have %v)", want, keys(files))
		}
	}
	if len(files) != 29 {
		t.Errorf("generated files = %d, want 29: %v", len(files), keys(files))
	}

	assertContains(t, files, "rpc_support.ts",
		"export class GestaltError extends Error",
		"export type DurationMs = number;",
		"export interface RpcStatus",
	)
	assertNotContains(t, files, "rpc_support.ts",
		"callUnary",
		"toWireDuration",
	)
	assertContains(t, files, "internal/codec/support.ts",
		"export async function callUnary",
		"export function toWireDuration",
		`from "../../rpc_support.ts"`,
	)
	assertContains(t, files, "cache.ts",
		"export class Cache {",
		"constructor(transport: Transport)",
		"static connect(name?: string): Cache",
		"ttl?: DurationMs;",
		"deleted: bigint;",
		"async get(key: string): Promise<Uint8Array | undefined>",
		"async getRaw(request: CacheGetRequest): Promise<CacheGetResponse>",
		"async getMany(keys: string[]): Promise<{ [key: string]: Uint8Array }>",
		"async set(key: string, value: Uint8Array, ttl?: DurationMs): Promise<void>",
		"async deleteMany(keys: string[]): Promise<bigint>",
		"async touch(key: string, ttl?: DurationMs): Promise<boolean>",
	)
	assertContains(t, files, "s3.ts",
		"async readObject(request: ReadObjectRequest): Promise<{ meta: S3ObjectMeta; data: ByteStream }>",
		"readObjectRaw(request: ReadObjectRequest): AsyncIterable<ReadObjectChunk>",
		"async writeObject(open: WriteObjectOpen, data: AsyncIterable<Uint8Array>): Promise<WriteObjectResponse>",
		"async writeObjectRaw(requests: AsyncIterable<WriteObjectRequest>): Promise<WriteObjectResponse>",
		"lastModified?: Date;",
	)
	assertContains(t, files, "indexeddb.ts",
		"export type TypedValueKind =",
		`| { case: "nullValue"; value: null }`,
		`| { case: "jsonValue"; value: JsonValue }`,
		"openCursor(requests: AsyncIterable<CursorClientMessage>): AsyncIterable<CursorResponse>",
		"transaction(requests: AsyncIterable<TransactionClientMessage>): AsyncIterable<TransactionServerMessage>",
		"error?: RpcStatus;",
		"range?: KeyRange;",
	)

	// Proto comments render as JSDoc in the public modules: message docs above
	// interfaces, field docs above properties, and service docs above clients.
	assertContains(t, files, "cache.ts",
		"/**\n * CacheSetRequest writes one cache key.\n */\nexport interface CacheSetRequest {",
		"  /**\n   * ttl applies an optional expiration to the entry.\n   */\n  ttl?: DurationMs;",
		"/**\n * Cache models the shared Gestalt cache-provider protocol.\n */\nexport class Cache {",
	)
	assertContains(t, files, "rpc_support.ts",
		// Byte payload streams buffer with SdkStreamMixin-style transforms.
		"export interface ByteStream extends AsyncIterable<Uint8Array> {",
		"transformToByteArray(): Promise<Uint8Array>;",
	)
	assertContains(t, files, "s3.ts",
		"data: ByteStream }> {",
	)
	assertContains(t, files, "app.ts",
		// Contextful services take a client-level default request context,
		// injected into flattened request literals directly and into
		// request-object calls only when the caller left context unset.
		"private readonly context: RequestContext | undefined;",
		"constructor(transport: Transport, options?: { context?: RequestContext }) {",
		"static connect(name?: string, options?: { context?: RequestContext }): App {",
		"...(this.context !== undefined ? { context: this.context } : {})",
		"...(request.context === undefined && this.context !== undefined",
	)

	// json_result methods decode the JSON operation envelope: the ergonomic
	// method returns the caller-typed decoded payload, the faithful Raw
	// method and the unannotated InvokeGraphQL keep the HTTP-shaped result.
	assertContains(t, files, "app.ts",
		"async invoke<T = unknown>(app: string, operation: string, connection: string, instance: string, idempotencyKey: string, credentialMode: string, params?: JsonObject): Promise<T> {",
		"return decodeAppResult<T>(request.app, request.operation, response);",
		"async invokeRaw(request: AppInvokeRequest): Promise<OperationResult> {",
		"async invokeGraphQL(app: string, document: string, connection: string, instance: string, idempotencyKey: string, variables?: JsonObject): Promise<OperationResult> {",
		"async invokeGraphQLRaw(request: AppInvokeGraphQLRequest): Promise<OperationResult> {",
		`import { decodeAppResult } from "./invoke_support.ts";`,
	)
	assertContains(t, files, "invoke_support.ts",
		"export class InvokeError extends Error",
		"export function decodeAppResult<T = unknown>(",
		"export function decodeGraphQLResult<T = unknown>(",
	)
	// Codec modules are undocumented plumbing: no JSDoc renders there.
	assertNotContains(t, files, "internal/codec/cache.ts", "/**")
	assertNotContains(t, files, "internal/codec/indexeddb.ts", "/**")

	// The wire-conversion seam is not part of the public surface: converters
	// live only in the internal codec modules, which the public clients import.
	for _, base := range []string{"cache.ts", "s3.ts", "indexeddb.ts"} {
		assertNotContains(t, files, base,
			"export function toWire",
			"export function fromWire",
		)
		assertContains(t, files, base, `from "./internal/codec/`+base+`";`)
	}
	assertContains(t, files, "internal/codec/cache.ts",
		"export function toWireCacheGetRequest(value: CacheGetRequest): wire.CacheGetRequest",
		"export function fromWireCacheGetResponse(value: wire.CacheGetResponse): CacheGetResponse",
		`import type { CacheDeleteManyRequest`,
	)
	assertContains(t, files, "internal/codec/indexeddb.ts",
		"export function toWireTypedValueKind(value: TypedValueKind)",
		"export function fromWireTypedValueKind(",
	)
	assertContains(t, files, "internal/codec/s3.ts",
		`import * as wire from "../gen/v1/s3_pb.ts";`,
		`import { fromWireTimestamp, toWireTimestamp } from "./support.ts";`,
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
			t.Errorf("%s unexpectedly contains %q", path, reject)
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
