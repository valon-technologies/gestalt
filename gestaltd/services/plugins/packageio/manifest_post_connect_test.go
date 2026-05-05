package packageio

import (
	"strings"
	"testing"
)

func TestDecodeSourceManifestPostConnectContract(t *testing.T) {
	t.Parallel()

	const manifest = `
source: github.com/test/gestalt-providers/plugins/pagerduty
version: 0.0.1-alpha.1
kind: plugin
spec:
  surfaces:
    openapi:
      connection: default
      document: https://example.com/openapi.json
  connections:
    default:
      auth:
        type: oauth2
        authorizationUrl: https://identity.example.com/oauth/authorize
        tokenUrl: https://identity.example.com/oauth/token
      postConnect:
        request:
          method: GET
          url: https://api.example.com/users/me
          headers:
            Accept: application/vnd.pagerduty+json;version=2
        sourcePath: user
        externalIdentity:
          type: pagerduty_identity
          id: user:{id}
        metadata:
          pagerduty.user_id: id
`
	parsed, err := DecodeSourceManifestFormat([]byte(manifest), ManifestFormatYAML)
	if err != nil {
		t.Fatalf("DecodeSourceManifestFormat: %v", err)
	}
	postConnect := parsed.Spec.Connections["default"].PostConnect
	if postConnect == nil {
		t.Fatal("expected postConnect to decode")
	}
	if postConnect.ExternalIdentity.ID != "user:{id}" {
		t.Fatalf("externalIdentity.id = %q, want user:{id}", postConnect.ExternalIdentity.ID)
	}
	if got := postConnect.Request.Headers["Accept"]; got != "application/vnd.pagerduty+json;version=2" {
		t.Fatalf("request.headers.Accept = %q, want PagerDuty API version", got)
	}
}

func TestDecodeSourceManifestRejectsInsecurePostConnectURL(t *testing.T) {
	t.Parallel()

	const manifest = `
source: github.com/test/gestalt-providers/plugins/pagerduty
version: 0.0.1-alpha.1
kind: plugin
spec:
  surfaces:
    openapi:
      connection: default
      document: https://example.com/openapi.json
  connections:
    default:
      auth:
        type: oauth2
        authorizationUrl: https://identity.example.com/oauth/authorize
        tokenUrl: https://identity.example.com/oauth/token
      postConnect:
        request:
          method: GET
          url: http://api.example.com/users/me
        metadata:
          pagerduty.user_id: id
`
	_, err := DecodeSourceManifestFormat([]byte(manifest), ManifestFormatYAML)
	if err == nil {
		t.Fatal("expected insecure postConnect URL to be rejected")
	}
	if !strings.Contains(err.Error(), "postConnect.request.url must be an absolute https URL") {
		t.Fatalf("error = %v, want https URL validation", err)
	}
}
