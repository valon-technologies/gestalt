package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestLoadSCIMConfiguration(t *testing.T) {
	t.Parallel()

	path := writeSCIMConfig(t, `
apiVersion: gestaltd.config/v8
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/indexeddb/sqlite
  secrets:
    secrets:
      source:
        path: ./providers/secrets/test
server:
  providers:
    indexeddb: sqlite
  scim:
    retryInterval: 2s
    driftInterval: 15m
    clients:
      rippling:
        credentials:
          - id: current
            bearerToken:
              secret:
                provider: secrets
                name: rippling-scim-token
          - id: next
            bearerToken: rotating-token
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, err := cfg.Server.SCIM.RetryIntervalDuration(); err != nil || got != 2*time.Second {
		t.Fatalf("RetryIntervalDuration = %v, %v", got, err)
	}
	if got, err := cfg.Server.SCIM.DriftIntervalDuration(); err != nil || got != 15*time.Minute {
		t.Fatalf("DriftIntervalDuration = %v, %v", got, err)
	}
	credential := cfg.Server.SCIM.Clients["rippling"].Credentials[0]
	ref, ok, err := config.ParseSecretRefTransport(credential.BearerToken)
	if err != nil || !ok || ref.Provider != "secrets" || ref.Name != "rippling-scim-token" {
		t.Fatalf("structured bearer token = %#v, %v, %v", ref, ok, err)
	}
}

func TestLoadSCIMConfigurationValidation(t *testing.T) {
	t.Parallel()

	validAuthorization := `
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/indexeddb/sqlite
  authorization:
    authz:
      source:
        path: ./providers/authorization/indexeddb
server:
  providers:
    indexeddb: sqlite
    authorization: authz
  scim:
    clients:
      rippling:
        credentials:
          - id: current
            bearerToken: rippling-token
        authoritativeUserDomains: [VALON.COM]
        activeUserRelationships:
          - relation: member
            resource:
              type: group
              id: valon-employees
authorization:
  models:
    default:
      resourceTypes:
        group:
          relations:
            member:
              subjectTypes: [subject]
          dynamic:
            allowAdditionalRelationships: true
`
	cfg, err := config.Load(writeSCIMConfig(t, "apiVersion: gestaltd.config/v8\n"+validAuthorization))
	if err != nil {
		t.Fatalf("valid Load: %v", err)
	}
	if got := cfg.Server.SCIM.Clients["rippling"].AuthoritativeUserDomains[0]; got != "valon.com" {
		t.Fatalf("normalized domain = %q", got)
	}
	withoutAuthorization := strings.Replace(validAuthorization, `  authorization:
    authz:
      source:
        path: ./providers/authorization/indexeddb
`, "", 1)
	withoutAuthorization = strings.Replace(withoutAuthorization, "    authorization: authz\n", "", 1)
	if _, err := config.Load(writeSCIMConfig(t, "apiVersion: gestaltd.config/v8\n"+withoutAuthorization)); err == nil || !strings.Contains(err.Error(), "require providers.authorization") {
		t.Fatalf("configuration without authorization error = %v", err)
	}

	cases := []struct {
		name    string
		replace string
		with    string
		want    string
	}{
		{name: "limits rotation credentials", replace: "          - id: current\n            bearerToken: rippling-token\n", with: "          - id: one\n            bearerToken: token-one\n          - id: two\n            bearerToken: token-two\n          - id: three\n            bearerToken: token-three\n", want: "one or two"},
		{name: "requires dynamic projection writes", replace: "allowAdditionalRelationships: true", with: "allowAdditionalRelationships: false", want: "allowAdditionalRelationships"},
		{name: "validates retry interval", replace: "  scim:\n", with: "  scim:\n    retryInterval: 0s\n", want: "retryInterval"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			yaml := "apiVersion: gestaltd.config/v8\n" + strings.Replace(validAuthorization, test.replace, test.with, 1)
			_, err := config.Load(writeSCIMConfig(t, yaml))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load error = %v, want %q", err, test.want)
			}
		})
	}
}

func writeSCIMConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gestalt.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
