package bootstrap

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestNotConnectedMessageFuncUsesBaseURLAsConnectURL(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Server: config.ServerConfig{
			BaseURL: "https://gestalt.example.test/root/",
		},
		Plugins: map[string]*config.ProviderEntry{
			"slack": {DisplayName: "Slack"},
		},
	}

	messageFunc := notConnectedMessageFunc(cfg)
	if messageFunc == nil {
		t.Fatal("message func = nil")
	}
	got := messageFunc("slack", "default", "")
	want := "Slack is not connected. Go to https://gestalt.example.test/root to connect Slack first."
	if got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestNotConnectedMessageFuncSupportsTemplatePlaceholders(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Server: config.ServerConfig{
			BaseURL: "https://gestalt.example.test",
			ConnectionHelp: config.ConnectionHelpConfig{
				NotConnectedMessage: "{DISPLAY_NAME} requires setup at {CONNECT_URL} for {CONNECTION}.",
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"slack": {DisplayName: "Slack"},
		},
	}

	messageFunc := notConnectedMessageFunc(cfg)
	if messageFunc == nil {
		t.Fatal("message func = nil")
	}
	got := messageFunc("slack", "workspace", "")
	want := "Slack requires setup at https://gestalt.example.test for workspace."
	if got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}
