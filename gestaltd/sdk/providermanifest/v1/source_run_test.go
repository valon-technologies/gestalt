package providermanifestv1_test

import (
	"encoding/json"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

type codec struct {
	name      string
	marshal   func(any) ([]byte, error)
	unmarshal func([]byte, any) error
}

func codecs() []codec {
	return []codec{
		{"json", json.Marshal, json.Unmarshal},
		{"yaml", yaml.Marshal, yaml.Unmarshal},
	}
}

func TestSourceRunRoundTrip(t *testing.T) {
	t.Parallel()

	for _, c := range codecs() {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			want := providermanifestv1.SourceRun{
				Command:      []string{"bun", "run", "dev"},
				Workdir:      "ui",
				Env:          map[string]string{"FOO": "bar"},
				ReadyTimeout: "30s",
			}
			encoded, err := c.marshal(want)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var got providermanifestv1.SourceRun
			if err := c.unmarshal(encoded, &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if len(got.Command) != 3 || got.Command[0] != "bun" || got.Workdir != "ui" ||
				got.Env["FOO"] != "bar" || got.ReadyTimeout != "30s" {
				t.Fatalf("round trip = %#v", got)
			}
		})
	}
}

func TestSourceRunRejectsInvalid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		json string
		yaml string
	}{
		{"sequence form", `["bun","run","dev"]`, `[bun, run, dev]`},
		{"missing command", `{"workdir":"ui"}`, `workdir: ui`},
		{"unknown field", `{"command":["bun"],"unexpected":"value"}`, "command: [bun]\nunexpected: value"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var run providermanifestv1.SourceRun
			if err := json.Unmarshal([]byte(tc.json), &run); err == nil {
				t.Errorf("json: want rejection")
			}
			if err := yaml.Unmarshal([]byte(tc.yaml), &run); err == nil {
				t.Errorf("yaml: want rejection")
			}
		})
	}
}
