package adminui_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/services/ui/adminui"
)

func TestEmbeddedHandlerServesAppRegistryRoutes(t *testing.T) {
	t.Parallel()

	handler := adminui.EmbeddedHandler(adminui.Options{BrandHref: "/workplace"})
	if handler == nil {
		t.Fatal("EmbeddedHandler returned nil")
	}
	req := httptest.NewRequest(http.MethodGet, "/registry/g-issues", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	resp := rec.Result()
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	html := string(body)
	for _, want := range []string{
		`href="/workplace"`,
		`href="/admin/registry"`,
		`/registry-apps`,
		`/app-rollouts/`,
		`credentials: "include"`,
		`App Registry`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("admin UI does not contain %q", want)
		}
	}
	for _, unwanted := range []string{
		`version-dialog`,
		`chooseVersion`,
		`method: "POST"`,
		`>Install<`,
		`>Upgrade<`,
	} {
		if strings.Contains(html, unwanted) {
			t.Fatalf("admin UI unexpectedly contains action %q", unwanted)
		}
	}
}
