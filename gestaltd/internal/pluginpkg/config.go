package pluginpkg

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/valon-technologies/gestalt/server/internal/configschema"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func ValidateConfigForManifest(manifestPath string, manifest *pluginmanifestv1.Manifest, kind string, config map[string]any) error {
	schemaPath, schemaName, ok, err := configSchemaForManifest(manifestPath, manifest, kind)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("read config schema %q: %w", schemaName, err)
	}

	if config == nil {
		config = map[string]any{}
	}
	return configschema.Validate(config, string(data))
}

func configSchemaForManifest(manifestPath string, manifest *pluginmanifestv1.Manifest, kind string) (path string, name string, ok bool, err error) {
	if manifest == nil {
		return "", "", false, nil
	}

	switch kind {
	case pluginmanifestv1.KindPlugin:
		if manifest.Plugin == nil || manifest.Plugin.ConfigSchemaPath == "" {
			return "", "", false, nil
		}
		return filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(manifest.Plugin.ConfigSchemaPath)), manifest.Plugin.ConfigSchemaPath, true, nil
	case pluginmanifestv1.KindAuth:
		if manifest.Auth == nil || manifest.Auth.ConfigSchemaPath == "" {
			return "", "", false, nil
		}
		return filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(manifest.Auth.ConfigSchemaPath)), manifest.Auth.ConfigSchemaPath, true, nil
	case pluginmanifestv1.KindDatastore:
		if manifest.Datastore == nil || manifest.Datastore.ConfigSchemaPath == "" {
			return "", "", false, nil
		}
		return filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(manifest.Datastore.ConfigSchemaPath)), manifest.Datastore.ConfigSchemaPath, true, nil
	default:
		return "", "", false, fmt.Errorf("unsupported manifest config kind %q", kind)
	}
}
