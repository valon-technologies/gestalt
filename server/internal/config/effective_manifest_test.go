package config

import (
	"testing"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestEffectiveManifestBackedInputsMergesResolvedManifestProviderOverrides(t *testing.T) {
	t.Parallel()

	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/test/plugins/google-docs",
		Version: "0.1.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Auth: &pluginmanifestv1.ProviderAuth{
				Type:             pluginmanifestv1.AuthTypeOAuth2,
				AuthorizationURL: "https://example.com/oauth/authorize",
				TokenURL:         "https://example.com/oauth/token",
				Scopes:           []string{"manifest.read"},
			},
			BaseURL: "https://manifest.example.com",
			Headers: map[string]string{
				"X-Manifest": "manifest",
				"X-Shared":   "manifest",
			},
			ManagedParameters: []pluginmanifestv1.ManagedParameter{
				{In: "header", Name: "Authorization", Value: "Bearer manifest"},
				{In: "path", Name: "tenant", Value: "manifest-tenant"},
			},
			OpenAPI:           "https://manifest.example.com/openapi.json",
			GraphQLURL:        "https://manifest.example.com/graphql",
			MCPURL:            "https://manifest.example.com/mcp",
			AllowedOperations: map[string]*pluginmanifestv1.ManifestOperationOverride{"manifest.read": {Alias: "manifest_read"}},
			OpenAPIConnection: "manifest-api",
			DefaultConnection: "manifest-default",
			Connections: map[string]*pluginmanifestv1.ManifestConnectionDef{
				"manifest-api": {
					Mode: "system",
					Auth: &pluginmanifestv1.ProviderAuth{
						Type:       pluginmanifestv1.AuthTypeOAuth2,
						Scopes:     []string{"manifest.api"},
						ClientAuth: "basic",
					},
				},
				"secondary": {
					Mode: "user",
					Auth: &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeManual},
				},
			},
			ResponseMapping: &pluginmanifestv1.ManifestResponseMapping{
				DataPath: "payload.items",
				Pagination: &pluginmanifestv1.ManifestPaginationMapping{
					HasMorePath: "page.more",
					CursorPath:  "page.cursor",
				},
			},
			Connection: map[string]pluginmanifestv1.ProviderConnectionParam{
				"tenant": {Required: false, Description: "tenant id", From: "query"},
				"region": {Required: true, Description: "region", From: "query"},
			},
		},
	}

	localAllowed := map[string]*OperationOverride{
		"docs.documents.get": {
			Alias:       "get_document",
			Description: "Fetch a document",
		},
	}

	gotManifest, gotAllowed, err := EffectiveManifestBackedInputs("google_docs", &PluginDef{
		Source:           "github.com/test/plugins/google-docs",
		Version:          "0.1.0",
		ResolvedManifest: manifest,
		Auth: &ConnectionAuthDef{
			ClientID:     "local-client",
			ClientSecret: "local-secret",
			Scopes:       []string{"local.read"},
			TokenParams:  map[string]string{"audience": "docs"},
		},
		BaseURL: "https://local.example.com",
		Headers: map[string]string{
			"X-Shared": "local",
			"X-Local":  "local",
		},
		ManagedParameters: []ManagedParameterDef{
			{In: "header", Name: "Authorization", Value: "Bearer local"},
			{In: "header", Name: "X-Trace", Value: "trace-id"},
		},
		OpenAPI:           "https://local.example.com/openapi.json",
		OpenAPIConnection: "plugin-api",
		DefaultConnection: "plugin-api",
		Connections: map[string]*ConnectionDef{
			"manifest-api": {
				Mode: "user",
				Auth: ConnectionAuthDef{ClientID: "named-client"},
			},
			"plugin-api": {
				Mode: "system",
				Auth: ConnectionAuthDef{Type: pluginmanifestv1.AuthTypeBearer},
			},
		},
		ResponseMapping: &ResponseMappingDef{
			DataPath: "results",
			Pagination: &PaginationMapping{
				HasMorePath: "pagination.has_more",
				CursorPath:  "pagination.next",
			},
		},
		ConnectionParams: map[string]ConnectionParamDef{
			"tenant":    {Required: true},
			"workspace": {Required: true},
		},
		AllowedOperations: localAllowed,
	})
	if err != nil {
		t.Fatalf("EffectiveManifestBackedInputs: %v", err)
	}

	if gotManifest == nil || gotManifest.Provider == nil {
		t.Fatal("expected manifest-backed provider")
	}
	gotProvider := gotManifest.Provider

	if gotProvider.BaseURL != "https://local.example.com" {
		t.Fatalf("BaseURL = %q", gotProvider.BaseURL)
	}
	if gotProvider.OpenAPI != "https://local.example.com/openapi.json" {
		t.Fatalf("OpenAPI = %q", gotProvider.OpenAPI)
	}
	if gotProvider.GraphQLURL != "https://manifest.example.com/graphql" {
		t.Fatalf("GraphQLURL = %q", gotProvider.GraphQLURL)
	}
	if gotProvider.OpenAPIConnection != "plugin-api" {
		t.Fatalf("OpenAPIConnection = %q", gotProvider.OpenAPIConnection)
	}
	if gotProvider.DefaultConnection != "plugin-api" {
		t.Fatalf("DefaultConnection = %q", gotProvider.DefaultConnection)
	}

	if gotProvider.Auth == nil {
		t.Fatal("expected merged auth")
	}
	if gotProvider.Auth.ClientID != "local-client" || gotProvider.Auth.ClientSecret != "local-secret" {
		t.Fatalf("merged auth client credentials = %#v", gotProvider.Auth)
	}
	if len(gotProvider.Auth.Scopes) != 1 || gotProvider.Auth.Scopes[0] != "local.read" {
		t.Fatalf("merged auth scopes = %#v", gotProvider.Auth.Scopes)
	}
	if gotProvider.Auth.AuthorizationURL != "https://example.com/oauth/authorize" {
		t.Fatalf("merged auth authorization_url = %q", gotProvider.Auth.AuthorizationURL)
	}

	if gotProvider.Headers["X-Manifest"] != "manifest" || gotProvider.Headers["X-Shared"] != "local" || gotProvider.Headers["X-Local"] != "local" {
		t.Fatalf("merged headers = %#v", gotProvider.Headers)
	}
	if len(gotProvider.ManagedParameters) != 3 {
		t.Fatalf("managed parameters = %#v", gotProvider.ManagedParameters)
	}
	if gotProvider.ManagedParameters[0].Value != "Bearer local" {
		t.Fatalf("first managed parameter = %#v", gotProvider.ManagedParameters[0])
	}

	if gotProvider.ResponseMapping == nil || gotProvider.ResponseMapping.DataPath != "results" {
		t.Fatalf("response mapping = %#v", gotProvider.ResponseMapping)
	}
	if gotProvider.ResponseMapping.Pagination == nil || gotProvider.ResponseMapping.Pagination.CursorPath != "pagination.next" {
		t.Fatalf("response mapping pagination = %#v", gotProvider.ResponseMapping)
	}

	if gotProvider.Connection["tenant"].Required != true {
		t.Fatalf("tenant connection param = %#v", gotProvider.Connection["tenant"])
	}
	if gotProvider.Connection["tenant"].Description != "tenant id" || gotProvider.Connection["tenant"].From != "query" {
		t.Fatalf("tenant connection param metadata lost: %#v", gotProvider.Connection["tenant"])
	}
	if gotProvider.Connection["workspace"].Required != true {
		t.Fatalf("workspace connection param = %#v", gotProvider.Connection["workspace"])
	}

	if gotProvider.Connections["manifest-api"] == nil || gotProvider.Connections["manifest-api"].Mode != "user" {
		t.Fatalf("manifest-api connection = %#v", gotProvider.Connections["manifest-api"])
	}
	if gotProvider.Connections["manifest-api"].Auth == nil || gotProvider.Connections["manifest-api"].Auth.ClientID != "named-client" {
		t.Fatalf("manifest-api auth = %#v", gotProvider.Connections["manifest-api"])
	}
	if gotProvider.Connections["manifest-api"].Auth.ClientAuth != "basic" {
		t.Fatalf("manifest-api auth client_auth = %#v", gotProvider.Connections["manifest-api"].Auth)
	}
	if gotProvider.Connections["plugin-api"] == nil || gotProvider.Connections["plugin-api"].Auth == nil || gotProvider.Connections["plugin-api"].Auth.Type != pluginmanifestv1.AuthTypeBearer {
		t.Fatalf("plugin-api connection = %#v", gotProvider.Connections["plugin-api"])
	}

	if gotAllowed == nil {
		t.Fatal("expected allowed operations")
	}
	if len(gotAllowed) != 1 {
		t.Fatalf("len(gotAllowed) = %d, want 1", len(gotAllowed))
	}
	if got, ok := gotAllowed["docs.documents.get"]; !ok || got == nil || got.Alias != "get_document" {
		t.Fatalf("gotAllowed[docs.documents.get] = %#v", got)
	}
	if _, ok := gotAllowed["manifest.read"]; ok {
		t.Fatal("expected local allowed_operations to replace manifest allowed_operations")
	}
}

func TestEffectiveManifestBackedInputsFallsBackToManifestAllowedOperations(t *testing.T) {
	t.Parallel()

	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/test/plugins/google-drive",
		Version: "0.1.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			OpenAPI: "https://example.com/openapi.yaml",
			AllowedOperations: map[string]*pluginmanifestv1.ManifestOperationOverride{
				"drive.files.list": {
					Alias: "files.list",
				},
			},
		},
	}

	_, gotAllowed, err := EffectiveManifestBackedInputs("google_drive", &PluginDef{
		ResolvedManifest: manifest,
	})
	if err != nil {
		t.Fatalf("EffectiveManifestBackedInputs: %v", err)
	}

	if gotAllowed == nil {
		t.Fatal("expected allowed operations from manifest")
	}
	got, ok := gotAllowed["drive.files.list"]
	if !ok || got == nil {
		t.Fatalf("gotAllowed[drive.files.list] missing: %#v", gotAllowed)
	}
	if got.Alias != "files.list" {
		t.Fatalf("got alias = %q, want files.list", got.Alias)
	}
}
