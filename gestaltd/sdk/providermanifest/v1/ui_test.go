package providermanifestv1

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPromptExamples(t *testing.T) {
	t.Parallel()

	if got := PromptExamples(nil); got != nil {
		t.Fatalf("nil manifest = %v, want nil", got)
	}

	manifest := &Manifest{
		Spec: &Spec{
			UI: &ManifestUI{
				PromptExamples: []string{
					"  Draft a reply  ",
					"",
					"Summarize my inbox",
				},
			},
		},
	}
	got := PromptExamples(manifest)
	if len(got) != 2 || got[0] != "Draft a reply" || got[1] != "Summarize my inbox" {
		t.Fatalf("PromptExamples() = %v", got)
	}
}

func TestManifestUISpecRoundTripYAML(t *testing.T) {
	t.Parallel()

	raw := `
kind: app
source: github.com/valon-technologies/gestalt-providers/app/gmail
version: 0.0.1-alpha.1
spec:
  ui:
    promptExamples:
      - Draft a short reply to my latest unread email
`
	var manifest Manifest
	if err := yaml.Unmarshal([]byte(raw), &manifest); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if manifest.Spec == nil || manifest.Spec.UI == nil {
		t.Fatal("expected spec.ui")
	}
	if len(manifest.Spec.UI.PromptExamples) != 1 {
		t.Fatalf("promptExamples = %v", manifest.Spec.UI.PromptExamples)
	}
}
