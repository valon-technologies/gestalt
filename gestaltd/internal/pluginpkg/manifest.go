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

	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

const ManifestFile = "manifest.json"

var ManifestFiles = []string{"manifest.json", "manifest.yaml", "manifest.yml"}

const (
	ManifestFormatJSON = "json"
	ManifestFormatYAML = "yaml"
)

func FindManifestFile(dir string) (string, error) {
	for _, name := range ManifestFiles {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no manifest file found in %s (tried %s)", dir, strings.Join(ManifestFiles, ", "))
}

func IsManifestFile(path string) bool {
	base := filepath.Base(path)
	return slices.Contains(ManifestFiles, base)
}

func ManifestFormatFromPath(path string) string {
	if isYAMLFile(path) {
		return ManifestFormatYAML
	}
	return ManifestFormatJSON
}

func isYAMLFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

func DecodeManifest(data []byte) (*pluginmanifestv1.Manifest, error) {
	return DecodeManifestFormat(data, ManifestFormatJSON)
}

func DecodeSourceManifestFormat(data []byte, format string) (*pluginmanifestv1.Manifest, error) {
	return decodeManifest(data, format, true)
}

func DecodeManifestFormat(data []byte, format string) (*pluginmanifestv1.Manifest, error) {
	return decodeManifest(data, format, false)
}

func decodeManifest(data []byte, format string, sourceMode bool) (*pluginmanifestv1.Manifest, error) {
	var manifest pluginmanifestv1.Manifest
	if err := decodeStrict(data, format, "manifest", &manifest); err == nil {
		if err := validateManifest(&manifest, sourceMode); err != nil {
			return nil, err
		}
		return &manifest, nil
	} else {
		if providerManifest, providerErr := decodeProviderManifestWire(data, format); providerErr == nil {
			if err := validateManifest(providerManifest, sourceMode); err != nil {
				return nil, err
			}
			return providerManifest, nil
		}

		compatData, changed, compatErr := normalizeLegacyManifestCompatibility(data, format)
		if compatErr == nil && changed {
			if compatStrictErr := decodeStrict(compatData, format, "manifest", &manifest); compatStrictErr == nil {
				if err := validateManifest(&manifest, sourceMode); err != nil {
					return nil, err
				}
				return &manifest, nil
			}
			if providerManifest, providerErr := decodeProviderManifestWire(compatData, format); providerErr == nil {
				if err := validateManifest(providerManifest, sourceMode); err != nil {
					return nil, err
				}
				return providerManifest, nil
			}
		}

		return nil, err
	}
}

func ValidateManifest(manifest *pluginmanifestv1.Manifest) error {
	return validateManifest(manifest, false)
}

func ManifestKind(manifest *pluginmanifestv1.Manifest) (string, error) {
	if manifest == nil {
		return "", fmt.Errorf("manifest is required")
	}

	var (
		kind  string
		count int
	)
	if manifest.Plugin != nil {
		kind = pluginmanifestv1.KindPlugin
		count++
	}
	if manifest.Auth != nil {
		kind = pluginmanifestv1.KindAuth
		count++
	}
	if manifest.Datastore != nil {
		kind = pluginmanifestv1.KindDatastore
		count++
	}
	if manifest.Secrets != nil {
		kind = pluginmanifestv1.KindSecrets
		count++
	}
	if manifest.WebUI != nil {
		kind = pluginmanifestv1.KindWebUI
		count++
	}
	if count != 1 {
		return "", fmt.Errorf("manifest must define exactly one of plugin, auth, datastore, secrets, or webui")
	}
	return kind, nil
}

func validateManifest(manifest *pluginmanifestv1.Manifest, sourceMode bool) error {
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
	if manifest.Release != nil {
		if !sourceMode {
			return fmt.Errorf("release metadata is only allowed in source manifests")
		}
		if err := validateReleaseMetadata(manifest.Release); err != nil {
			return err
		}
	}
	kind, err := ManifestKind(manifest)
	if err != nil {
		return err
	}

	allowsSourceExecutableEntrypointOmission := sourceMode && manifest.Entrypoints.Provider == nil
	allowsSourceAuthEntrypointOmission := sourceMode && manifest.Entrypoints.Auth == nil
	allowsSourceDatastoreEntrypointOmission := sourceMode && manifest.Entrypoints.Datastore == nil
	allowsSourceSecretsEntrypointOmission := sourceMode && manifest.Entrypoints.Secrets == nil

	needsArtifacts := len(manifest.Artifacts) > 0
	switch kind {
	case pluginmanifestv1.KindPlugin:
		needsArtifacts = needsArtifacts || manifest.Entrypoints.Provider != nil
	case pluginmanifestv1.KindAuth:
		needsArtifacts = needsArtifacts || !allowsSourceAuthEntrypointOmission
	case pluginmanifestv1.KindDatastore:
		needsArtifacts = needsArtifacts || !allowsSourceDatastoreEntrypointOmission
	case pluginmanifestv1.KindSecrets:
		needsArtifacts = needsArtifacts || !allowsSourceSecretsEntrypointOmission
	}

	var artifactPaths map[string]struct{}
	if needsArtifacts {
		artifactPaths = make(map[string]struct{}, len(manifest.Artifacts))
		artifactPlatforms := make(map[string]struct{}, len(manifest.Artifacts))
		for _, artifact := range manifest.Artifacts {
			if err := validateRelativePackagePath(artifact.Path, "artifact path"); err != nil {
				return err
			}
			if _, err := NormalizeArtifactLibC(artifact.OS, artifact.LibC); err != nil {
				return err
			}
			if artifact.SHA256 == "" && !sourceMode {
				return fmt.Errorf("artifact %s sha256 is required", artifact.Path)
			}
			if _, exists := artifactPaths[artifact.Path]; exists {
				return fmt.Errorf("duplicate artifact path %q", artifact.Path)
			}
			artifactPaths[artifact.Path] = struct{}{}

			key := PlatformString(artifact.OS, artifact.Arch, artifact.LibC)
			if _, exists := artifactPlatforms[key]; exists {
				return fmt.Errorf("duplicate artifact platform %q", key)
			}
			artifactPlatforms[key] = struct{}{}
		}
	}

	switch kind {
	case pluginmanifestv1.KindPlugin:
		if err := validateExecutableProviderMetadata(manifest.Plugin); err != nil {
			return err
		}
		if manifest.Plugin.ConfigSchemaPath != "" {
			if err := validateRelativePackagePath(manifest.Plugin.ConfigSchemaPath, "provider config schema path"); err != nil {
				return err
			}
		}
		if manifest.Plugin.IsDeclarative() {
			if err := validateDeclarativeProvider(manifest.Plugin); err != nil {
				return err
			}
		}
		if manifest.Entrypoints.Auth != nil || manifest.Entrypoints.Datastore != nil || manifest.Entrypoints.Secrets != nil {
			return fmt.Errorf("plugin manifests may only define entrypoints.provider")
		}
		switch {
		case manifest.Entrypoints.Provider != nil:
			if err := validateEntrypoint(kind, manifest.Entrypoints.Provider, artifactPaths); err != nil {
				return err
			}
		case manifest.IsDeclarativeOnlyProvider():
		case manifest.Plugin.IsSpecLoaded():
		case allowsSourceExecutableEntrypointOmission:
		default:
			return fmt.Errorf("%s is required", EntrypointFieldForKind(kind))
		}
	case pluginmanifestv1.KindAuth:
		if manifest.Auth.ConfigSchemaPath != "" {
			if err := validateRelativePackagePath(manifest.Auth.ConfigSchemaPath, "auth config schema path"); err != nil {
				return err
			}
		}
		if manifest.Entrypoints.Provider != nil || manifest.Entrypoints.Datastore != nil || manifest.Entrypoints.Secrets != nil {
			return fmt.Errorf("auth manifests may only define entrypoints.auth")
		}
		if manifest.Entrypoints.Auth == nil && !allowsSourceAuthEntrypointOmission {
			return fmt.Errorf("%s is required", EntrypointFieldForKind(kind))
		}
		if manifest.Entrypoints.Auth != nil {
			if err := validateEntrypoint(kind, manifest.Entrypoints.Auth, artifactPaths); err != nil {
				return err
			}
		}
	case pluginmanifestv1.KindDatastore:
		if manifest.Datastore.ConfigSchemaPath != "" {
			if err := validateRelativePackagePath(manifest.Datastore.ConfigSchemaPath, "datastore config schema path"); err != nil {
				return err
			}
		}
		if manifest.Entrypoints.Provider != nil || manifest.Entrypoints.Auth != nil || manifest.Entrypoints.Secrets != nil {
			return fmt.Errorf("datastore manifests may only define entrypoints.datastore")
		}
		if manifest.Entrypoints.Datastore == nil && !allowsSourceDatastoreEntrypointOmission {
			return fmt.Errorf("%s is required", EntrypointFieldForKind(kind))
		}
		if manifest.Entrypoints.Datastore != nil {
			if err := validateEntrypoint(kind, manifest.Entrypoints.Datastore, artifactPaths); err != nil {
				return err
			}
		}
	case pluginmanifestv1.KindSecrets:
		if manifest.Secrets.ConfigSchemaPath != "" {
			if err := validateRelativePackagePath(manifest.Secrets.ConfigSchemaPath, "secrets config schema path"); err != nil {
				return err
			}
		}
		if manifest.Entrypoints.Provider != nil || manifest.Entrypoints.Auth != nil || manifest.Entrypoints.Datastore != nil {
			return fmt.Errorf("secrets manifests may only define entrypoints.secrets")
		}
		if manifest.Entrypoints.Secrets == nil && !allowsSourceSecretsEntrypointOmission {
			return fmt.Errorf("%s is required", EntrypointFieldForKind(kind))
		}
		if manifest.Entrypoints.Secrets != nil {
			if err := validateEntrypoint(kind, manifest.Entrypoints.Secrets, artifactPaths); err != nil {
				return err
			}
		}
	case pluginmanifestv1.KindWebUI:
		if manifest.Entrypoints.Provider != nil || manifest.Entrypoints.Auth != nil || manifest.Entrypoints.Datastore != nil || manifest.Entrypoints.Secrets != nil {
			return fmt.Errorf("webui manifests may not define executable entrypoints")
		}
		if err := validateRelativePackagePath(manifest.WebUI.AssetRoot, "webui asset root"); err != nil {
			return err
		}
		if manifest.WebUI.ConfigSchemaPath != "" {
			if err := validateRelativePackagePath(manifest.WebUI.ConfigSchemaPath, "webui config schema path"); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported manifest kind %q", kind)
	}

	return nil
}

func CurrentPlatformArtifact(manifest *pluginmanifestv1.Manifest, runtimeLibC string) (*pluginmanifestv1.Artifact, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}
	currentLibC := runtimeLibC
	var generic *pluginmanifestv1.Artifact
	var muslSpecific *pluginmanifestv1.Artifact
	libcSpecific := make([]*pluginmanifestv1.Artifact, 0, 2)
	for _, artifact := range manifest.Artifacts {
		if artifact.OS != runtime.GOOS || artifact.Arch != runtime.GOARCH {
			continue
		}
		switch {
		case runtime.GOOS == "linux" && currentLibC != "" && artifact.LibC == currentLibC:
			artifact := artifact
			return &artifact, nil
		case runtime.GOOS == "linux" && currentLibC == "" && artifact.LibC != "":
			artifact := artifact
			if artifact.LibC == LinuxLibCMusl {
				muslSpecific = &artifact
			}
			libcSpecific = append(libcSpecific, &artifact)
		case artifact.LibC == "":
			artifact := artifact
			generic = &artifact
		}
	}
	if generic != nil {
		return generic, nil
	}
	if runtime.GOOS == "linux" && currentLibC == "" {
		if muslSpecific != nil {
			return muslSpecific, nil
		}
		switch len(libcSpecific) {
		case 1:
			return libcSpecific[0], nil
		case 0:
		default:
			return nil, fmt.Errorf("multiple artifacts for current platform %s/%s", runtime.GOOS, runtime.GOARCH)
		}
	}
	return nil, fmt.Errorf("no artifact for current platform %s/%s", runtime.GOOS, runtime.GOARCH)
}

func EntrypointFieldForKind(kind string) string {
	switch kind {
	case pluginmanifestv1.KindPlugin:
		return "entrypoints.provider"
	case pluginmanifestv1.KindAuth:
		return "entrypoints.auth"
	case pluginmanifestv1.KindDatastore:
		return "entrypoints.datastore"
	case pluginmanifestv1.KindSecrets:
		return "entrypoints.secrets"
	default:
		return "entrypoints." + kind
	}
}

func EntrypointForKind(manifest *pluginmanifestv1.Manifest, kind string) *pluginmanifestv1.Entrypoint {
	if manifest == nil {
		return nil
	}
	switch kind {
	case pluginmanifestv1.KindPlugin:
		return manifest.Entrypoints.Provider
	case pluginmanifestv1.KindAuth:
		return manifest.Entrypoints.Auth
	case pluginmanifestv1.KindDatastore:
		return manifest.Entrypoints.Datastore
	case pluginmanifestv1.KindSecrets:
		return manifest.Entrypoints.Secrets
	default:
		return nil
	}
}

func EnsureEntrypointForKind(manifest *pluginmanifestv1.Manifest, kind string) *pluginmanifestv1.Entrypoint {
	switch kind {
	case pluginmanifestv1.KindPlugin:
		if manifest.Entrypoints.Provider == nil {
			manifest.Entrypoints.Provider = &pluginmanifestv1.Entrypoint{}
		}
		return manifest.Entrypoints.Provider
	case pluginmanifestv1.KindAuth:
		if manifest.Entrypoints.Auth == nil {
			manifest.Entrypoints.Auth = &pluginmanifestv1.Entrypoint{}
		}
		return manifest.Entrypoints.Auth
	case pluginmanifestv1.KindDatastore:
		if manifest.Entrypoints.Datastore == nil {
			manifest.Entrypoints.Datastore = &pluginmanifestv1.Entrypoint{}
		}
		return manifest.Entrypoints.Datastore
	case pluginmanifestv1.KindSecrets:
		if manifest.Entrypoints.Secrets == nil {
			manifest.Entrypoints.Secrets = &pluginmanifestv1.Entrypoint{}
		}
		return manifest.Entrypoints.Secrets
	default:
		return nil
	}
}

func validateEntrypoint(kind string, entry *pluginmanifestv1.Entrypoint, artifactPaths map[string]struct{}) error {
	if entry == nil {
		return fmt.Errorf("%s is required", EntrypointFieldForKind(kind))
	}
	if entry.ArtifactPath == "" {
		return fmt.Errorf("%s.artifact_path is required", EntrypointFieldForKind(kind))
	}
	if err := validateRelativePackagePath(entry.ArtifactPath, EntrypointFieldForKind(kind)+".artifact_path"); err != nil {
		return err
	}
	if len(artifactPaths) == 0 {
		return fmt.Errorf("%s references unknown artifact %q", EntrypointFieldForKind(kind), entry.ArtifactPath)
	}
	if _, ok := artifactPaths[entry.ArtifactPath]; !ok {
		return fmt.Errorf("%s references unknown artifact %q", EntrypointFieldForKind(kind), entry.ArtifactPath)
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

func validateReleaseMetadata(release *pluginmanifestv1.ReleaseMetadata) error {
	if release == nil || release.Build == nil {
		return nil
	}
	if release.Build.Workdir != "" {
		if err := validateRelativePackagePath(release.Build.Workdir, "release.build.workdir"); err != nil {
			return err
		}
	}
	if len(release.Build.Command) == 0 {
		return fmt.Errorf("release.build.command is required")
	}
	for i, arg := range release.Build.Command {
		if strings.TrimSpace(arg) == "" {
			return fmt.Errorf("release.build.command[%d] is required", i)
		}
	}
	return nil
}

func validateProviderAuth(path string, auth *pluginmanifestv1.ProviderAuth) error {
	if auth == nil {
		return nil
	}
	switch auth.Type {
	case pluginmanifestv1.AuthTypeOAuth2:
		if auth.AuthorizationURL == "" {
			return fmt.Errorf("%s.authorization_url is required for oauth2", path)
		}
		if auth.TokenURL == "" {
			return fmt.Errorf("%s.token_url is required for oauth2", path)
		}
	case pluginmanifestv1.AuthTypeMCPOAuth, pluginmanifestv1.AuthTypeBearer, pluginmanifestv1.AuthTypeManual, pluginmanifestv1.AuthTypeNone:
	default:
		return fmt.Errorf("unsupported %s.type %q", path, auth.Type)
	}
	return nil
}

func validateExecutableProviderMetadata(provider *pluginmanifestv1.Plugin) error {
	if provider == nil {
		return nil
	}
	if err := validateProviderAuth("provider.auth", provider.Auth); err != nil {
		return err
	}
	if provider.Auth != nil && provider.Auth.Type == pluginmanifestv1.AuthTypeMCPOAuth && provider.MCPURL == "" {
		return fmt.Errorf("provider.auth.type %q requires an MCP surface", pluginmanifestv1.AuthTypeMCPOAuth)
	}
	for name, conn := range provider.Connections {
		if conn == nil {
			continue
		}
		if err := validateProviderAuth(fmt.Sprintf("provider.connections.%s.auth", name), conn.Auth); err != nil {
			return err
		}
		if conn.Auth != nil && conn.Auth.Type == pluginmanifestv1.AuthTypeMCPOAuth && provider.MCPURL == "" {
			return fmt.Errorf("provider.connections.%s.auth.type %q requires an MCP surface", name, pluginmanifestv1.AuthTypeMCPOAuth)
		}
		if conn.Mode == "" {
			continue
		}
		switch conn.Mode {
		case "none", "user", "identity", "either":
		default:
			return fmt.Errorf("unsupported provider.connections.%s.mode %q", name, conn.Mode)
		}
	}
	switch provider.ConnectionMode {
	case "", "none", "user", "identity", "either":
	default:
		return fmt.Errorf("unsupported provider.connection_mode %q", provider.ConnectionMode)
	}
	if provider.DefaultConnection != "" {
		if _, ok := provider.Connections[provider.DefaultConnection]; !ok {
			return fmt.Errorf("provider.default_connection %q references undefined provider.connections entry", provider.DefaultConnection)
		}
	}
	if len(provider.Connections) > 0 {
		for name := range provider.Connections {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("provider.connections keys must be non-empty")
			}
		}
	}
	checks := []struct {
		field   string
		present bool
	}{
		{field: "provider.base_url", present: provider.BaseURL != "" && !provider.IsDeclarative() && provider.OpenAPI == ""},
		{field: "provider.operations", present: len(provider.Operations) > 0 && !provider.IsDeclarative()},
	}
	for _, check := range checks {
		if check.present {
			return fmt.Errorf("%s is no longer supported for executable providers", check.field)
		}
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

func validateDeclarativeProvider(provider *pluginmanifestv1.Plugin) error {
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
	return EncodeManifestFormat(manifest, ManifestFormatJSON)
}

func EncodeManifestFormat(manifest *pluginmanifestv1.Manifest, format string) ([]byte, error) {
	return encodeManifestFormat(manifest, format, false)
}

func EncodeSourceManifestFormat(manifest *pluginmanifestv1.Manifest, format string) ([]byte, error) {
	return encodeManifestFormat(manifest, format, true)
}

func encodeManifestFormat(manifest *pluginmanifestv1.Manifest, format string, sourceMode bool) ([]byte, error) {
	if err := validateManifest(manifest, sourceMode); err != nil {
		return nil, err
	}
	if manifest != nil && manifest.Plugin != nil && manifest.Auth == nil && manifest.Datastore == nil {
		return encodeProviderManifestWire(manifest, format)
	}
	switch format {
	case ManifestFormatJSON:
		return encodeManifestJSON(manifest)
	case ManifestFormatYAML:
		return encodeManifestYAML(manifest)
	default:
		return nil, fmt.Errorf("unsupported manifest format %q", format)
	}
}

func encodeManifestJSON(manifest *pluginmanifestv1.Manifest) ([]byte, error) {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	return append(data, '\n'), nil
}

func encodeManifestYAML(manifest *pluginmanifestv1.Manifest) ([]byte, error) {
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal manifest YAML: %w", err)
	}
	return data, nil
}

func decodeStrict(data []byte, format, subject string, target any) error {
	switch format {
	case ManifestFormatJSON:
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(target); err != nil {
			return fmt.Errorf("parse %s JSON: %w", subject, err)
		}
		return nil
	case ManifestFormatYAML:
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(target); err != nil {
			return fmt.Errorf("parse %s YAML: %w", subject, err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported %s format %q", subject, format)
	}
}

func normalizeLegacyManifestCompatibility(data []byte, format string) ([]byte, bool, error) {
	root := make(map[string]any)
	switch format {
	case ManifestFormatJSON:
		if err := json.Unmarshal(data, &root); err != nil {
			return nil, false, err
		}
	case ManifestFormatYAML:
		if err := yaml.Unmarshal(data, &root); err != nil {
			return nil, false, err
		}
	default:
		return nil, false, fmt.Errorf("unsupported manifest format %q", format)
	}

	changed := false
	if _, ok := root["kinds"]; ok {
		delete(root, "kinds")
		changed = true
	}
	if normalizeLegacyProviderResponseMapping(root) {
		changed = true
	}
	if !changed {
		return data, false, nil
	}

	switch format {
	case ManifestFormatJSON:
		normalized, err := json.MarshalIndent(root, "", "  ")
		if err != nil {
			return nil, false, err
		}
		return append(normalized, '\n'), true, nil
	case ManifestFormatYAML:
		normalized, err := yaml.Marshal(root)
		if err != nil {
			return nil, false, err
		}
		return normalized, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported manifest format %q", format)
	}
}

func normalizeLegacyProviderResponseMapping(root map[string]any) bool {
	provider, ok := root["provider"].(map[string]any)
	if !ok {
		return false
	}
	responseMapping, ok := provider["response_mapping"].(map[string]any)
	if !ok {
		return false
	}
	pagination, ok := responseMapping["pagination"].(map[string]any)
	if !ok {
		return false
	}

	changed := false
	if path, ok := pagination["has_more_path"].(string); ok {
		delete(pagination, "has_more_path")
		if _, exists := pagination["has_more"]; !exists {
			pagination["has_more"] = map[string]any{
				"source": "body",
				"path":   path,
			}
		}
		changed = true
	}
	if path, ok := pagination["cursor_path"].(string); ok {
		delete(pagination, "cursor_path")
		if _, exists := pagination["cursor"]; !exists {
			pagination["cursor"] = map[string]any{
				"source": "body",
				"path":   path,
			}
		}
		changed = true
	}
	return changed
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
