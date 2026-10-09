package config

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAppPublicConfigYAML(t *testing.T) {
	t.Parallel()
	var entry ProviderEntry
	if err := yaml.Unmarshal([]byte(`static:
  publicConfig:
    supportMessage: Contact your workspace administrator.
    additionalDocs: [servicemac-access]
    nested:
      enabled: true
`), &entry); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(entry.Static.PublicConfig)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"additionalDocs":["servicemac-access"],"nested":{"enabled":true},"supportMessage":"Contact your workspace administrator."}` {
		t.Fatalf("public config: %s", body)
	}
}

func TestAppPublicConfigRejectsNonJSONValues(t *testing.T) {
	t.Parallel()
	cfg := &Config{Apps: map[string]*ProviderEntry{
		"home": {Static: &AppStaticConfig{PublicConfig: map[string]any{
			"nested": map[any]any{1: "not a JSON object key"},
		}}},
	}}
	if err := normalizeAppStaticMounts(cfg); err == nil {
		t.Fatal("expected invalid public config to fail validation")
	}
}
