package providermanifestv1

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	KindApp                 = "app"
	KindIdentity            = "identity"
	KindAuthorization       = "authorization"
	KindExternalCredentials = "externalcredentials"
	KindIndexedDB           = "indexeddb"
	KindCache               = "cache"
	KindS3                  = "s3"
	KindWorkflow            = "workflow"
	KindAgent               = "agent"
	KindSecrets             = "secrets"
	KindRuntime             = "runtime"
)

func NormalizeKind(kind string) string {
	return strings.TrimSpace(strings.ToLower(kind))
}

type Manifest struct {
	Kind          string                 `json:"kind,omitempty" yaml:"kind,omitempty"`
	Source        string                 `json:"source,omitempty" yaml:"source,omitempty"`
	Version       string                 `json:"version" yaml:"version"`
	DisplayName   string                 `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	Description   string                 `json:"description,omitempty" yaml:"description,omitempty"`
	IconFile      string                 `json:"iconFile,omitempty" yaml:"iconFile,omitempty"`
	Install       *SourceInstall         `json:"install,omitempty" yaml:"install,omitempty"`
	Build         *SourceBuild           `json:"build,omitempty" yaml:"build,omitempty"`
	Run           *SourceRun             `json:"run,omitempty" yaml:"run,omitempty"`
	Artifacts     []Artifact             `json:"artifacts,omitempty" yaml:"artifacts,omitempty"`
	Entrypoint    *Entrypoint            `json:"entrypoint,omitempty" yaml:"entrypoint,omitempty"`
	Spec          *Spec                  `json:"spec,omitempty" yaml:"spec,omitempty"`
	Dependencies  *ManifestDependencies  `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	Compatibility *ManifestCompatibility `json:"compatibility,omitempty" yaml:"compatibility,omitempty"`
}

// ManifestDependencies declares app registry contract requirements for a provider package.
type ManifestDependencies struct {
	Apps map[string]ManifestAppDependency `json:"apps,omitempty" yaml:"apps,omitempty"`
}

type ManifestAppDependency struct {
	Version    string                                 `json:"version,omitempty" yaml:"version,omitempty"`
	Operations map[string]ManifestOperationDependency `json:"operations,omitempty" yaml:"operations,omitempty"`
}

type ManifestOperationDependency struct {
	InputSchemaHash string `json:"inputSchemaHash,omitempty" yaml:"inputSchemaHash,omitempty"`
}

// ManifestCompatibility declares runtime compatibility constraints for a provider package.
type ManifestCompatibility struct {
	MinGestaltdVersion string `json:"minGestaltdVersion,omitempty" yaml:"minGestaltdVersion,omitempty"`
}

type SourceBuild struct {
	Workdir     string   `json:"workdir,omitempty" yaml:"workdir,omitempty"`
	Command     []string `json:"command" yaml:"command"`
	Inputs      []string `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	PrepareOnly bool     `json:"-" yaml:"-"`

	Commands []SourcePhaseCommand `json:"-" yaml:"-"`
	WireForm phaseWireForm        `json:"-" yaml:"-"`
}

func (b *SourceBuild) PhaseCommands() []SourcePhaseCommand {
	if b == nil {
		return nil
	}
	if len(b.Commands) > 0 {
		out := make([]SourcePhaseCommand, len(b.Commands))
		copy(out, b.Commands)
		return out
	}
	if len(b.Command) == 0 {
		return nil
	}
	return []SourcePhaseCommand{{
		Command: append([]string(nil), b.Command...),
		Workdir: b.Workdir,
		Inputs:  append([]string(nil), b.Inputs...),
	}}
}

func (b *SourceBuild) setCommands(commands []SourcePhaseCommand, wire phaseWireForm, prepareOnly bool) {
	b.Commands = commands
	b.WireForm = wire
	b.PrepareOnly = prepareOnly
	var env map[string]string
	var ready string
	syncLegacyPhaseFields(&b.Command, &b.Workdir, &env, &b.Inputs, &ready, commands, wire)
}

func (b *SourceBuild) UnmarshalJSON(data []byte) error {
	if b == nil {
		return nil
	}
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		*b = SourceBuild{}
		return nil
	}
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var nodes []json.RawMessage
		if err := json.Unmarshal(trimmed, &nodes); err != nil {
			return err
		}
		if allJSONScalars(nodes) {
			var command []string
			if err := json.Unmarshal(trimmed, &command); err != nil {
				return err
			}
			b.setCommands([]SourcePhaseCommand{{Command: command}}, phaseWirePrepareOnlySequence, true)
			return nil
		}
		commands, err := parsePhaseCommandListFromJSON(trimmed, sourceBuildWireFields, "build", false)
		if err != nil {
			return err
		}
		b.setCommands(commands, phaseWireCommandList, false)
		return nil
	}
	cmd, err := parsePhaseCommandFromJSON(trimmed, sourceBuildWireFields, "build")
	if err != nil {
		return err
	}
	b.setCommands([]SourcePhaseCommand{cmd}, phaseWireLegacyObject, false)
	return nil
}

func (b SourceBuild) MarshalJSON() ([]byte, error) {
	commands := b.PhaseCommands()
	if b.PrepareOnly || b.WireForm == phaseWirePrepareOnlySequence {
		if len(commands) != 1 {
			return nil, fmt.Errorf("prepare-only build must have exactly one command")
		}
		return json.Marshal(commands[0].Command)
	}
	switch b.WireForm {
	case phaseWirePrepareOnlySequence:
		if len(commands) != 1 {
			return nil, fmt.Errorf("prepare-only build must have exactly one command")
		}
		return json.Marshal(commands[0].Command)
	case phaseWireCommandList:
		return marshalPhaseCommandListJSON(commands, sourceBuildWireFields)
	default:
		if len(commands) != 1 {
			return marshalPhaseCommandListJSON(commands, sourceBuildWireFields)
		}
		return json.Marshal(sourcePhaseCommandWire(commands[0]))
	}
}

func (b *SourceBuild) UnmarshalYAML(value *yaml.Node) error {
	if b == nil {
		return nil
	}
	if value == nil {
		*b = SourceBuild{}
		return nil
	}
	switch value.Kind {
	case yaml.SequenceNode:
		commands, err := parsePhaseCommandListFromYAML(value, sourceBuildWireFields, "build", true)
		if err != nil {
			return err
		}
		wire := phaseWirePrepareOnlySequence
		if !allYAMLScalars(value) {
			wire = phaseWireCommandList
		}
		b.setCommands(commands, wire, wire == phaseWirePrepareOnlySequence)
		return nil
	case yaml.MappingNode:
		cmd, err := parsePhaseCommandFromYAML(value, sourceBuildWireFields, "build")
		if err != nil {
			return err
		}
		b.setCommands([]SourcePhaseCommand{cmd}, phaseWireLegacyObject, false)
		return nil
	default:
		return fmt.Errorf("build must be a sequence or mapping")
	}
}

func (b SourceBuild) MarshalYAML() (any, error) {
	commands := b.PhaseCommands()
	if b.PrepareOnly || b.WireForm == phaseWirePrepareOnlySequence {
		if len(commands) != 1 {
			return nil, fmt.Errorf("prepare-only build must have exactly one command")
		}
		return append([]string(nil), commands[0].Command...), nil
	}
	switch b.WireForm {
	case phaseWirePrepareOnlySequence:
		if len(commands) != 1 {
			return nil, fmt.Errorf("prepare-only build must have exactly one command")
		}
		return append([]string(nil), commands[0].Command...), nil
	case phaseWireCommandList:
		return marshalPhaseCommandListYAML(commands, sourceBuildWireFields)
	default:
		if len(commands) != 1 {
			return marshalPhaseCommandListYAML(commands, sourceBuildWireFields)
		}
		return sourcePhaseCommandWire(commands[0]), nil
	}
}

// SourceInstall declares a pre-build dependency-install command. It is a peer
// to Build and Run: gestaltd execs Command directly (no shell) once before any
// Build or Run execution for a local-source provider. Side-effect only — it
// never declares an entrypoint artifact and its output is not verified, like a
// prepare-only build. Inputs is the cache-key allowlist (typically the
// lockfile). Env may carry registry credentials.
type SourceInstall struct {
	Command []string          `json:"command"            yaml:"command"`
	Workdir string            `json:"workdir,omitempty"  yaml:"workdir,omitempty"`
	Inputs  []string          `json:"inputs,omitempty"   yaml:"inputs,omitempty"`
	Env     map[string]string `json:"env,omitempty"      yaml:"env,omitempty"`

	Commands []SourcePhaseCommand `json:"-" yaml:"-"`
	WireForm phaseWireForm        `json:"-" yaml:"-"`
}

func (i *SourceInstall) PhaseCommands() []SourcePhaseCommand {
	if i == nil {
		return nil
	}
	if len(i.Commands) > 0 {
		out := make([]SourcePhaseCommand, len(i.Commands))
		copy(out, i.Commands)
		return out
	}
	if len(i.Command) == 0 {
		return nil
	}
	return []SourcePhaseCommand{{
		Command: append([]string(nil), i.Command...),
		Workdir: i.Workdir,
		Inputs:  append([]string(nil), i.Inputs...),
		Env:     i.Env,
	}}
}

func (i *SourceInstall) setCommands(commands []SourcePhaseCommand, wire phaseWireForm) {
	i.Commands = commands
	i.WireForm = wire
	var ready string
	syncLegacyPhaseFields(&i.Command, &i.Workdir, &i.Env, &i.Inputs, &ready, commands, wire)
}

func (i *SourceInstall) UnmarshalJSON(data []byte) error {
	if i == nil {
		return nil
	}
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		*i = SourceInstall{}
		return nil
	}
	if len(trimmed) > 0 && trimmed[0] == '[' {
		commands, err := parsePhaseCommandListFromJSON(trimmed, sourceInstallWireFields, "install", false)
		if err != nil {
			return err
		}
		i.setCommands(commands, phaseWireCommandList)
		return nil
	}
	cmd, err := parsePhaseCommandFromJSON(trimmed, sourceInstallWireFields, "install")
	if err != nil {
		return err
	}
	i.setCommands([]SourcePhaseCommand{cmd}, phaseWireLegacyObject)
	return nil
}

func (i SourceInstall) MarshalJSON() ([]byte, error) {
	commands := i.PhaseCommands()
	switch i.WireForm {
	case phaseWireCommandList:
		return marshalPhaseCommandListJSON(commands, sourceInstallWireFields)
	default:
		if len(commands) != 1 {
			return marshalPhaseCommandListJSON(commands, sourceInstallWireFields)
		}
		return json.Marshal(sourcePhaseCommandWire(commands[0]))
	}
}

func (i *SourceInstall) UnmarshalYAML(value *yaml.Node) error {
	if i == nil {
		return nil
	}
	if value == nil {
		*i = SourceInstall{}
		return nil
	}
	switch value.Kind {
	case yaml.SequenceNode:
		commands, err := parsePhaseCommandListFromYAML(value, sourceInstallWireFields, "install", false)
		if err != nil {
			return err
		}
		i.setCommands(commands, phaseWireCommandList)
		return nil
	case yaml.MappingNode:
		cmd, err := parsePhaseCommandFromYAML(value, sourceInstallWireFields, "install")
		if err != nil {
			return err
		}
		i.setCommands([]SourcePhaseCommand{cmd}, phaseWireLegacyObject)
		return nil
	default:
		return fmt.Errorf("install must be a sequence or mapping")
	}
}

func (i SourceInstall) MarshalYAML() (any, error) {
	commands := i.PhaseCommands()
	switch i.WireForm {
	case phaseWireCommandList:
		return marshalPhaseCommandListYAML(commands, sourceInstallWireFields)
	default:
		if len(commands) != 1 {
			return marshalPhaseCommandListYAML(commands, sourceInstallWireFields)
		}
		return sourcePhaseCommandWire(commands[0]), nil
	}
}

// SourceRun declares how a local-source provider is executed from source.
// gestaltd execs Command directly (no shell) with the GESTALT_PROVIDER_SOCKET
// contract env vars set.
type SourceRun struct {
	Command      []string          `json:"command" yaml:"command"`
	Workdir      string            `json:"workdir,omitempty" yaml:"workdir,omitempty"`
	Env          map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	ReadyTimeout string            `json:"readyTimeout,omitempty" yaml:"readyTimeout,omitempty"`

	Commands []SourcePhaseCommand `json:"-" yaml:"-"`
	WireForm phaseWireForm        `json:"-" yaml:"-"`
}

func (r *SourceRun) PhaseCommands() []SourcePhaseCommand {
	if r == nil {
		return nil
	}
	if len(r.Commands) > 0 {
		out := make([]SourcePhaseCommand, len(r.Commands))
		copy(out, r.Commands)
		return out
	}
	if len(r.Command) == 0 {
		return nil
	}
	return []SourcePhaseCommand{{
		Command:      append([]string(nil), r.Command...),
		Workdir:      r.Workdir,
		Env:          r.Env,
		ReadyTimeout: r.ReadyTimeout,
	}}
}

func (r *SourceRun) setCommands(commands []SourcePhaseCommand, wire phaseWireForm) {
	r.Commands = commands
	r.WireForm = wire
	var inputs []string
	syncLegacyPhaseFields(&r.Command, &r.Workdir, &r.Env, &inputs, &r.ReadyTimeout, commands, wire)
}

func (r *SourceRun) UnmarshalJSON(data []byte) error {
	if r == nil {
		return nil
	}
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		*r = SourceRun{}
		return nil
	}
	if len(trimmed) > 0 && trimmed[0] == '[' {
		commands, err := parsePhaseCommandListFromJSON(trimmed, sourceRunWireFields, "run", false)
		if err != nil {
			return err
		}
		r.setCommands(commands, phaseWireCommandList)
		return nil
	}
	cmd, err := parsePhaseCommandFromJSON(trimmed, sourceRunWireFields, "run")
	if err != nil {
		return err
	}
	r.setCommands([]SourcePhaseCommand{cmd}, phaseWireLegacyObject)
	return nil
}

func (r SourceRun) MarshalJSON() ([]byte, error) {
	commands := r.PhaseCommands()
	switch r.WireForm {
	case phaseWireCommandList:
		return marshalPhaseCommandListJSON(commands, sourceRunWireFields)
	default:
		if len(commands) != 1 {
			return marshalPhaseCommandListJSON(commands, sourceRunWireFields)
		}
		return json.Marshal(sourcePhaseCommandWire(commands[0]))
	}
}

func (r *SourceRun) UnmarshalYAML(value *yaml.Node) error {
	if r == nil {
		return nil
	}
	if value == nil {
		*r = SourceRun{}
		return nil
	}
	switch value.Kind {
	case yaml.SequenceNode:
		commands, err := parsePhaseCommandListFromYAML(value, sourceRunWireFields, "run", false)
		if err != nil {
			return err
		}
		r.setCommands(commands, phaseWireCommandList)
		return nil
	case yaml.MappingNode:
		cmd, err := parsePhaseCommandFromYAML(value, sourceRunWireFields, "run")
		if err != nil {
			return err
		}
		r.setCommands([]SourcePhaseCommand{cmd}, phaseWireLegacyObject)
		return nil
	default:
		return fmt.Errorf("run must be a sequence or mapping")
	}
}

func (r SourceRun) MarshalYAML() (any, error) {
	commands := r.PhaseCommands()
	switch r.WireForm {
	case phaseWireCommandList:
		return marshalPhaseCommandListYAML(commands, sourceRunWireFields)
	default:
		if len(commands) != 1 {
			return marshalPhaseCommandListYAML(commands, sourceRunWireFields)
		}
		return sourcePhaseCommandWire(commands[0]), nil
	}
}

// Spec is a union type validated per kind. For auth/indexeddb/secrets only
// ConfigSchemaPath is valid. For apps all surface/connection fields are
// valid. For ui AssetRoot + ConfigSchemaPath.
type Spec struct {
	ConfigSchemaPath string `json:"configSchemaPath,omitempty" yaml:"configSchemaPath,omitempty"`

	// Plugin-specific fields
	RouteAuth         *RouteAuthRef                         `json:"auth,omitempty" yaml:"auth,omitempty"`
	SecuritySchemes   map[string]*HTTPSecurityScheme        `json:"securitySchemes,omitempty" yaml:"securitySchemes,omitempty"`
	HTTP              map[string]*HTTPBinding               `json:"http,omitempty" yaml:"http,omitempty"`
	MCP               bool                                  `json:"mcp,omitempty" yaml:"mcp,omitempty"`
	Headers           map[string]string                     `json:"headers,omitempty" yaml:"headers,omitempty"`
	ManagedParameters []ManagedParameter                    `json:"managedParameters,omitempty" yaml:"managedParameters,omitempty"`
	Surfaces          *ProviderSurfaces                     `json:"surfaces,omitempty" yaml:"surfaces,omitempty"`
	AllowedOperations map[string]*ManifestOperationOverride `json:"allowedOperations,omitempty" yaml:"allowedOperations,omitempty"`
	DefaultConnection string                                `json:"defaultConnection,omitempty" yaml:"defaultConnection,omitempty"`
	Connections       map[string]*ManifestConnectionDef     `json:"connections,omitempty" yaml:"connections,omitempty"`
	ResponseMapping   *ManifestResponseMapping              `json:"responseMapping,omitempty" yaml:"responseMapping,omitempty"`
	Pagination        *ManifestPaginationConfig             `json:"pagination,omitempty" yaml:"pagination,omitempty"`
	Access            *ProviderAccess                       `json:"access,omitempty" yaml:"access,omitempty"`
	Requires          []string                              `json:"requires,omitempty" yaml:"requires,omitempty"`
	AssetRoot         string                                `json:"assetRoot,omitempty" yaml:"assetRoot,omitempty"`
}

// ProviderAccess declares the initial user-facing app capability profile.
// These are app operation IDs, not upstream OAuth scopes; the connection
// provider remains responsible for requesting the scopes it needs.
type ProviderAccess struct {
	DefaultOperations []string `json:"defaultOperations,omitempty" yaml:"defaultOperations,omitempty"`
}

type RouteAuthRef struct {
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`
}

type HTTPSecuritySchemeType string

const (
	HTTPSecuritySchemeTypeHMAC   HTTPSecuritySchemeType = "hmac"
	HTTPSecuritySchemeTypeAPIKey HTTPSecuritySchemeType = "apiKey"
	HTTPSecuritySchemeTypeHTTP   HTTPSecuritySchemeType = "http"
	HTTPSecuritySchemeTypeNone   HTTPSecuritySchemeType = "none"
)

type HTTPIn string

const (
	HTTPInHeader HTTPIn = "header"
	HTTPInQuery  HTTPIn = "query"
)

type HTTPAuthScheme string

const (
	HTTPAuthSchemeBasic  HTTPAuthScheme = "basic"
	HTTPAuthSchemeBearer HTTPAuthScheme = "bearer"
)

type HTTPBinding struct {
	Path           string           `json:"path" yaml:"path"`
	Method         string           `json:"method" yaml:"method"`
	CredentialMode ConnectionMode   `json:"credentialMode,omitempty" yaml:"credentialMode,omitempty"`
	Streaming      bool             `json:"streaming,omitempty" yaml:"streaming,omitempty"`
	RequestBody    *HTTPRequestBody `json:"requestBody,omitempty" yaml:"requestBody,omitempty"`
	Security       string           `json:"security,omitempty" yaml:"security,omitempty"`
	Target         string           `json:"target" yaml:"target"`
}

type HTTPRequestBody struct {
	Required bool                      `json:"required,omitempty" yaml:"required,omitempty"`
	Content  map[string]*HTTPMediaType `json:"content,omitempty" yaml:"content,omitempty"`
}

type HTTPMediaType struct {
}

type HTTPSecurityScheme struct {
	Type            HTTPSecuritySchemeType `json:"type,omitempty" yaml:"type,omitempty"`
	Description     string                 `json:"description,omitempty" yaml:"description,omitempty"`
	SignatureHeader string                 `json:"signatureHeader,omitempty" yaml:"signatureHeader,omitempty"`
	SignaturePrefix string                 `json:"signaturePrefix,omitempty" yaml:"signaturePrefix,omitempty"`
	PayloadTemplate string                 `json:"payloadTemplate,omitempty" yaml:"payloadTemplate,omitempty"`
	TimestampHeader string                 `json:"timestampHeader,omitempty" yaml:"timestampHeader,omitempty"`
	MaxAgeSeconds   int                    `json:"maxAgeSeconds,omitempty" yaml:"maxAgeSeconds,omitempty"`
	Name            string                 `json:"name,omitempty" yaml:"name,omitempty"`
	In              HTTPIn                 `json:"in,omitempty" yaml:"in,omitempty"`
	Scheme          HTTPAuthScheme         `json:"scheme,omitempty" yaml:"scheme,omitempty"`
	Secret          *HTTPSecretRef         `json:"secret,omitempty" yaml:"secret,omitempty"`
}

type HTTPSecretRef struct {
	Env    string `json:"env,omitempty" yaml:"env,omitempty"`
	Secret string `json:"secret,omitempty" yaml:"secret,omitempty"`
}

func (s *Spec) IsDeclarative() bool {
	return s != nil && s.Surfaces != nil && s.Surfaces.REST != nil && len(s.Surfaces.REST.Operations) > 0
}

func (s *Spec) IsSpecLoaded() bool {
	return s != nil && s.Surfaces != nil &&
		(s.Surfaces.OpenAPI != nil || s.Surfaces.GraphQL != nil || s.Surfaces.MCP != nil)
}

func (s *Spec) IsManifestBacked() bool {
	return s != nil && (s.IsDeclarative() || s.IsSpecLoaded())
}

func (s *Spec) OpenAPIDocument() string {
	if s == nil || s.Surfaces == nil || s.Surfaces.OpenAPI == nil {
		return ""
	}
	return s.Surfaces.OpenAPI.Document
}

func (s *Spec) OpenAPIBaseURL() string {
	if s == nil || s.Surfaces == nil || s.Surfaces.OpenAPI == nil {
		return ""
	}
	return s.Surfaces.OpenAPI.BaseURL
}

func (s *Spec) SpecBaseURL() string {
	if u := s.RESTBaseURL(); u != "" {
		return u
	}
	return s.OpenAPIBaseURL()
}

func (s *Spec) GraphQLURL() string {
	if s == nil || s.Surfaces == nil || s.Surfaces.GraphQL == nil {
		return ""
	}
	return s.Surfaces.GraphQL.URL
}

func (s *Spec) MCPURL() string {
	if s == nil || s.Surfaces == nil || s.Surfaces.MCP == nil {
		return ""
	}
	return s.Surfaces.MCP.URL
}

func (s *Spec) RESTBaseURL() string {
	if s == nil || s.Surfaces == nil || s.Surfaces.REST == nil {
		return ""
	}
	return s.Surfaces.REST.BaseURL
}

func (s *Spec) RESTOperations() []ProviderOperation {
	if s == nil || s.Surfaces == nil || s.Surfaces.REST == nil {
		return nil
	}
	return s.Surfaces.REST.Operations
}

func (s *Spec) AccessDefaultOperations() []string {
	if s == nil || s.Access == nil {
		return nil
	}
	return slices.Clone(s.Access.DefaultOperations)
}

func (s *Spec) SurfaceConnectionName(surface string) string {
	if s == nil || s.Surfaces == nil {
		return ""
	}
	switch surface {
	case "openapi":
		if s.Surfaces.OpenAPI != nil {
			return s.Surfaces.OpenAPI.Connection
		}
	case "graphql":
		if s.Surfaces.GraphQL != nil {
			return s.Surfaces.GraphQL.Connection
		}
	case "mcp":
		if s.Surfaces.MCP != nil {
			return s.Surfaces.MCP.Connection
		}
	case "rest":
		if s.Surfaces.REST != nil {
			return s.Surfaces.REST.Connection
		}
	}
	return ""
}

func (s *Spec) DefaultConnectionDef() *ManifestConnectionDef {
	if s == nil || s.Connections == nil {
		return nil
	}
	return s.Connections["default"]
}

func (m *Manifest) IsHybridProvider() bool {
	return m != nil && m.Spec != nil && m.Spec.IsManifestBacked() && m.Entrypoint != nil
}

func (m *Manifest) IsDeclarativeOnlyProvider() bool {
	return m != nil && m.Spec != nil && m.Spec.IsManifestBacked() && m.Entrypoint == nil
}

type ProviderSurfaces struct {
	REST    *RESTSurface    `json:"rest,omitempty" yaml:"rest,omitempty"`
	OpenAPI *OpenAPISurface `json:"openapi,omitempty" yaml:"openapi,omitempty"`
	GraphQL *GraphQLSurface `json:"graphql,omitempty" yaml:"graphql,omitempty"`
	MCP     *MCPSurface     `json:"mcp,omitempty" yaml:"mcp,omitempty"`
}

type RESTSurface struct {
	Connection string              `json:"connection,omitempty" yaml:"connection,omitempty"`
	BaseURL    string              `json:"baseUrl" yaml:"baseUrl"`
	Operations []ProviderOperation `json:"operations" yaml:"operations"`
}

type OpenAPISurface struct {
	Connection string `json:"connection,omitempty" yaml:"connection,omitempty"`
	Document   string `json:"document" yaml:"document"`
	BaseURL    string `json:"baseUrl,omitempty" yaml:"baseUrl,omitempty"`
}

type GraphQLSurface struct {
	Connection string `json:"connection,omitempty" yaml:"connection,omitempty"`
	URL        string `json:"url" yaml:"url"`
}

type MCPSurface struct {
	Connection string `json:"connection,omitempty" yaml:"connection,omitempty"`
	URL        string `json:"url" yaml:"url"`
}

type ManifestResponseMapping struct {
	DataPath   string                     `json:"dataPath" yaml:"dataPath"`
	Pagination *ManifestPaginationMapping `json:"pagination,omitempty" yaml:"pagination,omitempty"`
}

type ManifestValueSelector struct {
	Source string `json:"source" yaml:"source"`
	Path   string `json:"path" yaml:"path"`
}

type ManifestPaginationMapping struct {
	HasMore *ManifestValueSelector `json:"hasMore,omitempty" yaml:"hasMore,omitempty"`
	Cursor  *ManifestValueSelector `json:"cursor,omitempty" yaml:"cursor,omitempty"`
}

type ProviderDiscovery struct {
	URL      string            `json:"url" yaml:"url"`
	IDPath   string            `json:"idPath,omitempty" yaml:"idPath,omitempty"`
	NamePath string            `json:"namePath,omitempty" yaml:"namePath,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

type ProviderConnectionParam struct {
	Required        bool   `json:"required,omitempty" yaml:"required,omitempty"`
	Description     string `json:"description,omitempty" yaml:"description,omitempty"`
	Default         string `json:"default,omitempty" yaml:"default,omitempty"`
	From            string `json:"from,omitempty" yaml:"from,omitempty"`
	Field           string `json:"field,omitempty" yaml:"field,omitempty"`
	AccountIdentity bool   `json:"accountIdentity,omitempty" yaml:"accountIdentity,omitempty"`
}

type ManifestPaginationConfig struct {
	Style        PaginationStyle        `json:"style" yaml:"style"`
	CursorParam  string                 `json:"cursorParam,omitempty" yaml:"cursorParam,omitempty"`
	Cursor       *ManifestValueSelector `json:"cursor,omitempty" yaml:"cursor,omitempty"`
	LimitParam   string                 `json:"limitParam,omitempty" yaml:"limitParam,omitempty"`
	DefaultLimit int                    `json:"defaultLimit,omitempty" yaml:"defaultLimit,omitempty"`
	ResultsPath  string                 `json:"resultsPath,omitempty" yaml:"resultsPath,omitempty"`
	MaxPages     int                    `json:"maxPages,omitempty" yaml:"maxPages,omitempty"`
}

type ManifestOperationOverride struct {
	Alias       string                    `json:"alias,omitempty" yaml:"alias,omitempty"`
	Description string                    `json:"description,omitempty" yaml:"description,omitempty"`
	Tags        []string                  `json:"tags,omitempty" yaml:"tags,omitempty"`
	Paginate    bool                      `json:"paginate,omitempty" yaml:"paginate,omitempty"`
	Pagination  *ManifestPaginationConfig `json:"pagination,omitempty" yaml:"pagination,omitempty"`
	GraphQL     *ManifestGraphQLOperation `json:"graphql,omitempty" yaml:"graphql,omitempty"`
}

type ManifestGraphQLOperation struct {
	Document      string `json:"document" yaml:"document"`
	OperationName string `json:"operationName,omitempty" yaml:"operationName,omitempty"`
}

type ManifestConnectionDef struct {
	DisplayName       string                             `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	Mode              ConnectionMode                     `json:"mode,omitempty" yaml:"mode,omitempty"`
	Exposure          string                             `json:"exposure,omitempty" yaml:"exposure,omitempty"`
	Auth              *ProviderAuth                      `json:"auth,omitempty" yaml:"auth,omitempty"`
	Params            map[string]ProviderConnectionParam `json:"params,omitempty" yaml:"params,omitempty"`
	Discovery         *ProviderDiscovery                 `json:"discovery,omitempty" yaml:"discovery,omitempty"`
	CredentialRefresh *CredentialRefreshConfig           `json:"credentialRefresh,omitempty" yaml:"credentialRefresh,omitempty"`
}

type CredentialRefreshConfig struct {
	RefreshInterval     string `json:"refreshInterval,omitempty" yaml:"refreshInterval,omitempty"`
	RefreshBeforeExpiry string `json:"refreshBeforeExpiry,omitempty" yaml:"refreshBeforeExpiry,omitempty"`
}

type ProviderOperation struct {
	Name               string                       `json:"name" yaml:"name"`
	Description        string                       `json:"description,omitempty" yaml:"description,omitempty"`
	Method             string                       `json:"method" yaml:"method"`
	Path               string                       `json:"path" yaml:"path"`
	Connection         string                       `json:"connection,omitempty" yaml:"connection,omitempty"`
	ConnectionSelector *OperationConnectionSelector `json:"connectionSelector,omitempty" yaml:"connectionSelector,omitempty"`
	Tags               []string                     `json:"tags,omitempty" yaml:"tags,omitempty"`
	Parameters         []ProviderParameter          `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

type OperationConnectionSelector struct {
	Parameter string            `json:"parameter" yaml:"parameter"`
	Default   string            `json:"default,omitempty" yaml:"default,omitempty"`
	Values    map[string]string `json:"values" yaml:"values"`
}

type ProviderParameter struct {
	Name        string `json:"name" yaml:"name"`
	Type        string `json:"type" yaml:"type"`
	In          string `json:"in" yaml:"in"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Required    bool   `json:"required,omitempty" yaml:"required,omitempty"`
	Internal    bool   `json:"internal,omitempty" yaml:"internal,omitempty"`
}

type ManagedParameter struct {
	In    string `json:"in" yaml:"in"`
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value" yaml:"value"`
}

type AuthType string

const (
	AuthTypeOAuth2   AuthType = "oauth2"
	AuthTypeMCPOAuth AuthType = "mcp_oauth"
	AuthTypeBearer   AuthType = "bearer"
	AuthTypeManual   AuthType = "manual"
	AuthTypeNone     AuthType = "none"
)

type ConnectionMode string

const (
	ConnectionModeNone    ConnectionMode = "none"
	ConnectionModeSubject ConnectionMode = "subject"
)

func NormalizeConnectionMode(mode ConnectionMode) ConnectionMode {
	normalized := normalizeConnectionModeValue(mode)
	switch normalized {
	case "", ConnectionModeSubject:
		return ConnectionModeSubject
	case ConnectionModeNone:
		return ConnectionModeNone
	default:
		return normalized
	}
}

func NormalizeOptionalConnectionMode(mode ConnectionMode) ConnectionMode {
	normalized := normalizeConnectionModeValue(mode)
	if normalized == "" {
		return ""
	}
	return NormalizeConnectionMode(normalized)
}

func normalizeConnectionModeValue(mode ConnectionMode) ConnectionMode {
	return ConnectionMode(strings.ToLower(strings.TrimSpace(string(mode))))
}

type PaginationStyle string

const (
	PaginationStyleCursor PaginationStyle = "cursor"
	PaginationStyleOffset PaginationStyle = "offset"
	PaginationStylePage   PaginationStyle = "page"
)

type ProviderAuth struct {
	Type                 AuthType              `json:"type" yaml:"type"`
	AuthorizationURL     string                `json:"authorizationUrl,omitempty" yaml:"authorizationUrl,omitempty"`
	TokenURL             string                `json:"tokenUrl,omitempty" yaml:"tokenUrl,omitempty"`
	ClientID             string                `json:"clientId,omitempty" yaml:"clientId,omitempty"`
	ClientSecret         string                `json:"clientSecret,omitempty" yaml:"clientSecret,omitempty"`
	Scopes               []string              `json:"scopes,omitempty" yaml:"scopes,omitempty"`
	PKCE                 bool                  `json:"pkce,omitempty" yaml:"pkce,omitempty"`
	ClientAuth           string                `json:"clientAuth,omitempty" yaml:"clientAuth,omitempty"`
	TokenExchange        string                `json:"tokenExchange,omitempty" yaml:"tokenExchange,omitempty"`
	TokenPrefix          string                `json:"tokenPrefix,omitempty" yaml:"tokenPrefix,omitempty"`
	AccessTokenPath      string                `json:"accessTokenPath,omitempty" yaml:"accessTokenPath,omitempty"`
	ScopeParam           string                `json:"scopeParam,omitempty" yaml:"scopeParam,omitempty"`
	ScopeSeparator       string                `json:"scopeSeparator,omitempty" yaml:"scopeSeparator,omitempty"`
	AuthorizationParams  map[string]string     `json:"authorizationParams,omitempty" yaml:"authorizationParams,omitempty"`
	TokenParams          map[string]string     `json:"tokenParams,omitempty" yaml:"tokenParams,omitempty"`
	RefreshParams        map[string]string     `json:"refreshParams,omitempty" yaml:"refreshParams,omitempty"`
	AcceptHeader         string                `json:"acceptHeader,omitempty" yaml:"acceptHeader,omitempty"`
	TokenMetadata        []string              `json:"tokenMetadata,omitempty" yaml:"tokenMetadata,omitempty"`
	TokenExchangeDrivers []TokenExchangeDriver `json:"tokenExchangeDrivers,omitempty" yaml:"tokenExchangeDrivers,omitempty"`
	Credentials          []CredentialField     `json:"credentials,omitempty" yaml:"credentials,omitempty"`
	AuthMapping          *AuthMapping          `json:"authMapping,omitempty" yaml:"authMapping,omitempty"`
}

type TokenExchangeDriver struct {
	Type            string            `json:"type" yaml:"type"`
	TargetPrincipal string            `json:"targetPrincipal,omitempty" yaml:"targetPrincipal,omitempty"`
	Scopes          []string          `json:"scopes,omitempty" yaml:"scopes,omitempty"`
	LifetimeSeconds int               `json:"lifetimeSeconds,omitempty" yaml:"lifetimeSeconds,omitempty"`
	Endpoint        string            `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	Params          map[string]string `json:"params,omitempty" yaml:"params,omitempty"`
}

type CredentialField struct {
	Name        string `json:"name" yaml:"name"`
	Label       string `json:"label,omitempty" yaml:"label,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

type AuthMapping struct {
	Headers map[string]AuthValue `json:"headers,omitempty" yaml:"headers,omitempty"`
	Basic   *BasicAuthMapping    `json:"basic,omitempty" yaml:"basic,omitempty"`
}

type BasicAuthMapping struct {
	Username AuthValue `json:"username" yaml:"username"`
	Password AuthValue `json:"password" yaml:"password"`
}

type AuthValue struct {
	Value     string         `json:"value,omitempty" yaml:"value,omitempty"`
	ValueFrom *AuthValueFrom `json:"valueFrom,omitempty" yaml:"valueFrom,omitempty"`
}

type AuthValueFrom struct {
	CredentialFieldRef *CredentialFieldRef `json:"credentialFieldRef,omitempty" yaml:"credentialFieldRef,omitempty"`
}

type CredentialFieldRef struct {
	Name string `json:"name" yaml:"name"`
}

type Artifact struct {
	OS     string `json:"os" yaml:"os"`
	Arch   string `json:"arch" yaml:"arch"`
	LibC   string `json:"libc,omitempty" yaml:"libc,omitempty"`
	Path   string `json:"path" yaml:"path"`
	SHA256 string `json:"sha256,omitempty" yaml:"sha256,omitempty"`
}

type Entrypoint struct {
	ArtifactPath string   `json:"artifactPath" yaml:"artifactPath"`
	Args         []string `json:"args,omitempty" yaml:"args,omitempty"`
}

type specJSONWire struct {
	ConfigSchemaPath  string                                `json:"configSchemaPath,omitempty"`
	Auth              *RouteAuthRef                         `json:"auth,omitempty"`
	SecuritySchemes   map[string]*HTTPSecurityScheme        `json:"securitySchemes,omitempty"`
	HTTP              map[string]*HTTPBinding               `json:"http,omitempty"`
	MCP               bool                                  `json:"mcp,omitempty"`
	Headers           map[string]string                     `json:"headers,omitempty"`
	ManagedParameters []ManagedParameter                    `json:"managedParameters,omitempty"`
	Surfaces          *ProviderSurfaces                     `json:"surfaces,omitempty"`
	AllowedOperations map[string]*ManifestOperationOverride `json:"allowedOperations,omitempty"`
	DefaultConnection string                                `json:"defaultConnection,omitempty"`
	Connections       map[string]*ManifestConnectionDef     `json:"connections,omitempty"`
	ResponseMapping   *ManifestResponseMapping              `json:"responseMapping,omitempty"`
	Pagination        *ManifestPaginationConfig             `json:"pagination,omitempty"`
	Access            *ProviderAccess                       `json:"access,omitempty"`
	Requires          []string                              `json:"requires,omitempty"`
	AssetRoot         string                                `json:"assetRoot,omitempty"`
}

type specYAMLWire struct {
	ConfigSchemaPath  string                                `yaml:"configSchemaPath,omitempty"`
	Auth              *RouteAuthRef                         `yaml:"auth,omitempty"`
	SecuritySchemes   map[string]*HTTPSecurityScheme        `yaml:"securitySchemes,omitempty"`
	HTTP              map[string]*HTTPBinding               `yaml:"http,omitempty"`
	MCP               bool                                  `yaml:"mcp,omitempty"`
	Headers           map[string]string                     `yaml:"headers,omitempty"`
	ManagedParameters []ManagedParameter                    `yaml:"managedParameters,omitempty"`
	Surfaces          *ProviderSurfaces                     `yaml:"surfaces,omitempty"`
	AllowedOperations map[string]*ManifestOperationOverride `yaml:"allowedOperations,omitempty"`
	DefaultConnection string                                `yaml:"defaultConnection,omitempty"`
	Connections       map[string]*ManifestConnectionDef     `yaml:"connections,omitempty"`
	ResponseMapping   *ManifestResponseMapping              `yaml:"responseMapping,omitempty"`
	Pagination        *ManifestPaginationConfig             `yaml:"pagination,omitempty"`
	Access            *ProviderAccess                       `yaml:"access,omitempty"`
	Requires          []string                              `yaml:"requires,omitempty"`
	AssetRoot         string                                `yaml:"assetRoot,omitempty"`
}

type specWire struct {
	ConfigSchemaPath  string                                `json:"configSchemaPath,omitempty" yaml:"configSchemaPath,omitempty"`
	Auth              *RouteAuthRef                         `json:"auth,omitempty" yaml:"auth,omitempty"`
	SecuritySchemes   map[string]*HTTPSecurityScheme        `json:"securitySchemes,omitempty" yaml:"securitySchemes,omitempty"`
	HTTP              map[string]*HTTPBinding               `json:"http,omitempty" yaml:"http,omitempty"`
	MCP               bool                                  `json:"mcp,omitempty" yaml:"mcp,omitempty"`
	Headers           map[string]string                     `json:"headers,omitempty" yaml:"headers,omitempty"`
	ManagedParameters []ManagedParameter                    `json:"managedParameters,omitempty" yaml:"managedParameters,omitempty"`
	Surfaces          *ProviderSurfaces                     `json:"surfaces,omitempty" yaml:"surfaces,omitempty"`
	AllowedOperations map[string]*ManifestOperationOverride `json:"allowedOperations,omitempty" yaml:"allowedOperations,omitempty"`
	DefaultConnection string                                `json:"defaultConnection,omitempty" yaml:"defaultConnection,omitempty"`
	Connections       map[string]*ManifestConnectionDef     `json:"connections,omitempty" yaml:"connections,omitempty"`
	ResponseMapping   *ManifestResponseMapping              `json:"responseMapping,omitempty" yaml:"responseMapping,omitempty"`
	Pagination        *ManifestPaginationConfig             `json:"pagination,omitempty" yaml:"pagination,omitempty"`
	Access            *ProviderAccess                       `json:"access,omitempty" yaml:"access,omitempty"`
	Requires          []string                              `json:"requires,omitempty" yaml:"requires,omitempty"`
	AssetRoot         string                                `json:"assetRoot,omitempty" yaml:"assetRoot,omitempty"`
}

func (s *Spec) UnmarshalJSON(data []byte) error {
	if err := validateJSONWireObjectFields(data, specWireFields); err != nil {
		return err
	}

	var raw specJSONWire
	if err := decodeJSONKnownFields(data, &raw); err != nil {
		return err
	}

	spec := Spec{
		ConfigSchemaPath:  raw.ConfigSchemaPath,
		RouteAuth:         raw.Auth,
		SecuritySchemes:   cloneHTTPSecuritySchemes(raw.SecuritySchemes),
		HTTP:              cloneHTTPBindings(raw.HTTP),
		MCP:               raw.MCP,
		Headers:           raw.Headers,
		ManagedParameters: raw.ManagedParameters,
		Surfaces:          raw.Surfaces,
		AllowedOperations: raw.AllowedOperations,
		DefaultConnection: raw.DefaultConnection,
		Connections:       raw.Connections,
		ResponseMapping:   raw.ResponseMapping,
		Pagination:        raw.Pagination,
		Access:            cloneProviderAccess(raw.Access),
		Requires:          raw.Requires,
		AssetRoot:         raw.AssetRoot,
	}

	*s = spec
	return nil
}

func (s Spec) MarshalJSON() ([]byte, error) {
	wire, err := s.canonicalWire()
	if err != nil {
		return nil, err
	}
	return json.Marshal(wire)
}

func (s *Spec) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		*s = Spec{}
		return nil
	}
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("spec must be a mapping")
	}
	if err := validateYAMLWireObjectFields(value, specWireFields, "spec"); err != nil {
		return err
	}

	var raw specYAMLWire
	if err := decodeYAMLKnownFields(value, &raw); err != nil {
		return err
	}

	spec := Spec{
		ConfigSchemaPath:  raw.ConfigSchemaPath,
		RouteAuth:         raw.Auth,
		SecuritySchemes:   cloneHTTPSecuritySchemes(raw.SecuritySchemes),
		HTTP:              cloneHTTPBindings(raw.HTTP),
		MCP:               raw.MCP,
		Headers:           raw.Headers,
		ManagedParameters: raw.ManagedParameters,
		Surfaces:          raw.Surfaces,
		AllowedOperations: raw.AllowedOperations,
		DefaultConnection: raw.DefaultConnection,
		Connections:       raw.Connections,
		ResponseMapping:   raw.ResponseMapping,
		Pagination:        raw.Pagination,
		Access:            cloneProviderAccess(raw.Access),
		Requires:          raw.Requires,
		AssetRoot:         raw.AssetRoot,
	}

	*s = spec
	return nil
}

func (s *Spec) MarshalYAML() (any, error) {
	if s == nil {
		return nil, nil
	}
	return s.canonicalWire()
}

func (s Spec) canonicalWire() (specWire, error) {
	return specWire{
		ConfigSchemaPath:  s.ConfigSchemaPath,
		Auth:              s.RouteAuth,
		SecuritySchemes:   cloneHTTPSecuritySchemes(s.SecuritySchemes),
		HTTP:              cloneHTTPBindings(s.HTTP),
		MCP:               s.MCP,
		Headers:           s.Headers,
		ManagedParameters: s.ManagedParameters,
		Surfaces:          s.Surfaces,
		AllowedOperations: s.AllowedOperations,
		DefaultConnection: s.DefaultConnection,
		Connections:       cloneManifestConnections(s.Connections),
		ResponseMapping:   s.ResponseMapping,
		Pagination:        s.Pagination,
		Access:            cloneProviderAccess(s.Access),
		Requires:          s.Requires,
		AssetRoot:         s.AssetRoot,
	}, nil
}

func cloneProviderAccess(src *ProviderAccess) *ProviderAccess {
	if src == nil {
		return nil
	}
	dst := *src
	dst.DefaultOperations = slices.Clone(src.DefaultOperations)
	return &dst
}

func cloneManifestConnections(src map[string]*ManifestConnectionDef) map[string]*ManifestConnectionDef {
	if src == nil {
		return nil
	}
	cloned := make(map[string]*ManifestConnectionDef, len(src))
	for name, def := range src {
		if def == nil {
			cloned[name] = nil
			continue
		}
		copyDef := *def
		copyDef.Params = maps.Clone(def.Params)
		copyDef.Discovery = cloneProviderDiscovery(def.Discovery)
		copyDef.CredentialRefresh = cloneCredentialRefreshConfig(def.CredentialRefresh)
		cloned[name] = &copyDef
	}
	return cloned
}

func cloneProviderDiscovery(src *ProviderDiscovery) *ProviderDiscovery {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Metadata = maps.Clone(src.Metadata)
	return &dst
}

func cloneCredentialRefreshConfig(src *CredentialRefreshConfig) *CredentialRefreshConfig {
	if src == nil {
		return nil
	}
	dst := *src
	return &dst
}

func cloneHTTPSecuritySchemes(src map[string]*HTTPSecurityScheme) map[string]*HTTPSecurityScheme {
	if src == nil {
		return nil
	}
	cloned := make(map[string]*HTTPSecurityScheme, len(src))
	for name, scheme := range src {
		cloned[name] = cloneHTTPSecurityScheme(scheme)
	}
	return cloned
}

func cloneHTTPSecurityScheme(src *HTTPSecurityScheme) *HTTPSecurityScheme {
	if src == nil {
		return nil
	}
	cloned := *src
	cloned.Secret = cloneHTTPSecretRef(src.Secret)
	return &cloned
}

func cloneHTTPSecretRef(src *HTTPSecretRef) *HTTPSecretRef {
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
		cloned[name] = cloneHTTPBinding(binding)
	}
	return cloned
}

func cloneHTTPBinding(src *HTTPBinding) *HTTPBinding {
	if src == nil {
		return nil
	}
	cloned := *src
	cloned.RequestBody = cloneHTTPRequestBody(src.RequestBody)
	return &cloned
}

func cloneHTTPRequestBody(src *HTTPRequestBody) *HTTPRequestBody {
	if src == nil {
		return nil
	}
	cloned := *src
	if src.Content != nil {
		cloned.Content = make(map[string]*HTTPMediaType, len(src.Content))
		for name, mediaType := range src.Content {
			cloned.Content[name] = cloneHTTPMediaType(mediaType)
		}
	}
	return &cloned
}

func cloneHTTPMediaType(src *HTTPMediaType) *HTTPMediaType {
	if src == nil {
		return nil
	}
	cloned := *src
	return &cloned
}

func decodeJSONKnownFields(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	return nil
}

func decodeYAMLKnownFields(node *yaml.Node, out any) error {
	data, err := yaml.Marshal(node)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil && err != io.EOF {
		return err
	}
	return nil
}

func validateJSONWireObjectFields(data []byte, allowed map[string]struct{}) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for name := range fields {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("json: unknown field %q", name)
		}
	}
	return nil
}

func validateYAMLWireObjectFields(node *yaml.Node, allowed map[string]struct{}, subject string) error {
	for i := 0; i+1 < len(node.Content); i += 2 {
		name := node.Content[i].Value
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("%s.%s is not supported", subject, name)
		}
	}
	return nil
}

var sourceBuildWireFields = map[string]struct{}{
	"workdir": {},
	"command": {},
	"inputs":  {},
}

var sourceInstallWireFields = map[string]struct{}{
	"command": {},
	"workdir": {},
	"inputs":  {},
	"env":     {},
}

var sourceRunWireFields = map[string]struct{}{
	"command":      {},
	"workdir":      {},
	"env":          {},
	"readyTimeout": {},
	"role":         {},
}

var specWireFields = map[string]struct{}{
	"configSchemaPath":  {},
	"auth":              {},
	"securitySchemes":   {},
	"http":              {},
	"mcp":               {},
	"headers":           {},
	"managedParameters": {},
	"surfaces":          {},
	"allowedOperations": {},
	"defaultConnection": {},
	"connections":       {},
	"responseMapping":   {},
	"pagination":        {},
	"access":            {},
	"requires":          {},
	"assetRoot":         {},
}

//go:embed manifest.jsonschema.json
var ManifestJSONSchema []byte
