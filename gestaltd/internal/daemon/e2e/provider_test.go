package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

const (
	releaseTestAppName             = "release-test"
	releaseTestSource              = "github.com/testowner/apps/catalog/release-test"
	releaseTestModule              = "example.com/release-test"
	releaseTestIconPath            = "branding/icon.svg"
	releaseProviderSchemaPath      = "schemas/provider.schema.json"
	declarativeReleaseAppName      = "declarative-release"
	declarativeReleaseSource       = "github.com/testowner/apps/catalog/declarative-release"
	uiTestAppName                  = "ui-test"
	uiTestSource                   = "github.com/testowner/apps/catalog/ui-test"
	uiTestAssetRoot                = "out"
	prebuiltProviderAppName        = "prebuilt-provider"
	prebuiltProviderSource         = "github.com/testowner/apps/prebuilt-provider"
	prebuiltProviderBinaryPath     = "bin/provider"
	authReleaseAppName             = "auth-release"
	authReleaseSource              = "github.com/testowner/apps/auth-release"
	authReleaseSchemaPath          = "schemas/auth.schema.json"
	authorizationReleaseAppName    = "authorization-release"
	authorizationReleaseSource     = "github.com/testowner/apps/authorization-release"
	authorizationReleaseSchemaPath = "schemas/authorization.schema.json"
	secretsReleaseAppName          = "secrets-release"
	secretsReleaseSource           = "github.com/testowner/apps/secrets-release"
	secretsReleaseSchemaPath       = "schemas/secrets.schema.json"
)

func TestRun_ProviderCLIUsageAndErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		args      []string
		wantErr   bool
		wantParts []string
		notWant   []string
	}{
		{
			name:      "root help",
			args:      []string{"--help"},
			wantParts: []string{"gestaltd provider <command> [flags]", "list", "package", "release"},
			notWant:   []string{"attach"},
		},
		{
			name:      "release help",
			args:      []string{"release", "--help"},
			wantParts: []string{"--version"},
		},
		{
			name:      "root defaults to help",
			args:      nil,
			wantParts: []string{"gestaltd provider <command> [flags]"},
			notWant:   []string{"attach"},
		},
		{
			name:      "unknown subcommand",
			args:      []string{"bogus"},
			wantErr:   true,
			wantParts: []string{"unknown provider command", "bogus"},
		},
		{
			name:      "package help",
			args:      []string{"package", "--help"},
			wantParts: []string{"gestaltd provider package", "--platform"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, err := runProviderCommandResult("", tc.args...)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for provider %v, got output: %s", tc.args, out)
				}
			} else if err != nil {
				t.Fatalf("expected success for provider %v, got error: %v\noutput: %s", tc.args, err, out)
			}
			for _, want := range tc.wantParts {
				if !strings.Contains(string(out), want) {
					t.Fatalf("expected output to contain %q, got: %s", want, out)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(string(out), notWant) {
					t.Fatalf("expected %q absent from output, got: %s", notWant, out)
				}
			}
		})
	}
}

func sha256HexForTest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
