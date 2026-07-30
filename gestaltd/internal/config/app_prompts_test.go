package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRootAppPromptExamples(t *testing.T) {
	t.Parallel()

	var node yaml.Node
	if err := yaml.Unmarshal([]byte(`
prompts:
  gmail:
    - id: draft-reply
      text: "  Draft a short reply to my latest unread email  "
  google_drive:
    - id: summarize-deck
      text: Find the deck I edited yesterday and summarize the changes
`), &node); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	got, err := RootAppPromptExamples(map[string]*ProviderEntry{
		"home": {
			Static: &AppStaticConfig{Mount: "/"},
			Config: node,
		},
	})
	if err != nil {
		t.Fatalf("RootAppPromptExamples: %v", err)
	}
	if got["gmail"][0] != "Draft a short reply to my latest unread email" {
		t.Fatalf("gmail prompt = %q", got["gmail"][0])
	}
	if got["google_drive"][0] != "Find the deck I edited yesterday and summarize the changes" {
		t.Fatalf("google_drive prompt = %q", got["google_drive"][0])
	}
}

func TestRootAppPromptExamplesValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "missing id",
			yaml: "prompts:\n  gmail:\n    - text: Draft a reply\n",
			want: "id must not be empty",
		},
		{
			name: "duplicate id",
			yaml: "prompts:\n  gmail:\n    - id: same\n      text: First\n    - id: same\n      text: Second\n",
			want: "duplicate prompt id",
		},
		{
			name: "too many prompts",
			yaml: "prompts:\n  gmail:\n    - {id: one, text: One}\n    - {id: two, text: Two}\n    - {id: three, text: Three}\n    - {id: four, text: Four}\n    - {id: five, text: Five}\n    - {id: six, text: Six}\n",
			want: "maximum is 5",
		},
		{
			name: "too long",
			yaml: "prompts:\n  gmail:\n    - id: long\n      text: " + strings.Repeat("x", 281) + "\n",
			want: "exceeds 280 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var node yaml.Node
			if err := yaml.Unmarshal([]byte(tt.yaml), &node); err != nil {
				t.Fatalf("yaml.Unmarshal: %v", err)
			}
			_, err := RootAppPromptExamples(map[string]*ProviderEntry{
				"home": {Static: &AppStaticConfig{Mount: "/"}, Config: node},
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestRootAppPromptExamplesIgnoresNonRootApps(t *testing.T) {
	t.Parallel()

	var node yaml.Node
	if err := yaml.Unmarshal([]byte(`prompts: {gmail: [{id: reply, text: Reply}]}`), &node); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	got, err := RootAppPromptExamples(map[string]*ProviderEntry{
		"gmail": {Static: &AppStaticConfig{Mount: "/gmail"}, Config: node},
	})
	if err != nil {
		t.Fatalf("RootAppPromptExamples: %v", err)
	}
	if got != nil {
		t.Fatalf("got prompts for non-root app: %v", got)
	}
}
