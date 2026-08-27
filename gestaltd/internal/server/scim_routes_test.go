package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/server"
)

func TestSCIMRoutesAreMountedOnPublicListenersOnly(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		profile server.RouteProfile
		want    int
	}{
		{name: "combined", profile: server.RouteProfileAll, want: http.StatusNoContent},
		{name: "public", profile: server.RouteProfilePublic, want: http.StatusNoContent},
		{name: "management", profile: server.RouteProfileManagement, want: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			handler := newTestHandler(t, func(cfg *server.Config) {
				cfg.RouteProfile = test.profile
				cfg.SCIMHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNoContent)
				})
			})
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/scim/v2/Schemas", nil))
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d", recorder.Code, test.want)
			}
		})
	}
}
