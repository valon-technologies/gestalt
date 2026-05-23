package server

import (
	"net/http"
	"net/url"
	"testing"
)

func TestAppScopedAdminAuthorizationRoutePluginAllowsEncodedPluginFragmentID(t *testing.T) {
	t.Parallel()

	req := &http.Request{URL: &url.URL{
		Path:    "/admin/api/v1/authorization/fragments/app/github",
		RawPath: "/admin/api/v1/authorization/fragments/app%2Fgithub",
	}}

	appName, ok := appScopedAdminAuthorizationRoutePlugin(req)
	if !ok || appName != "github" {
		t.Fatalf("appScopedAdminAuthorizationRouteApp = (%q, %v), want (github, true)", appName, ok)
	}
}
