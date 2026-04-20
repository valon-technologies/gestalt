package providerpkg

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
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
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

func DecodeManifest(data []byte) (*providermanifestv1.Manifest, error) {
	return DecodeManifestFormat(data, ManifestFormatJSON)
}

func DecodeSourceManifestFormat(data []byte, format string) (*providermanifestv1.Manifest, error) {
	return decodeManifest(data, format, true)
}

func DecodeManifestFormat(data []byte, format string) (*providermanifestv1.Manifest, error) {
	return decodeManifest(data, format, false)
}

func decodeManifest(data []byte, format string, sourceMode bool) (*providermanifestv1.Manifest, error) {
	var manifest providermanifestv1.Manifest
	if err := decodeStrict(data, format, "manifest", &manifest); err != nil {
		return nil, err
	}
	if err := validateManifest(&manifest, sourceMode); err != nil {
		return nil, err
	}
	return &manifest, nil
}

var validManifestKinds = map[string]bool{
	providermanifestv1.KindPlugin:        true,
	providermanifestv1.KindAuth:          true,
	providermanifestv1.KindAuthorization: true,
	providermanifestv1.KindIndexedDB:     true,
	providermanifestv1.KindCache:         true,
	providermanifestv1.KindS3:            true,
	providermanifestv1.KindWorkflow:      true,
	providermanifestv1.KindSecrets:       true,
	providermanifestv1.KindWebUI:         true,
}

func ManifestKind(manifest *providermanifestv1.Manifest) (string, error) {
	if manifest == nil {
		return "", fmt.Errorf("manifest is required")
	}
	if manifest.Kind == "" {
		return "", fmt.Errorf("manifest kind is required")
	}
	if !validManifestKinds[manifest.Kind] {
		return "", fmt.Errorf("manifest kind %q is not valid; expected one of plugin, auth, authorization, indexeddb, cache, s3, workflow, secrets, or webui", manifest.Kind)
	}
	return manifest.Kind, nil
}

func validateManifest(manifest *providermanifestv1.Manifest, sourceMode bool) error {
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
		if err := validateRelativePackagePath(manifest.IconFile, "iconFile"); err != nil {
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

	allowsSourceEntrypointOmission := sourceMode && manifest.Entrypoint == nil

	needsArtifacts := len(manifest.Artifacts) > 0
	switch kind {
	case providermanifestv1.KindPlugin:
		needsArtifacts = needsArtifacts || manifest.Entrypoint != nil
	case providermanifestv1.KindAuth, providermanifestv1.KindAuthorization, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindSecrets:
		needsArtifacts = needsArtifacts || !allowsSourceEntrypointOmission
	}

	var artifactPaths map[string]struct{}
	if needsArtifacts {
		artifactPaths = make(map[string]struct{}, len(manifest.Artifacts))
		artifactPlatforms := make(map[string]struct{}, len(manifest.Artifacts))
		for _, artifact := range manifest.Artifacts {
			if err := validateRelativePackagePath(artifact.Path, "artifact path"); err != nil {
				return err
			}
			if artifact.SHA256 == "" && !sourceMode {
				return fmt.Errorf("artifact %s sha256 is required", artifact.Path)
			}
			if _, exists := artifactPaths[artifact.Path]; exists {
				return fmt.Errorf("duplicate artifact path %q", artifact.Path)
			}
			artifactPaths[artifact.Path] = struct{}{}

			key := PlatformString(artifact.OS, artifact.Arch)
			if _, exists := artifactPlatforms[key]; exists {
				return fmt.Errorf("duplicate artifact platform %q", key)
			}
			artifactPlatforms[key] = struct{}{}
		}
	}

	spec := manifest.Spec
	if spec != nil && spec.ConfigSchemaPath != "" {
		if err := validateRelativePackagePath(spec.ConfigSchemaPath, "spec config schema path"); err != nil {
			return err
		}
	}

	switch kind {
	case providermanifestv1.KindPlugin:
		if spec == nil {
			return fmt.Errorf("spec is required for plugin manifests")
		}
		if err := validateExecutableProviderMetadata(spec); err != nil {
			return err
		}
		if err := validateOwnedUI(spec.UI, sourceMode); err != nil {
			return err
		}
		if spec != nil && spec.IsDeclarative() {
			if err := validateDeclarativeProvider(spec); err != nil {
				return err
			}
		}
		switch {
		case manifest.Entrypoint != nil:
			if err := validateEntrypoint(kind, manifest.Entrypoint, artifactPaths); err != nil {
				return err
			}
		case manifest.IsDeclarativeOnlyProvider():
		case spec != nil && spec.IsSpecLoaded():
		case allowsSourceEntrypointOmission:
		default:
			return fmt.Errorf("entrypoint is required")
		}
	case providermanifestv1.KindAuth, providermanifestv1.KindAuthorization, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindSecrets:
		if manifest.Entrypoint == nil && !allowsSourceEntrypointOmission {
			return fmt.Errorf("entrypoint is required")
		}
		if manifest.Entrypoint != nil {
			if err := validateEntrypoint(kind, manifest.Entrypoint, artifactPaths); err != nil {
				return err
			}
		}
	case providermanifestv1.KindWebUI:
		if manifest.Entrypoint != nil {
			return fmt.Errorf("webui manifests may not define entrypoints")
		}
		if spec == nil || spec.AssetRoot == "" {
			return fmt.Errorf("spec.assetRoot is required for webui manifests")
		}
		if err := validateRelativePackagePath(spec.AssetRoot, "spec.assetRoot"); err != nil {
			return err
		}
		if err := validateWebUIRoutes(spec.Routes); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported manifest kind %q", kind)
	}

	return nil
}

func validateOwnedUI(ownedUI *providermanifestv1.OwnedUI, sourceMode bool) error {
	if ownedUI == nil {
		return nil
	}
	pathValue := strings.TrimSpace(ownedUI.Path)
	if pathValue != "" {
		if !sourceMode {
			if err := validateRelativePackagePath(pathValue, "spec.ui.path"); err != nil {
				return err
			}
			ownedUI.Path = pathValue
		} else {
			if filepath.IsAbs(pathValue) {
				return fmt.Errorf("spec.ui.path must be relative")
			}
			cleaned := path.Clean(filepath.ToSlash(pathValue))
			if cleaned == "." || cleaned == "" {
				return fmt.Errorf("spec.ui.path must not be empty")
			}
			ownedUI.Path = cleaned
		}
		return nil
	}
	return fmt.Errorf("spec.ui.path is required when spec.ui is set")
}

func validateWebUIRoutes(routes []providermanifestv1.WebUIRoute) error {
	seenPaths := make(map[string]struct{}, len(routes))
	for i := range routes {
		normalized, err := NormalizeWebUIRoutePath(fmt.Sprintf("spec.routes[%d].path", i), routes[i].Path)
		if err != nil {
			return err
		}
		routes[i].Path = normalized
		if _, exists := seenPaths[normalized]; exists {
			return fmt.Errorf("spec.routes[%d].path %q duplicates another route", i, normalized)
		}
		seenPaths[normalized] = struct{}{}

		roles, err := NormalizeWebUIAllowedRoles(fmt.Sprintf("spec.routes[%d].allowedRoles", i), routes[i].AllowedRoles)
		if err != nil {
			return err
		}
		routes[i].AllowedRoles = roles
	}
	return nil
}

func ValidatePolicyBoundWebUIRoutes(routes []providermanifestv1.WebUIRoute) error {
	if len(routes) == 0 {
		return fmt.Errorf("policy-bound UIs must declare at least one route")
	}
	coversRoot := false
	for i := range routes {
		if len(routes[i].AllowedRoles) == 0 {
			return fmt.Errorf("spec.routes[%d].allowedRoles must not be empty", i)
		}
		if WebUIRouteMatches(routes[i].Path, "/") {
			coversRoot = true
		}
	}
	if !coversRoot {
		return fmt.Errorf("policy-bound UIs must declare a route covering /")
	}
	return nil
}

func CurrentPlatformArtifact(manifest *providermanifestv1.Manifest) (*providermanifestv1.Artifact, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}
	for i := range manifest.Artifacts {
		a := &manifest.Artifacts[i]
		if a.OS == runtime.GOOS && a.Arch == runtime.GOARCH {
			return a, nil
		}
	}
	return nil, fmt.Errorf("no artifact for current platform %s/%s", runtime.GOOS, runtime.GOARCH)
}

func EntrypointForKind(manifest *providermanifestv1.Manifest, _ string) *providermanifestv1.Entrypoint {
	if manifest == nil {
		return nil
	}
	return manifest.Entrypoint
}

func EnsureEntrypoint(manifest *providermanifestv1.Manifest) *providermanifestv1.Entrypoint {
	if manifest.Entrypoint == nil {
		manifest.Entrypoint = &providermanifestv1.Entrypoint{}
	}
	return manifest.Entrypoint
}

func validateEntrypoint(_ string, entry *providermanifestv1.Entrypoint, artifactPaths map[string]struct{}) error {
	if entry == nil {
		return fmt.Errorf("entrypoint is required")
	}
	if entry.ArtifactPath == "" {
		return fmt.Errorf("entrypoint.artifactPath is required")
	}
	if err := validateRelativePackagePath(entry.ArtifactPath, "entrypoint.artifactPath"); err != nil {
		return err
	}
	if len(artifactPaths) == 0 {
		return fmt.Errorf("entrypoint references unknown artifact %q", entry.ArtifactPath)
	}
	if _, ok := artifactPaths[entry.ArtifactPath]; !ok {
		return fmt.Errorf("entrypoint references unknown artifact %q", entry.ArtifactPath)
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

func validateReleaseMetadata(release *providermanifestv1.ReleaseMetadata) error {
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

func validateProviderAuth(path string, auth *providermanifestv1.ProviderAuth) error {
	if auth == nil {
		return nil
	}
	switch auth.Type {
	case providermanifestv1.AuthTypeOAuth2:
		if auth.AuthorizationURL == "" {
			return fmt.Errorf("%s.authorizationUrl is required for oauth2", path)
		}
		if auth.TokenURL == "" {
			return fmt.Errorf("%s.tokenUrl is required for oauth2", path)
		}
	case providermanifestv1.AuthTypeMCPOAuth, providermanifestv1.AuthTypeBearer, providermanifestv1.AuthTypeManual, providermanifestv1.AuthTypeNone:
	default:
		return fmt.Errorf("unsupported %s.type %q", path, auth.Type)
	}
	return nil
}

func validateExecutableProviderMetadata(provider *providermanifestv1.Spec) error {
	if provider == nil {
		return nil
	}
	if err := validateProviderAuth("provider.auth", provider.Auth); err != nil {
		return err
	}
	if provider.Auth != nil && provider.Auth.Type == providermanifestv1.AuthTypeMCPOAuth && provider.MCPURL() == "" {
		return fmt.Errorf("provider.auth.type %q requires an MCP surface", providermanifestv1.AuthTypeMCPOAuth)
	}
	for name, conn := range provider.Connections {
		if conn == nil {
			continue
		}
		if err := validateProviderAuth(fmt.Sprintf("provider.connections.%s.auth", name), conn.Auth); err != nil {
			return err
		}
		if conn.Auth != nil && conn.Auth.Type == providermanifestv1.AuthTypeMCPOAuth && provider.MCPURL() == "" {
			return fmt.Errorf("provider.connections.%s.auth.type %q requires an MCP surface", name, providermanifestv1.AuthTypeMCPOAuth)
		}
		if conn.Mode == "" {
			continue
		}
		switch conn.Mode {
		case "none", "user", "identity":
		default:
			return fmt.Errorf("unsupported provider.connections.%s.mode %q", name, conn.Mode)
		}
	}
	switch provider.ConnectionMode {
	case "", "none", "user", "identity":
	default:
		return fmt.Errorf("unsupported provider.connectionMode %q", provider.ConnectionMode)
	}
	if provider.DefaultConnection != "" {
		if _, ok := provider.Connections[provider.DefaultConnection]; !ok {
			return fmt.Errorf("provider.defaultConnection %q references undefined provider.connections entry", provider.DefaultConnection)
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
		{field: "provider.baseUrl", present: provider.RESTBaseURL() != "" && !provider.IsDeclarative() && provider.OpenAPIDocument() == ""},
		{field: "provider.operations", present: len(provider.RESTOperations()) > 0 && !provider.IsDeclarative()},
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

func validateDeclarativeProvider(provider *providermanifestv1.Spec) error {
	if provider.RESTBaseURL() == "" {
		return fmt.Errorf("provider.baseUrl is required for declarative providers")
	}
	ops := provider.RESTOperations()
	seen := make(map[string]struct{}, len(ops))
	for i, op := range ops {
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

func EncodeManifest(manifest *providermanifestv1.Manifest) ([]byte, error) {
	return EncodeManifestFormat(manifest, ManifestFormatJSON)
}

func EncodeManifestFormat(manifest *providermanifestv1.Manifest, format string) ([]byte, error) {
	return encodeManifestFormat(manifest, format, false)
}

func EncodeSourceManifestFormat(manifest *providermanifestv1.Manifest, format string) ([]byte, error) {
	return encodeManifestFormat(manifest, format, true)
}

func encodeManifestFormat(manifest *providermanifestv1.Manifest, format string, sourceMode bool) ([]byte, error) {
	if err := validateManifest(manifest, sourceMode); err != nil {
		return nil, err
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

func encodeManifestJSON(manifest *providermanifestv1.Manifest) ([]byte, error) {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	return append(data, '\n'), nil
}

func encodeManifestYAML(manifest *providermanifestv1.Manifest) ([]byte, error) {
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

func ManifestEqual(a, b *providermanifestv1.Manifest) bool {
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
