package server

import (
	"net/http"
	"net/url"
	"testing"
)

func TestAppScopedAdminAuthorizationRoutePluginAllowsEncodedAppGrantID(t *testing.T) {
	t.Parallel()

	req := &http.Request{URL: &url.URL{
		Path:    "/api/v1/authorization/grants/app/github",
		RawPath: "/api/v1/authorization/grants/app%2Fgithub",
	}}

	appName, ok := appScopedAdminAuthorizationRoutePlugin(req)
	if !ok || appName != "github" {
		t.Fatalf("appScopedAdminAuthorizationRouteApp = (%q, %v), want (github, true)", appName, ok)
	}
}
