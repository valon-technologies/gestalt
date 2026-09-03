package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
  authorization:
    authz:
      source:
        path: ./providers/authorization/test
server:
  providers:
    indexeddb: sqlite
    authorization: authz
  scim:
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
        authoritativeUserDomains: [valon.com]
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
              allowedTargets:
                - subjectType: subject
                - subjectSet:
                    resourceType: group
                    relation: member
          dynamic:
            allowAdditionalRelationships: true
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
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
              allowedTargets:
                - subjectType: subject
                - subjectSet:
                    resourceType: group
                    relation: member
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
	subjectTypesOnly := strings.Replace(validAuthorization, "                - subjectType: subject\n", "", 1)
	if _, err := config.Load(writeSCIMConfig(t, "apiVersion: gestaltd.config/v8\n"+subjectTypesOnly)); err != nil {
		t.Fatalf("subjectTypes-only direct target should be valid: %v", err)
	}
	duplicateClient := "      entra:\n        credentials:\n          - id: current\n            bearerToken: entra-token\n        activeUserRelationships:\n          - relation: member\n            resource:\n              type: group\n              id: valon-employees\n"
	duplicateProjection := strings.Replace(validAuthorization, "      rippling:\n", duplicateClient+"      rippling:\n", 1)
	if _, err := config.Load(writeSCIMConfig(t, "apiVersion: gestaltd.config/v8\n"+duplicateProjection)); err == nil || !strings.Contains(err.Error(), "duplicates active projection") {
		t.Fatalf("duplicate active projection error = %v", err)
	}

	cases := []struct {
		name    string
		replace string
		with    string
		want    string
	}{
		{name: "limits rotation credentials", replace: "          - id: current\n            bearerToken: rippling-token\n", with: "          - id: one\n            bearerToken: token-one\n          - id: two\n            bearerToken: token-two\n          - id: three\n            bearerToken: token-three\n", want: "one or two"},
		{name: "requires dynamic projection writes", replace: "allowAdditionalRelationships: true", with: "allowAdditionalRelationships: false", want: "allowAdditionalRelationships"},
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
