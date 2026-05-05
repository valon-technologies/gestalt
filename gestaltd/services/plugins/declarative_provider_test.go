package plugins

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestDeclarativeProvider_POSTUsesExplicitJSONContentType(t *testing.T) {
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
		Kind:    providermanifestv1.KindPlugin,
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
}

func TestDeclarativeProvider_PostConnectMapsManifestConnectionMetadata(t *testing.T) {
	t.Parallel()

	var gotMethod string
	var gotAuthorization string
	var gotAccept string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuthorization = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"team_id":"T123","user_id":"U456"}`))
	}))
	testutil.CloseOnCleanup(t, srv)

	manifest := &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindPlugin,
		Source:  "github.com/test/slack",
		Version: "0.0.1-alpha.1",
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				REST: &providermanifestv1.RESTSurface{
					BaseURL: "https://slack.com/api",
					Operations: []providermanifestv1.ProviderOperation{{
						Name:   "conversations.list",
						Method: http.MethodGet,
						Path:   "/conversations.list",
					}},
				},
			},
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"default": {
					PostConnect: &providermanifestv1.ProviderPostConnect{
						Request: providermanifestv1.ProviderPostConnectRequest{
							Method: http.MethodPost,
							URL:    srv.URL,
							Headers: map[string]string{
								"Accept": "application/json",
							},
						},
						Success: &providermanifestv1.ProviderPostConnectSuccess{
							Path:   "ok",
							Equals: true,
						},
						ExternalIdentity: &providermanifestv1.ProviderPostConnectExternalIdentity{
							Type: "slack_identity",
							ID:   "team:{team_id}:user:{user_id}",
						},
						Metadata: map[string]string{
							"slack.team_id": "team_id",
							"slack.user_id": "user_id",
						},
					},
				},
			},
		},
	}

	prov, err := NewDeclarativeProvider(manifest, srv.Client())
	if err != nil {
		t.Fatalf("NewDeclarativeProvider: %v", err)
	}
	metadata, supported, err := core.PostConnect(context.Background(), prov, &core.ExternalCredential{
		Connection:  "default",
		AccessToken: "slack-token",
	})
	if err != nil {
		t.Fatalf("PostConnect: %v", err)
	}
	if !supported {
		t.Fatal("expected post-connect support")
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotAuthorization != "Bearer slack-token" {
		t.Fatalf("Authorization = %q, want bearer token", gotAuthorization)
	}
	if gotAccept != "application/json" {
		t.Fatalf("Accept = %q, want application/json", gotAccept)
	}
	if len(gotBody) != 0 {
		t.Fatalf("POST body = %q, want empty", string(gotBody))
	}
	want := map[string]string{
		"gestalt.external_identity.type": "slack_identity",
		"gestalt.external_identity.id":   "team:T123:user:U456",
		"slack.team_id":                  "T123",
		"slack.user_id":                  "U456",
	}
	if !reflect.DeepEqual(metadata, want) {
		t.Fatalf("metadata = %#v, want %#v", metadata, want)
	}
}
