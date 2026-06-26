package packageio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/httpbinding"
	"github.com/valon-technologies/gestalt/server/services/apps/source"
	"gopkg.in/yaml.v3"
)

const ManifestFile = "manifest.json"

var ManifestFiles = []string{"manifest.json", "manifest.yaml", "manifest.yml"}

const (
	ManifestFormatJSON = "json"
	ManifestFormatYAML = "yaml"
)

func FindManifestFile(dir string) (string, error) {
	return FindManifestFileIn(dir, ManifestFiles)
}

func FindManifestFileIn(dir string, names []string) (string, error) {
	for _, name := range names {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no manifest file found in %s (tried %s)", dir, strings.Join(names, ", "))
}

func IsManifestFile(path string) bool {
	return IsManifestFileIn(path, ManifestFiles)
}

func IsManifestFileIn(path string, names []string) bool {
	base := filepath.Base(path)
	return slices.Contains(names, base)
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
	if _, err := normalizeManifestKind(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

var validManifestKinds = map[string]bool{
	providermanifestv1.KindApp:                 true,
	providermanifestv1.KindIdentity:            true,
	providermanifestv1.KindAuthorization:       true,
	providermanifestv1.KindExternalCredentials: true,
	providermanifestv1.KindIndexedDB:           true,
	providermanifestv1.KindCache:               true,
	providermanifestv1.KindS3:                  true,
	providermanifestv1.KindWorkflow:            true,
	providermanifestv1.KindAgent:               true,
	providermanifestv1.KindSecrets:             true,
	providermanifestv1.KindUI:                  true,
	providermanifestv1.KindRuntime:             true,
}

func ManifestKind(manifest *providermanifestv1.Manifest) (string, error) {
	if manifest == nil {
		return "", fmt.Errorf("manifest is required")
	}
	if manifest.Kind == "" {
		return "", fmt.Errorf("manifest kind is required")
	}
	kind := providermanifestv1.NormalizeKind(manifest.Kind)
	if !validManifestKinds[manifest.Kind] && !validManifestKinds[kind] {
		return "", fmt.Errorf("manifest kind %q is not valid; expected one of app, identity, authentication, authorization, externalcredentials, indexeddb, cache, s3, workflow, agent, secrets, runtime, or ui", manifest.Kind)
	}
	return kind, nil
}

func normalizeManifestKind(manifest *providermanifestv1.Manifest) (string, error) {
	kind, err := ManifestKind(manifest)
	if err != nil {
		return "", err
	}
	manifest.Kind = kind
	return kind, nil
}

func validateManifest(manifest *providermanifestv1.Manifest, sourceMode bool) error {
	if manifest == nil {
		return fmt.Errorf("manifest is required")
	}

	if manifest.Source == "" {
		return fmt.Errorf("manifest source is required")
	}
	if _, err := source.Parse(manifest.Source); err != nil {
		return fmt.Errorf("manifest source: %w", err)
	}
	if err := source.ValidateVersion(manifest.Version); err != nil {
		return fmt.Errorf("manifest version: %w", err)
	}
	if manifest.IconFile != "" {
		if err := validateRelativePackagePath(manifest.IconFile, "iconFile"); err != nil {
			return err
		}
	}
	kind, err := ManifestKind(manifest)
	if err != nil {
		return err
	}
	if manifest.Build != nil {
		if !sourceMode {
			return fmt.Errorf("build metadata is only allowed in source manifests")
		}
		if err := validateSourceBuild(manifest.Build); err != nil {
			return err
		}
	}
	if manifest.Install != nil {
		if !sourceMode {
			return fmt.Errorf("install metadata is only allowed in source manifests")
		}
		if err := validateSourceInstall(manifest.Install); err != nil {
			return err
		}
	}
	if manifest.Run != nil {
		if !sourceMode {
			return fmt.Errorf("run metadata is only allowed in source manifests")
		}
		if err := validateSourceRun(manifest.Run); err != nil {
			return err
		}
	}

	if sourceMode && len(manifest.Artifacts) > 0 {
		return fmt.Errorf("artifacts are not allowed in source manifests; prepared and released manifests generate them")
	}
	if err := validateSourceEntrypointDecl(manifest, kind, sourceMode); err != nil {
		return err
	}

	needsArtifacts := len(manifest.Artifacts) > 0
	switch kind {
	case providermanifestv1.KindApp:
		needsArtifacts = needsArtifacts || manifest.Entrypoint != nil
	case providermanifestv1.KindIdentity, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime:
		needsArtifacts = needsArtifacts || !sourceMode
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
	case providermanifestv1.KindApp:
		if spec == nil {
			return fmt.Errorf("spec is required for app manifests")
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
			if err := validateEntrypoint(kind, manifest.Entrypoint, artifactPaths, sourceMode); err != nil {
				return err
			}
		case manifest.Build != nil && !manifest.Build.PrepareOnly && !sourceMode:
			return fmt.Errorf("entrypoint is required when build is set")
		case manifest.IsDeclarativeOnlyProvider():
		case spec != nil && spec.IsSpecLoaded():
		case sourceMode:
		default:
			return fmt.Errorf("entrypoint is required")
		}
	case providermanifestv1.KindIdentity, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime:
		if manifest.Entrypoint == nil && !sourceMode {
			return fmt.Errorf("entrypoint is required")
		}
		if manifest.Entrypoint != nil {
			if err := validateEntrypoint(kind, manifest.Entrypoint, artifactPaths, sourceMode); err != nil {
				return err
			}
		}
	case providermanifestv1.KindUI:
		if manifest.Entrypoint != nil {
			return fmt.Errorf("ui manifests may not define entrypoints")
		}
		assetRoot := ""
		if spec != nil {
			assetRoot = spec.AssetRoot
		}
		if sourceMode {
			if manifest.Build == nil {
				return fmt.Errorf("build is required for source ui manifests")
			}
			if manifest.Build.PrepareOnly {
				return fmt.Errorf("source ui manifests require object-form build metadata")
			}
			if assetRoot == "" {
				return fmt.Errorf("spec.assetRoot is required for source ui manifests")
			}
		} else if assetRoot == "" {
			return fmt.Errorf("spec.assetRoot is required for ui manifests")
		}
		if assetRoot != "" {
			if err := validateRelativePackagePath(assetRoot, "spec.assetRoot"); err != nil {
				return err
			}
		}
		if spec != nil {
			if err := validateUIRoutes(spec.Routes); err != nil {
				return err
			}
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

func validateUIRoutes(routes []providermanifestv1.UIRoute) error {
	seenPaths := make(map[string]struct{}, len(routes))
	for i := range routes {
		normalized, err := NormalizeUIRoutePath(fmt.Sprintf("spec.routes[%d].path", i), routes[i].Path)
		if err != nil {
			return err
		}
		routes[i].Path = normalized
		if _, exists := seenPaths[normalized]; exists {
			return fmt.Errorf("spec.routes[%d].path %q duplicates another route", i, normalized)
		}
		seenPaths[normalized] = struct{}{}

		roles, err := NormalizeUIAllowedRoles(fmt.Sprintf("spec.routes[%d].allowedRoles", i), routes[i].AllowedRoles)
		if err != nil {
			return err
		}
		routes[i].AllowedRoles = roles
	}
	return nil
}

func ValidatePolicyBoundUIRoutes(routes []providermanifestv1.UIRoute) error {
	if len(routes) == 0 {
		return fmt.Errorf("policy-bound UIs must declare at least one route")
	}
	coversRoot := false
	for i := range routes {
		if len(routes[i].AllowedRoles) == 0 {
			return fmt.Errorf("spec.routes[%d].allowedRoles must not be empty", i)
		}
		if UIRouteMatches(routes[i].Path, "/") {
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

func validateEntrypoint(_ string, entry *providermanifestv1.Entrypoint, artifactPaths map[string]struct{}, sourceMode bool) error {
	if entry == nil {
		return fmt.Errorf("entrypoint is required")
	}
	if entry.ArtifactPath == "" {
		return fmt.Errorf("entrypoint.artifactPath is required")
	}
	if err := validateRelativePackagePath(entry.ArtifactPath, "entrypoint.artifactPath"); err != nil {
		return err
	}
	if sourceMode && len(artifactPaths) == 0 {
		return nil
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

func validateRelativeSourcePath(value, label string) error {
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	if strings.HasPrefix(value, "/") {
		return fmt.Errorf("%s must be relative", label)
	}
	cleaned := path.Clean(value)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
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

func validateSourceBuild(build *providermanifestv1.SourceBuild) error {
	if build == nil {
		return nil
	}
	if build.PrepareOnly && build.Workdir != "" {
		return fmt.Errorf("build.workdir is only supported with object-form build metadata")
	}
	if build.PrepareOnly && len(build.Inputs) > 0 {
		return fmt.Errorf("build.inputs is only supported with object-form build metadata")
	}
	if build.Workdir != "" {
		if err := validateRelativeSourcePath(build.Workdir, "build.workdir"); err != nil {
			return err
		}
	}
	if len(build.Command) == 0 {
		return fmt.Errorf("build.command is required")
	}
	for i, arg := range build.Command {
		if strings.TrimSpace(arg) == "" {
			return fmt.Errorf("build.command[%d] is required", i)
		}
	}
	for i, input := range build.Inputs {
		label := fmt.Sprintf("build.inputs[%d]", i)
		if strings.ContainsAny(input, "*?[") {
			return fmt.Errorf("%s does not support glob syntax", label)
		}
		if err := validateRelativeSourcePath(input, label); err != nil {
			return err
		}
	}
	return nil
}

func validateSourceInstall(install *providermanifestv1.SourceInstall) error {
	if install == nil {
		return nil
	}
	if install.Workdir != "" {
		if err := validateRelativeSourcePath(install.Workdir, "install.workdir"); err != nil {
			return err
		}
	}
	if len(install.Command) == 0 {
		return fmt.Errorf("install.command is required")
	}
	for i, arg := range install.Command {
		if strings.TrimSpace(arg) == "" {
			return fmt.Errorf("install.command[%d] is required", i)
		}
	}
	for i, input := range install.Inputs {
		label := fmt.Sprintf("install.inputs[%d]", i)
		if strings.ContainsAny(input, "*?[") {
			return fmt.Errorf("%s does not support glob syntax", label)
		}
		if err := validateRelativeSourcePath(input, label); err != nil {
			return err
		}
	}
	return nil
}

func validateSourceRun(run *providermanifestv1.SourceRun) error {
	if run == nil {
		return nil
	}
	if run.Workdir != "" {
		if err := validateRelativeSourcePath(run.Workdir, "run.workdir"); err != nil {
			return err
		}
	}
	if len(run.Command) == 0 {
		return fmt.Errorf("run.command is required")
	}
	for i, arg := range run.Command {
		if strings.TrimSpace(arg) == "" {
			return fmt.Errorf("run.command[%d] is required", i)
		}
	}
	if strings.TrimSpace(run.ReadyTimeout) != "" {
		if _, err := time.ParseDuration(strings.TrimSpace(run.ReadyTimeout)); err != nil {
			return fmt.Errorf("run.readyTimeout: %w", err)
		}
	}
	return nil
}

func SourceUIBuildOutput(manifest *providermanifestv1.Manifest) string {
	if manifest == nil {
		return ""
	}
	if manifest.Spec != nil {
		return manifest.Spec.AssetRoot
	}
	return ""
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

func validateRouteAuthRef(path string, auth *providermanifestv1.RouteAuthRef) error {
	if auth == nil {
		return nil
	}
	if strings.TrimSpace(auth.Provider) == "" {
		return fmt.Errorf("%s.provider is required", path)
	}
	return nil
}

func normalizeHTTPBindingPath(field, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if strings.Contains(value, "*") {
		return "", fmt.Errorf("%s must not contain wildcards", field)
	}
	cleaned := path.Clean(strings.ReplaceAll(value, "\\", "/"))
	if cleaned == "." {
		cleaned = "/"
	}
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	return cleaned, nil
}

func normalizeHTTPContentType(field, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s must not be empty", field)
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return "", fmt.Errorf("%s must be a valid media type", field)
	}
	return strings.ToLower(mediaType), nil
}

func validateHTTPSecurityScheme(path string, scheme *providermanifestv1.HTTPSecurityScheme) error {
	return httpbinding.ValidateHTTPSecurityScheme(path, scheme)
}

func validateHTTPBinding(path string, binding *providermanifestv1.HTTPBinding, schemes map[string]*providermanifestv1.HTTPSecurityScheme) error {
	if binding == nil {
		return fmt.Errorf("%s is required", path)
	}
	normalizedPath, err := normalizeHTTPBindingPath(path+".path", binding.Path)
	if err != nil {
		return err
	}
	binding.Path = normalizedPath
	method := strings.ToUpper(strings.TrimSpace(binding.Method))
	if !validHTTPMethods[method] {
		return fmt.Errorf("%s.method %q is not a valid HTTP method", path, binding.Method)
	}
	binding.Method = method
	binding.CredentialMode = providermanifestv1.NormalizeOptionalConnectionMode(binding.CredentialMode)
	switch binding.CredentialMode {
	case "", providermanifestv1.ConnectionModeNone, providermanifestv1.ConnectionModeSubject:
	default:
		return fmt.Errorf("%s.credentialMode %q is not supported", path, binding.CredentialMode)
	}
	if strings.TrimSpace(binding.Target) == "" {
		return fmt.Errorf("%s.target is required", path)
	}
	binding.Target = strings.TrimSpace(binding.Target)
	binding.Security = strings.TrimSpace(binding.Security)
	if binding.Security == "" {
		return fmt.Errorf("%s.security is required", path)
	}
	if schemes == nil || schemes[binding.Security] == nil {
		return fmt.Errorf("%s.security %q references an undefined security scheme", path, binding.Security)
	}
	if binding.RequestBody != nil {
		normalizedContent := make(map[string]*providermanifestv1.HTTPMediaType, len(binding.RequestBody.Content))
		for mediaType := range binding.RequestBody.Content {
			normalizedMediaType, err := normalizeHTTPContentType(path+".requestBody.content", mediaType)
			if err != nil {
				return err
			}
			if _, exists := normalizedContent[normalizedMediaType]; exists {
				return fmt.Errorf("%s.requestBody.content %q is duplicated after normalization", path, normalizedMediaType)
			}
			normalizedContent[normalizedMediaType] = binding.RequestBody.Content[mediaType]
		}
		binding.RequestBody.Content = normalizedContent
	}
	return nil
}

func validateExecutableProviderMetadata(provider *providermanifestv1.Spec) error {
	if provider == nil {
		return nil
	}
	if err := validateRouteAuthRef("provider.auth", provider.RouteAuth); err != nil {
		return err
	}
	for name, scheme := range provider.SecuritySchemes {
		if err := validateHTTPSecurityScheme(fmt.Sprintf("provider.securitySchemes.%s", name), scheme); err != nil {
			return err
		}
	}
	for name, binding := range provider.HTTP {
		if err := validateHTTPBinding(fmt.Sprintf("provider.http.%s", name), binding, provider.SecuritySchemes); err != nil {
			return err
		}
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
		conn.Mode = providermanifestv1.NormalizeOptionalConnectionMode(conn.Mode)
		switch conn.Mode {
		case "", providermanifestv1.ConnectionModeNone, providermanifestv1.ConnectionModeSubject:
		default:
			return fmt.Errorf("unsupported provider.connections.%s.mode %q", name, conn.Mode)
		}
		if conn.Exposure != "" {
			return fmt.Errorf("provider.connections.%s.exposure is not supported", name)
		}
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
	for i := range ops {
		op := &ops[i]
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
	encodedManifest, err := CloneManifest(manifest)
	if err != nil {
		return nil, fmt.Errorf("clone manifest: %w", err)
	}
	if err := validateManifest(encodedManifest, sourceMode); err != nil {
		return nil, err
	}
	if _, err := normalizeManifestKind(encodedManifest); err != nil {
		return nil, err
	}
	switch format {
	case ManifestFormatJSON:
		return encodeManifestJSON(encodedManifest)
	case ManifestFormatYAML:
		return encodeManifestYAML(encodedManifest)
	default:
		return nil, fmt.Errorf("unsupported manifest format %q", format)
	}
}

func CloneManifest(manifest *providermanifestv1.Manifest) (*providermanifestv1.Manifest, error) {
	if manifest == nil {
		return nil, nil
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}

	var cloned providermanifestv1.Manifest
	if err := json.Unmarshal(data, &cloned); err != nil {
		return nil, err
	}
	return &cloned, nil
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
