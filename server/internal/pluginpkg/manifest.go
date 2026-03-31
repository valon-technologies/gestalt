package pluginpkg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

const ManifestFile = "plugin.json"

var ManifestFiles = []string{"plugin.json", "plugin.yaml", "plugin.yml"}

func FindManifestFile(dir string) (string, error) {
	for _, name := range ManifestFiles {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no manifest file found in %s (tried %s)", dir, strings.Join(ManifestFiles, ", "))
}

func isYAMLFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

func yamlToJSON(data []byte) ([]byte, error) {
	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse manifest YAML: %w", err)
	}
	doc = normalizeYAML(doc)
	return json.Marshal(doc)
}

func normalizeYAML(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, v := range val {
			out[k] = normalizeYAML(v)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, v := range val {
			out[i] = normalizeYAML(v)
		}
		return out
	default:
		return val
	}
}

func DecodeManifest(data []byte) (*pluginmanifestv1.Manifest, error) {
	return DecodeManifestFormat(data, "json")
}

func DecodeManifestFormat(data []byte, format string) (*pluginmanifestv1.Manifest, error) {
	jsonData := data
	if format == "yaml" {
		var err error
		jsonData, err = yamlToJSON(data)
		if err != nil {
			return nil, err
		}
	}
	return decodeManifestJSON(jsonData)
}

func decodeManifestJSON(data []byte) (*pluginmanifestv1.Manifest, error) {
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse manifest JSON: %w", err)
	}
	var schemaDoc any
	if err := json.Unmarshal(pluginmanifestv1.ManifestJSONSchema, &schemaDoc); err != nil {
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

	if manifest.Source == "" {
		return fmt.Errorf("manifest source is required")
	}
	if _, err := pluginsource.Parse(manifest.Source); err != nil {
		return fmt.Errorf("manifest source: %w", err)
	}
	if err := pluginsource.ValidateVersion(manifest.Version); err != nil {
		return fmt.Errorf("manifest version: %w", err)
	}
	if manifest.IconFile != "" {
		if err := validateRelativePackagePath(manifest.IconFile, "icon_file"); err != nil {
			return err
		}
	}

	isDeclarative := manifest.Provider.IsDeclarative()

	needsArtifacts := false
	for _, kind := range manifest.Kinds {
		if kind == pluginmanifestv1.KindRuntime || (kind == pluginmanifestv1.KindProvider && (!isDeclarative || manifest.IsHybridProvider())) {
			needsArtifacts = true
			break
		}
	}

	var artifactPaths map[string]struct{}
	if needsArtifacts {
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
			if err := validateProviderAuth(manifest.Provider.Auth); err != nil {
				return err
			}
			if manifest.Provider.ConfigSchemaPath != "" {
				if err := validateRelativePackagePath(manifest.Provider.ConfigSchemaPath, "provider config schema path"); err != nil {
					return err
				}
			}
			if isDeclarative {
				if err := validateDeclarativeProvider(manifest.Provider); err != nil {
					return err
				}
			}
			if !isDeclarative || manifest.IsHybridProvider() {
				if manifest.Provider.Protocol.Min > manifest.Provider.Protocol.Max {
					return fmt.Errorf("provider.protocol.min must be <= provider.protocol.max")
				}
				if err := validateEntrypoint(kind, manifest.Entrypoints.Provider, artifactPaths); err != nil {
					return err
				}
			}
		case pluginmanifestv1.KindRuntime:
			if err := validateEntrypoint(kind, manifest.Entrypoints.Runtime, artifactPaths); err != nil {
				return err
			}
		case pluginmanifestv1.KindWebUI:
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
	case pluginmanifestv1.AuthTypeBearer, pluginmanifestv1.AuthTypeManual, pluginmanifestv1.AuthTypeNone:
	default:
		return fmt.Errorf("unsupported provider.auth.type %q", auth.Type)
	}
	return nil
}

var validParamIn = map[string]bool{
	"query": true,
	"body":  true,
	"path":  true,
}

var validHTTPMethods = map[string]bool{
	"GET":    true,
	"POST":   true,
	"PUT":    true,
	"PATCH":  true,
	"DELETE": true,
}

func validateDeclarativeProvider(provider *pluginmanifestv1.Provider) error {
	if provider.BaseURL == "" {
		return fmt.Errorf("provider.base_url is required for declarative providers")
	}
	seen := make(map[string]struct{}, len(provider.Operations))
	for i, op := range provider.Operations {
		if op.Name == "" {
			return fmt.Errorf("provider.operations[%d].name is required", i)
		}
		if _, exists := seen[op.Name]; exists {
			return fmt.Errorf("duplicate operation name %q", op.Name)
		}
		seen[op.Name] = struct{}{}
		if !validHTTPMethods[op.Method] {
			return fmt.Errorf("provider.operations[%d].method %q is not a valid HTTP method", i, op.Method)
		}
		if op.Path == "" {
			return fmt.Errorf("provider.operations[%d].path is required", i)
		}
		seenParams := make(map[string]struct{}, len(op.Parameters))
		for j, param := range op.Parameters {
			if param.Name == "" {
				return fmt.Errorf("provider.operations[%d].parameters[%d].name is required", i, j)
			}
			if _, dup := seenParams[param.Name]; dup {
				return fmt.Errorf("provider.operations[%d] has duplicate parameter name %q", i, param.Name)
			}
			seenParams[param.Name] = struct{}{}
			if !validParamIn[param.In] {
				return fmt.Errorf("provider.operations[%d].parameters[%d].in %q must be query, body, or path", i, j, param.In)
			}
			if param.In == "path" && !strings.Contains(op.Path, "{"+param.Name+"}") {
				return fmt.Errorf("provider.operations[%d].parameters[%d] %q declared as path param but %q has no {%s} placeholder", i, j, param.Name, op.Path, param.Name)
			}
		}
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
