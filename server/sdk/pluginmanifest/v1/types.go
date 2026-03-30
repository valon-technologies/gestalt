package pluginmanifestv1

import _ "embed"

const (
	KindProvider = "provider"
	KindRuntime  = "runtime"
	KindWebUI    = "webui"
)

type Manifest struct {
	Source      string         `json:"source,omitempty"`
	Version     string         `json:"version"`
	DisplayName string         `json:"display_name,omitempty"`
	Description string         `json:"description,omitempty"`
	Kinds       []string       `json:"kinds"`
	Provider    *Provider      `json:"provider,omitempty"`
	WebUI       *WebUIMetadata `json:"webui,omitempty"`
	Artifacts   []Artifact     `json:"artifacts,omitempty"`
	Entrypoints Entrypoints    `json:"entrypoints,omitzero"`
}

type WebUIMetadata struct {
	AssetRoot string `json:"asset_root"`
}

type Provider struct {
	Protocol         ProtocolRange       `json:"protocol,omitzero"`
	ConfigSchemaPath string              `json:"config_schema_path,omitempty"`
	Auth             *ProviderAuth       `json:"auth,omitempty"`
	MCP              bool                `json:"mcp,omitempty"`
	BaseURL          string              `json:"base_url,omitempty"`
	Operations       []ProviderOperation `json:"operations,omitempty"`
}

func (p *Provider) IsDeclarative() bool {
	return p != nil && len(p.Operations) > 0
}

type ProviderOperation struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Method      string              `json:"method"`
	Path        string              `json:"path"`
	Parameters  []ProviderParameter `json:"parameters,omitempty"`
}

type ProviderParameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	In          string `json:"in"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

const (
	AuthTypeOAuth2 = "oauth2"
	AuthTypeBearer = "bearer"
	AuthTypeManual = "manual"
	AuthTypeNone   = "none"
)

type ProviderAuth struct {
	Type                string            `json:"type"`
	AuthorizationURL    string            `json:"authorization_url,omitempty"`
	TokenURL            string            `json:"token_url,omitempty"`
	Scopes              []string          `json:"scopes,omitempty"`
	PKCE                bool              `json:"pkce,omitempty"`
	ClientAuth          string            `json:"client_auth,omitempty"`
	TokenExchange       string            `json:"token_exchange,omitempty"`
	ScopeParam          string            `json:"scope_param,omitempty"`
	ScopeSeparator      string            `json:"scope_separator,omitempty"`
	AuthorizationParams map[string]string `json:"authorization_params,omitempty"`
	Credentials         []CredentialField `json:"credentials,omitempty"`
}

type CredentialField struct {
	Name        string `json:"name"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	HelpURL     string `json:"help_url,omitempty"`
}

type ProtocolRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

type Artifact struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type Entrypoints struct {
	Provider *Entrypoint `json:"provider,omitempty"`
	Runtime  *Entrypoint `json:"runtime,omitempty"`
}

type Entrypoint struct {
	ArtifactPath string   `json:"artifact_path"`
	Args         []string `json:"args,omitempty"`
}

//go:embed manifest.jsonschema.json
var ManifestJSONSchema []byte
