package providerhost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
