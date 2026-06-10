package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

// TestRenderIntoTree reconciles emitter output into the SDK trees using only
// a local `buf build` (no remote plugins), for iterating on emitters without
// regenerating wire stubs. Opt in explicitly with a comma-separated target
// list: it writes to the working tree.
//
//	SDKGEN_RENDER_TARGETS=ts,go,python,rust go test ./internal/pipeline/ -run TestRenderIntoTree
func TestRenderIntoTree(t *testing.T) {
	t.Parallel()
	spec := os.Getenv("SDKGEN_RENDER_TARGETS")
	if spec == "" {
		t.Skip("set SDKGEN_RENDER_TARGETS=ts,python,go,rust to render emitter output into the SDK trees")
	}
	targets, err := emit.ParseTargets(spec)
	if err != nil {
		t.Fatal(err)
	}
	bufTool := toolchain.Buf()
	if err := bufTool.Verify(); err != nil {
		t.Skipf("skipping: %v", err)
	}
	root := repoRoot(t)
	scratch := t.TempDir()
	schema, err := BuildSchema(bufTool, root, scratch)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range emittersFor(targets) {
		set, err := EmitFormatted(e, schema, scratch)
		if err != nil {
			t.Fatalf("emit %s: %v", e.Target(), err)
		}
		report, err := fileset.Reconcile(filepath.Join(root, filepath.FromSlash(e.OutputRoot())), set, e.HeaderStyle())
		if err != nil {
			t.Fatalf("reconcile %s: %v", e.Target(), err)
		}
		t.Logf("%s: rendered %d files (%d written, %d removed)", e.Target(), set.Len(), len(report.Written), len(report.Deleted))
	}
}
