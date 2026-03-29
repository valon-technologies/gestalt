package pluginpkg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"runtime"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/valon-technologies/gestalt/internal/pluginsource"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/pluginmanifest/v1"
)

const ManifestFile = "plugin.json"

func DecodeManifest(data []byte) (*pluginmanifestv1.Manifest, error) {
	var header struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, fmt.Errorf("parse manifest JSON: %w", err)
	}

	var rawSchema []byte
	switch header.SchemaVersion {
	case pluginmanifestv1.SchemaVersion:
		rawSchema = pluginmanifestv1.ManifestJSONSchema
	case pluginmanifestv1.SchemaVersion2:
		rawSchema = pluginmanifestv1.ManifestV2JSONSchema
	default:
		return nil, fmt.Errorf("unsupported manifest schema_version %d", header.SchemaVersion)
	}

	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse manifest JSON: %w", err)
	}
	var schemaDoc any
	if err := json.Unmarshal(rawSchema, &schemaDoc); err != nil {
		return nil, fmt.Errorf("parse embedded manifest schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("manifest.schema.json", schemaDoc); err != nil {
		return nil, fmt.Errorf("load manifest schema: %w", err)
	}
	schema, err := compiler.Compile("manifest.schema.json")
	if err != nil {
		return nil, fmt.Errorf("compile manifest schema: %w", err)
	}
	if err := schema.Validate(doc); err != nil {
		return nil, fmt.Errorf("manifest validation failed: %w", err)
	}

	var manifest pluginmanifestv1.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if err := ValidateManifest(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func ValidateManifest(manifest *pluginmanifestv1.Manifest) error {
	if manifest == nil {
		return fmt.Errorf("manifest is required")
	}

	switch manifest.SchemaVersion {
	case pluginmanifestv1.SchemaVersion:
		if manifest.ID == "" {
			return fmt.Errorf("manifest id is required for schema_version %d", pluginmanifestv1.SchemaVersion)
		}
		if manifest.Source != "" {
			return fmt.Errorf("manifest source must not be set for schema_version %d", pluginmanifestv1.SchemaVersion)
		}
	case pluginmanifestv1.SchemaVersion2:
		if manifest.ID != "" {
			return fmt.Errorf("manifest id must not be set for schema_version %d", pluginmanifestv1.SchemaVersion2)
		}
		if manifest.Source == "" {
			return fmt.Errorf("manifest source is required for schema_version %d", pluginmanifestv1.SchemaVersion2)
		}
		if _, err := pluginsource.Parse(manifest.Source); err != nil {
			return fmt.Errorf("manifest source: %w", err)
		}
		if err := pluginsource.ValidateVersion(manifest.Version); err != nil {
			return fmt.Errorf("manifest version: %w", err)
		}
	default:
		return fmt.Errorf("unsupported manifest schema_version %d", manifest.SchemaVersion)
	}

	hasExecutableKinds := false
	for _, kind := range manifest.Kinds {
		if kind == pluginmanifestv1.KindProvider || kind == pluginmanifestv1.KindRuntime {
			hasExecutableKinds = true
			break
		}
	}

	var artifactPaths map[string]struct{}
	if hasExecutableKinds {
		artifactPaths = make(map[string]struct{}, len(manifest.Artifacts))
		artifactPlatforms := make(map[string]struct{}, len(manifest.Artifacts))
		for _, artifact := range manifest.Artifacts {
			if err := validateRelativePackagePath(artifact.Path, "artifact path"); err != nil {
				return err
			}
			if _, exists := artifactPaths[artifact.Path]; exists {
				return fmt.Errorf("duplicate artifact path %q", artifact.Path)
			}
			artifactPaths[artifact.Path] = struct{}{}

			key := artifact.OS + "/" + artifact.Arch
			if _, exists := artifactPlatforms[key]; exists {
				return fmt.Errorf("duplicate artifact platform %q", key)
			}
			artifactPlatforms[key] = struct{}{}
		}
	}

	for _, kind := range manifest.Kinds {
		switch kind {
		case pluginmanifestv1.KindProvider:
			if manifest.Provider == nil {
				return fmt.Errorf("provider metadata is required when kind %q is present", pluginmanifestv1.KindProvider)
			}
			if manifest.Provider.Protocol.Min > manifest.Provider.Protocol.Max {
				return fmt.Errorf("provider.protocol.min must be <= provider.protocol.max")
			}
			if manifest.Provider.ConfigSchemaPath != "" {
				if err := validateRelativePackagePath(manifest.Provider.ConfigSchemaPath, "provider config schema path"); err != nil {
					return err
				}
			}
			if err := validateProviderAuth(manifest.Provider.Auth); err != nil {
				return err
			}
			if err := validateEntrypoint(kind, manifest.Entrypoints.Provider, artifactPaths); err != nil {
				return err
			}
		case pluginmanifestv1.KindRuntime:
			if err := validateEntrypoint(kind, manifest.Entrypoints.Runtime, artifactPaths); err != nil {
				return err
			}
		case pluginmanifestv1.KindWebUI:
			if manifest.SchemaVersion != pluginmanifestv1.SchemaVersion2 {
				return fmt.Errorf("webui kind requires schema_version %d", pluginmanifestv1.SchemaVersion2)
			}
			if manifest.WebUI == nil {
				return fmt.Errorf("webui metadata is required when kind %q is present", pluginmanifestv1.KindWebUI)
			}
			if err := validateRelativePackagePath(manifest.WebUI.AssetRoot, "webui asset root"); err != nil {
				return err
			}
			for _, other := range manifest.Kinds {
				if other != pluginmanifestv1.KindWebUI {
					return fmt.Errorf("webui kind cannot be combined with %q", other)
				}
			}
		default:
			return fmt.Errorf("unsupported manifest kind %q", kind)
		}
	}

	return nil
}

func CurrentPlatformArtifact(manifest *pluginmanifestv1.Manifest) (*pluginmanifestv1.Artifact, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.OS == runtime.GOOS && artifact.Arch == runtime.GOARCH {
			artifact := artifact
			return &artifact, nil
		}
	}
	return nil, fmt.Errorf("no artifact for current platform %s/%s", runtime.GOOS, runtime.GOARCH)
}

func validateEntrypoint(kind string, entry *pluginmanifestv1.Entrypoint, artifactPaths map[string]struct{}) error {
	if entry == nil {
		return fmt.Errorf("%s entrypoint is required", kind)
	}
	if err := validateRelativePackagePath(entry.ArtifactPath, kind+" entrypoint artifact path"); err != nil {
		return err
	}
	if _, ok := artifactPaths[entry.ArtifactPath]; !ok {
		return fmt.Errorf("%s entrypoint references unknown artifact %q", kind, entry.ArtifactPath)
	}
	return nil
}

func validateRelativePackagePath(value, label string) error {
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	if strings.HasPrefix(value, "/") {
		return fmt.Errorf("%s must be relative", label)
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("%s must stay within the package", label)
	}
	if strings.Contains(value, "\\") {
		return fmt.Errorf("%s must use forward slashes", label)
	}
	if cleaned != value {
		return fmt.Errorf("%s must be normalized", label)
	}
	return nil
}

func validateProviderAuth(auth *pluginmanifestv1.ProviderAuth) error {
	if auth == nil {
		return nil
	}
	switch auth.Type {
	case pluginmanifestv1.AuthTypeOAuth2:
		if auth.AuthorizationURL == "" {
			return fmt.Errorf("provider.auth.authorization_url is required for oauth2")
		}
		if auth.TokenURL == "" {
			return fmt.Errorf("provider.auth.token_url is required for oauth2")
		}
	case pluginmanifestv1.AuthTypeManual, pluginmanifestv1.AuthTypeNone:
	default:
		return fmt.Errorf("unsupported provider.auth.type %q", auth.Type)
	}
	return nil
}

func EncodeManifest(manifest *pluginmanifestv1.Manifest) ([]byte, error) {
	if err := ValidateManifest(manifest); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	return append(data, '\n'), nil
}

func Kinds(manifest *pluginmanifestv1.Manifest) []string {
	if manifest == nil {
		return nil
	}
	out := append([]string(nil), manifest.Kinds...)
	slices.Sort(out)
	return out
}

func ManifestEqual(a, b *pluginmanifestv1.Manifest) bool {
	if a == nil || b == nil {
		return a == b
	}
	aj, err := EncodeManifest(a)
	if err != nil {
		return false
	}
	bj, err := EncodeManifest(b)
	if err != nil {
		return false
	}
	return bytes.Equal(aj, bj)
}
