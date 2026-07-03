package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
)

func TestWriteInvocationErrorProviderActivatingReturns503(t *testing.T) {
	t.Parallel()

	s := &Server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)

	err := fmt.Errorf("%w: %q", core.ErrProviderActivating, "github")
	s.writeInvocationError(rec, req, "github", "list_issues", err)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["code"] != "provider_activating" {
		t.Fatalf("error code = %v, want provider_activating (body=%s)", body["code"], rec.Body.String())
	}
}
