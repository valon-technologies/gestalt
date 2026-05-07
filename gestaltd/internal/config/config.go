package config

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"maps"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/plugins/packageio"
	"gopkg.in/yaml.v3"
)

// Callback paths must match the routes registered in server.go.
const (
	AuthCallbackPath        = "/api/v1/auth/login/callback"
	IntegrationCallbackPath = "/api/v1/auth/callback"
)

const (
	DefaultProviderRepo = "github.com/valon-technologies/gestalt-providers"

	DefaultIndexedDBProvider           = DefaultProviderRepo + "/indexeddb/relationaldb"
	DefaultIndexedDBVersion            = "0.0.1-alpha.4"
	DefaultExternalCredentialsProvider = DefaultProviderRepo + "/external_credentials/default"
	DefaultExternalCredentialsVersion  = "0.0.1-alpha.5"
	DefaultUIProvider                  = DefaultProviderRepo + "/ui/default"
	DefaultUIVersion                   = "0.0.1-alpha.27"
	DefaultProviderInstance            = "default"
)

const PluginConnectionName = core.PluginConnectionName
const PluginConnectionAlias = core.PluginConnectionAlias
const ConfigAPIVersion = "gestaltd.config/v5"

type Config struct {
	APIVersion           string                              `yaml:"apiVersion,omitempty"`
	ProviderRepositories map[string]ProviderRepositoryConfig `yaml:"providerRepositories,omitempty"`
	Server               ServerConfig                        `yaml:"server"`
	Authorization        AuthorizationConfig                 `yaml:"authorization,omitempty"`
	Connections          map[string]*ConnectionDef           `yaml:"connections,omitempty"`
	Providers            ProvidersConfig                     `yaml:"providers"`
	Runtime              RuntimeConfig                       `yaml:"runtime,omitempty"`
	Workflows            WorkflowsConfig                     `yaml:"workflows,omitempty"`
	Plugins              map[string]*ProviderEntry           `yaml:"plugins,omitempty"`
}

type ProviderRepositoryConfig struct {
	URL string `yaml:"url,omitempty"`
}

type ProvidersConfig struct {
	Authentication      map[string]*ProviderEntry `yaml:"authentication,omitempty"`
	Authorization       map[string]*ProviderEntry `yaml:"authorization,omitempty"`
	ExternalCredentials map[string]*ProviderEntry `yaml:"externalCredentials,omitempty"`
	Secrets             map[string]*ProviderEntry `yaml:"secrets,omitempty"`
	Telemetry           map[string]*ProviderEntry `yaml:"telemetry,omitempty"`
	Audit               map[string]*ProviderEntry `yaml:"audit,omitempty"`
	UI                  map[string]*UIEntry       `yaml:"ui,omitempty"`
	IndexedDB           map[string]*ProviderEntry `yaml:"indexeddb,omitempty"`
	Cache               map[string]*ProviderEntry `yaml:"cache,omitempty"`
	S3                  map[string]*ProviderEntry `yaml:"s3,omitempty"`
	Workflow            map[string]*ProviderEntry `yaml:"workflow,omitempty"`
	Agent               map[string]*ProviderEntry `yaml:"agent,omitempty"`
}

type RuntimeConfig struct {
	Providers map[string]*RuntimeProviderEntry `yaml:"providers,omitempty"`
}

type RuntimeProviderDriver string

const RuntimeProviderDriverLocal RuntimeProviderDriver = "local"

type RuntimeProviderEntry struct {
	ProviderEntry `yaml:",inline"`
	Driver        RuntimeProviderDriver `yaml:"driver,omitempty"`
}

type runtimeProviderEntryYAML struct {
	providerEntryYAML `yaml:",inline"`
	Driver            RuntimeProviderDriver `yaml:"driver,omitempty"`
}

func (e *RuntimeProviderEntry) UnmarshalYAML(value *yaml.Node) error {
	var raw runtimeProviderEntryYAML
	if err := decodeYAMLNodeKnownFields(value, &raw); err != nil {
		return err
	}
	entry, err := raw.decode()
	if err != nil {
		return err
	}
	*e = RuntimeProviderEntry{
		ProviderEntry: entry,
		Driver:        raw.Driver,
	}
	return nil
}

type HostProviderKind string

const (
	HostProviderKindAuthentication      HostProviderKind = "authentication"
	HostProviderKindAuthorization       HostProviderKind = "authorization"
	HostProviderKindExternalCredentials HostProviderKind = "externalCredentials"
	HostProviderKindSecrets             HostProviderKind = "secrets"
	HostProviderKindTelemetry           HostProviderKind = "telemetry"
	HostProviderKindAudit               HostProviderKind = "audit"
	HostProviderKindIndexedDB           HostProviderKind = "indexeddb"
	HostProviderKindCache               HostProviderKind = "cache"
	HostProviderKindWorkflow            HostProviderKind = "workflow"
	HostProviderKindAgent               HostProviderKind = "agent"
	HostProviderKindRuntime             HostProviderKind = "runtime"
)

type ServerProvidersConfig struct {
	Authentication      string `yaml:"authentication,omitempty"`
	Authorization       string `yaml:"authorization,omitempty"`
	ExternalCredentials string `yaml:"externalCredentials,omitempty"`
	Secrets             string `yaml:"secrets,omitempty"`
	Telemetry           string `yaml:"telemetry,omitempty"`
	Audit               string `yaml:"audit,omitempty"`
	IndexedDB           string `yaml:"indexeddb,omitempty"`
}

// ProviderSource supports handwritten config in three forms via custom
// UnmarshalYAML:
//   - Builtin:  recognized host builtins such as source: "env" or "stdout"
//   - Metadata: source: "https://.../provider-release.yaml"  -> ProviderSource{metadataURL: "..."}
//   - GitHub:   source: {githubRelease: {repo, tag, asset}}  -> ProviderSource{GitHubRelease: ...}
//   - Local:    source: {path} or source: "./manifest.yaml"  -> ProviderSource{Path: "..."}
//   - Local metadata: source: {path} or source: "./dist/provider-release.yaml"
//     -> ProviderSource{metadataPath: "..."}
type ProviderSource struct {
	Builtin             string                  `yaml:"-"`
	scalar              string                  `yaml:"-"`
	metadataURL         string                  `yaml:"-"`
	metadataPath        string                  `yaml:"-"`
	packageRepo         string                  `yaml:"-"`
	packageName         string                  `yaml:"-"`
	packageVersion      string                  `yaml:"-"`
	resolvedMetadataURL string                  `yaml:"-"`
	resolvedVersion     string                  `yaml:"-"`
	unsupported         string                  `yaml:"-"`
	GitHubRelease       *GitHubReleaseSourceDef `yaml:"githubRelease,omitempty"`
	Path                string                  `yaml:"path,omitempty"`
	Auth                *SourceAuthDef          `yaml:"auth,omitempty"`
}

type providerSourceYAML struct {
	URL           string                  `yaml:"url,omitempty"`
	Repo          string                  `yaml:"repo,omitempty"`
	Package       string                  `yaml:"package,omitempty"`
	Version       string                  `yaml:"version,omitempty"`
	GitHubRelease *GitHubReleaseSourceDef `yaml:"githubRelease,omitempty"`
	Path          string                  `yaml:"path,omitempty"`
	Auth          *SourceAuthDef          `yaml:"auth,omitempty"`
}

type RouteAuthDef struct {
	Provider string `yaml:"provider,omitempty"`
}

type GitHubReleaseSourceDef struct {
	Repo  string `yaml:"repo,omitempty"`
	Tag   string `yaml:"tag,omitempty"`
	Asset string `yaml:"asset,omitempty"`
}

func (s *ProviderSource) UnmarshalYAML(value *yaml.Node) error {
	*s = ProviderSource{}
	if value.Kind == yaml.ScalarNode {
		scalar := strings.TrimSpace(value.Value)
		switch {
		case looksLikeMetadataURL(scalar):
			s.metadataURL = scalar
		case looksLikeUnsupportedScalarSource(scalar):
			s.unsupported = scalar
		default:
			s.scalar = scalar
		}
		return nil
	}
	if value.Kind == yaml.MappingNode {
		var raw providerSourceYAML
		if err := decodeYAMLNodeKnownFields(value, &raw); err != nil {
			return err
		}
		s.GitHubRelease = cloneGitHubReleaseSourceDef(raw.GitHubRelease)
		s.Path = strings.TrimSpace(raw.Path)
		s.metadataURL = strings.TrimSpace(raw.URL)
		s.packageRepo = strings.TrimSpace(raw.Repo)
		s.packageName = strings.TrimSpace(raw.Package)
		s.packageVersion = strings.TrimSpace(raw.Version)
		s.Auth = cloneSourceAuthDef(raw.Auth)
		return nil
	}
	var raw providerSourceYAML
	if err := decodeYAMLNodeKnownFields(value, &raw); err != nil {
		return err
	}
	s.GitHubRelease = cloneGitHubReleaseSourceDef(raw.GitHubRelease)
	s.Path = strings.TrimSpace(raw.Path)
	s.metadataURL = strings.TrimSpace(raw.URL)
	s.packageRepo = strings.TrimSpace(raw.Repo)
	s.packageName = strings.TrimSpace(raw.Package)
	s.packageVersion = strings.TrimSpace(raw.Version)
	s.Auth = cloneSourceAuthDef(raw.Auth)
	return nil
}

func (s ProviderSource) MarshalYAML() (any, error) {
	if s.Builtin != "" {
		return s.Builtin, nil
	}
	auth := cloneSourceAuthDef(s.Auth)
	if s.metadataURL != "" && s.Path == "" && s.metadataPath == "" {
		return providerSourceYAML{URL: s.metadataURL, Auth: auth}, nil
	}
	if s.packageName != "" && s.Path == "" && s.metadataPath == "" && s.metadataURL == "" && s.GitHubRelease == nil {
		return providerSourceYAML{
			Repo:    strings.TrimSpace(s.packageRepo),
			Package: strings.TrimSpace(s.packageName),
			Version: strings.TrimSpace(s.packageVersion),
			Auth:    auth,
		}, nil
	}
	if s.GitHubRelease != nil && s.Path == "" && s.metadataPath == "" {
		return providerSourceYAML{
			GitHubRelease: cloneGitHubReleaseSourceDef(s.GitHubRelease),
			Auth:          auth,
		}, nil
	}
	if s.scalar != "" && s.Path == "" && s.metadataPath == "" && auth == nil {
		return s.scalar, nil
	}
	if s.metadataPath != "" && s.Path == "" {
		return providerSourceYAML{Path: s.metadataPath, Auth: auth}, nil
	}
	return providerSourceYAML{
		URL:           s.metadataURL,
		Repo:          strings.TrimSpace(s.packageRepo),
		Package:       strings.TrimSpace(s.packageName),
		Version:       strings.TrimSpace(s.packageVersion),
		GitHubRelease: cloneGitHubReleaseSourceDef(s.GitHubRelease),
		Path:          s.Path,
		Auth:          auth,
	}, nil
}

func (s ProviderSource) IsBuiltin() bool       { return s.Builtin != "" }
func (s ProviderSource) IsMetadataURL() bool   { return s.metadataURL != "" }
func (s ProviderSource) IsGitHubRelease() bool { return s.GitHubRelease != nil }
func (s ProviderSource) IsPackage() bool       { return s.packageName != "" }
func (s ProviderSource) IsLocal() bool         { return s.Path != "" }
func (s ProviderSource) IsLocalMetadataPath() bool {
	return s.metadataPath != ""
}
func (s ProviderSource) MetadataURL() string              { return s.metadataURL }
func (s ProviderSource) MetadataPath() string             { return s.metadataPath }
func (s ProviderSource) PackageRepo() string              { return strings.TrimSpace(s.packageRepo) }
func (s ProviderSource) PackageAddress() string           { return strings.TrimSpace(s.packageName) }
func (s ProviderSource) PackageVersionConstraint() string { return strings.TrimSpace(s.packageVersion) }
func (s ProviderSource) ResolvedPackageMetadataURL() string {
	return strings.TrimSpace(s.resolvedMetadataURL)
}
func (s ProviderSource) ResolvedPackageVersion() string { return strings.TrimSpace(s.resolvedVersion) }
func (s *ProviderSource) SetResolvedPackage(metadataURL, version string) {
	if s == nil {
		return
	}
	s.resolvedMetadataURL = strings.TrimSpace(metadataURL)
	s.resolvedVersion = strings.TrimSpace(version)
}
func (s ProviderSource) GitHubReleaseSource() *GitHubReleaseSourceDef {
	return cloneGitHubReleaseSourceDef(s.GitHubRelease)
}
func (s ProviderSource) UnsupportedURL() string {
	return s.unsupported
}

func (s ProviderSource) GitHubReleaseLocation() string {
	if s.GitHubRelease == nil {
		return ""
	}
	return s.GitHubRelease.Location()
}

func NewMetadataSource(rawURL string) ProviderSource {
	return ProviderSource{metadataURL: strings.TrimSpace(rawURL)}
}

func DefaultProviderMetadataURL(source, version string) string {
	rel := strings.TrimPrefix(strings.TrimSpace(source), DefaultProviderRepo+"/")
	return fmt.Sprintf("https://github.com/valon-technologies/gestalt-providers/releases/download/%s/v%s/provider-release.yaml", rel, strings.TrimSpace(version))
}

func DefaultLocalProviderManifestPath(providersDir, source string) string {
	return defaultLocalProviderPath(providersDir, source, "manifest.yaml")
}

func defaultLocalProviderMetadataPath(providersDir, source string) string {
	metadataPath := defaultLocalProviderPath(providersDir, source, "provider-release.yaml")
	if metadataPath == "" {
		return ""
	}
	if _, err := os.Stat(metadataPath); err == nil {
		return metadataPath
	}
	return ""
}

func defaultLocalProviderPath(providersDir, source, fileName string) string {
	providersDir = strings.TrimSpace(providersDir)
	if providersDir == "" {
		return ""
	}
	if trimmed, ok := strings.CutPrefix(strings.TrimSpace(source), DefaultProviderRepo+"/"); ok {
		source = trimmed
	}
	return filepath.Join(providersDir, filepath.FromSlash(source), fileName)
}

func DefaultProviderSource(source, version string) ProviderSource {
	if metadataPath := defaultLocalProviderMetadataPath(os.Getenv("GESTALT_PROVIDERS_DIR"), source); metadataPath != "" {
		return NewLocalReleaseMetadataSource(metadataPath)
	}
	return NewMetadataSource(DefaultProviderMetadataURL(source, version))
}

func NewLocalReleaseMetadataSource(rawPath string) ProviderSource {
	return ProviderSource{metadataPath: strings.TrimSpace(rawPath)}
}

func cloneSourceAuthDef(src *SourceAuthDef) *SourceAuthDef {
	if src == nil {
		return nil
	}
	cloned := *src
	cloned.Token = strings.TrimSpace(cloned.Token)
	return &cloned
}

func cloneGitHubReleaseSourceDef(src *GitHubReleaseSourceDef) *GitHubReleaseSourceDef {
	if src == nil {
		return nil
	}
	cloned := *src
	cloned.Repo = strings.TrimSpace(cloned.Repo)
	cloned.Tag = strings.TrimSpace(cloned.Tag)
	cloned.Asset = strings.TrimSpace(cloned.Asset)
	return &cloned
}

func (g GitHubReleaseSourceDef) Location() string {
	repo := strings.TrimSpace(g.Repo)
	tag := strings.TrimSpace(g.Tag)
	asset := strings.TrimSpace(g.Asset)
	if repo == "" || tag == "" || asset == "" {
		return ""
	}
	return (&url.URL{
		Scheme: "github-release",
		Host:   "github.com",
		Path:   "/" + repo,
		RawQuery: url.Values{
			"tag":   []string{tag},
			"asset": []string{asset},
		}.Encode(),
	}).String()
}

// ProviderEntry is the universal configuration for any provider.
type ProviderEntry struct {
	Source          ProviderSource                         `yaml:"source"`
	Config          yaml.Node                              `yaml:"config,omitempty"`
	Default         bool                                   `yaml:"default,omitempty"`
	Env             map[string]string                      `yaml:"env,omitempty"`
	Egress          *ProviderEgressConfig                  `yaml:"egress,omitempty"`
	DisplayName     string                                 `yaml:"displayName,omitempty"`
	Description     string                                 `yaml:"description,omitempty"`
	IconFile        string                                 `yaml:"iconFile,omitempty"`
	DefaultHarness  string                                 `yaml:"defaultHarness,omitempty"`
	Harnesses       map[string]*ProviderEntryHarnessConfig `yaml:"harnesses,omitempty"`
	LocalHarness    *ProviderEntryHarnessConfig            `yaml:"localHarness,omitempty"`
	Lifecycle       *AgentProviderLifecycleConfig          `yaml:"lifecycle,omitempty"`
	RouteAuth       *RouteAuthDef                          `yaml:"-"`
	SecuritySchemes map[string]*HTTPSecurityScheme         `yaml:"securitySchemes,omitempty"`
	HTTP            map[string]*HTTPBinding                `yaml:"http,omitempty"`
	// AuthorizationPolicy binds this provider to a shared subject access policy.
	AuthorizationPolicy string                  `yaml:"authorizationPolicy,omitempty"`
	Dev                 *ProviderEntryDevConfig `yaml:"dev,omitempty"`

	// Plugin-specific runtime fields populated from the canonical ui object.
	MountPath         string                        `yaml:"-"`
	UI                string                        `yaml:"-"`
	Connections       map[string]*ConnectionDef     `yaml:"connections,omitempty"`
	AllowedOperations map[string]*OperationOverride `yaml:"allowedOperations,omitempty"`
	Invokes           []PluginInvocationDependency  `yaml:"invokes,omitempty"`
	IndexedDB         *HostIndexedDBBindingConfig   `yaml:"indexeddb,omitempty"`
	Cache             []string                      `yaml:"cache,omitempty"`
	S3                []string                      `yaml:"s3,omitempty"`
	Runtime           *RuntimePlacementConfig       `yaml:"runtime,omitempty"`
	Surfaces          *ProviderSurfaceOverrides     `yaml:"surfaces,omitempty"`
	MCP               bool                          `yaml:"mcp,omitempty"`

	// Runtime-resolved fields (populated during init/bootstrap, not from YAML)
	Command                    string                                `yaml:"-"`
	Args                       []string                              `yaml:"-"`
	ResolvedManifestPath       string                                `yaml:"-"`
	ResolvedManifest           *providermanifestv1.Manifest          `yaml:"-"`
	ResolvedCatalog            *catalog.Catalog                      `yaml:"-"`
	ResolvedCatalogAvailable   bool                                  `yaml:"-"`
	ResolvedCatalogSessionOnly bool                                  `yaml:"-"`
	StaticManifestUnavailable  bool                                  `yaml:"-"`
	ResolvedIconFile           string                                `yaml:"-"`
	HostBinary                 string                                `yaml:"-"`
	ConnectionMode             providermanifestv1.ConnectionMode     `yaml:"-"`
	Auth                       *ConnectionAuthDef                    `yaml:"-"`
	DefaultConnection          string                                `yaml:"-"`
	ConnectionParams           map[string]ConnectionParamDef         `yaml:"-"`
	Discovery                  *providermanifestv1.ProviderDiscovery `yaml:"-"`
	ResolvedAssetRoot          string                                `yaml:"-"`
	MCPToolPrefix              string                                `yaml:"-"`
}

type providerEntryFields ProviderEntry

// ProviderEntryHarnessConfig describes a process harness that can start an
// agent provider outside gestaltd's hosted runtime.
type ProviderEntryHarnessConfig struct {
	Command          string                             `yaml:"command,omitempty"`
	Args             []string                           `yaml:"args,omitempty"`
	Env              map[string]string                  `yaml:"env,omitempty"`
	WorkingDirectory string                             `yaml:"workingDirectory,omitempty"`
	RequiredCommands []string                           `yaml:"requiredCommands,omitempty"`
	Install          *ProviderEntryHarnessInstallConfig `yaml:"install,omitempty"`
}

// ProviderEntryLocalHarnessConfig is kept as a compatibility alias for configs
// that still use providers.agent.<name>.localHarness.
type ProviderEntryLocalHarnessConfig = ProviderEntryHarnessConfig

// ProviderEntryHarnessInstallConfig describes optional local install guidance
// for commands required by an agent harness.
type ProviderEntryHarnessInstallConfig struct {
	Instructions string                               `yaml:"instructions,omitempty"`
	Commands     []ProviderEntryHarnessInstallCommand `yaml:"commands,omitempty"`
}

type ProviderEntryHarnessInstallCommand struct {
	Description string            `yaml:"description,omitempty"`
	Command     string            `yaml:"command,omitempty"`
	Args        []string          `yaml:"args,omitempty"`
	Shell       string            `yaml:"shell,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
}

type AgentProviderLifecycleConfig struct {
	SessionStart []AgentProviderSessionStartHookConfig `yaml:"sessionStart,omitempty"`
}

type AgentProviderSessionStartHookConfig struct {
	ID      string                                    `yaml:"id,omitempty"`
	Type    string                                    `yaml:"type,omitempty"`
	Command []string                                  `yaml:"command,omitempty"`
	CWD     string                                    `yaml:"cwd,omitempty"`
	Timeout string                                    `yaml:"timeout,omitempty"`
	Env     map[string]string                         `yaml:"env,omitempty"`
	Output  AgentProviderSessionStartHookOutputConfig `yaml:"output,omitempty"`
}

type AgentProviderSessionStartHookOutputConfig struct {
	AdditionalContext bool `yaml:"additionalContext,omitempty"`
	Metadata          bool `yaml:"metadata,omitempty"`
}

type ProviderEntryDevConfig struct {
	Attach ProviderEntryDevAttachConfig `yaml:"attach,omitempty"`
}

type ProviderEntryDevAttachConfig struct {
	AllowedRoles []string `yaml:"allowedRoles,omitempty"`
}

type providerEntryYAML struct {
	providerEntryFields `yaml:",inline"`
	Auth                *RouteAuthDef `yaml:"auth,omitempty"`
}

type providerEntryMarshalYAML struct {
	providerEntryFields `yaml:",inline"`
	Auth                *RouteAuthDef          `yaml:"auth,omitempty"`
	UI                  *pluginUIBindingConfig `yaml:"ui,omitempty"`
}

type uiEntryYAML struct {
	providerEntryYAML `yaml:",inline"`
	Path              string `yaml:"path,omitempty"`
}

type uiEntryMarshalYAML struct {
	providerEntryMarshalYAML `yaml:",inline"`
	Path                     string `yaml:"path,omitempty"`
}

func (e *ProviderEntry) UnmarshalYAML(value *yaml.Node) error {
	normalized := cloneYAMLNode(value)
	if mappingValueNode(normalized, "execution") != nil {
		return fmt.Errorf("config validation: provider execution has been removed; use runtime instead")
	}
	uiBinding, err := normalizeProviderEntryUINode(normalized)
	if err != nil {
		return err
	}
	var raw providerEntryYAML
	if err := decodeYAMLNodeKnownFields(normalized, &raw); err != nil {
		return err
	}
	decoded, err := raw.decode()
	if err != nil {
		return err
	}
	if uiBinding != nil {
		uiBinding.Path = strings.TrimSpace(uiBinding.Path)
		uiBinding.Bundle = strings.TrimSpace(uiBinding.Bundle)
		if uiBinding.Bundle != "" {
			decoded.UI = uiBinding.Bundle
		}
		if uiBinding.Path != "" {
			decoded.MountPath = uiBinding.Path
		}
	}
	*e = decoded
	return nil
}

func (e ProviderEntry) MarshalYAML() (any, error) {
	raw := providerEntryMarshalYAML{
		providerEntryFields: providerEntryFieldsFromEntry(e),
		Auth:                cloneRouteAuthDef(e.RouteAuth),
	}
	if strings.TrimSpace(e.MountPath) != "" {
		raw.UI = &pluginUIBindingConfig{
			Bundle: strings.TrimSpace(e.UI),
			Path:   strings.TrimSpace(e.MountPath),
		}
	}
	return raw, nil
}

type ProviderSurfaceOverrides struct {
	REST    *ProviderRESTSurfaceOverride    `yaml:"rest,omitempty"`
	OpenAPI *ProviderOpenAPISurfaceOverride `yaml:"openapi,omitempty"`
	GraphQL *ProviderGraphQLSurfaceOverride `yaml:"graphql,omitempty"`
	MCP     *ProviderMCPSurfaceOverride     `yaml:"mcp,omitempty"`
}

type HTTPSecurityScheme = providermanifestv1.HTTPSecurityScheme
type HTTPBinding = providermanifestv1.HTTPBinding

type HostIndexedDBBindingConfig struct {
	Provider     string   `yaml:"provider,omitempty"`
	DB           string   `yaml:"db,omitempty"`
	ObjectStores []string `yaml:"objectStores,omitempty"`
}

type pluginUIBindingConfig struct {
	Path   string `yaml:"path,omitempty"`
	Bundle string `yaml:"bundle,omitempty"`
}

type ProviderEgressConfig struct {
	AllowedHosts []string `yaml:"allowedHosts,omitempty"`
}

type RuntimePlacementConfig struct {
	Provider      string                           `yaml:"provider,omitempty"`
	Template      string                           `yaml:"template,omitempty"`
	Image         string                           `yaml:"image,omitempty"`
	ImagePullAuth *RuntimePlacementImagePullAuth   `yaml:"imagePullAuth,omitempty"`
	Metadata      map[string]string                `yaml:"metadata,omitempty"`
	Pool          *RuntimePlacementPoolConfig      `yaml:"pool,omitempty"`
	Workspace     *RuntimePlacementWorkspaceConfig `yaml:"workspace,omitempty"`
}

type RuntimePlacementImagePullAuth struct {
	DockerConfigJSON string `yaml:"dockerConfigJson,omitempty"`
}

type RuntimePlacementWorkspaceConfig struct {
	PrepareTimeout string                              `yaml:"prepareTimeout,omitempty"`
	Git            *RuntimePlacementWorkspaceGitConfig `yaml:"git,omitempty"`
}

type RuntimePlacementWorkspaceGitConfig struct {
	AllowedRepositories []string `yaml:"allowedRepositories,omitempty"`
}

type RuntimePlacementPoolConfig struct {
	MinReadyInstances   int                           `yaml:"minReadyInstances,omitempty"`
	MaxReadyInstances   int                           `yaml:"maxReadyInstances,omitempty"`
	StartupTimeout      string                        `yaml:"startupTimeout,omitempty"`
	HealthCheckInterval string                        `yaml:"healthCheckInterval,omitempty"`
	RestartPolicy       RuntimePlacementRestartPolicy `yaml:"restartPolicy,omitempty"`
	DrainTimeout        string                        `yaml:"drainTimeout,omitempty"`
}

type RuntimePlacementRestartPolicy string

const (
	RuntimePlacementRestartPolicyAlways RuntimePlacementRestartPolicy = "always"
	RuntimePlacementRestartPolicyNever  RuntimePlacementRestartPolicy = "never"
)

type RuntimePlacementLifecyclePolicy struct {
	MinReadyInstances   int
	MaxReadyInstances   int
	StartupTimeout      time.Duration
	HealthCheckInterval time.Duration
	RestartPolicy       RuntimePlacementRestartPolicy
	DrainTimeout        time.Duration
}

func (c *RuntimePlacementConfig) LifecyclePolicyFieldsSet() bool {
	if c == nil {
		return false
	}
	return c.Pool != nil && c.Pool.lifecyclePolicyFieldsSet()
}

func (c *RuntimePlacementPoolConfig) lifecyclePolicyFieldsSet() bool {
	if c == nil {
		return false
	}
	return c.MinReadyInstances != 0 ||
		c.MaxReadyInstances != 0 ||
		strings.TrimSpace(c.StartupTimeout) != "" ||
		strings.TrimSpace(c.HealthCheckInterval) != "" ||
		strings.TrimSpace(string(c.RestartPolicy)) != "" ||
		strings.TrimSpace(c.DrainTimeout) != ""
}

func (c *RuntimePlacementConfig) LifecyclePolicy() (RuntimePlacementLifecyclePolicy, error) {
	if c == nil {
		return RuntimePlacementLifecyclePolicy{}, fmt.Errorf("runtime config is required")
	}
	lifecycle := c.lifecyclePolicyConfig()
	startupTimeout, err := ParseDuration(strings.TrimSpace(lifecycle.StartupTimeout))
	if err != nil {
		return RuntimePlacementLifecyclePolicy{}, fmt.Errorf("startupTimeout: %w", err)
	}
	healthCheckInterval, err := ParseDuration(strings.TrimSpace(lifecycle.HealthCheckInterval))
	if err != nil {
		return RuntimePlacementLifecyclePolicy{}, fmt.Errorf("healthCheckInterval: %w", err)
	}
	drainTimeout, err := ParseDuration(strings.TrimSpace(lifecycle.DrainTimeout))
	if err != nil {
		return RuntimePlacementLifecyclePolicy{}, fmt.Errorf("drainTimeout: %w", err)
	}
	return RuntimePlacementLifecyclePolicy{
		MinReadyInstances:   lifecycle.MinReadyInstances,
		MaxReadyInstances:   lifecycle.MaxReadyInstances,
		StartupTimeout:      startupTimeout,
		HealthCheckInterval: healthCheckInterval,
		RestartPolicy:       RuntimePlacementRestartPolicy(strings.TrimSpace(string(lifecycle.RestartPolicy))),
		DrainTimeout:        drainTimeout,
	}, nil
}

func (c *RuntimePlacementConfig) lifecyclePolicyConfig() RuntimePlacementPoolConfig {
	if c == nil {
		return RuntimePlacementPoolConfig{}
	}
	if c.Pool != nil {
		return *c.Pool
	}
	return RuntimePlacementPoolConfig{}
}

type WorkflowsConfig struct {
	Schedules     map[string]WorkflowScheduleConfig     `yaml:"schedules,omitempty"`
	EventTriggers map[string]WorkflowEventTriggerConfig `yaml:"eventTriggers,omitempty"`
}

type WorkflowScheduleConfig struct {
	Provider    string                  `yaml:"provider,omitempty"`
	Target      *WorkflowTargetConfig   `yaml:"target,omitempty"`
	Permissions []core.AccessPermission `yaml:"permissions,omitempty"`
	Cron        string                  `yaml:"cron,omitempty"`
	Timezone    string                  `yaml:"timezone,omitempty"`
	Paused      bool                    `yaml:"paused,omitempty"`
}

type WorkflowEventTriggerConfig struct {
	Provider    string                  `yaml:"provider,omitempty"`
	Target      *WorkflowTargetConfig   `yaml:"target,omitempty"`
	Permissions []core.AccessPermission `yaml:"permissions,omitempty"`
	Match       WorkflowEventMatch      `yaml:"match,omitempty"`
	Paused      bool                    `yaml:"paused,omitempty"`
}

type WorkflowTargetConfig struct {
	Plugin *WorkflowPluginTargetConfig `yaml:"plugin,omitempty"`
	Agent  *WorkflowAgentConfig        `yaml:"agent,omitempty"`
}

type WorkflowPluginTargetConfig struct {
	Name           string                            `yaml:"name,omitempty"`
	Operation      string                            `yaml:"operation,omitempty"`
	Connection     string                            `yaml:"connection,omitempty"`
	Instance       string                            `yaml:"instance,omitempty"`
	CredentialMode providermanifestv1.ConnectionMode `yaml:"credentialMode,omitempty"`
	Input          map[string]any                    `yaml:"input,omitempty"`
}

type WorkflowAgentConfig struct {
	Provider       string                        `yaml:"provider,omitempty"`
	Model          string                        `yaml:"model,omitempty"`
	Prompt         string                        `yaml:"prompt,omitempty"`
	Messages       []WorkflowAgentMessage        `yaml:"messages,omitempty"`
	Tools          []WorkflowAgentToolRef        `yaml:"tools,omitempty"`
	OutputDelivery *WorkflowOutputDeliveryConfig `yaml:"outputDelivery,omitempty"`
	ResponseSchema map[string]any                `yaml:"responseSchema,omitempty"`
	Metadata       map[string]any                `yaml:"metadata,omitempty"`
	ModelOptions   map[string]any                `yaml:"modelOptions,omitempty"`
	Timeout        string                        `yaml:"timeout,omitempty"`
}

type WorkflowOutputDeliveryConfig struct {
	Target         WorkflowPluginTargetConfig        `yaml:"target,omitempty"`
	InputBindings  []WorkflowOutputBindingConfig     `yaml:"inputBindings,omitempty"`
	CredentialMode providermanifestv1.ConnectionMode `yaml:"credentialMode,omitempty"`
}

type WorkflowOutputBindingConfig struct {
	InputField string                          `yaml:"inputField,omitempty"`
	Value      WorkflowOutputValueSourceConfig `yaml:"value,omitempty"`
}

type WorkflowOutputValueSourceConfig struct {
	AgentOutput    string `yaml:"agentOutput,omitempty"`
	SignalPayload  string `yaml:"signalPayload,omitempty"`
	SignalMetadata string `yaml:"signalMetadata,omitempty"`
	Literal        any    `yaml:"literal,omitempty"`
}

type WorkflowAgentMessage struct {
	Role     string         `yaml:"role,omitempty"`
	Text     string         `yaml:"text,omitempty"`
	Metadata map[string]any `yaml:"metadata,omitempty"`
}

type WorkflowAgentToolRef struct {
	System      string `yaml:"system,omitempty"`
	Plugin      string `yaml:"plugin,omitempty"`
	Operation   string `yaml:"operation,omitempty"`
	Connection  string `yaml:"connection,omitempty"`
	Instance    string `yaml:"instance,omitempty"`
	Title       string `yaml:"title,omitempty"`
	Description string `yaml:"description,omitempty"`
}

type WorkflowEventMatch struct {
	Type    string `yaml:"type,omitempty"`
	Source  string `yaml:"source,omitempty"`
	Subject string `yaml:"subject,omitempty"`
}

func (c *HostIndexedDBBindingConfig) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		c.Provider = strings.TrimSpace(value.Value)
		return nil
	case yaml.SequenceNode:
		return fmt.Errorf("indexeddb must be a mapping or scalar provider name")
	default:
		type raw HostIndexedDBBindingConfig
		return decodeYAMLNodeKnownFields(value, (*raw)(c))
	}
}

type ProviderRESTSurfaceOverride struct {
	BaseURL string `yaml:"baseUrl,omitempty"`
}

type ProviderOpenAPISurfaceOverride struct {
	BaseURL string `yaml:"baseUrl,omitempty"`
}

type ProviderGraphQLSurfaceOverride struct {
	URL string `yaml:"url,omitempty"`
}

type ProviderMCPSurfaceOverride struct {
	URL string `yaml:"url,omitempty"`
}

// UIEntry configures a mounted web UI bundle served under a fixed path prefix.
type UIEntry struct {
	ProviderEntry `yaml:",inline"`
	Path          string `yaml:"path,omitempty"`
	OwnerPlugin   string `yaml:"-"`
}

func (e *UIEntry) UnmarshalYAML(value *yaml.Node) error {
	var raw uiEntryYAML
	if err := decodeYAMLNodeKnownFields(value, &raw); err != nil {
		return err
	}
	decoded, err := raw.decode()
	if err != nil {
		return err
	}
	*e = UIEntry{
		ProviderEntry: decoded,
		Path:          raw.Path,
	}
	return nil
}

func (e UIEntry) MarshalYAML() (any, error) {
	raw, err := e.ProviderEntry.MarshalYAML()
	if err != nil {
		return nil, err
	}
	entry, ok := raw.(providerEntryMarshalYAML)
	if !ok {
		return nil, fmt.Errorf("marshal ui entry: unexpected provider entry shape %T", raw)
	}
	return uiEntryMarshalYAML{
		providerEntryMarshalYAML: entry,
		Path:                     e.Path,
	}, nil
}

func (raw providerEntryYAML) decode() (ProviderEntry, error) {
	entry := raw.toProviderEntry()
	entry.RouteAuth = cloneRouteAuthDef(raw.Auth)
	normalizeProviderEntryAliases(&entry)
	return entry, nil
}

func decodeYAMLNodeKnownFields(node *yaml.Node, out any) error {
	data, err := yaml.Marshal(node)
	if err != nil {
		return fmt.Errorf("marshal yaml node: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil && err != io.EOF {
		return err
	}
	return nil
}

func cloneYAMLNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	cloned := *node
	if len(node.Content) > 0 {
		cloned.Content = make([]*yaml.Node, len(node.Content))
		for i, child := range node.Content {
			cloned.Content[i] = cloneYAMLNode(child)
		}
	}
	return &cloned
}

func normalizeProviderEntryUINode(node *yaml.Node) (*pluginUIBindingConfig, error) {
	raw := documentValueNode(node)
	if raw == nil || raw.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(raw.Content); i += 2 {
		key := raw.Content[i]
		value := raw.Content[i+1]
		if key == nil || strings.TrimSpace(key.Value) != "ui" {
			continue
		}
		if value == nil || value.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("ui must be an object with path")
		}
		var binding pluginUIBindingConfig
		if err := decodeYAMLNodeKnownFields(value, &binding); err != nil {
			return nil, err
		}
		binding.Path = strings.TrimSpace(binding.Path)
		binding.Bundle = strings.TrimSpace(binding.Bundle)
		if binding.Path == "" {
			return nil, fmt.Errorf("ui.path is required when ui is an object")
		}
		if mappingValueNode(raw, "mountPath") != nil {
			return nil, fmt.Errorf("mountPath is no longer supported; use ui.path")
		}
		raw.Content = append(raw.Content[:i], raw.Content[i+2:]...)
		return &binding, nil
	}
	if mappingValueNode(raw, "mountPath") != nil {
		return nil, fmt.Errorf("mountPath is no longer supported; use ui.path")
	}
	return nil, nil
}

func cloneRouteAuthDef(src *RouteAuthDef) *RouteAuthDef {
	if src == nil {
		return nil
	}
	cloned := *src
	cloned.Provider = strings.TrimSpace(cloned.Provider)
	return &cloned
}

func (f providerEntryFields) toProviderEntry() ProviderEntry {
	return ProviderEntry(f)
}

func providerEntryFieldsFromEntry(e ProviderEntry) providerEntryFields {
	e.Egress = cloneProviderEgressConfig(e.Egress)
	e.Runtime = cloneRuntimePlacementConfig(e.Runtime)
	e.Dev = cloneProviderEntryDevConfig(e.Dev)
	e.Harnesses = cloneProviderEntryHarnessConfigMap(e.Harnesses)
	e.LocalHarness = cloneProviderEntryHarnessConfig(e.LocalHarness)
	e.Lifecycle = cloneAgentProviderLifecycleConfig(e.Lifecycle)
	normalizeProviderEntryAliases(&e)
	return providerEntryFields(e)
}

func cloneAgentProviderLifecycleConfig(src *AgentProviderLifecycleConfig) *AgentProviderLifecycleConfig {
	if src == nil {
		return nil
	}
	dst := &AgentProviderLifecycleConfig{
		SessionStart: make([]AgentProviderSessionStartHookConfig, len(src.SessionStart)),
	}
	for i := range src.SessionStart {
		dst.SessionStart[i] = cloneAgentProviderSessionStartHookConfig(src.SessionStart[i])
	}
	return dst
}

func cloneAgentProviderSessionStartHookConfig(src AgentProviderSessionStartHookConfig) AgentProviderSessionStartHookConfig {
	src.Command = slices.Clone(src.Command)
	src.Env = maps.Clone(src.Env)
	return src
}

func cloneProviderEntryHarnessConfigMap(src map[string]*ProviderEntryHarnessConfig) map[string]*ProviderEntryHarnessConfig {
	if src == nil {
		return nil
	}
	dst := make(map[string]*ProviderEntryHarnessConfig, len(src))
	for name, harness := range src {
		dst[name] = cloneProviderEntryHarnessConfig(harness)
	}
	return dst
}

func cloneProviderEntryHarnessConfig(src *ProviderEntryHarnessConfig) *ProviderEntryHarnessConfig {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Args = slices.Clone(src.Args)
	dst.Env = maps.Clone(src.Env)
	dst.RequiredCommands = slices.Clone(src.RequiredCommands)
	dst.Install = cloneProviderEntryHarnessInstallConfig(src.Install)
	return &dst
}

func cloneProviderEntryHarnessInstallConfig(src *ProviderEntryHarnessInstallConfig) *ProviderEntryHarnessInstallConfig {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Commands = slices.Clone(src.Commands)
	for i := range dst.Commands {
		dst.Commands[i].Args = slices.Clone(src.Commands[i].Args)
		dst.Commands[i].Env = maps.Clone(src.Commands[i].Env)
	}
	return &dst
}

func cloneProviderEntryDevConfig(src *ProviderEntryDevConfig) *ProviderEntryDevConfig {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Attach.AllowedRoles = slices.Clone(src.Attach.AllowedRoles)
	return &dst
}

func normalizeProviderEntryAliases(entry *ProviderEntry) {
	if entry == nil {
		return
	}
	if entry.Egress != nil {
		entry.Egress.AllowedHosts = trimStringSlice(entry.Egress.AllowedHosts)
	}
	if entry.Dev != nil {
		entry.Dev.Attach.AllowedRoles = trimStringSlice(entry.Dev.Attach.AllowedRoles)
	}
	normalizeAgentProviderLifecycle(entry.Lifecycle)
	entry.DefaultHarness = strings.TrimSpace(entry.DefaultHarness)
	normalizeProviderEntryHarness(entry.LocalHarness)
	for name, harness := range entry.Harnesses {
		trimmed := strings.TrimSpace(name)
		if trimmed != name {
			delete(entry.Harnesses, name)
			if trimmed != "" {
				entry.Harnesses[trimmed] = harness
			}
		}
		normalizeProviderEntryHarness(harness)
	}
}

func normalizeAgentProviderLifecycle(lifecycle *AgentProviderLifecycleConfig) {
	if lifecycle == nil {
		return
	}
	for i := range lifecycle.SessionStart {
		hook := &lifecycle.SessionStart[i]
		hook.ID = strings.TrimSpace(hook.ID)
		hook.Type = strings.TrimSpace(hook.Type)
		hook.CWD = strings.TrimSpace(hook.CWD)
		hook.Timeout = strings.TrimSpace(hook.Timeout)
		if len(hook.Command) > 0 {
			hook.Command[0] = strings.TrimSpace(hook.Command[0])
		}
		if hook.Env != nil {
			trimmed := make(map[string]string, len(hook.Env))
			for key, value := range hook.Env {
				trimmed[strings.TrimSpace(key)] = value
			}
			hook.Env = trimmed
		}
	}
}

func normalizeProviderEntryHarness(harness *ProviderEntryHarnessConfig) {
	if harness == nil {
		return
	}
	harness.Command = strings.TrimSpace(harness.Command)
	harness.WorkingDirectory = strings.TrimSpace(harness.WorkingDirectory)
	harness.RequiredCommands = trimStringSlice(harness.RequiredCommands)
	normalizeProviderEntryHarnessInstall(harness.Install)
}

func normalizeProviderEntryHarnessInstall(install *ProviderEntryHarnessInstallConfig) {
	if install == nil {
		return
	}
	install.Instructions = strings.TrimSpace(install.Instructions)
	out := install.Commands[:0]
	for _, command := range install.Commands {
		command.Description = strings.TrimSpace(command.Description)
		command.Command = strings.TrimSpace(command.Command)
		command.Shell = strings.TrimSpace(command.Shell)
		command.Args = trimStringSlice(command.Args)
		if command.Env != nil {
			env := make(map[string]string, len(command.Env))
			for key, value := range command.Env {
				key = strings.TrimSpace(key)
				if key != "" {
					env[key] = value
				}
			}
			command.Env = env
		}
		if command.Command != "" || command.Shell != "" {
			out = append(out, command)
		}
	}
	install.Commands = out
}

func trimStringSlice(values []string) []string {
	if values == nil {
		return nil
	}
	out := values[:0]
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (e *ProviderEntry) EffectiveAllowedHosts() []string {
	if e == nil {
		return nil
	}
	if e.Egress != nil {
		return slices.Clone(e.Egress.AllowedHosts)
	}
	return nil
}

func (e *ProviderEntry) UsesRuntimePlacement() bool {
	if e == nil {
		return false
	}
	return e.Runtime != nil
}

func (e *ProviderEntry) RuntimePlacementConfig() *RuntimePlacementConfig {
	if e == nil {
		return nil
	}
	return e.Runtime
}

func cloneProviderEgressConfig(src *ProviderEgressConfig) *ProviderEgressConfig {
	if src == nil {
		return nil
	}
	return &ProviderEgressConfig{AllowedHosts: slices.Clone(src.AllowedHosts)}
}

func cloneRuntimePlacementConfig(src *RuntimePlacementConfig) *RuntimePlacementConfig {
	if src == nil {
		return nil
	}
	return &RuntimePlacementConfig{
		Provider:      src.Provider,
		Template:      src.Template,
		Image:         src.Image,
		ImagePullAuth: cloneRuntimePlacementImagePullAuth(src.ImagePullAuth),
		Metadata:      maps.Clone(src.Metadata),
		Pool:          cloneRuntimePlacementPoolConfig(src.Pool),
		Workspace:     cloneRuntimePlacementWorkspaceConfig(src.Workspace),
	}
}

func cloneRuntimePlacementImagePullAuth(src *RuntimePlacementImagePullAuth) *RuntimePlacementImagePullAuth {
	if src == nil {
		return nil
	}
	return &RuntimePlacementImagePullAuth{
		DockerConfigJSON: src.DockerConfigJSON,
	}
}

func cloneRuntimePlacementPoolConfig(src *RuntimePlacementPoolConfig) *RuntimePlacementPoolConfig {
	if src == nil {
		return nil
	}
	return &RuntimePlacementPoolConfig{
		MinReadyInstances:   src.MinReadyInstances,
		MaxReadyInstances:   src.MaxReadyInstances,
		StartupTimeout:      src.StartupTimeout,
		HealthCheckInterval: src.HealthCheckInterval,
		RestartPolicy:       src.RestartPolicy,
		DrainTimeout:        src.DrainTimeout,
	}
}

func cloneRuntimePlacementWorkspaceConfig(src *RuntimePlacementWorkspaceConfig) *RuntimePlacementWorkspaceConfig {
	if src == nil {
		return nil
	}
	return &RuntimePlacementWorkspaceConfig{
		PrepareTimeout: src.PrepareTimeout,
		Git:            cloneRuntimePlacementWorkspaceGitConfig(src.Git),
	}
}

func cloneRuntimePlacementWorkspaceGitConfig(src *RuntimePlacementWorkspaceGitConfig) *RuntimePlacementWorkspaceGitConfig {
	if src == nil {
		return nil
	}
	return &RuntimePlacementWorkspaceGitConfig{
		AllowedRepositories: slices.Clone(src.AllowedRepositories),
	}
}

func cloneHTTPSecuritySchemes(src map[string]*HTTPSecurityScheme) map[string]*HTTPSecurityScheme {
	if src == nil {
		return nil
	}
	cloned := make(map[string]*HTTPSecurityScheme, len(src))
	for name, scheme := range src {
		if scheme == nil {
			cloned[name] = nil
			continue
		}
		copyScheme := *scheme
		copyScheme.Secret = cloneHTTPSecretRef(scheme.Secret)
		cloned[name] = &copyScheme
	}
	return cloned
}

func cloneHTTPSecretRef(src *providermanifestv1.HTTPSecretRef) *providermanifestv1.HTTPSecretRef {
	if src == nil {
		return nil
	}
	cloned := *src
	return &cloned
}

func cloneHTTPBindings(src map[string]*HTTPBinding) map[string]*HTTPBinding {
	if src == nil {
		return nil
	}
	cloned := make(map[string]*HTTPBinding, len(src))
	for name, binding := range src {
		if binding == nil {
			cloned[name] = nil
			continue
		}
		copyBinding := *binding
		copyBinding.RequestBody = cloneHTTPRequestBody(binding.RequestBody)
		copyBinding.Ack = cloneHTTPAck(binding.Ack)
		cloned[name] = &copyBinding
	}
	return cloned
}

func cloneHTTPRequestBody(src *providermanifestv1.HTTPRequestBody) *providermanifestv1.HTTPRequestBody {
	if src == nil {
		return nil
	}
	cloned := *src
	if src.Content != nil {
		cloned.Content = make(map[string]*providermanifestv1.HTTPMediaType, len(src.Content))
		for mediaType, value := range src.Content {
			if value == nil {
				cloned.Content[mediaType] = nil
				continue
			}
			copyMediaType := *value
			cloned.Content[mediaType] = &copyMediaType
		}
	}
	return &cloned
}

func cloneHTTPAck(src *providermanifestv1.HTTPAck) *providermanifestv1.HTTPAck {
	if src == nil {
		return nil
	}
	cloned := *src
	if src.Headers != nil {
		cloned.Headers = maps.Clone(src.Headers)
	}
	cloned.Body = cloneHTTPBodyValue(src.Body)
	return &cloned
}

func cloneHTTPBodyValue(src any) any {
	if src == nil {
		return nil
	}
	return cloneHTTPBodyReflectValue(reflect.ValueOf(src)).Interface()
}

func cloneHTTPBodyReflectValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return reflect.Value{}
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		return cloneHTTPBodyReflectValue(value.Elem())
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.New(value.Type().Elem())
		cloned.Elem().Set(cloneHTTPBodyReflectValue(value.Elem()))
		return cloned
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			cloned.SetMapIndex(cloneHTTPBodyReflectValue(iter.Key()), cloneHTTPBodyReflectValue(iter.Value()))
		}
		return cloned
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			cloned.Index(i).Set(cloneHTTPBodyReflectValue(value.Index(i)))
		}
		return cloned
	case reflect.Array:
		cloned := reflect.New(value.Type()).Elem()
		for i := 0; i < value.Len(); i++ {
			cloned.Index(i).Set(cloneHTTPBodyReflectValue(value.Index(i)))
		}
		return cloned
	default:
		return value
	}
}

func mergeHTTPSecurityScheme(base, override *HTTPSecurityScheme) *HTTPSecurityScheme {
	if base == nil {
		if override == nil {
			return nil
		}
		cloned := cloneHTTPSecuritySchemes(map[string]*HTTPSecurityScheme{"_": override})
		return cloned["_"]
	}
	if override == nil {
		cloned := cloneHTTPSecuritySchemes(map[string]*HTTPSecurityScheme{"_": base})
		return cloned["_"]
	}
	merged := *base
	if override.Type != "" {
		merged.Type = override.Type
	}
	if override.Description != "" {
		merged.Description = override.Description
	}
	if override.SignatureHeader != "" {
		merged.SignatureHeader = override.SignatureHeader
	}
	if override.SignaturePrefix != "" {
		merged.SignaturePrefix = override.SignaturePrefix
	}
	if override.PayloadTemplate != "" {
		merged.PayloadTemplate = override.PayloadTemplate
	}
	if override.TimestampHeader != "" {
		merged.TimestampHeader = override.TimestampHeader
	}
	if override.MaxAgeSeconds != 0 {
		merged.MaxAgeSeconds = override.MaxAgeSeconds
	}
	if override.Name != "" {
		merged.Name = override.Name
	}
	if override.In != "" {
		merged.In = override.In
	}
	if override.Scheme != "" {
		merged.Scheme = override.Scheme
	}
	if override.Secret != nil {
		merged.Secret = cloneHTTPSecretRef(override.Secret)
	}
	return &merged
}

func mergeHTTPBinding(base, override *HTTPBinding) *HTTPBinding {
	if base == nil {
		if override == nil {
			return nil
		}
		cloned := cloneHTTPBindings(map[string]*HTTPBinding{"_": override})
		return cloned["_"]
	}
	if override == nil {
		cloned := cloneHTTPBindings(map[string]*HTTPBinding{"_": base})
		return cloned["_"]
	}
	merged := *base
	if override.Path != "" {
		merged.Path = override.Path
	}
	if override.Method != "" {
		merged.Method = override.Method
	}
	if override.CredentialMode != "" {
		merged.CredentialMode = override.CredentialMode
	}
	if override.Security != "" {
		merged.Security = override.Security
	}
	if override.Target != "" {
		merged.Target = override.Target
	}
	if override.RequestBody != nil {
		if merged.RequestBody == nil {
			merged.RequestBody = cloneHTTPRequestBody(override.RequestBody)
		} else {
			requestBody := *merged.RequestBody
			if override.RequestBody.Required {
				requestBody.Required = true
			}
			if override.RequestBody.Content != nil {
				requestBody.Content = cloneHTTPRequestBody(override.RequestBody).Content
			}
			merged.RequestBody = &requestBody
		}
	}
	if override.Ack != nil {
		if merged.Ack == nil {
			merged.Ack = cloneHTTPAck(override.Ack)
		} else {
			ack := cloneHTTPAck(merged.Ack)
			if override.Ack.Status != 0 {
				ack.Status = override.Ack.Status
			}
			if override.Ack.Headers != nil {
				if ack.Headers == nil {
					ack.Headers = map[string]string{}
				}
				for key, value := range override.Ack.Headers {
					ack.Headers[key] = value
				}
			}
			if override.Ack.Body != nil {
				ack.Body = cloneHTTPBodyValue(override.Ack.Body)
			}
			merged.Ack = ack
		}
	}
	return &merged
}

func (e *ProviderEntry) HasMetadataSource() bool {
	return e != nil && (e.Source.IsMetadataURL() || e.Source.IsGitHubRelease() || e.Source.IsPackage())
}

func (e *ProviderEntry) HasReleaseMetadataSource() bool {
	return e != nil && (e.Source.IsMetadataURL() || e.Source.IsGitHubRelease() || e.Source.IsLocalMetadataPath() || e.Source.IsPackage())
}

func (e *ProviderEntry) HasRemoteSource() bool {
	return e != nil && (e.Source.IsMetadataURL() || e.Source.IsGitHubRelease() || e.Source.IsPackage())
}

func (e *ProviderEntry) HasRemoteReleaseSource() bool {
	return e != nil && (e.Source.IsMetadataURL() || e.Source.IsGitHubRelease() || e.Source.IsPackage())
}

func (e *ProviderEntry) HasLocalSource() bool {
	return e != nil && e.Source.IsLocal()
}

func (e *ProviderEntry) HasLocalReleaseSource() bool {
	return e != nil && e.Source.IsLocalMetadataPath()
}

func (e *ProviderEntry) SourceMetadataURL() string {
	if e == nil {
		return ""
	}
	if e.Source.IsPackage() {
		return e.Source.ResolvedPackageMetadataURL()
	}
	return e.Source.MetadataURL()
}

func (e *ProviderEntry) SourceRemoteLocation() string {
	if e == nil {
		return ""
	}
	if e.Source.IsMetadataURL() {
		return e.Source.MetadataURL()
	}
	if e.Source.IsGitHubRelease() {
		return e.Source.GitHubReleaseLocation()
	}
	if e.Source.IsPackage() {
		return e.Source.ResolvedPackageMetadataURL()
	}
	return ""
}

func (e *ProviderEntry) SourceReleaseLocation() string {
	if e == nil {
		return ""
	}
	if e.Source.IsMetadataURL() {
		return e.Source.MetadataURL()
	}
	if e.Source.IsGitHubRelease() {
		return e.Source.GitHubReleaseLocation()
	}
	if e.Source.IsLocalMetadataPath() {
		return e.Source.MetadataPath()
	}
	if e.Source.IsPackage() {
		return e.Source.ResolvedPackageMetadataURL()
	}
	return ""
}

func (e *ProviderEntry) SourcePath() string {
	if e.Source.IsLocal() {
		return e.Source.Path
	}
	return ""
}

func (e *ProviderEntry) SourceReleasePath() string {
	if e.Source.IsLocalMetadataPath() {
		return e.Source.MetadataPath()
	}
	return ""
}

func (e *ProviderEntry) HasResolvedManifest() bool {
	return e != nil && e.ResolvedManifest != nil
}

func (e *ProviderEntry) ManifestSpec() *providermanifestv1.Spec {
	if e == nil || e.ResolvedManifest == nil {
		return nil
	}
	return e.ResolvedManifest.Spec
}

func (e *ProviderEntry) EffectiveHTTPSecuritySchemes() map[string]*HTTPSecurityScheme {
	var merged map[string]*HTTPSecurityScheme
	if spec := e.ManifestSpec(); spec != nil && spec.SecuritySchemes != nil {
		merged = cloneHTTPSecuritySchemes(spec.SecuritySchemes)
	}
	if e == nil || e.SecuritySchemes == nil {
		return merged
	}
	if merged == nil {
		merged = map[string]*HTTPSecurityScheme{}
	}
	for name, scheme := range e.SecuritySchemes {
		if scheme == nil {
			merged[name] = nil
			continue
		}
		merged[name] = mergeHTTPSecurityScheme(merged[name], scheme)
	}
	return merged
}

func (e *ProviderEntry) EffectiveHTTPBindings() map[string]*HTTPBinding {
	var merged map[string]*HTTPBinding
	if spec := e.ManifestSpec(); spec != nil && spec.HTTP != nil {
		merged = cloneHTTPBindings(spec.HTTP)
	}
	if e == nil || e.HTTP == nil {
		return merged
	}
	if merged == nil {
		merged = map[string]*HTTPBinding{}
	}
	for name, binding := range e.HTTP {
		if binding == nil {
			merged[name] = nil
			continue
		}
		merged[name] = mergeHTTPBinding(merged[name], binding)
	}
	return merged
}

func (e *ProviderEntry) DeclaresMCP() bool {
	if e == nil {
		return false
	}
	if e.MCP {
		return true
	}
	spec := e.ManifestSpec()
	if spec == nil {
		return false
	}
	return spec.MCP
}

func (e *ProviderEntry) HasMCPSurface() bool {
	if e == nil {
		return false
	}
	if ProviderSurfaceURLOverride(e, SpecSurfaceMCP) != "" {
		return true
	}
	return ManifestProviderSurfaceURL(e.ManifestSpec(), SpecSurfaceMCP) != ""
}

func (e *ProviderEntry) ExposesMCP() bool {
	return e.DeclaresMCP() || e.HasMCPSurface()
}

func (e *ProviderEntry) IncludeRESTInMCP() bool {
	return e.DeclaresMCP()
}

type SourceAuthDef struct {
	Token string `yaml:"token"`
}

type EgressConfig struct {
	DefaultAction string `yaml:"defaultAction"`
}

type AuthorizationConfig struct {
	Policies map[string]SubjectPolicyDef `yaml:"policies,omitempty"`
}

type SubjectPolicyDef struct {
	Default string                   `yaml:"default,omitempty"`
	Members []SubjectPolicyMemberDef `yaml:"members,omitempty"`
}

type SubjectPolicyMemberDef struct {
	SubjectID string `yaml:"subjectID,omitempty"`
	Role      string `yaml:"role"`
}

type ListenerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type ManagementListenerConfig struct {
	ListenerConfig `yaml:",inline"`
	BaseURL        string `yaml:"baseUrl,omitempty"`
}

type AdminConfig struct {
	AuthorizationPolicy string   `yaml:"authorizationPolicy,omitempty"`
	AllowedRoles        []string `yaml:"allowedRoles,omitempty"`
	UI                  string   `yaml:"ui,omitempty"`
}

type ServerConfig struct {
	Public        ListenerConfig           `yaml:"public"`
	Management    ManagementListenerConfig `yaml:"management"`
	BaseURL       string                   `yaml:"baseUrl"`
	EncryptionKey string                   `yaml:"encryptionKey"`
	APITokenTTL   string                   `yaml:"apiTokenTtl"`
	ArtifactsDir  string                   `yaml:"artifactsDir"`
	Providers     ServerProvidersConfig    `yaml:"providers,omitempty"`
	Agent         ServerAgentConfig        `yaml:"agent,omitempty"`
	Dev           DevConfig                `yaml:"dev,omitempty"`
	Runtime       ServerRuntimeConfig      `yaml:"runtime,omitempty"`
	Egress        EgressConfig             `yaml:"egress,omitempty"`
	Admin         AdminConfig              `yaml:"admin,omitempty"`
}

type ServerAgentConfig struct {
	DefaultToolNarrowingThreshold *int `yaml:"defaultToolNarrowingThreshold,omitempty"`
}

type DevConfig struct {
	AttachmentState DevAttachmentState `yaml:"attachmentState,omitempty"`
}

type DevAttachmentState string

const DevAttachmentStateIndexedDB DevAttachmentState = "indexeddb"

type ServerRuntimeConfig struct {
	DefaultProvider string `yaml:"defaultProvider,omitempty"`
	RelayBaseURL    string `yaml:"relayBaseUrl,omitempty"`
}

type serverRuntimeConfigFields ServerRuntimeConfig

func (s *ServerRuntimeConfig) UnmarshalYAML(value *yaml.Node) error {
	if mappingValueNode(value, "defaultHostedProvider") != nil {
		return fmt.Errorf("config validation: server.runtime.defaultHostedProvider has been removed; use server.runtime.defaultProvider")
	}
	var decoded serverRuntimeConfigFields
	if err := decodeYAMLNodeKnownFields(value, &decoded); err != nil {
		return err
	}
	*s = ServerRuntimeConfig(decoded)
	return nil
}

func (s ServerConfig) PublicListener() ListenerConfig {
	port := s.Public.Port
	if port == 0 {
		port = 8080
	}
	return ListenerConfig{
		Host: s.Public.Host,
		Port: port,
	}
}

func (s ServerConfig) PublicAddr() string {
	listener := s.PublicListener()
	return net.JoinHostPort(listener.Host, strconv.Itoa(listener.Port))
}

func (s ServerConfig) ManagementListener() (ListenerConfig, bool) {
	if s.Management.Port == 0 {
		return ListenerConfig{}, false
	}
	return s.Management.ListenerConfig, true
}

func (s ServerConfig) ManagementAddr() string {
	listener, ok := s.ManagementListener()
	if !ok {
		return ""
	}
	return net.JoinHostPort(listener.Host, strconv.Itoa(listener.Port))
}

func (s ServerConfig) ManagementBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(s.Management.BaseURL), "/")
}

// ConnectionDef owns authentication and connection parameters for a named
// connection.
type ConnectionDef struct {
	Ref               string                                  `yaml:"ref,omitempty"`
	DisplayName       string                                  `yaml:"displayName,omitempty"`
	Mode              providermanifestv1.ConnectionMode       `yaml:"mode"`
	Exposure          providermanifestv1.ConnectionExposure   `yaml:"exposure,omitempty"`
	Auth              ConnectionAuthDef                       `yaml:"auth"`
	ConnectionParams  map[string]ConnectionParamDef           `yaml:"params"`
	CredentialRefresh *CredentialRefreshDef                   `yaml:"credentialRefresh,omitempty"`
	Discovery         *providermanifestv1.ProviderDiscovery   `yaml:"-"`
	PostConnect       *providermanifestv1.ProviderPostConnect `yaml:"-"`
	ConnectionID      string                                  `yaml:"-"`
	BindingResolved   bool                                    `yaml:"-"`
}

type CredentialRefreshDef = providermanifestv1.CredentialRefreshConfig

type ConnectionAuthDef struct {
	Type                 providermanifestv1.AuthType `yaml:"type"`
	Token                string                      `yaml:"token"`
	GrantType            string                      `yaml:"grantType"`
	RefreshToken         string                      `yaml:"refreshToken"`
	AuthorizationURL     string                      `yaml:"authorizationUrl"`
	TokenURL             string                      `yaml:"tokenUrl"`
	ClientID             string                      `yaml:"clientId"`
	ClientSecret         string                      `yaml:"clientSecret"`
	RedirectURL          string                      `yaml:"redirectUrl"`
	ClientAuth           string                      `yaml:"clientAuth"`
	TokenExchange        string                      `yaml:"tokenExchange"`
	TokenPrefix          string                      `yaml:"tokenPrefix"`
	Scopes               []string                    `yaml:"scopes"`
	ScopeParam           string                      `yaml:"scopeParam"`
	ScopeSeparator       string                      `yaml:"scopeSeparator"`
	PKCE                 bool                        `yaml:"pkce"`
	AuthorizationParams  map[string]string           `yaml:"authorizationParams"`
	TokenParams          map[string]string           `yaml:"tokenParams"`
	RefreshParams        map[string]string           `yaml:"refreshParams"`
	AcceptHeader         string                      `yaml:"acceptHeader"`
	AccessTokenPath      string                      `yaml:"accessTokenPath"`
	TokenMetadata        []string                    `yaml:"tokenMetadata"`
	TokenExchangeDrivers []TokenExchangeDriverDef    `yaml:"tokenExchangeDrivers"`
	Credentials          []CredentialFieldDef        `yaml:"credentials"`
	AuthMapping          *AuthMappingDef             `yaml:"authMapping"`
}

type CredentialFieldDef = providermanifestv1.CredentialField
type TokenExchangeDriverDef = providermanifestv1.TokenExchangeDriver
type AuthMappingDef = providermanifestv1.AuthMapping
type BasicAuthMappingDef = providermanifestv1.BasicAuthMapping
type AuthValueDef = providermanifestv1.AuthValue
type AuthValueFromDef = providermanifestv1.AuthValueFrom
type CredentialFieldRefDef = providermanifestv1.CredentialFieldRef

type ConnectionParamDef = providermanifestv1.ProviderConnectionParam

// ResolveConnectionAlias maps the user-facing "plugin" alias to the
// internal PluginConnectionName. All other names pass through unchanged.
func ResolveConnectionAlias(name string) string {
	return core.ResolveConnectionAlias(name)
}

func MergeConnectionAuth(dst *ConnectionAuthDef, src ConnectionAuthDef) {
	if dst == nil {
		return
	}
	if src.Type != "" && dst.Type != "" && src.Type != dst.Type {
		*dst = ConnectionAuthDef{}
	}
	setString := func(dst *string, src string) {
		if src != "" {
			*dst = src
		}
	}
	if src.Type != "" {
		dst.Type = src.Type
	}
	setString(&dst.Token, src.Token)
	setString(&dst.GrantType, src.GrantType)
	setString(&dst.RefreshToken, src.RefreshToken)
	setString(&dst.AuthorizationURL, src.AuthorizationURL)
	setString(&dst.TokenURL, src.TokenURL)
	setString(&dst.ClientID, src.ClientID)
	setString(&dst.ClientSecret, src.ClientSecret)
	setString(&dst.RedirectURL, src.RedirectURL)
	setString(&dst.ClientAuth, src.ClientAuth)
	setString(&dst.TokenExchange, src.TokenExchange)
	setString(&dst.TokenPrefix, src.TokenPrefix)
	if src.Scopes != nil {
		dst.Scopes = src.Scopes
	}
	setString(&dst.ScopeParam, src.ScopeParam)
	setString(&dst.ScopeSeparator, src.ScopeSeparator)
	if src.PKCE {
		dst.PKCE = true
	}
	if src.AuthorizationParams != nil {
		dst.AuthorizationParams = src.AuthorizationParams
	}
	if src.TokenParams != nil {
		dst.TokenParams = src.TokenParams
	}
	if src.RefreshParams != nil {
		dst.RefreshParams = src.RefreshParams
	}
	setString(&dst.AcceptHeader, src.AcceptHeader)
	setString(&dst.AccessTokenPath, src.AccessTokenPath)
	if src.TokenMetadata != nil {
		dst.TokenMetadata = src.TokenMetadata
	}
	if src.TokenExchangeDrivers != nil {
		dst.TokenExchangeDrivers = slices.Clone(src.TokenExchangeDrivers)
	}
	if src.Credentials != nil {
		if len(src.Credentials) == 0 {
			dst.Credentials = nil
		} else {
			dst.Credentials = mergeCredentialFields(dst.Credentials, src.Credentials)
		}
	}
	if src.AuthMapping != nil {
		dst.AuthMapping = src.AuthMapping
	}
}

func mergeCredentialFields(base, override []CredentialFieldDef) []CredentialFieldDef {
	if len(base) == 0 {
		return append([]CredentialFieldDef(nil), override...)
	}

	merged := append([]CredentialFieldDef(nil), base...)
	indexByName := make(map[string]int, len(merged))
	for i, field := range merged {
		if field.Name != "" {
			indexByName[field.Name] = i
		}
	}

	for _, field := range override {
		if idx, ok := indexByName[field.Name]; ok {
			mergeCredentialField(&merged[idx], field)
			continue
		}
		merged = append(merged, field)
		if field.Name != "" {
			indexByName[field.Name] = len(merged) - 1
		}
	}

	return merged
}

func mergeCredentialField(dst *CredentialFieldDef, src CredentialFieldDef) {
	if dst == nil {
		return
	}
	if dst.Name == "" {
		dst.Name = src.Name
	}
	if src.Label != "" {
		dst.Label = src.Label
	}
	if src.Description != "" {
		dst.Description = src.Description
	}
}

func MergeConnectionDef(dst *ConnectionDef, src *ConnectionDef) {
	if dst == nil || src == nil {
		return
	}
	if src.Ref != "" {
		dst.Ref = src.Ref
	}
	if src.DisplayName != "" {
		dst.DisplayName = src.DisplayName
	}
	if src.Mode != "" {
		dst.Mode = src.Mode
	}
	if src.Exposure != "" {
		dst.Exposure = src.Exposure
	}
	MergeConnectionAuth(&dst.Auth, src.Auth)
	if len(src.ConnectionParams) > 0 {
		dst.ConnectionParams = maps.Clone(src.ConnectionParams)
	}
	if src.CredentialRefresh != nil {
		dst.CredentialRefresh = cloneCredentialRefreshDef(src.CredentialRefresh)
	}
	if src.Discovery != nil {
		dst.Discovery = src.Discovery
	}
	if src.PostConnect != nil {
		dst.PostConnect = providermanifestv1.CloneProviderPostConnect(src.PostConnect)
	}
	if src.ConnectionID != "" {
		dst.ConnectionID = src.ConnectionID
	}
	if src.BindingResolved {
		dst.BindingResolved = true
	}
}

func ManifestAuthToConnectionAuthDef(auth *providermanifestv1.ProviderAuth) ConnectionAuthDef {
	if auth == nil {
		return ConnectionAuthDef{}
	}
	out := ConnectionAuthDef{
		Type:                 auth.Type,
		AuthorizationURL:     auth.AuthorizationURL,
		TokenURL:             auth.TokenURL,
		ClientID:             auth.ClientID,
		ClientSecret:         auth.ClientSecret,
		ClientAuth:           auth.ClientAuth,
		TokenExchange:        auth.TokenExchange,
		TokenPrefix:          auth.TokenPrefix,
		Scopes:               slices.Clone(auth.Scopes),
		ScopeParam:           auth.ScopeParam,
		ScopeSeparator:       auth.ScopeSeparator,
		PKCE:                 auth.PKCE,
		AuthorizationParams:  maps.Clone(auth.AuthorizationParams),
		TokenParams:          maps.Clone(auth.TokenParams),
		RefreshParams:        maps.Clone(auth.RefreshParams),
		AcceptHeader:         auth.AcceptHeader,
		AccessTokenPath:      auth.AccessTokenPath,
		TokenMetadata:        slices.Clone(auth.TokenMetadata),
		TokenExchangeDrivers: slices.Clone(auth.TokenExchangeDrivers),
		Credentials:          slices.Clone(auth.Credentials),
		AuthMapping:          CloneAuthMapping(auth.AuthMapping),
	}
	return out
}

func CloneAuthMapping(src *AuthMappingDef) *AuthMappingDef {
	if src == nil {
		return nil
	}
	dst := &AuthMappingDef{}
	if len(src.Headers) > 0 {
		dst.Headers = make(map[string]AuthValueDef, len(src.Headers))
		for name, value := range src.Headers {
			dst.Headers[name] = cloneAuthValue(value)
		}
	}
	if src.Basic != nil {
		dst.Basic = &BasicAuthMappingDef{
			Username: cloneAuthValue(src.Basic.Username),
			Password: cloneAuthValue(src.Basic.Password),
		}
	}
	return dst
}

func cloneAuthValue(src AuthValueDef) AuthValueDef {
	dst := AuthValueDef{Value: src.Value}
	if src.ValueFrom != nil && src.ValueFrom.CredentialFieldRef != nil {
		dst.ValueFrom = &AuthValueFromDef{
			CredentialFieldRef: &CredentialFieldRefDef{Name: src.ValueFrom.CredentialFieldRef.Name},
		}
	}
	return dst
}

// OperationOverride holds optional alias and description for an allowed operation.
type OperationOverride = providermanifestv1.ManifestOperationOverride

type PluginInvocationDependency struct {
	Plugin         string                            `yaml:"plugin,omitempty"`
	Operation      string                            `yaml:"operation,omitempty"`
	Surface        string                            `yaml:"surface,omitempty"`
	CredentialMode providermanifestv1.ConnectionMode `yaml:"credentialMode,omitempty"`
	RunAs          *PluginInvocationRunAsConfig      `yaml:"runAs,omitempty"`
}

type PluginInvocationRunAsConfig struct {
	Subject          *PluginInvocationRunAsSubjectConfig     `yaml:"subject,omitempty"`
	ExternalIdentity *PluginInvocationExternalIdentityConfig `yaml:"externalIdentity,omitempty"`
}

type PluginInvocationRunAsSubjectConfig struct {
	ID                  string `yaml:"id,omitempty"`
	Kind                string `yaml:"kind,omitempty"`
	CredentialSubjectID string `yaml:"credentialSubjectId,omitempty"`
	DisplayName         string `yaml:"displayName,omitempty"`
	AuthSource          string `yaml:"authSource,omitempty"`
}

// TODO(#1823): Reconcile runAs.externalIdentity grants from readable refs at
// deploy time.
type PluginInvocationExternalIdentityConfig struct {
	Type string `yaml:"type,omitempty"`
	ID   string `yaml:"id,omitempty"`
}

func (d PluginInvocationDependency) RunAsSubject() *core.RunAsSubject {
	if d.RunAs == nil {
		return nil
	}
	if subject := d.RunAs.Subject; subject != nil {
		return core.NormalizeRunAsSubject(&core.RunAsSubject{
			SubjectID:           subject.ID,
			SubjectKind:         subject.Kind,
			CredentialSubjectID: subject.CredentialSubjectID,
			DisplayName:         subject.DisplayName,
			AuthSource:          subject.AuthSource,
		})
	}
	return nil
}

func (d PluginInvocationDependency) RunAsExternalIdentity() *core.ExternalIdentityRef {
	if d.RunAs == nil || d.RunAs.ExternalIdentity == nil {
		return nil
	}
	return core.NormalizeExternalIdentityRef(&core.ExternalIdentityRef{
		Type: d.RunAs.ExternalIdentity.Type,
		ID:   d.RunAs.ExternalIdentity.ID,
	})
}

func Load(path string) (*Config, error) {
	return LoadPaths([]string{path})
}

func LoadPaths(paths []string) (*Config, error) {
	return loadWithLookupPaths(paths, os.LookupEnv, false)
}

func LoadWithLookup(path string, lookup func(string) (string, bool)) (*Config, error) {
	return LoadWithLookupPaths([]string{path}, lookup)
}

func LoadWithLookupPaths(paths []string, lookup func(string) (string, bool)) (*Config, error) {
	return loadWithLookupPaths(paths, lookup, false)
}

func LoadAllowMissingEnv(path string) (*Config, error) {
	return LoadAllowMissingEnvPaths([]string{path})
}

func LoadAllowMissingEnvPaths(paths []string) (*Config, error) {
	return loadWithLookupPaths(paths, os.LookupEnv, true)
}

// LoadPartialAllowMissingEnvPaths loads config for commands that inspect a
// local subset of a larger deployment config. It preserves shape
// normalization, defaults, and relative path resolution, but skips full
// structural validation so unrelated deployment-only entries with missing
// local env vars do not block the caller.
func LoadPartialAllowMissingEnvPaths(paths []string) (*Config, error) {
	return loadWithLookupPathsValidation(paths, os.LookupEnv, true, false)
}

// ValidateSelectedAgentHarnessEnvPaths fails if the selected agent harness
// references missing environment variables.
func ValidateSelectedAgentHarnessEnvPaths(paths []string, providerName string, harnessName string) error {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return nil
	}
	harnessName = strings.TrimSpace(harnessName)
	missingEnvSentinelContext, err := newMissingEnvSentinelPrefix()
	if err != nil {
		return err
	}
	root, err := loadMergedConfigRoot(paths, os.LookupEnv, envMissingSentinel, missingEnvSentinelContext)
	if err != nil {
		return err
	}
	providers := mappingValueNode(&root, "providers")
	agentProviders := mappingValueNode(providers, "agent")
	entry := mappingValueNode(agentProviders, providerName)
	harness := selectedAgentHarnessNode(entry, harnessName)
	if harness == nil {
		return nil
	}
	if firstMissing := firstMissingEnvSentinel(harness, missingEnvSentinelContext); firstMissing != "" {
		return fmt.Errorf("expanding providers.agent.%s harness environment variables: environment variable %q not set; use ${%s:-} to allow an empty default", providerName, firstMissing, firstMissing)
	}
	return nil
}

// ValidateSelectedAgentLocalHarnessEnvPaths preserves the old helper API.
func ValidateSelectedAgentLocalHarnessEnvPaths(paths []string, providerName string) error {
	return ValidateSelectedAgentHarnessEnvPaths(paths, providerName, "")
}

func selectedAgentHarnessNode(entry *yaml.Node, harnessName string) *yaml.Node {
	if entry == nil {
		return nil
	}
	harnessName = strings.TrimSpace(harnessName)
	if harnessName == "" {
		if defaultHarness := mappingValueNode(entry, "defaultHarness"); defaultHarness != nil && defaultHarness.Kind == yaml.ScalarNode {
			harnessName = strings.TrimSpace(defaultHarness.Value)
		}
	}
	harnesses := mappingValueNode(entry, "harnesses")
	if harnessName == "" && harnesses != nil {
		if defaultHarness := mappingValueNode(harnesses, DefaultAgentHarnessName); defaultHarness != nil {
			return defaultHarness
		}
		if len(harnesses.Content) == 2 {
			return harnesses.Content[1]
		}
	}
	if harnessName != "" && harnesses != nil {
		if harness := mappingValueNode(harnesses, harnessName); harness != nil {
			return harness
		}
	}
	if harnessName == "" || harnessName == DefaultAgentHarnessName {
		return mappingValueNode(entry, "localHarness")
	}
	return nil
}

func normalizeConfigShape(cfg *Config) error {
	normalizeProviderSourceShapes(cfg)
	normalizeProviderEntries(cfg)
	normalizeServerRuntimeConfig(cfg)
	if err := normalizeAuthorizationConfig(cfg); err != nil {
		return err
	}
	if err := normalizeAdminConfig(cfg); err != nil {
		return err
	}
	return applyPluginMountBindings(cfg)
}

func normalizeServerRuntimeConfig(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.Server.Runtime.DefaultProvider = strings.TrimSpace(cfg.Server.Runtime.DefaultProvider)
}

func normalizeProviderEntries(cfg *Config) {
	if cfg == nil {
		return
	}
	for _, entry := range cfg.Plugins {
		normalizeProviderEntryAliases(entry)
	}
	for _, entry := range cfg.Providers.Authentication {
		normalizeProviderEntryAliases(entry)
	}
	for _, entry := range cfg.Providers.Authorization {
		normalizeProviderEntryAliases(entry)
	}
	for _, entry := range cfg.Providers.ExternalCredentials {
		normalizeProviderEntryAliases(entry)
	}
	for _, entry := range cfg.Providers.Secrets {
		normalizeProviderEntryAliases(entry)
	}
	for _, entry := range cfg.Providers.Telemetry {
		normalizeProviderEntryAliases(entry)
	}
	for _, entry := range cfg.Providers.Audit {
		normalizeProviderEntryAliases(entry)
	}
	for _, entry := range cfg.Providers.IndexedDB {
		normalizeProviderEntryAliases(entry)
	}
	for _, entry := range cfg.Providers.Cache {
		normalizeProviderEntryAliases(entry)
	}
	for _, entry := range cfg.Providers.S3 {
		normalizeProviderEntryAliases(entry)
	}
	for _, entry := range cfg.Providers.Workflow {
		normalizeProviderEntryAliases(entry)
	}
	for _, entry := range cfg.Providers.Agent {
		normalizeProviderEntryAliases(entry)
	}
	for _, entry := range cfg.Providers.UI {
		if entry != nil {
			normalizeProviderEntryAliases(&entry.ProviderEntry)
		}
	}
	for _, entry := range cfg.Runtime.Providers {
		if entry != nil {
			normalizeProviderEntryAliases(&entry.ProviderEntry)
		}
	}
}

func OverlayRemotePluginConfig(path string, cfg *Config) error {
	return OverlayRemotePluginConfigPaths([]string{path}, cfg)
}

func OverlayRemotePluginConfigPaths(paths []string, cfg *Config) error {
	root, err := loadMergedConfigRoot(paths, os.LookupEnv, envMissingPreserve, "")
	if err != nil {
		return err
	}
	doc := documentValueNode(&root)
	providersNode := mappingValueNode(documentValueNode(&root), "providers")
	pluginsNode := mappingValueNode(doc, "plugins")
	for name, entry := range cfg.Plugins {
		if entry == nil || !entry.HasRemoteSource() {
			continue
		}
		if err := overlayRemoteEntryConfigNode(mappingValueNode(pluginsNode, name), entry, "plugin "+strconv.Quote(name)); err != nil {
			return err
		}
	}
	for _, collection := range []struct {
		kind    HostProviderKind
		entries map[string]*ProviderEntry
	}{
		{HostProviderKindAuthentication, cfg.Providers.Authentication},
		{HostProviderKindAuthorization, cfg.Providers.Authorization},
		{HostProviderKindExternalCredentials, cfg.Providers.ExternalCredentials},
		{HostProviderKindSecrets, cfg.Providers.Secrets},
		{HostProviderKindTelemetry, cfg.Providers.Telemetry},
		{HostProviderKindAudit, cfg.Providers.Audit},
		{HostProviderKindIndexedDB, cfg.Providers.IndexedDB},
		{HostProviderKindCache, cfg.Providers.Cache},
		{HostProviderKindWorkflow, cfg.Providers.Workflow},
		{HostProviderKindAgent, cfg.Providers.Agent},
	} {
		kindNode := mappingValueNode(providersNode, string(collection.kind))
		for name, entry := range collection.entries {
			if entry == nil || !entry.HasRemoteSource() {
				continue
			}
			subject := fmt.Sprintf("%s %q", collection.kind, name)
			if err := overlayRemoteEntryConfigNode(mappingValueNode(kindNode, name), entry, subject); err != nil {
				return err
			}
		}
	}
	s3Node := mappingValueNode(providersNode, "s3")
	for name, entry := range cfg.Providers.S3 {
		if entry == nil || !entry.HasRemoteSource() {
			continue
		}
		if err := overlayRemoteEntryConfigNode(mappingValueNode(s3Node, name), entry, "s3 "+strconv.Quote(name)); err != nil {
			return err
		}
	}
	uiNode := mappingValueNode(providersNode, "ui")
	for name, entry := range cfg.Providers.UI {
		if entry == nil || !entry.HasRemoteSource() {
			continue
		}
		if err := overlayRemoteEntryConfigNode(mappingValueNode(uiNode, name), &entry.ProviderEntry, "ui "+strconv.Quote(name)); err != nil {
			return err
		}
	}
	return nil
}

type envMissingMode int

const (
	envMissingBlank envMissingMode = iota
	envMissingSentinel
	envMissingPreserve
)

func overlayRemoteEntryConfigNode(raw *yaml.Node, entry *ProviderEntry, subject string) error {
	if entry == nil || !entry.HasRemoteSource() || raw == nil {
		return nil
	}
	configNode := mappingValueNode(raw, "config")
	if configNode == nil || configNode.Kind == 0 {
		return nil
	}
	node, err := overlayEnvIntoNode(*configNode, os.LookupEnv, true)
	if err != nil {
		return fmt.Errorf("expanding managed provider config for %s: %w", subject, err)
	}
	allowSecretRefs := !strings.HasPrefix(subject, "secrets ")
	if err := NormalizeOpaqueSecretRefs(&node, allowSecretRefs); err != nil {
		return err
	}
	entry.Config = node
	return nil
}

func documentValueNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
		return node.Content[0]
	}
	return node
}

func mappingValueNode(node *yaml.Node, key string) *yaml.Node {
	node = documentValueNode(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func normalizeConfigRoot(root *yaml.Node) error {
	doc := documentValueNode(root)
	if doc == nil || doc.Kind == 0 {
		return nil
	}
	if doc.Kind != yaml.MappingNode {
		return fmt.Errorf("parsing config YAML: expected mapping document")
	}
	return nil
}

func loadWithLookupPaths(paths []string, lookup func(string) (string, bool), allowMissing bool) (*Config, error) {
	return loadWithLookupPathsValidation(paths, lookup, allowMissing, true)
}

func loadWithLookupPathsValidation(paths []string, lookup func(string) (string, bool), allowMissing bool, validate bool) (*Config, error) {
	mode := envMissingBlank
	missingEnvSentinelContext := ""
	if !allowMissing {
		mode = envMissingSentinel
		var err error
		missingEnvSentinelContext, err = newMissingEnvSentinelPrefix()
		if err != nil {
			return nil, err
		}
	}

	root, err := loadMergedConfigRoot(paths, lookup, mode, missingEnvSentinelContext)
	if err != nil {
		return nil, err
	}
	if err := NormalizeConfigSecretRefs(&root); err != nil {
		return nil, err
	}
	if !allowMissing {
		firstMissing := firstMissingEnvSentinel(&root, missingEnvSentinelContext)
		if firstMissing != "" {
			return nil, fmt.Errorf("expanding config environment variables: environment variable %q not set; use ${%s:-} to allow an empty default", firstMissing, firstMissing)
		}
	}
	normalized, err := yaml.Marshal(documentValueNode(&root))
	if err != nil {
		return nil, fmt.Errorf("marshaling normalized config YAML: %w", err)
	}
	normalizedInput := string(normalized)
	if !allowMissing {
		normalizedInput = restoreMissingEnvSentinels(normalizedInput, missingEnvSentinelContext)
	}

	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(normalizedInput))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && err != io.EOF {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}

	if err := CanonicalizeStructure(&cfg); err != nil {
		return nil, err
	}
	applyDefaults(&cfg)
	resolveBaseURL(&cfg)
	resolveRelativePaths(primaryConfigPath(paths), &cfg)

	if validate {
		if err := ValidateCanonicalStructure(&cfg); err != nil {
			return nil, err
		}
	}

	return &cfg, nil
}

func primaryConfigPath(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

func loadMergedConfigRoot(paths []string, lookup func(string) (string, bool), mode envMissingMode, sentinelPrefix string) (yaml.Node, error) {
	if len(paths) == 0 {
		return yaml.Node{}, fmt.Errorf("reading config file: no config files provided")
	}

	roots := make([]yaml.Node, len(paths))
	for i, path := range paths {
		root, err := loadValidatedConfigRoot(path, lookup, mode, sentinelPrefix)
		if err != nil {
			return yaml.Node{}, err
		}
		roots[i] = root
	}

	var merged any
	for i, path := range paths {
		value, err := loadConfigValue(path, roots[i])
		if err != nil {
			return yaml.Node{}, err
		}
		merged = mergeConfigValues(merged, value)
	}

	if merged == nil {
		return yaml.Node{}, nil
	}

	data, err := yaml.Marshal(merged)
	if err != nil {
		return yaml.Node{}, fmt.Errorf("marshaling merged config YAML: %w", err)
	}

	var root yaml.Node
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	if err := dec.Decode(&root); err != nil && err != io.EOF {
		return yaml.Node{}, fmt.Errorf("parsing config YAML: %w", err)
	}
	if err := normalizeConfigRoot(&root); err != nil {
		return yaml.Node{}, err
	}
	return root, nil
}

func validateConfigRootAPIVersion(root yaml.Node) error {
	doc := documentValueNode(&root)
	if doc == nil || doc.Kind == 0 {
		return requiredAPIVersionError()
	}
	if doc.Kind != yaml.MappingNode {
		return fmt.Errorf("parsing config YAML: expected mapping document")
	}
	node := mappingValueNode(doc, "apiVersion")
	if node == nil {
		return requiredAPIVersionError()
	}
	value := strings.TrimSpace(node.Value)
	if value == "" {
		return requiredAPIVersionError()
	}
	if value != ConfigAPIVersion {
		return fmt.Errorf("config validation: unsupported apiVersion %q", value)
	}
	return nil
}

func loadConfigValue(path string, root yaml.Node) (any, error) {
	doc := documentValueNode(&root)
	if doc == nil || doc.Kind == 0 {
		return nil, nil
	}

	var raw any
	if err := doc.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}

	normalized, err := normalizeConfigValue(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}
	if normalized != nil {
		root, ok := normalized.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected mapping document")
		}
		resolveRelativePathsInValue(path, root)
	}
	return normalized, nil
}

func loadValidatedConfigRoot(path string, lookup func(string) (string, bool), mode envMissingMode, sentinelPrefix string) (yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return yaml.Node{}, fmt.Errorf("reading config file: %w", err)
	}

	resolved, err := expandConfigInput(string(data), lookup, mode, sentinelPrefix)
	if err != nil {
		return yaml.Node{}, err
	}

	var root yaml.Node
	dec := yaml.NewDecoder(strings.NewReader(resolved))
	if err := dec.Decode(&root); err != nil && err != io.EOF {
		return yaml.Node{}, fmt.Errorf("parsing config YAML: %w", err)
	}
	if err := normalizeConfigRoot(&root); err != nil {
		return yaml.Node{}, err
	}
	if err := validateConfigRootAPIVersion(root); err != nil {
		return yaml.Node{}, err
	}

	validationRoot, err := cloneConfigRoot(root)
	if err != nil {
		return yaml.Node{}, err
	}
	if err := NormalizeOpaqueSecretRefs(&validationRoot, true); err != nil {
		return yaml.Node{}, err
	}
	normalizedData, err := yaml.Marshal(documentValueNode(&validationRoot))
	if err != nil {
		return yaml.Node{}, fmt.Errorf("marshaling config YAML: %w", err)
	}
	normalizedInput := string(normalizedData)
	if mode == envMissingSentinel {
		normalizedInput = restoreMissingEnvSentinels(normalizedInput, sentinelPrefix)
	}
	validationInput := normalizedInput
	if mode == envMissingSentinel || mode == envMissingPreserve {
		validationInput, _, err = expandEnvVariables(normalizedInput, func(string) (string, bool) {
			return "", false
		}, false)
		if err != nil {
			return yaml.Node{}, err
		}
	}
	var cfg Config
	dec = yaml.NewDecoder(strings.NewReader(validationInput))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && err != io.EOF {
		return yaml.Node{}, fmt.Errorf("parsing config YAML: %w", err)
	}

	return root, nil
}

func cloneConfigRoot(root yaml.Node) (yaml.Node, error) {
	data, err := yaml.Marshal(documentValueNode(&root))
	if err != nil {
		return yaml.Node{}, fmt.Errorf("marshaling config YAML: %w", err)
	}

	var clone yaml.Node
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	if err := dec.Decode(&clone); err != nil && err != io.EOF {
		return yaml.Node{}, fmt.Errorf("parsing config YAML: %w", err)
	}
	if err := normalizeConfigRoot(&clone); err != nil {
		return yaml.Node{}, err
	}
	return clone, nil
}

func expandConfigInput(input string, lookup func(string) (string, bool), mode envMissingMode, sentinelPrefix string) (string, error) {
	switch mode {
	case envMissingBlank:
		resolved, _, err := expandEnvVariables(input, lookup, false)
		return resolved, err
	case envMissingSentinel:
		return expandEnvVariablesWithMissingSentinels(input, lookup, sentinelPrefix)
	case envMissingPreserve:
		resolved, _, err := expandEnvVariables(input, lookup, true)
		return resolved, err
	default:
		return "", fmt.Errorf("unsupported env expansion mode %d", mode)
	}
}

func normalizeConfigValue(value any) (any, error) {
	switch current := value.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		out := make(map[string]any, len(current))
		for key, child := range current {
			normalized, err := normalizeConfigValue(child)
			if err != nil {
				return nil, err
			}
			out[key] = normalized
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(current))
		for key, child := range current {
			keyString, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("expected string mapping key, got %T", key)
			}
			normalized, err := normalizeConfigValue(child)
			if err != nil {
				return nil, err
			}
			out[keyString] = normalized
		}
		return out, nil
	case []any:
		out := make([]any, len(current))
		for i, child := range current {
			normalized, err := normalizeConfigValue(child)
			if err != nil {
				return nil, err
			}
			out[i] = normalized
		}
		return out, nil
	default:
		return current, nil
	}
}

func mergeConfigValues(base, overlay any) any {
	if overlay == nil {
		return cloneConfigValue(base)
	}

	baseMap, baseIsMap := base.(map[string]any)
	overlayMap, overlayIsMap := overlay.(map[string]any)
	if baseIsMap && overlayIsMap {
		out := cloneConfigValue(baseMap).(map[string]any)
		for key, value := range overlayMap {
			if value == nil {
				delete(out, key)
				continue
			}
			out[key] = mergeConfigValues(out[key], value)
		}
		return out
	}

	return cloneConfigValue(overlay)
}

func cloneConfigValue(value any) any {
	switch current := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(current))
		for key, child := range current {
			out[key] = cloneConfigValue(child)
		}
		return out
	case []any:
		out := make([]any, len(current))
		for i, child := range current {
			out[i] = cloneConfigValue(child)
		}
		return out
	default:
		return current
	}
}

const (
	missingEnvSentinelPrefix = "__GESTALT_MISSING_ENV_"
	missingEnvSentinelSuffix = "__"
)

func firstMissingEnvSentinel(node *yaml.Node, sentinelPrefix string) string {
	node = documentValueNode(node)
	if node == nil {
		return ""
	}
	if node.Kind == yaml.ScalarNode {
		return firstMissingEnvSentinelInString(node.Value, sentinelPrefix)
	}
	for _, child := range node.Content {
		if name := firstMissingEnvSentinel(child, sentinelPrefix); name != "" {
			return name
		}
	}
	return ""
}

func firstMissingEnvSentinelInString(value, sentinelPrefix string) string {
	if sentinelPrefix == "" {
		return ""
	}
	for start := 0; start < len(value); {
		idx := strings.Index(value[start:], sentinelPrefix)
		if idx < 0 {
			break
		}
		idx += start + len(sentinelPrefix)
		end := strings.Index(value[idx:], missingEnvSentinelSuffix)
		if end < 0 {
			break
		}
		decoded, err := base64.RawURLEncoding.DecodeString(value[idx : idx+end])
		if err == nil {
			return string(decoded)
		}
		start = idx + end + len(missingEnvSentinelSuffix)
	}
	ref, ok, err := ParseSecretRefTransport(value)
	if err != nil || !ok {
		return ""
	}
	if name := firstMissingEnvSentinelInString(ref.Provider, sentinelPrefix); name != "" {
		return name
	}
	return firstMissingEnvSentinelInString(ref.Name, sentinelPrefix)
}

func restoreMissingEnvSentinels(input, sentinelPrefix string) string {
	if sentinelPrefix == "" {
		return input
	}
	var b strings.Builder
	b.Grow(len(input))
	for start := 0; start < len(input); {
		idx := strings.Index(input[start:], sentinelPrefix)
		if idx < 0 {
			b.WriteString(input[start:])
			break
		}
		idx += start
		b.WriteString(input[start:idx])
		encodedStart := idx + len(sentinelPrefix)
		end := strings.Index(input[encodedStart:], missingEnvSentinelSuffix)
		if end < 0 {
			b.WriteString(input[idx:])
			break
		}
		encodedEnd := encodedStart + end
		decoded, err := base64.RawURLEncoding.DecodeString(input[encodedStart:encodedEnd])
		if err != nil {
			b.WriteString(input[idx : encodedEnd+len(missingEnvSentinelSuffix)])
			start = encodedEnd + len(missingEnvSentinelSuffix)
			continue
		}
		b.WriteString("${")
		b.Write(decoded)
		b.WriteString("}")
		start = encodedEnd + len(missingEnvSentinelSuffix)
	}
	return b.String()
}

func newMissingEnvSentinelPrefix() (string, error) {
	var token [9]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("creating missing env sentinel: %w", err)
	}
	return missingEnvSentinelPrefix + base64.RawURLEncoding.EncodeToString(token[:]) + "_", nil
}

func parseEnvPlaceholder(key string) (name string, allowEmptyDefault bool, err error) {
	if !strings.Contains(key, ":-") {
		return key, false, nil
	}
	name, defaultValue, _ := strings.Cut(key, ":-")
	if name == "" {
		return "", false, fmt.Errorf("invalid environment placeholder ${%s}", key)
	}
	if defaultValue != "" {
		return "", false, fmt.Errorf("unsupported environment placeholder ${%s}: only ${%s:-} is supported for empty defaults", key, name)
	}
	return name, true, nil
}

func expandEnvVariables(input string, lookup func(string) (string, bool), preserveMissing bool) (string, string, error) {
	var expandErr error
	var firstMissing string
	resolved := os.Expand(input, func(key string) string {
		if expandErr != nil {
			return ""
		}
		name, allowEmptyDefault, err := parseEnvPlaceholder(key)
		if err != nil {
			expandErr = err
			return ""
		}
		if val, ok := lookup(name); ok {
			return val
		}
		filePath, ok := lookup(name + "_FILE")
		if !ok || filePath == "" {
			if allowEmptyDefault {
				return ""
			}
			if preserveMissing {
				if firstMissing == "" {
					firstMissing = name
				}
				return "${" + key + "}"
			}
			return ""
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			expandErr = fmt.Errorf("resolving %s_FILE: %w", name, err)
			return ""
		}
		return strings.TrimRight(string(data), "\r\n")
	})
	if expandErr != nil {
		return "", "", fmt.Errorf("expanding config environment variables: %w", expandErr)
	}
	return resolved, firstMissing, nil
}

func expandEnvVariablesWithMissingSentinels(input string, lookup func(string) (string, bool), sentinelPrefix string) (string, error) {
	var expandErr error
	resolved := os.Expand(input, func(key string) string {
		if expandErr != nil {
			return ""
		}
		name, allowEmptyDefault, err := parseEnvPlaceholder(key)
		if err != nil {
			expandErr = err
			return ""
		}
		if val, ok := lookup(name); ok {
			return val
		}
		filePath, ok := lookup(name + "_FILE")
		if !ok || filePath == "" {
			if allowEmptyDefault {
				return ""
			}
			return sentinelPrefix + base64.RawURLEncoding.EncodeToString([]byte(name)) + missingEnvSentinelSuffix
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			expandErr = fmt.Errorf("resolving %s_FILE: %w", name, err)
			return ""
		}
		return strings.TrimRight(string(data), "\r\n")
	})
	if expandErr != nil {
		return "", fmt.Errorf("expanding config environment variables: %w", expandErr)
	}
	return resolved, nil
}

func overlayEnvIntoNode(node yaml.Node, lookup func(string) (string, bool), preserveMissing bool) (yaml.Node, error) {
	data, err := yaml.Marshal(&node)
	if err != nil {
		return yaml.Node{}, fmt.Errorf("marshaling config node: %w", err)
	}

	resolved, _, err := expandEnvVariables(string(data), lookup, preserveMissing)
	if err != nil {
		return yaml.Node{}, err
	}

	var out yaml.Node
	dec := yaml.NewDecoder(strings.NewReader(resolved))
	if err := dec.Decode(&out); err != nil && err != io.EOF {
		return yaml.Node{}, fmt.Errorf("parsing config YAML: %w", err)
	}
	if out.Kind == yaml.DocumentNode && len(out.Content) == 1 {
		return *out.Content[0], nil
	}
	return out, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Public.Port == 0 {
		cfg.Server.Public.Port = 8080
	}
	if cfg.Connections == nil {
		cfg.Connections = map[string]*ConnectionDef{}
	}
	cfg.Plugins = nonNilProviderEntryMap(cfg.Plugins)
	cfg.Workflows.Schedules = nonNilWorkflowScheduleMap(cfg.Workflows.Schedules)
	cfg.Workflows.EventTriggers = nonNilWorkflowEventTriggerMap(cfg.Workflows.EventTriggers)
	cfg.Providers.UI = nonNilUIEntryMap(cfg.Providers.UI)
	cfg.Providers.Authentication = nonNilProviderEntryMap(cfg.Providers.Authentication)
	cfg.Providers.Authorization = nonNilProviderEntryMap(cfg.Providers.Authorization)
	cfg.Providers.ExternalCredentials = applyDefaultSourceProviderEntries(cfg.Providers.ExternalCredentials, DefaultProviderInstance, DefaultProviderSource(DefaultExternalCredentialsProvider, DefaultExternalCredentialsVersion))
	cfg.Providers.Secrets = applyDefaultBuiltinProviderEntries(cfg.Providers.Secrets, DefaultProviderInstance, "env")
	cfg.Providers.Telemetry = applyDefaultBuiltinProviderEntries(cfg.Providers.Telemetry, DefaultProviderInstance, "stdout")
	cfg.Providers.Audit = applyDefaultBuiltinProviderEntries(cfg.Providers.Audit, DefaultProviderInstance, "inherit")
	cfg.Providers.IndexedDB = nonNilProviderEntryMap(cfg.Providers.IndexedDB)
	cfg.Providers.Cache = nonNilProviderEntryMap(cfg.Providers.Cache)
	cfg.Providers.S3 = nonNilProviderEntryMap(cfg.Providers.S3)
	cfg.Providers.Workflow = nonNilProviderEntryMap(cfg.Providers.Workflow)
	cfg.Providers.Agent = nonNilProviderEntryMap(cfg.Providers.Agent)
}

func nonNilWorkflowScheduleMap(in map[string]WorkflowScheduleConfig) map[string]WorkflowScheduleConfig {
	if in == nil {
		return map[string]WorkflowScheduleConfig{}
	}
	return in
}

func nonNilWorkflowEventTriggerMap(in map[string]WorkflowEventTriggerConfig) map[string]WorkflowEventTriggerConfig {
	if in == nil {
		return map[string]WorkflowEventTriggerConfig{}
	}
	return in
}

func normalizeProviderSourceShapes(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.APIVersion = strings.TrimSpace(cfg.APIVersion)

	normalizeEntry := func(kind string, entry *ProviderEntry) {
		if entry == nil {
			return
		}
		normalizeProviderSource(kind, &entry.Source)
	}

	for _, entry := range cfg.Plugins {
		normalizeEntry(providermanifestv1.KindPlugin, entry)
	}
	for _, collection := range []struct {
		kind    string
		entries map[string]*ProviderEntry
	}{
		{providermanifestv1.KindAuthentication, cfg.Providers.Authentication},
		{providermanifestv1.KindAuthorization, cfg.Providers.Authorization},
		{providermanifestv1.KindExternalCredentials, cfg.Providers.ExternalCredentials},
		{providermanifestv1.KindSecrets, cfg.Providers.Secrets},
		{string(HostProviderKindTelemetry), cfg.Providers.Telemetry},
		{string(HostProviderKindAudit), cfg.Providers.Audit},
		{providermanifestv1.KindIndexedDB, cfg.Providers.IndexedDB},
		{providermanifestv1.KindCache, cfg.Providers.Cache},
		{providermanifestv1.KindS3, cfg.Providers.S3},
		{string(HostProviderKindWorkflow), cfg.Providers.Workflow},
		{string(HostProviderKindAgent), cfg.Providers.Agent},
	} {
		for _, entry := range collection.entries {
			normalizeEntry(collection.kind, entry)
		}
	}
	for _, entry := range cfg.Runtime.Providers {
		if entry != nil {
			normalizeEntry(providermanifestv1.KindRuntime, &entry.ProviderEntry)
		}
	}
	for _, entry := range cfg.Providers.UI {
		if entry != nil {
			normalizeEntry(providermanifestv1.KindUI, &entry.ProviderEntry)
		}
	}
}

func normalizeProviderSource(kind string, source *ProviderSource) {
	if source == nil {
		return
	}
	source.scalar = strings.TrimSpace(source.scalar)
	source.Path = strings.TrimSpace(source.Path)
	source.metadataURL = strings.TrimSpace(source.metadataURL)
	source.metadataPath = strings.TrimSpace(source.metadataPath)
	source.packageRepo = strings.TrimSpace(source.packageRepo)
	source.packageName = strings.TrimSpace(source.packageName)
	source.packageVersion = strings.TrimSpace(source.packageVersion)
	source.resolvedMetadataURL = strings.TrimSpace(source.resolvedMetadataURL)
	source.resolvedVersion = strings.TrimSpace(source.resolvedVersion)
	source.unsupported = strings.TrimSpace(source.unsupported)
	source.Auth = cloneSourceAuthDef(source.Auth)
	if source.Path != "" && source.metadataPath == "" && isLocalReleaseMetadataPath(source.Path) {
		source.metadataPath = source.Path
		source.Path = ""
	}
	if source.GitHubRelease != nil {
		source.GitHubRelease = cloneGitHubReleaseSourceDef(source.GitHubRelease)
	}
	if source.Builtin != "" || source.Path != "" || source.metadataURL != "" || source.metadataPath != "" || source.packageName != "" || source.GitHubRelease != nil {
		source.scalar = ""
		return
	}
	if source.scalar == "" {
		return
	}
	switch {
	case isBuiltinScalarSource(kind, source.scalar):
		source.Builtin = source.scalar
	case looksLikeMetadataURL(source.scalar):
		source.metadataURL = source.scalar
	case looksLikeUnsupportedScalarSource(source.scalar):
		source.unsupported = source.scalar
	case isLocalReleaseMetadataPath(source.scalar):
		source.metadataPath = source.scalar
	default:
		source.Path = source.scalar
	}
	source.scalar = ""
}

func isLocalReleaseMetadataPath(value string) bool {
	return path.Base(filepath.ToSlash(strings.TrimSpace(value))) == "provider-release.yaml"
}

func isBuiltinScalarSource(kind, source string) bool {
	switch kind {
	case providermanifestv1.KindSecrets:
		switch source {
		case "env", "file":
			return true
		}
	case string(HostProviderKindTelemetry):
		switch source {
		case "noop", "stdout", "otlp":
			return true
		}
	case string(HostProviderKindAudit):
		switch source {
		case "inherit", "noop", "stdout", "otlp":
			return true
		}
	}
	return false
}

func looksLikeMetadataURL(value string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	switch parsed.Scheme {
	case "http", "https":
		return parsed.Host != ""
	default:
		return false
	}
}

func looksLikeUnsupportedScalarSource(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "git+") {
		return true
	}
	if strings.Contains(trimmed, "://") {
		parsed, err := url.ParseRequestURI(trimmed)
		if err != nil {
			return true
		}
		switch parsed.Scheme {
		case "http", "https":
			return true
		default:
			return parsed.Scheme != ""
		}
	}
	for _, prefix := range []string{"http:", "https:", "ssh:", "file:", "ftp:", "ftps:"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func nonNilProviderEntryMap(entries map[string]*ProviderEntry) map[string]*ProviderEntry {
	if entries == nil {
		return make(map[string]*ProviderEntry)
	}
	return entries
}

func nonNilUIEntryMap(entries map[string]*UIEntry) map[string]*UIEntry {
	if entries == nil {
		return make(map[string]*UIEntry)
	}
	return entries
}

func applyDefaultBuiltinProviderEntries(entries map[string]*ProviderEntry, defaultName, builtin string) map[string]*ProviderEntry {
	if len(entries) == 0 {
		return map[string]*ProviderEntry{
			defaultName: {
				Source:  ProviderSource{Builtin: builtin},
				Default: true,
			},
		}
	}
	for _, entry := range entries {
		if entry == nil || entry.Source.IsBuiltin() || entry.Source.IsMetadataURL() || entry.Source.IsGitHubRelease() || entry.Source.IsLocalMetadataPath() || entry.Source.IsLocal() || entry.Source.IsPackage() || entry.Source.UnsupportedURL() != "" {
			continue
		}
		entry.Source.Builtin = builtin
	}
	return entries
}

func applyDefaultSourceProviderEntries(entries map[string]*ProviderEntry, defaultName string, source ProviderSource) map[string]*ProviderEntry {
	if len(entries) == 0 {
		return map[string]*ProviderEntry{
			defaultName: {
				Source:  source,
				Default: true,
			},
		}
	}
	return entries
}

func normalizeAuthorizationConfig(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	cfg.Authorization = normalizedAuthorizationConfig(cfg.Authorization)
	return nil
}

func normalizedAuthorizationConfig(cfg AuthorizationConfig) AuthorizationConfig {
	if len(cfg.Policies) == 0 {
		cfg.Policies = nil
	} else {
		policies := make(map[string]SubjectPolicyDef, len(cfg.Policies))
		for name, policy := range cfg.Policies {
			policies[name] = normalizedSubjectPolicyDef(policy)
		}
		cfg.Policies = policies
	}
	return cfg
}

func normalizeAdminConfig(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	admin := cfg.Server.Admin
	admin.AuthorizationPolicy = strings.TrimSpace(admin.AuthorizationPolicy)
	admin.UI = strings.TrimSpace(admin.UI)
	if len(admin.AllowedRoles) == 0 {
		if admin.AuthorizationPolicy != "" {
			admin.AllowedRoles = []string{"admin"}
		} else {
			admin.AllowedRoles = nil
		}
		cfg.Server.Admin = admin
		return nil
	}

	roles, err := packageio.NormalizeUIAllowedRoles("server.admin.allowedRoles", admin.AllowedRoles)
	if err != nil {
		if admin.AuthorizationPolicy == "" {
			return fmt.Errorf("normalize admin config: server.admin.allowedRoles requires server.admin.authorizationPolicy")
		}
		return fmt.Errorf("normalize admin config: %w", err)
	}
	admin.AllowedRoles = roles
	cfg.Server.Admin = admin
	return nil
}

func normalizedSubjectPolicyDef(policy SubjectPolicyDef) SubjectPolicyDef {
	if len(policy.Members) == 0 {
		policy.Members = nil
	}
	return policy
}

func applyPluginMountBindings(cfg *Config) error {
	if cfg == nil || len(cfg.Plugins) == 0 {
		return nil
	}

	pluginNames := slices.Sorted(maps.Keys(cfg.Plugins))
	seenUIs := make(map[string]string, len(pluginNames))
	for _, pluginName := range pluginNames {
		plugin := cfg.Plugins[pluginName]
		if plugin == nil {
			return fmt.Errorf("config validation: plugins.%s is required", pluginName)
		}
		plugin.UI = strings.TrimSpace(plugin.UI)
		plugin.MountPath = strings.TrimSpace(plugin.MountPath)
		plugin.AuthorizationPolicy = strings.TrimSpace(plugin.AuthorizationPolicy)

		if plugin.MountPath == "" {
			if plugin.UI != "" {
				return fmt.Errorf("config validation: plugins.%s.ui.bundle requires plugins.%s.ui.path", pluginName, pluginName)
			}
			continue
		}
		normalizedPath, err := normalizeMountedUIPath(plugin.MountPath)
		if err != nil {
			return fmt.Errorf("config validation: plugins.%s.ui.path: %w", pluginName, err)
		}
		plugin.MountPath = normalizedPath
		if err := validateAuthorizationPolicyReference(cfg, "plugin", pluginName, plugin.AuthorizationPolicy); err != nil {
			return err
		}
		if plugin.UI == "" {
			continue
		}
		if prev, exists := seenUIs[plugin.UI]; exists && prev != pluginName {
			return fmt.Errorf("config validation: plugins.%s.ui %q duplicates plugins.%s", pluginName, plugin.UI, prev)
		}
		ui := cfg.Providers.UI[plugin.UI]
		if ui == nil {
			return fmt.Errorf("config validation: plugins.%s.ui references unknown ui %q", pluginName, plugin.UI)
		}
		if current := strings.TrimSpace(ui.AuthorizationPolicy); current != "" && current != plugin.AuthorizationPolicy {
			return fmt.Errorf("config validation: plugins.%s.ui %q conflicts with providers.ui.%s.authorizationPolicy", pluginName, plugin.UI, plugin.UI)
		}
		if current := strings.TrimSpace(ui.Path); current != "" && current != plugin.MountPath {
			return fmt.Errorf("config validation: plugins.%s.ui %q conflicts with providers.ui.%s.path", pluginName, plugin.UI, plugin.UI)
		}
		if current := strings.TrimSpace(ui.OwnerPlugin); current != "" && current != pluginName {
			return fmt.Errorf("config validation: plugins.%s.ui %q conflicts with providers.ui.%s owner", pluginName, plugin.UI, plugin.UI)
		}
		ui.AuthorizationPolicy = plugin.AuthorizationPolicy
		ui.Path = plugin.MountPath
		ui.OwnerPlugin = pluginName
		seenUIs[plugin.UI] = pluginName
	}

	return nil
}

func resolveBaseURL(cfg *Config) {
	cfg.Server.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.Server.BaseURL), "/")
	cfg.Server.Management.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.Server.Management.BaseURL), "/")
	cfg.Server.Runtime.RelayBaseURL = strings.TrimRight(strings.TrimSpace(cfg.Server.Runtime.RelayBaseURL), "/")
}

func resolveRelativePathsInValue(configPath string, root map[string]any) {
	baseDir := filepath.Dir(configPath)
	if absPath, err := filepath.Abs(configPath); err == nil {
		baseDir = filepath.Dir(absPath)
	}

	if server := nestedMap(root, "server"); server != nil {
		resolveRelativeStringField(server, "artifactsDir", baseDir)
	}

	if providers := nestedMap(root, "providers"); providers != nil {
		for _, section := range []struct {
			key  string
			kind string
		}{
			{key: "authentication", kind: providermanifestv1.KindAuthentication},
			{key: "authorization", kind: providermanifestv1.KindAuthorization},
			{key: "externalCredentials", kind: providermanifestv1.KindExternalCredentials},
			{key: "secrets", kind: providermanifestv1.KindSecrets},
			{key: "telemetry", kind: string(HostProviderKindTelemetry)},
			{key: "audit", kind: string(HostProviderKindAudit)},
			{key: "ui", kind: providermanifestv1.KindUI},
			{key: "indexeddb", kind: providermanifestv1.KindIndexedDB},
			{key: "cache", kind: providermanifestv1.KindCache},
			{key: "s3", kind: providermanifestv1.KindS3},
			{key: "workflow", kind: string(HostProviderKindWorkflow)},
			{key: "agent", kind: string(HostProviderKindAgent)},
		} {
			for _, entry := range mapValues(nestedMap(providers, section.key)) {
				resolveRelativePathsInEntry(section.kind, entry, baseDir)
			}
		}
	}
	if runtimeConfig := nestedMap(root, "runtime"); runtimeConfig != nil {
		for _, entry := range mapValues(nestedMap(runtimeConfig, "providers")) {
			resolveRelativePathsInEntry(providermanifestv1.KindRuntime, entry, baseDir)
		}
	}

	for _, entry := range mapValues(nestedMap(root, "plugins")) {
		resolveRelativePathsInEntry(providermanifestv1.KindPlugin, entry, baseDir)
	}
}

func resolveRelativePathsInEntry(kind string, entry map[string]any, baseDir string) {
	if entry == nil {
		return
	}
	resolveRelativeStringField(entry, "iconFile", baseDir)
	if localHarness, ok := entry["localHarness"].(map[string]any); ok {
		resolveRelativeStringField(localHarness, "workingDirectory", baseDir)
	}
	if harnesses, ok := entry["harnesses"].(map[string]any); ok {
		for _, harness := range mapValues(harnesses) {
			resolveRelativeStringField(harness, "workingDirectory", baseDir)
		}
	}
	if lifecycle, ok := entry["lifecycle"].(map[string]any); ok {
		if hooks, ok := lifecycle["sessionStart"].([]any); ok {
			for _, value := range hooks {
				if hook, ok := value.(map[string]any); ok {
					resolveRelativeStringField(hook, "cwd", baseDir)
				}
			}
		}
	}
	if source, ok := entry["source"].(map[string]any); ok {
		resolveRelativeStringField(source, "path", baseDir)
		return
	}
	sourceValue, ok := entry["source"].(string)
	if !ok {
		return
	}
	sourceValue = strings.TrimSpace(sourceValue)
	if sourceValue == "" || isBuiltinScalarSource(kind, sourceValue) || looksLikeMetadataURL(sourceValue) || looksLikeUnsupportedScalarSource(sourceValue) {
		return
	}
	entry["source"] = resolveRelativeConfigValue(baseDir, sourceValue)
}

func resolveRelativeStringField(fields map[string]any, key, baseDir string) {
	if fields == nil {
		return
	}
	value, ok := fields[key].(string)
	if !ok {
		return
	}
	fields[key] = resolveRelativeConfigValue(baseDir, value)
}

func nestedMap(root map[string]any, key string) map[string]any {
	if root == nil {
		return nil
	}
	current, _ := root[key].(map[string]any)
	return current
}

func mapValues(entries map[string]any) []map[string]any {
	if len(entries) == 0 {
		return nil
	}
	values := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		values = append(values, entryMap)
	}
	return values
}

func resolveRelativePaths(configPath string, cfg *Config) {
	baseDir := filepath.Dir(configPath)
	if absPath, err := filepath.Abs(configPath); err == nil {
		baseDir = filepath.Dir(absPath)
	}

	resolveEntry := func(entry *ProviderEntry) {
		if entry == nil {
			return
		}
		entry.IconFile = resolveRelativePath(baseDir, entry.IconFile)
		entry.Source.Path = resolveRelativePath(baseDir, entry.Source.Path)
		entry.Source.metadataPath = resolveRelativePath(baseDir, entry.Source.metadataPath)
		if entry.LocalHarness != nil {
			entry.LocalHarness.WorkingDirectory = resolveRelativePath(baseDir, entry.LocalHarness.WorkingDirectory)
		}
		for _, harness := range entry.Harnesses {
			if harness != nil {
				harness.WorkingDirectory = resolveRelativePath(baseDir, harness.WorkingDirectory)
			}
		}
		if entry.Lifecycle != nil {
			for i := range entry.Lifecycle.SessionStart {
				entry.Lifecycle.SessionStart[i].CWD = resolveRelativePath(baseDir, entry.Lifecycle.SessionStart[i].CWD)
			}
		}
	}

	for _, entry := range cfg.Providers.Authentication {
		resolveEntry(entry)
	}
	for _, entry := range cfg.Providers.Authorization {
		resolveEntry(entry)
	}
	for _, entry := range cfg.Providers.ExternalCredentials {
		resolveEntry(entry)
	}
	for _, entry := range cfg.Providers.Secrets {
		resolveEntry(entry)
	}
	for _, entry := range cfg.Providers.Telemetry {
		resolveEntry(entry)
	}
	for _, entry := range cfg.Providers.Audit {
		resolveEntry(entry)
	}
	for _, entry := range cfg.Providers.UI {
		if entry != nil {
			resolveEntry(&entry.ProviderEntry)
		}
	}
	for _, entry := range cfg.Providers.IndexedDB {
		resolveEntry(entry)
	}
	for _, entry := range cfg.Providers.Cache {
		resolveEntry(entry)
	}
	for _, entry := range cfg.Providers.S3 {
		resolveEntry(entry)
	}
	for _, entry := range cfg.Providers.Workflow {
		resolveEntry(entry)
	}
	for _, entry := range cfg.Providers.Agent {
		resolveEntry(entry)
	}
	for _, entry := range cfg.Runtime.Providers {
		if entry != nil {
			resolveEntry(&entry.ProviderEntry)
		}
	}
	for _, entry := range cfg.Plugins {
		resolveEntry(entry)
	}
}

func resolveRelativePath(baseDir, value string) string {
	return resolveRelativeConfigValue(baseDir, value)
}

func resolveRelativeConfigValue(baseDir, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	if strings.Contains(value, "${") {
		return value
	}
	return filepath.Clean(filepath.Join(baseDir, value))
}

func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration string")
	}
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid day duration %q: %w", s, err)
		}
		if days <= 0 {
			return 0, fmt.Errorf("duration must be positive, got %q", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration must be positive, got %q", s)
	}
	return d, nil
}

func NodeToMap(node yaml.Node) (map[string]any, error) {
	if node.Kind == 0 {
		return nil, nil
	}
	var out map[string]any
	if err := node.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}
