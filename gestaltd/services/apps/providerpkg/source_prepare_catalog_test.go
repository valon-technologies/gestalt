package providerpkg

import (
	"os"
	"path/filepath"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestEnsureSourceStaticCatalogSkipsNonAppKinds(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := mustWriteManifestData(t, dir, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindCache,
		Source:  "github.com/test/providers/cache",
		Version: "0.0.1-alpha.1",
		Spec:    &providermanifestv1.Spec{},
		Run:     sourceRunCommand("go", "run", "example"),
	}))

	if err := EnsureSourceStaticCatalog(manifestPath, &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindCache,
		Source:  "github.com/test/providers/cache",
		Version: "0.0.1-alpha.1",
		Spec:    &providermanifestv1.Spec{},
		Run:     sourceRunCommand("go", "run", "example"),
	}); err != nil {
		t.Fatalf("EnsureSourceStaticCatalog: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, StaticCatalogFile)); err == nil {
		t.Fatalf("catalog should not be generated for kind cache")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat catalog: %v", err)
	}
}
