package operator

import (
	"os"
	"path/filepath"

	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

// WriteDefaultRootAppFixture creates providersDir/app/default for generated local
// gestaltd configs that mount the default home app at /.
func WriteDefaultRootAppFixture(providersDir string) error {
	appDir := filepath.Join(providersDir, "app", "default")
	distDir := filepath.Join(appDir, "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte(`<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <title>Default Gestalt UI</title>
  </head>
  <body>
    <div id="app">Default Gestalt UI</div>
  </body>
</html>
`), 0o644); err != nil {
		return err
	}
	manifestData, err := providerpkg.EncodeManifestFormat(&providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      config.DefaultRootAppProvider,
		Version:     config.DefaultRootAppVersion,
		DisplayName: "Home",
		Spec:        &providermanifestv1.Spec{AssetRoot: "dist"},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(appDir, "manifest.yaml"), manifestData, 0o644)
}
