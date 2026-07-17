package publicsurface_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/pipeline"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

func TestManifestGolden(t *testing.T) {
	t.Parallel()
	schema := manifestRealSchema(t)
	plan, err := publicsurface.PrepareEmit(schema)
	if err != nil {
		t.Fatalf("PrepareEmit: %v", err)
	}
	built := publicsurface.BuildManifest(plan.View, plan.Methods)
	manifest, err := publicsurface.MarshalManifest(built)
	if err != nil {
		t.Fatalf("MarshalManifest: %v", err)
	}
	availability := publicsurface.MarshalAvailabilityDoc(built)

	root, err := pipeline.FindRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join(root, "sdk", "testdata", "public_surface", "manifest.json")
	availabilityGolden := filepath.Join(root, "sdk", "testdata", "public_surface", "api_availability.md")
	if os.Getenv("SDKGEN_UPDATE_GOLDENS") != "" {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, append(manifest, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(availabilityGolden, []byte(availability), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated golden %s and %s", golden, availabilityGolden)
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden: %v (set SDKGEN_UPDATE_GOLDENS=1 to create)", err)
	}
	if string(want) != string(append(manifest, '\n')) {
		t.Fatalf("manifest drift: rerun with SDKGEN_UPDATE_GOLDENS=1")
	}
	wantAvailability, err := os.ReadFile(availabilityGolden)
	if err != nil {
		t.Fatalf("read availability golden: %v (set SDKGEN_UPDATE_GOLDENS=1 to create)", err)
	}
	if string(wantAvailability) != availability {
		t.Fatalf("availability doc drift: rerun with SDKGEN_UPDATE_GOLDENS=1")
	}
}

func manifestRealSchema(t *testing.T) *model.Schema {
	t.Helper()
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
		t.Fatalf("build schema: %v", err)
	}
	return schema
}
