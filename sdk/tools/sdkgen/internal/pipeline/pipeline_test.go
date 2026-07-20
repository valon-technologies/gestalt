package pipeline

import (
	"bytes"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := FindRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
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
