package python_test

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/python"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/pipeline"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

// TestEmitSpikeSurface renders the real provider schema and asserts the shape
// of the emitted Python: native dataclasses with presence, oneof variant
// unions, frame-level streaming clients, the canonical error model, and the
// public/internal-codec module split. Requires the pinned buf; skips when
// unavailable, exactly like the pipeline test.
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
	set, err := python.New().Emit(schema)
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
	want := []string{"rpc_support.py", "_codec/__init__.py", "_codec/support.py"}
	for _, base := range bases {
		want = append(want, base+".py", "_codec/"+base+".py")
	}
	for _, path := range want {
		if _, ok := files[path]; !ok {
			t.Fatalf("missing generated file %s (have %v)", path, keys(files))
		}
	}
	if len(files) != len(want) {
		t.Errorf("generated files = %d, want %d: %v", len(files), len(want), keys(files))
	}

	// rpc_support keeps only the public error model; the call helpers and
	// well-known-type converters live in the internal codec runtime.
	assertContains(t, files, "rpc_support.py",
		"class GestaltError(Exception):",
		"class GestaltErrorCode:",
		"class RpcStatus:",
		`JsonValue = bool | int | float | str | list["JsonValue"] | dict[str, "JsonValue"] | None`,
	)
	assertNotContains(t, files, "rpc_support.py",
		"import grpc",
		"call_unary",
		"to_wire_",
	)
	assertContains(t, files, "_codec/support.py",
		"def to_gestalt_error(error: BaseException) -> GestaltError:",
		"def call_unary(call: Callable[[], _Result]) -> _Result:",
		"def to_wire_duration(value: datetime.timedelta) -> Any:",
		"def read_header_frame(",
		"def chunk_frames(",
		"def framed_send(",
		"from ..rpc_support import GestaltError, GestaltErrorCode, JsonValue, RpcStatus",
	)
	assertContains(t, files, "_codec/__init__.py",
		"Internal wire codec",
	)

	assertContains(t, files, "cache.py",
		"class Cache:",
		"def __init__(self, channel: grpc.Channel) -> None:",
		"ttl: datetime.timedelta | None = None",
		"deleted: int = 0",
		// Proto comments land as docstrings on dataclasses and as #: doc
		// comments on fields, and the service comment leads the client
		// docstring.
		"    \"\"\"CacheSetRequest writes one cache key.\"\"\"\n\n    key: str",
		"    #: ttl applies an optional expiration to the entry.\n    ttl: datetime.timedelta | None = None",
		"    \"\"\"Cache models the shared Gestalt cache-provider protocol.\n\n    Client for the gestalt.provider.v1.Cache service.\n    \"\"\"",
		// The host_binding annotation generates a zero-configuration connect
		// classmethod over the handwritten transport helpers.
		"    @classmethod\n    def connect(cls, name: str | None = None) -> Cache:",
		"        channel = host_service_channel(\n            \"cache\", target, token=token.strip(), binding=(name or \"\").strip()\n        )\n        return cls(channel)",
		"from ._grpc_transport import (\n    ENV_HOST_SERVICE_SOCKET,\n    ENV_HOST_SERVICE_TOKEN,\n    host_service_channel,\n)",
		// Signature-annotated methods are dual-mode: @overload stubs advertise
		// the request-object and keyword-only forms, the implementation
		// guards against mixing them, and response collapses still apply.
		// The faithful _raw sibling exists only when the response collapses.
		"    @overload\n    def get(self, request: CacheGetRequest) -> bytes | None: ...",
		"    @overload\n    def get(self, *, key: str = ...) -> bytes | None: ...",
		"def get(self, request: CacheGetRequest | None = None, *, key: str | None = None) -> bytes | None:",
		"        return response.value if response.found else None",
		"def get_raw(self, request: CacheGetRequest) -> CacheGetResponse:",
		"def get_many(self, request: CacheGetManyRequest | None = None, *, keys: list[str] | None = None) -> dict[str, bytes]:",
		"            request = CacheGetManyRequest(keys=keys if keys is not None else [])",
		"        out: dict[str, bytes] = {}",
		"            if entry.found:",
		"    @overload\n    def set(self, *, key: str = ..., value: bytes = ..., ttl: datetime.timedelta | None = ...) -> None: ...",
		"def set(self, request: CacheSetRequest | None = None, *, key: str | None = None, value: bytes | None = None, ttl: datetime.timedelta | None = None) -> None:",
		"            request = CacheSetRequest(key=key or \"\", value=value or b\"\", ttl=ttl)",
		"        elif key is not None or value is not None or ttl is not None:",
		"            raise ValueError(\"pass either request or keyword arguments, not both\")",
		"def delete_many(self, request: CacheDeleteManyRequest | None = None, *, keys: list[str] | None = None) -> int:",
		"def touch(self, request: CacheTouchRequest | None = None, *, key: str | None = None, ttl: datetime.timedelta | None = None) -> bool:",
		// Clients reach converters through the codec module object only.
		"response = _support.call_unary(lambda: self._stub.Get(_codec.to_wire_cache_get_request(request)))",
		"return _codec.from_wire_cache_get_response(response)",
		"self._stub = _cache_pb2_grpc.CacheStub(channel)",
	)

	assertContains(t, files, "app.py",
		// Contextful services take a client-level default request context,
		// injected (via dataclasses.replace, never mutating the caller's
		// request) only when the outgoing request left context unset.
		"def __init__(self, channel: grpc.Channel, *, context: RequestContext | None = None) -> None:",
		"        return cls(channel, context=context)",
		"        if request.context is None and self._context is not None:\n            request = replace(request, context=self._context)",
	)

	// The wire-conversion seam is not part of the public surface: converters
	// live only in the internal codec modules, which the public clients
	// import as module objects so the circular import resolves.
	for _, base := range bases {
		assertContains(t, files, base+".py",
			"from ._codec import "+base+" as _codec",
			"from ._codec import support as _support",
		)
		assertNotContains(t, files, base+".py",
			"_to_wire",
			"_from_wire",
			"def to_wire_",
			"def from_wire_",
			"_wire: Any",
			"_wire_grpc: Any",
		)
	}

	// Converters build the typed pb2 messages directly and reference native
	// types through the public counterpart module.
	assertContains(t, files, "_codec/cache.py",
		"from .. import cache as native",
		"def to_wire_cache_get_request(value: native.CacheGetRequest) -> Any:",
		"return _cache_pb2.CacheGetRequest(",
		"def from_wire_cache_get_response(value: Any) -> native.CacheGetResponse:",
	)
	assertContains(t, files, "_codec/agent.py",
		"from . import app",
		"app.to_wire_agent_tool_ref",
	)

	// The framing annotation replaces raw frames with the header and a native
	// payload stream; the frame-level forms keep the _raw suffix.
	assertContains(t, files, "s3.py",
		"def read_object(self, request: ReadObjectRequest) -> tuple[S3ObjectMeta, Iterator[bytes]]:",
		"meta = _support.read_header_frame(frames, lambda frame: frame.result.value if isinstance(frame.result, ReadObjectChunkMeta) else None)",
		"return meta, _support.chunk_frames(frames, lambda frame: frame.result.value if isinstance(frame.result, ReadObjectChunkData) else None)",
		"def read_object_raw(self, request: ReadObjectRequest) -> Iterator[ReadObjectChunk]:",
		"def write_object(self, open: WriteObjectOpen, data: Iterable[bytes]) -> WriteObjectResponse:",
		"requests = _support.framed_send(",
		"_codec.to_wire_write_object_request(WriteObjectRequest(msg=WriteObjectRequestOpen(value=open))),",
		"lambda chunk: _codec.to_wire_write_object_request(WriteObjectRequest(msg=WriteObjectRequestData(value=chunk))),",
		"def write_object_raw(self, requests: Iterable[WriteObjectRequest]) -> WriteObjectResponse:",
		"last_modified: datetime.datetime | None = None",
		"start: int | None = None",
	)
	// The unwrap annotation collapses the response to its only meaningful
	// field; a presence-bearing unwrapped field stays absent-capable.
	assertContains(t, files, "secrets.py",
		"def get_secret(self, request: GetSecretRequest | None = None, *, name: str | None = None) -> str:",
		"        return response.value",
		"def get_secret_raw(self, request: GetSecretRequest) -> GetSecretResponse:",
	)
	// Empty-input ergonomic methods take no parameters at all.
	assertContains(t, files, "authorization.py",
		"def get_active_model_ref(self) -> AuthorizationModelRef | None:",
		"        return response.model",
		"def get_active_model_ref_raw(self) -> GetActiveModelRefResponse:",
	)
	// Keyword-named proto fields (ConnectionParamDef.from) are renamed with a
	// trailing underscore; conversions keep the true wire field name.
	assertContains(t, files, "app.py",
		`from_: str = ""`,
	)
	assertContains(t, files, "_codec/app.py",
		`**{"from": value.from_},`,
		`from_=getattr(value, "from"),`,
	)
	assertContains(t, files, "indexeddb.py",
		"TypedValueKind = (",
		"class TypedValueNullValue:",
		"    value: JsonValue",
		"def open_cursor(self, requests: Iterable[CursorClientMessage]) -> Iterator[CursorResponse]:",
		"def transaction(self, requests: Iterable[TransactionClientMessage]) -> Iterator[TransactionServerMessage]:",
		"error: RpcStatus | None = None",
		"range: KeyRange | None = None",
	)
	assertContains(t, files, "_codec/indexeddb.py",
		"def to_wire_typed_value_kind(value: native.TypedValueKind) -> dict[str, Any]:",
		"def from_wire_typed_value_kind(value: Any) -> native.TypedValueKind:",
		"if isinstance(value, native.TypedValueNullValue):",
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
