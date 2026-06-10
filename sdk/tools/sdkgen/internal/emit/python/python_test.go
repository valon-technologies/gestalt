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
// unions, frame-level streaming clients, and the canonical error model.
// Requires the pinned buf; skips when unavailable, exactly like the pipeline
// test.
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
	want := []string{
		"rpc_support.py",
		"agent.py",
		"app.py",
		"authentication.py",
		"authorization.py",
		"cache.py",
		"datastore.py",
		"external_credential.py",
		"runtime.py",
		"runtime_provider.py",
		"s3.py",
		"secrets.py",
		"test.py",
		"workflow.py",
	}
	for _, path := range want {
		if _, ok := files[path]; !ok {
			t.Fatalf("missing generated file %s (have %v)", path, keys(files))
		}
	}
	if len(files) != len(want) {
		t.Errorf("generated files = %d, want %d: %v", len(files), len(want), keys(files))
	}

	// Internal plumbing carries a leading underscore; only the error model
	// and the native well-known representations are public.
	assertContains(t, files, "rpc_support.py",
		"class GestaltError(Exception):",
		"def _to_gestalt_error(error: BaseException) -> GestaltError:",
		"def _call_unary(call: Callable[[], _Result]) -> _Result:",
		"class RpcStatus:",
		`JsonValue = bool | int | float | str | list["JsonValue"] | dict[str, "JsonValue"] | None`,
	)
	assertContains(t, files, "cache.py",
		"class Cache:",
		"def __init__(self, channel: grpc.Channel) -> None:",
		"ttl: datetime.timedelta | None = None",
		"deleted: int = 0",
		"def get(self, request: CacheGetRequest) -> CacheGetResponse:",
		"def set(self, request: CacheSetRequest) -> None:",
		// Converters are module-internal and build the typed pb2 messages
		// directly: no Any-typed _wire alias.
		"def _to_wire_cache_get_request(value: CacheGetRequest) -> Any:",
		"return _cache_pb2.CacheGetRequest(",
		"self._stub = _cache_pb2_grpc.CacheStub(channel)",
	)
	assertNotContains(t, files, "cache.py",
		"_wire: Any",
		"_wire_grpc: Any",
		"def to_wire_",
		"def from_wire_",
	)
	assertContains(t, files, "s3.py",
		"def read_object(self, request: ReadObjectRequest) -> Iterator[ReadObjectChunk]:",
		"def write_object(self, requests: Iterable[WriteObjectRequest]) -> WriteObjectResponse:",
		"last_modified: datetime.datetime | None = None",
		"start: int | None = None",
	)
	// Keyword-named proto fields (ConnectionParamDef.from) are renamed with a
	// trailing underscore; conversions keep the true wire field name.
	assertContains(t, files, "app.py",
		`from_: str = ""`,
		`**{"from": value.from_},`,
		`from_=getattr(value, "from"),`,
	)
	assertContains(t, files, "datastore.py",
		"TypedValueKind = (",
		"class TypedValueNullValue:",
		"    value: JsonValue",
		"def open_cursor(self, requests: Iterable[CursorClientMessage]) -> Iterator[CursorResponse]:",
		"def transaction(self, requests: Iterable[TransactionClientMessage]) -> Iterator[TransactionServerMessage]:",
		"error: RpcStatus | None = None",
		"range: KeyRange | None = None",
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
