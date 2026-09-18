package catalog

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAPIExposureModeAcceptsLegacyBooleansAndBrowserSession(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		json string
		want APIExposureMode
	}{
		{name: "true", json: `true`, want: APIExposurePublic},
		{name: "false", json: `false`, want: APIExposurePrivate},
		{name: "browser session", json: `"browserSession"`, want: APIExposureBrowserSession},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var mode APIExposureMode
			if err := json.Unmarshal([]byte(tc.json), &mode); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if mode != tc.want {
				t.Fatalf("mode = %q, want %q", mode, tc.want)
			}
			encoded, err := json.Marshal(mode)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if string(encoded) != tc.json {
				t.Fatalf("encoded = %s, want %s", encoded, tc.json)
			}
		})
	}

	var fromYAML struct {
		API APIExposureMode `yaml:"api"`
	}
	if err := yaml.Unmarshal([]byte("api: browserSession\n"), &fromYAML); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if fromYAML.API != APIExposureBrowserSession {
		t.Fatalf("yaml mode = %q, want browserSession", fromYAML.API)
	}
}

func TestAPIExposureModeRejectsUnknownValues(t *testing.T) {
	t.Parallel()
	for _, input := range []string{`"true"`, `"private"`, `1`, `null`} {
		var mode APIExposureMode
		if err := json.Unmarshal([]byte(input), &mode); err == nil {
			t.Errorf("json %s unexpectedly accepted as %q", input, mode)
		}
	}
	var cat Catalog
	if err := json.Unmarshal([]byte(`{"name":"test","operations":[{"id":"op","api":"private"}]}`), &cat); err == nil {
		t.Fatal("catalog unexpectedly accepted invalid api mode")
	}
}
