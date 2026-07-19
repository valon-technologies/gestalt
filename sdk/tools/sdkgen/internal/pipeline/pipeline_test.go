package pipeline

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := FindRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// realSchema builds the normalized model from the real proto module. Requires
// the pinned buf; skips when it is unavailable so plain `go test ./...` works
// without the full toolchain. CI always has buf installed.
func realSchema(t *testing.T) *model.Schema {
	t.Helper()
	bufTool := toolchain.Buf()
	if err := bufTool.Verify(); err != nil {
		t.Skipf("skipping: %v", err)
	}
	schema, err := BuildSchema(bufTool, repoRoot(t), t.TempDir())
	if err != nil {
		t.Fatalf("build schema from real protos: %v", err)
	}
	return schema
}

func TestRealProtosValidateCleanly(t *testing.T) {
	t.Parallel()
	schema := realSchema(t)

	wantServices := []string{
		"gestalt.provider.v1.Agent",
		"gestalt.provider.v1.App",
		"gestalt.provider.v1.AppProvider",
		"gestalt.provider.v1.Authorization",
		"gestalt.provider.v1.Cache",
		"gestalt.provider.v1.ExternalCredentials",
		"gestalt.provider.v1.Identity",
		"gestalt.provider.v1.IndexedDB",
		"gestalt.provider.v1.ProviderLifecycle",
		"gestalt.provider.v1.Runtime",
		"gestalt.provider.v1.S3",
		"gestalt.provider.v1.S3ObjectAccess",
		"gestalt.provider.v1.Secrets",
		"gestalt.provider.v1.Test",
		"gestalt.provider.v1.Workflow",
	}
	var gotServices []string
	for _, svc := range schema.Services {
		gotServices = append(gotServices, svc.FullName)
	}
	if !reflect.DeepEqual(gotServices, wantServices) {
		t.Errorf("discovered services = %v, want %v", gotServices, wantServices)
	}

	wantStreams := map[string]model.StreamKind{
		"gestalt.provider.v1.S3.ReadObject":         model.ServerStream,
		"gestalt.provider.v1.S3.WriteObject":        model.ClientStream,
		"gestalt.provider.v1.IndexedDB.OpenCursor":  model.Bidi,
		"gestalt.provider.v1.IndexedDB.Transaction": model.Bidi,
	}
	streaming := 0
	for _, svc := range schema.Services {
		for _, m := range svc.Methods {
			full := svc.FullName + "." + m.Name
			if m.Stream != model.Unary {
				streaming++
				if want, ok := wantStreams[full]; !ok || m.Stream != want {
					t.Errorf("method %s: stream = %v, want %v", full, m.Stream, wantStreams[full])
				}
			}
			if (m.Input == nil) != m.InputIsEmpty {
				t.Errorf("method %s: Input/InputIsEmpty inconsistent", full)
			}
			if (m.Output == nil) != m.OutputIsEmpty {
				t.Errorf("method %s: Output/OutputIsEmpty inconsistent", full)
			}
		}
	}
	if streaming != len(wantStreams) {
		t.Errorf("streaming methods = %d, want %d", streaming, len(wantStreams))
	}
}

// TestEmittersAreDeterministic guards against map-iteration ordering creeping
// into emitter output once emitters produce real files.
func TestEmittersAreDeterministic(t *testing.T) {
	t.Parallel()
	schema := &model.Schema{}
	for _, e := range Emitters() {
		first, err := e.Emit(schema)
		if err != nil {
			t.Fatalf("emit %s: %v", e.Target(), err)
		}
		second, err := e.Emit(schema)
		if err != nil {
			t.Fatalf("emit %s: %v", e.Target(), err)
		}
		if !bytes.Equal(renderAll(first, e.HeaderStyle()), renderAll(second, e.HeaderStyle())) {
			t.Errorf("emitter %s output is not byte-stable", e.Target())
		}
	}
}

func TestPublicSurfaceMethodCounts(t *testing.T) {
	t.Parallel()
	schema := realSchema(t)
	view := publicsurface.Build(schema)

	if got := publicsurface.GRPCMethodCount(view); got != 71 {
		t.Errorf("public gRPC methods = %d, want 71", got)
	}
	if got := publicsurface.RESTMethodCount(view); got != 51 {
		t.Errorf("public REST methods = %d, want 51", got)
	}
}

func TestEmitterRegistryCoversAllTargets(t *testing.T) {
	t.Parallel()
	emitters := Emitters()
	want := []emit.Target{
		emit.TargetTS,
		emit.TargetPublicTS,
		emit.TargetPublicTSWeb,
		emit.TargetPython,
		emit.TargetPublicPython,
		emit.TargetGo,
		emit.TargetPublicGo,
		emit.TargetRust,
		emit.TargetPublicRust,
	}
	if len(emitters) != len(want) {
		t.Fatalf("emitters = %d, want %d", len(emitters), len(want))
	}
	for i, target := range want {
		if emitters[i].Target() != target {
			t.Errorf("emitter %d = %s, want %s", i, emitters[i].Target(), target)
		}
		if emitters[i].OutputRoot() == "" {
			t.Errorf("emitter %s has no output root", target)
		}
	}
}

func renderAll(set *fileset.FileSet, style fileset.CommentStyle) []byte {
	var out []byte
	for _, f := range set.Files() {
		out = append(out, []byte(f.Path)...)
		out = append(out, 0)
		out = append(out, fileset.Rendered(f, style)...)
		out = append(out, 0)
	}
	return out
}
