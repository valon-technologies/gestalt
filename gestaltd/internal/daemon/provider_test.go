package daemon

import (
	"crypto/sha256"
	"encoding/hex"
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

func sha256HexForTest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
