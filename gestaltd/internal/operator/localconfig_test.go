package operator

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestDefaultManagedConfigPinsManagedProviders(t *testing.T) {
	cfg := defaultManagedConfig("/tmp/gestalt.db", "test-key")

	for _, want := range []string{
		"ref: " + config.DefaultIndexedDBProvider,
		"version: " + config.DefaultIndexedDBVersion,
		"ref: " + config.DefaultUIProvider,
		"version: " + config.DefaultUIVersion,
		"ref: " + defaultHTTPBinProvider,
		"version: " + defaultHTTPBinVersion,
		"path: /",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("default managed config missing %q\n%s", want, cfg)
		}
	}
}
