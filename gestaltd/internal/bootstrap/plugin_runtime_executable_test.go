package bootstrap

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"gopkg.in/yaml.v3"
)

func TestRuntimeProviderConfigMapUsesRawConfig(t *testing.T) {
	t.Parallel()

	entry := &config.RuntimeProviderEntry{
		ProviderEntry: config.ProviderEntry{
			Env: map[string]string{
				"MODAL_TOKEN_ID":     "${MODAL_TOKEN_ID}",
				"MODAL_TOKEN_SECRET": "${MODAL_TOKEN_SECRET}",
			},
			Config: mustRuntimeNode(t, map[string]any{
				"app":         "gestalt-runtime",
				"environment": "main",
			}),
		},
	}

	got, err := runtimeProviderConfigMap("modal", entry)
	if err != nil {
		t.Fatalf("runtimeProviderConfigMap: %v", err)
	}

	if got["app"] != "gestalt-runtime" {
		t.Fatalf("config app = %#v, want gestalt-runtime", got["app"])
	}
	if got["environment"] != "main" {
		t.Fatalf("config environment = %#v, want main", got["environment"])
	}
	for _, key := range []string{"env", "config", "source", "name", "MODAL_TOKEN_ID", "MODAL_TOKEN_SECRET"} {
		if _, exists := got[key]; exists {
			t.Fatalf("config unexpectedly contains %q: %#v", key, got)
		}
	}
}

func TestRuntimeProviderConfigMapUnwrapsComponentRuntimeConfig(t *testing.T) {
	t.Parallel()

	rawConfig := mustRuntimeNode(t, map[string]any{
		"app":         "gestalt-runtime",
		"environment": "main",
	})
	entry := &config.RuntimeProviderEntry{
		ProviderEntry: config.ProviderEntry{
			Source: config.ProviderSource{Path: "./runtime/modal/provider-release.yaml"},
			Env: map[string]string{
				"MODAL_TOKEN_ID":     "${MODAL_TOKEN_ID}",
				"MODAL_TOKEN_SECRET": "${MODAL_TOKEN_SECRET}",
			},
			Config: rawConfig,
		},
	}
	wrapped, err := config.BuildComponentRuntimeConfigNode("modal", "runtime", &entry.ProviderEntry, rawConfig)
	if err != nil {
		t.Fatalf("BuildComponentRuntimeConfigNode: %v", err)
	}
	entry.Config = wrapped

	got, err := runtimeProviderConfigMap("modal", entry)
	if err != nil {
		t.Fatalf("runtimeProviderConfigMap: %v", err)
	}

	if got["app"] != "gestalt-runtime" {
		t.Fatalf("config app = %#v, want gestalt-runtime", got["app"])
	}
	if got["environment"] != "main" {
		t.Fatalf("config environment = %#v, want main", got["environment"])
	}
	for _, key := range []string{"env", "config", "source", "name", "MODAL_TOKEN_ID", "MODAL_TOKEN_SECRET"} {
		if _, exists := got[key]; exists {
			t.Fatalf("config unexpectedly contains %q: %#v", key, got)
		}
	}
}

func mustRuntimeNode(t *testing.T, value any) yaml.Node {
	t.Helper()

	var node yaml.Node
	if err := node.Encode(value); err != nil {
		t.Fatalf("node.Encode: %v", err)
	}
	return node
}
