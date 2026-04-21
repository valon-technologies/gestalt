package gestalt

import "testing"

func TestProviderListenTargetParsesUnixAndTCPTargets(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		network string
		address string
	}{
		{name: "raw unix path", raw: "/tmp/plugin.sock", network: "unix", address: "/tmp/plugin.sock"},
		{name: "unix uri absolute", raw: "unix:///tmp/plugin.sock", network: "unix", address: "/tmp/plugin.sock"},
		{name: "unix uri relative-ish", raw: "unix://tmp/plugin.sock", network: "unix", address: "tmp/plugin.sock"},
		{name: "tcp uri", raw: "tcp://127.0.0.1:50051", network: "tcp", address: "127.0.0.1:50051"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			network, address, err := providerListenTarget(tc.raw)
			if err != nil {
				t.Fatalf("providerListenTarget(%q): %v", tc.raw, err)
			}
			if network != tc.network || address != tc.address {
				t.Fatalf("providerListenTarget(%q) = (%q, %q), want (%q, %q)", tc.raw, network, address, tc.network, tc.address)
			}
		})
	}
}
