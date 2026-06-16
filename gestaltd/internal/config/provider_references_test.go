package config

import (
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestIndexedDBIsReferenced(t *testing.T) {
	t.Parallel()

	t.Run("ui only", func(t *testing.T) {
		t.Parallel()
		path := mustWriteConfigFile(t, `
providers:
  ui:
    demo:
      path: /demo
      source:
        path: ./ui
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if IndexedDBIsReferenced(cfg) {
			t.Fatal("IndexedDBIsReferenced = true, want false")
		}
	})

	t.Run("app indexeddb binding", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Apps: map[string]*ProviderEntry{
				"demo": {IndexedDB: &IndexedDBBindingConfig{DB: "demo"}},
			},
		}
		if !IndexedDBIsReferenced(cfg) {
			t.Fatal("IndexedDBIsReferenced = false, want true")
		}
	})

	t.Run("workflow indexeddb binding", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Providers: ProvidersConfig{
				Workflow: map[string]*ProviderEntry{
					"local": {IndexedDB: &IndexedDBBindingConfig{Provider: "main-db"}},
				},
			},
		}
		if !IndexedDBIsReferenced(cfg) {
			t.Fatal("IndexedDBIsReferenced = false, want true")
		}
	})
}

func TestExternalCredentialsReferenced(t *testing.T) {
	t.Parallel()

	t.Run("ui only", func(t *testing.T) {
		t.Parallel()
		path := mustWriteConfigFile(t, `
providers:
  ui:
    demo:
      path: /demo
      source:
        path: ./ui
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if ExternalCredentialsReferenced(cfg) {
			t.Fatal("ExternalCredentialsReferenced = true, want false")
		}
	})

	t.Run("subject connection", func(t *testing.T) {
		t.Parallel()
		path := mustWriteConfigFile(t, `
connections:
  github:
    auth:
      type: oauth2
      authorizationUrl: https://github.com/login/oauth/authorize
      tokenUrl: https://github.com/login/oauth/access_token
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !ExternalCredentialsReferenced(cfg) {
			t.Fatal("ExternalCredentialsReferenced = false, want true")
		}
	})

	t.Run("resolved example app without credentials", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Apps: map[string]*ProviderEntry{
				"example": {
					ResolvedManifest: &providermanifestv1.Manifest{
						Spec: &providermanifestv1.Spec{
							Connections: map[string]*providermanifestv1.ManifestConnectionDef{
								"default": {
									Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
								},
							},
						},
					},
				},
			},
		}
		if ExternalCredentialsReferenced(cfg) {
			t.Fatal("ExternalCredentialsReferenced = true, want false for auth-none app")
		}
	})
}
