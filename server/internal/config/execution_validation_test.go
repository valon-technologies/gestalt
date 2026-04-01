package config

import (
	"strings"
	"testing"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestValidateExecution_PluginFieldRules(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cfg     *Config
		wantErr string
	}{
		{
			name: "rejects plugin connection",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"bad": {Plugin: &PluginDef{Command: "echo", Connection: "default"}},
				},
			},
			wantErr: "plugin.connection is not supported",
		},
		{
			name: "rejects response mapping without api surface",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"bad": {Plugin: &PluginDef{
						BaseURL: "https://api.example.test",
						Operations: []InlineOperationDef{
							{Name: "list", Method: "GET", Path: "/items"},
						},
						ResponseMapping: &ResponseMappingDef{DataPath: "items"},
					}},
				},
			},
			wantErr: "plugin.response_mapping is only valid for inline openapi/graphql integrations",
		},
		{
			name: "rejects base url without operations or api surface",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"bad": {Plugin: &PluginDef{
						Command: "echo",
						BaseURL: "https://api.example.test",
					}},
				},
			},
			wantErr: "plugin.base_url is only valid with inline operations or openapi/graphql surfaces",
		},
		{
			name: "rejects managed parameters without api surface",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"bad": {Plugin: &PluginDef{
						MCPURL: "https://mcp.example.test",
						ManagedParameters: []ManagedParameterDef{
							{In: ManagedParameterInHeader, Name: "x-version", Value: "1"},
						},
					}},
				},
			},
			wantErr: "plugin.managed_parameters are only valid with openapi/graphql surfaces",
		},
		{
			name: "rejects surface connection without matching surface",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"bad": {Plugin: &PluginDef{
						Command:           "echo",
						OpenAPIConnection: PluginConnectionAlias,
					}},
				},
			},
			wantErr: "plugin.openapi_connection is only valid when openapi is configured",
		},
		{
			name: "allows inline openapi response mapping",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"ok": {Plugin: &PluginDef{
						OpenAPI:         "https://example.com/openapi.json",
						ResponseMapping: &ResponseMappingDef{DataPath: "items"},
					}},
				},
			},
		},
		{
			name: "allows manifest provided openapi connection",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"ok": {Plugin: &PluginDef{
						Command:           "echo",
						OpenAPIConnection: PluginConnectionAlias,
						ResolvedManifest: &pluginmanifestv1.Manifest{
							Provider: &pluginmanifestv1.Provider{
								OpenAPI: "https://example.com/openapi.json",
							},
						},
					}},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateExecution(tc.cfg)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateExecution() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateExecution() error = nil, want substring %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateExecution() error = %q, want substring %q", err, tc.wantErr)
			}
		})
	}
}
