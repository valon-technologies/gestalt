package plugins

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func TestDeclarativeProvider_HTTPRequestMaterialization(t *testing.T) {
	t.Parallel()

	t.Run("post uses explicit JSON content type", func(t *testing.T) {
		t.Parallel()

		var gotContentType string
		var gotHeader string
		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotContentType = r.Header.Get("Content-Type")
			gotHeader = r.Header.Get("X-Test")
			_ = json.NewDecoder(r.Body).Decode(&gotBody)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}))
		testutil.CloseOnCleanup(t, srv)

		manifest := &providermanifestv1.Manifest{
			Kind:    providermanifestv1.KindApp,
			Source:  "github.com/test/declarative",
			Version: "0.0.1-alpha.1",
			Spec: &providermanifestv1.Spec{
				Headers: map[string]string{
					"Content-Type": "text/plain",
					"X-Test":       "kept",
				},
				Surfaces: &providermanifestv1.ProviderSurfaces{
					REST: &providermanifestv1.RESTSurface{
						BaseURL: srv.URL,
						Operations: []providermanifestv1.ProviderOperation{{
							Name:   "chat.postMessage",
							Method: http.MethodPost,
							Path:   "/api/chat.postMessage",
							Tags:   []string{"chat", "message"},
							Parameters: []providermanifestv1.ProviderParameter{
								{Name: "channel", Type: "string", In: "body", Required: true},
								{Name: "text", Type: "string", In: "body", Required: true},
							},
						}},
					},
				},
			},
		}

		prov, err := NewDeclarativeProvider(manifest, srv.Client())
		if err != nil {
			t.Fatalf("NewDeclarativeProvider: %v", err)
		}

		_, err = prov.Execute(context.Background(), "chat.postMessage", map[string]any{
			"channel": "D024BGTKK33",
			"text":    "hello",
		}, "")
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}

		if gotContentType != declarativeJSONContentType {
			t.Fatalf("Content-Type = %q, want %q", gotContentType, declarativeJSONContentType)
		}
		if gotHeader != "kept" {
			t.Fatalf("X-Test = %q, want kept", gotHeader)
		}
		if gotBody["channel"] != "D024BGTKK33" {
			t.Fatalf("body[channel] = %v, want D024BGTKK33", gotBody["channel"])
		}
		if gotBody["text"] != "hello" {
			t.Fatalf("body[text] = %v, want hello", gotBody["text"])
		}
		if got := prov.Catalog().Operations[0].Tags; len(got) != 2 || got[0] != "chat" || got[1] != "message" {
			t.Fatalf("catalog operation tags = %#v, want chat/message", got)
		}
	})

	t.Run("named connection auth mapping", func(t *testing.T) {
		t.Parallel()

		newProvider := func(t *testing.T, opts ...DeclarativeProviderOption) (*DeclarativeProvider, func() (apiKey, defaultKey, authorization string)) {
			t.Helper()

			var gotAPIKey string
			var gotDefaultKey string
			var gotAuthorization string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAPIKey = r.Header.Get("X-API-Key")
				gotDefaultKey = r.Header.Get("X-Default-Key")
				gotAuthorization = r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			}))
			testutil.CloseOnCleanup(t, srv)

			manifest := &providermanifestv1.Manifest{
				Kind:    providermanifestv1.KindApp,
				Source:  "github.com/test/declarative",
				Version: "0.0.1-alpha.1",
				Spec: &providermanifestv1.Spec{
					Connections: map[string]*providermanifestv1.ManifestConnectionDef{
						"default": {
							Mode: providermanifestv1.ConnectionModeSubject,
							Auth: &providermanifestv1.ProviderAuth{
								Type: providermanifestv1.AuthTypeManual,
								Credentials: []providermanifestv1.CredentialField{{
									Name:  "api_key",
									Label: "API Key",
								}},
								AuthMapping: &providermanifestv1.AuthMapping{
									Headers: map[string]providermanifestv1.AuthValue{
										"X-Default-Key": {
											ValueFrom: &providermanifestv1.AuthValueFrom{
												CredentialFieldRef: &providermanifestv1.CredentialFieldRef{Name: "api_key"},
											},
										},
									},
								},
							},
						},
						"plain": {
							Mode: providermanifestv1.ConnectionModeSubject,
							Auth: &providermanifestv1.ProviderAuth{
								Type: providermanifestv1.AuthTypeBearer,
							},
						},
						"prod": {
							Mode: providermanifestv1.ConnectionModeSubject,
							Auth: &providermanifestv1.ProviderAuth{
								Type: providermanifestv1.AuthTypeManual,
								Credentials: []providermanifestv1.CredentialField{{
									Name:  "api_key",
									Label: "API Key",
								}},
								AuthMapping: &providermanifestv1.AuthMapping{
									Headers: map[string]providermanifestv1.AuthValue{
										"X-API-Key": {
											ValueFrom: &providermanifestv1.AuthValueFrom{
												CredentialFieldRef: &providermanifestv1.CredentialFieldRef{Name: "api_key"},
											},
										},
									},
								},
							},
						},
					},
					Surfaces: &providermanifestv1.ProviderSurfaces{
						REST: &providermanifestv1.RESTSurface{
							BaseURL: srv.URL,
							Operations: []providermanifestv1.ProviderOperation{{
								Name:   "schema.versions",
								Method: http.MethodGet,
								Path:   "/v1/schema/versions",
							}},
						},
					},
				},
			}
			prov, err := NewDeclarativeProvider(manifest, srv.Client(), opts...)
			if err != nil {
				t.Fatalf("NewDeclarativeProvider: %v", err)
			}
			return prov, func() (apiKey, defaultKey, authorization string) {
				return gotAPIKey, gotDefaultKey, gotAuthorization
			}
		}
		token := `{"api_key":"prod-key"}`

		t.Run("default connection fallback", func(t *testing.T) {
			t.Parallel()

			prov, requestHeaders := newProvider(t)

			if _, err := prov.Execute(context.Background(), "schema.versions", nil, `{"api_key":"default-key"}`); err != nil {
				t.Fatalf("Execute: %v", err)
			}

			gotAPIKey, gotDefaultKey, gotAuthorization := requestHeaders()
			if gotDefaultKey != "default-key" {
				t.Fatalf("X-Default-Key = %q, want default-key", gotDefaultKey)
			}
			if gotAPIKey != "" {
				t.Fatalf("X-API-Key = %q, want empty", gotAPIKey)
			}
			if gotAuthorization != "" {
				t.Fatalf("Authorization = %q, want empty", gotAuthorization)
			}
		})

		t.Run("credential context", func(t *testing.T) {
			t.Parallel()

			prov, requestHeaders := newProvider(t)
			ctx := invocation.WithCredentialContext(context.Background(), invocation.CredentialContext{
				Mode:       core.ConnectionModeSubject,
				SubjectID:  "service_account:data-schema-explorer",
				Connection: "prod",
				Instance:   "default",
			})

			if _, err := prov.Execute(ctx, "schema.versions", nil, token); err != nil {
				t.Fatalf("Execute: %v", err)
			}

			gotAPIKey, gotDefaultKey, _ := requestHeaders()
			if gotAPIKey != "prod-key" {
				t.Fatalf("X-API-Key = %q, want prod-key", gotAPIKey)
			}
			if gotDefaultKey != "" {
				t.Fatalf("X-Default-Key = %q, want empty", gotDefaultKey)
			}
		})

		t.Run("operation connection fallback", func(t *testing.T) {
			t.Parallel()

			prov, requestHeaders := newProvider(t,
				WithDeclarativeOperationConnections(map[string]string{"schema.versions": "prod"}, nil, nil),
			)

			if _, err := prov.Execute(context.Background(), "schema.versions", nil, token); err != nil {
				t.Fatalf("Execute: %v", err)
			}

			gotAPIKey, gotDefaultKey, _ := requestHeaders()
			if gotAPIKey != "prod-key" {
				t.Fatalf("X-API-Key = %q, want prod-key", gotAPIKey)
			}
			if gotDefaultKey != "" {
				t.Fatalf("X-Default-Key = %q, want empty", gotDefaultKey)
			}
		})

		t.Run("unmapped connection uses generic credential materialization", func(t *testing.T) {
			t.Parallel()

			prov, requestHeaders := newProvider(t)
			ctx := invocation.WithCredentialContext(context.Background(), invocation.CredentialContext{
				Mode:       core.ConnectionModeSubject,
				SubjectID:  "service_account:data-schema-explorer",
				Connection: "plain",
				Instance:   "default",
			})

			if _, err := prov.Execute(ctx, "schema.versions", nil, "plain-token"); err != nil {
				t.Fatalf("Execute: %v", err)
			}

			gotAPIKey, gotDefaultKey, gotAuthorization := requestHeaders()
			if gotAuthorization != "Bearer plain-token" {
				t.Fatalf("Authorization = %q, want Bearer plain-token", gotAuthorization)
			}
			if gotDefaultKey != "" {
				t.Fatalf("X-Default-Key = %q, want empty", gotDefaultKey)
			}
			if gotAPIKey != "" {
				t.Fatalf("X-API-Key = %q, want empty", gotAPIKey)
			}
		})
	})
}
