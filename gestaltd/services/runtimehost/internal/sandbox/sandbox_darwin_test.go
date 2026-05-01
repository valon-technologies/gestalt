//go:build darwin

package sandbox

import (
	"strings"
	"testing"
)

func TestBuildSBPLProfileConstrainsProxyPort(t *testing.T) {
	t.Parallel()

	profile := buildSBPLProfile(&Policy{ProxyPort: 3128})
	if !strings.Contains(profile, `(remote tcp "localhost:3128")`) {
		t.Fatalf("profile = %s, want tcp-scoped proxy port", profile)
	}
	if !strings.Contains(profile, `"localhost:3128"`) {
		t.Fatalf("profile = %s, want exact proxy port", profile)
	}
	if strings.Contains(profile, "localhost:*") {
		t.Fatalf("profile = %s, must not allow localhost wildcard port", profile)
	}
}
