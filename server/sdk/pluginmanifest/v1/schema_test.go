package pluginmanifestv1

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// configOnlyFields lists Go struct fields (by JSON tag name) that are
// intentionally absent from manifest.jsonschema.json. These fields are
// populated from server config (config.yaml) at runtime via
// InlineToManifest / connectionAuthToManifest, not from plugin manifests.
//
// When adding a new field to a manifest Go type, either:
//   - Add it to manifest.jsonschema.json if plugin authors set it in plugin.yaml
//   - Add it here if it is set from server config at runtime
var configOnlyFields = map[reflect.Type]map[string]bool{
	reflect.TypeOf(Provider{}): {
		"openapi":            true, // PluginDef.OpenAPI in config.yaml
		"graphql_url":        true, // PluginDef.GraphQLURL
		"mcp_url":            true, // PluginDef.MCPURL
		"allowed_operations": true, // PluginDef.AllowedOperations
		"openapi_connection": true, // PluginDef.OpenAPIConnection
		"graphql_connection": true, // PluginDef.GraphQLConnection
		"mcp_connection":     true, // PluginDef.MCPConnection
		"connections":        true, // PluginDef.Connections
	},
	reflect.TypeOf(ProviderAuth{}): {
		"client_id":      true, // ConnectionAuthDef.ClientID
		"client_secret":  true, // ConnectionAuthDef.ClientSecret
		"token_params":   true, // ConnectionAuthDef.TokenParams
		"refresh_params": true, // ConnectionAuthDef.RefreshParams
		"accept_header":  true, // ConnectionAuthDef.AcceptHeader
		"token_metadata": true, // ConnectionAuthDef.TokenMetadata
	},
}

// TestSchemaCoversGoTypes verifies that the JSON schema and Go types stay in
// sync. For every type pair it checks both directions:
//
//   - schema → Go: every schema property has a Go struct field (catches removed
//     or renamed Go fields)
//   - Go → schema: every Go field is either in the schema or in the
//     configOnlyFields allowlist (catches new manifest fields missing from the
//     schema)
func TestSchemaCoversGoTypes(t *testing.T) {
	t.Parallel()

	var schema map[string]any
	if err := json.Unmarshal(ManifestJSONSchema, &schema); err != nil {
		t.Fatalf("parse schema: %v", err)
	}

	cases := []struct {
		name       string
		schemaPath []string
		goType     reflect.Type
	}{
		{"Manifest", nil, reflect.TypeOf(Manifest{})},
		{"Provider", p("properties", "provider"), reflect.TypeOf(Provider{})},
		{"WebUIMetadata", p("properties", "webui"), reflect.TypeOf(WebUIMetadata{})},
		{"Entrypoints", p("properties", "entrypoints"), reflect.TypeOf(Entrypoints{})},
		{"ProviderPostConnectDiscovery", p("properties", "provider", "properties", "post_connect_discovery"), reflect.TypeOf(ProviderPostConnectDiscovery{})},
		{"ProviderConnectionParam", p("properties", "provider", "properties", "connection", "additionalProperties"), reflect.TypeOf(ProviderConnectionParam{})},
		{"ProviderAuth", p("$defs", "provider_auth"), reflect.TypeOf(ProviderAuth{})},
		{"ProviderOperation", p("$defs", "provider_operation"), reflect.TypeOf(ProviderOperation{})},
		{"ProviderParameter", p("$defs", "provider_parameter"), reflect.TypeOf(ProviderParameter{})},
		{"Artifact", p("$defs", "artifact"), reflect.TypeOf(Artifact{})},
		{"Entrypoint", p("$defs", "entrypoint"), reflect.TypeOf(Entrypoint{})},
		{"CredentialField", p("$defs", "provider_auth", "properties", "credentials", "items"), reflect.TypeOf(CredentialField{})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			node := navigateSchema(schema, tc.schemaPath)
			if node == nil {
				t.Fatalf("schema path %v resolved to nil", tc.schemaPath)
			}

			schemaProps := schemaPropertyNames(node)
			if len(schemaProps) == 0 {
				t.Fatalf("schema node at %v has no properties", tc.schemaPath)
			}

			goFields := jsonFieldNames(tc.goType)
			allowed := configOnlyFields[tc.goType]

			for prop := range schemaProps {
				if !goFields[prop] {
					t.Errorf("schema property %q has no Go struct field", prop)
				}
			}

			for field := range goFields {
				if !schemaProps[field] && !allowed[field] {
					t.Errorf("Go field %q not in schema or configOnlyFields — "+
						"add to manifest.jsonschema.json (manifest field) or configOnlyFields in this test (config-only field)",
						field)
				}
			}
		})
	}
}

func p(keys ...string) []string { return keys }

func navigateSchema(node map[string]any, path []string) map[string]any {
	current := node
	for _, key := range path {
		val, ok := current[key]
		if !ok {
			return nil
		}
		obj, ok := val.(map[string]any)
		if !ok {
			return nil
		}
		current = obj
	}
	return current
}

func schemaPropertyNames(node map[string]any) map[string]bool {
	props, ok := node["properties"].(map[string]any)
	if !ok {
		return nil
	}
	names := make(map[string]bool, len(props))
	for k := range props {
		names[k] = true
	}
	return names
}

func jsonFieldNames(typ reflect.Type) map[string]bool {
	names := make(map[string]bool, typ.NumField())
	for i := range typ.NumField() {
		tag := typ.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		names[name] = true
	}
	return names
}
