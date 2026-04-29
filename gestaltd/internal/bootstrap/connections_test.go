package bootstrap

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestBuildConnectionRuntimePlatformManualDirectAuthMapping(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"gong": {
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{
						Connections: map[string]*providermanifestv1.ManifestConnectionDef{
							"default": {
								Mode: providermanifestv1.ConnectionModeUser,
								Auth: &providermanifestv1.ProviderAuth{
									Type: providermanifestv1.AuthTypeManual,
									Credentials: []providermanifestv1.CredentialField{
										{Name: "access_key_id"},
										{Name: "secret_key"},
									},
									AuthMapping: &providermanifestv1.AuthMapping{
										Basic: &providermanifestv1.BasicAuthMapping{
											Username: providermanifestv1.AuthValue{
												ValueFrom: &providermanifestv1.AuthValueFrom{
													CredentialFieldRef: &providermanifestv1.CredentialFieldRef{Name: "access_key_id"},
												},
											},
											Password: providermanifestv1.AuthValue{
												ValueFrom: &providermanifestv1.AuthValueFrom{
													CredentialFieldRef: &providermanifestv1.CredentialFieldRef{Name: "secret_key"},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				Connections: map[string]*config.ConnectionDef{
					"default": {
						Mode: providermanifestv1.ConnectionModePlatform,
						Auth: config.ConnectionAuthDef{
							Type:        providermanifestv1.AuthTypeManual,
							Credentials: []config.CredentialFieldDef{},
							AuthMapping: &config.AuthMappingDef{
								Basic: &config.BasicAuthMappingDef{
									Username: config.AuthValueDef{Value: "access-key-id"},
									Password: config.AuthValueDef{Value: "access-key-secret"},
								},
							},
						},
					},
				},
			},
		},
	}

	runtime, err := BuildConnectionRuntime(cfg)
	if err != nil {
		t.Fatalf("BuildConnectionRuntime() error = %v", err)
	}
	info, ok := runtime.Resolve("gong", "default")
	if !ok {
		t.Fatal("runtime.Resolve(gong, default) not found")
	}
	if info.Mode != core.ConnectionModePlatform {
		t.Fatalf("Mode = %q, want %q", info.Mode, core.ConnectionModePlatform)
	}
	if info.Token != "{}" {
		t.Fatalf("Token = %q, want placeholder JSON token", info.Token)
	}
}

func TestBuildConnectionRuntimePlatformManualCredentialRefsRequireToken(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"sample": {
				Connections: map[string]*config.ConnectionDef{
					"default": {
						Mode: providermanifestv1.ConnectionModePlatform,
						Auth: config.ConnectionAuthDef{
							Type: providermanifestv1.AuthTypeManual,
							AuthMapping: &config.AuthMappingDef{
								Headers: map[string]config.AuthValueDef{
									"X-API-Key": {
										ValueFrom: &config.AuthValueFromDef{
											CredentialFieldRef: &config.CredentialFieldRefDef{Name: "api_key"},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := BuildConnectionRuntime(cfg)
	if err == nil {
		t.Fatal("BuildConnectionRuntime() error = nil, want credential ref error")
	}
	if !strings.Contains(err.Error(), "manual auth with credential refs requires auth.token") {
		t.Fatalf("BuildConnectionRuntime() error = %v, want credential ref token error", err)
	}
}
