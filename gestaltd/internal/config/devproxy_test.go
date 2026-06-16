package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestUIEntryYAMLRoundTripsDevProxy(t *testing.T) {
	const src = "source: ./ui/manifest.yaml\npath: /dash\ndevProxy:\n  url: http://127.0.0.1:3000\n"

	var entry UIEntry
	if err := yaml.Unmarshal([]byte(src), &entry); err != nil {
		t.Fatalf("unmarshal ui entry with devProxy: %v", err)
	}
	if !entry.HasDevProxy() {
		t.Fatal("HasDevProxy() = false after YAML decode")
	}
	if got := entry.DevProxyURL(); got != "http://127.0.0.1:3000" {
		t.Fatalf("DevProxyURL() = %q, want http://127.0.0.1:3000", got)
	}
	// Source is neutralized so no lifecycle path treats a dev-proxy mount as
	// source-backed and tries to build it.
	if entry.HasLocalSource() {
		t.Fatal("dev-proxy entry still reports a local source; prepare paths would build it")
	}

	out, err := yaml.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal ui entry: %v", err)
	}
	var round UIEntry
	if err := yaml.Unmarshal(out, &round); err != nil {
		t.Fatalf("re-unmarshal marshalled ui entry: %v", err)
	}
	if !round.HasDevProxy() || round.DevProxyURL() != "http://127.0.0.1:3000" {
		t.Fatalf("devProxy lost on round-trip: %q", round.DevProxyURL())
	}
}
