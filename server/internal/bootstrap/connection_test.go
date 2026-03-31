package bootstrap

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestPluginConnectionDef_NilPlugin(t *testing.T) {
	conn := pluginConnectionDef(nil, "")
	if conn.Mode != "" {
		t.Errorf("Mode = %q, want empty", conn.Mode)
	}
	if conn.Auth.Type != "" {
		t.Errorf("Auth.Type = %q, want empty", conn.Auth.Type)
	}
}

func TestPluginConnectionDef_EmptyConnNameUsesTopLevelAuth(t *testing.T) {
	plugin := &config.PluginDef{
		Auth: &config.ConnectionAuthDef{
			Type: "oauth2",
		},
	}
	conn := pluginConnectionDef(plugin, "")
	if conn.Auth.Type != "oauth2" {
		t.Errorf("Auth.Type = %q, want %q", conn.Auth.Type, "oauth2")
	}
}

func TestPluginConnectionDef_NamedConnectionWithAuthMapping(t *testing.T) {
	plugin := &config.PluginDef{
		Connections: map[string]*config.ConnectionDef{
			"api": {
				Mode: "user",
				Auth: config.ConnectionAuthDef{
					Type: "manual",
					Credentials: []config.CredentialFieldDef{
						{Name: "api_key", Label: "API Key"},
						{Name: "app_key", Label: "Application Key"},
					},
					AuthMapping: &config.AuthMappingDef{
						Headers: map[string]string{
							"DD-API-KEY":         "api_key",
							"DD-APPLICATION-KEY": "app_key",
						},
					},
				},
			},
		},
	}
	conn := pluginConnectionDef(plugin, "api")
	if conn.Mode != "user" {
		t.Errorf("Mode = %q, want %q", conn.Mode, "user")
	}
	if conn.Auth.AuthMapping == nil {
		t.Fatal("expected AuthMapping to be set")
	}
	if got := conn.Auth.AuthMapping.Headers["DD-API-KEY"]; got != "api_key" {
		t.Errorf("DD-API-KEY mapping = %q, want %q", got, "api_key")
	}
	if got := conn.Auth.AuthMapping.Headers["DD-APPLICATION-KEY"]; got != "app_key" {
		t.Errorf("DD-APPLICATION-KEY mapping = %q, want %q", got, "app_key")
	}
}

func TestPluginConnectionDef_ConnNameNotFoundFallsBack(t *testing.T) {
	plugin := &config.PluginDef{
		Auth: &config.ConnectionAuthDef{
			Type: "oauth2",
		},
		Connections: map[string]*config.ConnectionDef{
			"other": {
				Mode: "user",
				Auth: config.ConnectionAuthDef{Type: "manual"},
			},
		},
	}
	conn := pluginConnectionDef(plugin, "missing")
	if conn.Auth.Type != "oauth2" {
		t.Errorf("Auth.Type = %q, want %q (fallback to top-level)", conn.Auth.Type, "oauth2")
	}
}

func TestPluginConnectionDef_ConnNameNilEntryFallsBack(t *testing.T) {
	plugin := &config.PluginDef{
		Auth: &config.ConnectionAuthDef{
			Type: "oauth2",
		},
		Connections: map[string]*config.ConnectionDef{
			"api": nil,
		},
	}
	conn := pluginConnectionDef(plugin, "api")
	if conn.Auth.Type != "oauth2" {
		t.Errorf("Auth.Type = %q, want %q (fallback to top-level)", conn.Auth.Type, "oauth2")
	}
}
