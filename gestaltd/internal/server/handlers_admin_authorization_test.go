package server

import (
	"net/http"
	"net/url"
	"testing"
)

func TestPluginScopedAdminAuthorizationRoutePluginAllowsEncodedPluginFragmentID(t *testing.T) {
	t.Parallel()

	req := &http.Request{URL: &url.URL{
		Path:    "/admin/api/v1/authorization/fragments/plugin/github",
		RawPath: "/admin/api/v1/authorization/fragments/plugin%2Fgithub",
	}}

	plugin, ok := pluginScopedAdminAuthorizationRoutePlugin(req)
	if !ok || plugin != "github" {
		t.Fatalf("pluginScopedAdminAuthorizationRoutePlugin = (%q, %v), want (github, true)", plugin, ok)
	}
}
