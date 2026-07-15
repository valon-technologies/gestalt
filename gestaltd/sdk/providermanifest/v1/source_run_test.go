package providermanifestv1_test

import (
	"encoding/json"
	"strings"
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

	for _, tc := range []struct {
		name string
		run  providermanifestv1.SourceRun
		want string
	}{
		{
			name: "standard",
			run: providermanifestv1.SourceRun{
				Command:      []string{"bun", "run", "dev"},
				Workdir:      "ui",
				Env:          map[string]string{"FOO": "bar"},
				ReadyTimeout: "30s",
			},
			want: "standard",
		},
		{
			name: "frontend-only",
			run: providermanifestv1.SourceRun{
				Command:      []string{"npm", "run", "dev"},
				FrontendOnly: true,
			},
			want: "frontend-only",
		},
	} {
		tc := tc
		for _, c := range codecs() {
			c, tc := c, tc
			t.Run(tc.name+"/"+c.name, func(t *testing.T) {
				t.Parallel()

				encoded, err := c.marshal(tc.run)
				if err != nil {
					t.Fatalf("Marshal: %v", err)
				}
				var got providermanifestv1.SourceRun
				if err := c.unmarshal(encoded, &got); err != nil {
					t.Fatalf("Unmarshal: %v", err)
				}
				switch tc.want {
				case "standard":
					if len(got.Command) != 3 || got.Command[0] != "bun" || got.Workdir != "ui" ||
						got.Env["FOO"] != "bar" || got.ReadyTimeout != "30s" {
						t.Fatalf("round trip = %#v", got)
					}
				case "frontend-only":
					if !strings.Contains(string(encoded), "frontendOnly") {
						t.Fatalf("encoded run = %s, want frontendOnly", encoded)
					}
					commands := got.PhaseCommands()
					if !got.FrontendOnly || len(commands) != 1 || !commands[0].FrontendOnly {
						t.Fatalf("frontend-only round trip = %#v, commands = %#v", got, commands)
					}
				default:
					t.Fatalf("unexpected case %q", tc.want)
				}
			})
		}
	}
}

func TestSourceRunRejectsFrontendOnlyMultiCommand(t *testing.T) {
	t.Parallel()

	for _, c := range codecs() {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			var run providermanifestv1.SourceRun
			var encoded []byte
			if c.name == "json" {
				encoded = []byte(`[{"command":["npm","run","dev"],"frontendOnly":true},{"command":["go","run","."]}]`)
			} else {
				encoded = []byte("- command: [npm, run, dev]\n  frontendOnly: true\n- command: [go, run, .]\n")
			}
			if err := c.unmarshal(encoded, &run); err == nil || !strings.Contains(err.Error(), "frontendOnly") {
				t.Fatalf("Unmarshal error = %v, want frontendOnly validation error", err)
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
