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
		"export type Init<T> = T extends InitAtom",
	)
	assertNotContains(t, files, "rpc_support.ts",
		"callUnary",
		"toWireDuration",
	)
	assertContains(t, files, "internal/codec/support.ts",
		"export async function callUnary",
		"export function toWireDuration",
		"export function callOptions(timeoutMs?: number): { timeoutMs: number } | undefined",
		`from "../../rpc_support.ts"`,
	)
	assertContains(t, files, "cache.ts",
		"export class Cache {",
		"constructor(transport: Transport, options?: { timeoutMs?: number | undefined })",
		"static connect(options?: { name?: string | undefined; timeoutMs?: number | undefined }): Cache",
		"ttl?: DurationMs;",
		"deleted: bigint;",
		"async get(key: string): Promise<Uint8Array | undefined>",
		"async getRaw(request: Init<CacheGetRequest>): Promise<CacheGetResponse>",
		"async getMany(keys: string[]): Promise<{ [key: string]: Uint8Array }>",
		"async set(key: string, value: Uint8Array, ttl?: DurationMs): Promise<void>",
		"async deleteMany(keys: string[]): Promise<bigint>",
		"async touch(key: string, ttl?: DurationMs): Promise<boolean>",
		// Client-level timeoutMs becomes per-call options on unary calls.
		"this.client.get(toWireCacheGetRequest(request), callOptions(this.timeoutMs))",
	)
	assertContains(t, files, "s3.ts",
		"async readObject(request: Init<ReadObjectRequest>): Promise<{ meta: S3ObjectMeta; data: ByteStream }>",
		"readObjectRaw(request: Init<ReadObjectRequest>): AsyncIterable<ReadObjectChunk>",
		"async writeObject(open: Init<WriteObjectOpen>, data: AsyncIterable<Uint8Array>): Promise<WriteObjectResponse>",
		"async writeObjectRaw(requests: AsyncIterable<Init<WriteObjectRequest>>): Promise<WriteObjectResponse>",
		"lastModified?: Date;",
		// optional_signature fields surface as a trailing options object; the
		// request literal fills unset options with proto defaults.
		"async presignObject(method: PresignMethod, expiresSeconds: bigint, ref?: Init<S3ObjectRef>, options?: { contentType?: string | undefined; contentDisposition?: string | undefined }): Promise<PresignObjectResponse> {",
		"contentType: options?.contentType ?? \"\"",
		// Oneof variant constructors are emitted beside the union type.
		"export function readObjectChunkResultMeta(value: S3ObjectMeta): ReadObjectChunkResult {",
		`  return { case: "meta", value };`,
	)
	// Enum members shed a uniform SCREAMING_SNAKE enum-name prefix.
	assertContains(t, files, "s3.ts",
		"  GET: 1,",
	)
	assertNotContains(t, files, "s3.ts",
		"PRESIGN_METHOD_GET",
	)
	assertContains(t, files, "indexeddb.ts",
		"export type TypedValueKind =",
		`| { case: "nullValue"; value: null }`,
		`| { case: "jsonValue"; value: JsonValue }`,
		// Unit-like variants take no argument.
		"export function typedValueKindNullValue(): TypedValueKind {",
		"openCursor(requests: AsyncIterable<Init<CursorClientMessage>>): AsyncIterable<CursorResponse>",
		"transaction(requests: AsyncIterable<Init<TransactionClientMessage>>): AsyncIterable<TransactionServerMessage>",
		"error?: RpcStatus;",
		"range?: KeyRange;",
		// CursorDirection members are not uniformly prefixed, so they keep
		// their verbatim proto names.
		"CURSOR_NEXT: 0,",
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
		"constructor(transport: Transport, options?: { context?: RequestContext | undefined; timeoutMs?: number | undefined }) {",
		"static connect(options?: { name?: string | undefined; context?: RequestContext | undefined; timeoutMs?: number | undefined }): App {",
		"...(this.context !== undefined ? { context: this.context } : {})",
		"...(request.context === undefined && this.context !== undefined",
	)

	// json_result methods decode the JSON operation envelope: the ergonomic
	// method returns the caller-typed decoded payload, the faithful Raw
	// method and the unannotated InvokeGraphQL keep the HTTP-shaped result.
	// optional_signature fields move into the trailing options object.
	assertContains(t, files, "app.ts",
		"async invoke<T = unknown>(app: string, operation: string, params?: JsonObject, options?: { connection?: string | undefined; instance?: string | undefined; idempotencyKey?: string | undefined; credentialMode?: string | undefined; runAs?: Init<SubjectContext> | undefined }): Promise<T> {",
		"connection: options?.connection ?? \"\"",
		"return decodeAppResult<T>(request.app, request.operation, response);",
		"async invokeRaw(request: Init<AppInvokeRequest>): Promise<OperationResult> {",
		"async invokeGraphQL(app: string, document: string, options?: { connection?: string | undefined; instance?: string | undefined; idempotencyKey?: string | undefined; variables?: JsonObject | undefined }): Promise<OperationResult> {",
		"async invokeGraphQLRaw(request: Init<AppInvokeGraphQLRequest>): Promise<OperationResult> {",
		`import { decodeAppResult } from "./invoke_support.ts";`,
	)
	// Methods with an empty signature but a non-empty optional_signature
	// render an options-only ergonomic surface.
	assertContains(t, files, "agent.ts",
		"async listSessions(options?: { sessionIds?: string[] | undefined; state?: AgentSessionState | undefined; limit?: number | undefined; summaryOnly?: boolean | undefined; providerName?: string | undefined }): Promise<AgentSession[]> {",
		"providerName: options?.providerName ?? \"\"",
	)
	assertContains(t, files, "invoke_support.ts",
		"export class InvokeError extends Error",
		"export function decodeAppResult<T = unknown>(",
		"export function decodeGraphQLResult<T = unknown>(",
		"export function isOk(status: number): boolean",
		"export function requireOk(",
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
	// toWire converters accept sparse Init values and zero-fill unset
	// non-presence fields; fromWire converters are unchanged.
	assertContains(t, files, "internal/codec/cache.ts",
		"export function toWireCacheGetRequest(value: Init<CacheGetRequest>): wire.CacheGetRequest",
		"key: (value.key ?? \"\"),",
		"export function fromWireCacheGetResponse(value: wire.CacheGetResponse): CacheGetResponse",
		`import type { CacheDeleteManyRequest`,
	)
	assertContains(t, files, "internal/codec/indexeddb.ts",
		"export function toWireTypedValueKind(value: Init<TypedValueKind>)",
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
