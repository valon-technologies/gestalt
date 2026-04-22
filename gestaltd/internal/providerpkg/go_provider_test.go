package providerpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestPrepareSourceManifest_MergesGeneratedGoManifestMetadata(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	testutil.CopyExampleProviderPlugin(t, root)
	injectGoManifestMetadata(t, filepath.Join(root, "provider.go"))

	manifestPath := filepath.Join(root, "manifest.yaml")
	preparedData, preparedManifest, err := PrepareSourceManifest(manifestPath)
	if err != nil {
		t.Fatalf("PrepareSourceManifest: %v", err)
	}
	if preparedManifest == nil || preparedManifest.Spec == nil {
		t.Fatalf("prepared manifest = %+v, want provider metadata", preparedManifest)
	}
	if !containsString(string(preparedData), "securitySchemes:") {
		t.Fatalf("prepared manifest data = %q, want merged security scheme metadata", string(preparedData))
	}
	if !containsString(string(preparedData), "path: /command") {
		t.Fatalf("prepared manifest data = %q, want merged HTTP binding metadata", string(preparedData))
	}

	scheme := preparedManifest.Spec.SecuritySchemes["slack"]
	if scheme == nil {
		t.Fatal(`manifest.Spec.SecuritySchemes["slack"] = nil, want generated scheme`)
	}
	if scheme.Type != providermanifestv1.HTTPSecuritySchemeTypeSlackSignature {
		t.Fatalf("scheme.Type = %q, want %q", scheme.Type, providermanifestv1.HTTPSecuritySchemeTypeSlackSignature)
	}
	if scheme.Secret == nil || scheme.Secret.Env != "SLACK_SIGNING_SECRET" {
		t.Fatalf("scheme.Secret = %+v, want env-backed secret", scheme.Secret)
	}

	binding := preparedManifest.Spec.HTTP["command"]
	if binding == nil {
		t.Fatal(`manifest.Spec.HTTP["command"] = nil, want generated binding`)
	}
	if binding.Path != "/command" {
		t.Fatalf("binding.Path = %q, want %q", binding.Path, "/command")
	}
	if binding.Method != "POST" {
		t.Fatalf("binding.Method = %q, want %q", binding.Method, "POST")
	}
	if binding.Security != "slack" {
		t.Fatalf("binding.Security = %q, want %q", binding.Security, "slack")
	}
	if binding.Target != "echo" {
		t.Fatalf("binding.Target = %q, want %q", binding.Target, "echo")
	}
	if binding.RequestBody == nil {
		t.Fatal("binding.RequestBody = nil, want request body metadata")
	}
	if _, ok := binding.RequestBody.Content["application/x-www-form-urlencoded"]; !ok {
		t.Fatalf("binding.RequestBody.Content = %#v, want form content type", binding.RequestBody.Content)
	}
	if binding.Ack == nil || binding.Ack.Status != 200 {
		t.Fatalf("binding.Ack = %+v, want 200 ack metadata", binding.Ack)
	}
}

func injectGoManifestMetadata(t *testing.T, providerPath string) {
	t.Helper()

	data, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", providerPath, err)
	}
	old := "\t)\n)"
	new := "\t).WithManifestMetadata(gestalt.ManifestMetadata{\n\t\tSecuritySchemes: map[string]gestalt.HTTPSecurityScheme{\n\t\t\t\"slack\": {\n\t\t\t\tType: gestalt.HTTPSecuritySchemeTypeSlackSignature,\n\t\t\t\tSecret: &gestalt.HTTPSecretRef{Env: \"SLACK_SIGNING_SECRET\"},\n\t\t\t},\n\t\t},\n\t\tHTTP: map[string]gestalt.HTTPBinding{\n\t\t\t\"command\": {\n\t\t\t\tPath:     \"/command\",\n\t\t\t\tMethod:   http.MethodPost,\n\t\t\t\tSecurity: \"slack\",\n\t\t\t\tTarget:   \"echo\",\n\t\t\t\tRequestBody: &gestalt.HTTPRequestBody{\n\t\t\t\t\tRequired: true,\n\t\t\t\t\tContent: map[string]gestalt.HTTPMediaType{\n\t\t\t\t\t\t\"application/x-www-form-urlencoded\": {},\n\t\t\t\t\t},\n\t\t\t\t},\n\t\t\t\tAck: &gestalt.HTTPAck{\n\t\t\t\t\tStatus: 200,\n\t\t\t\t\tBody: map[string]any{\n\t\t\t\t\t\t\"response_type\": \"ephemeral\",\n\t\t\t\t\t\t\"text\":          \"Working on it...\",\n\t\t\t\t\t},\n\t\t\t\t},\n\t\t\t},\n\t\t},\n\t})\n)"
	updated := strings.Replace(string(data), old, new, 1)
	if updated == string(data) {
		t.Fatalf("provider fixture %s missing router terminator %q", providerPath, old)
	}
	if err := os.WriteFile(providerPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", providerPath, err)
	}
}
