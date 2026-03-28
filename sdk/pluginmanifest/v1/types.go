package pluginmanifestv1

import _ "embed"

const (
	SchemaVersion  = 1
	SchemaVersion2 = 2

	KindProvider = "provider"
	KindRuntime  = "runtime"
	KindWebUI    = "webui"
)

type Manifest struct {
	SchemaVersion int            `json:"schema_version"`
	ID            string         `json:"id,omitempty"`
	Source        string         `json:"source,omitempty"`
	Version       string         `json:"version"`
	DisplayName   string         `json:"display_name,omitempty"`
	Description   string         `json:"description,omitempty"`
	Kinds         []string       `json:"kinds"`
	Provider      *Provider      `json:"provider,omitempty"`
	WebUI         *WebUIMetadata `json:"webui,omitempty"`
	Artifacts     []Artifact     `json:"artifacts,omitempty"`
	Entrypoints   Entrypoints    `json:"entrypoints,omitempty"`
}

type WebUIMetadata struct {
	AssetRoot string `json:"asset_root"`
}

type Provider struct {
	Protocol         ProtocolRange `json:"protocol"`
	ConfigSchemaPath string        `json:"config_schema_path,omitempty"`
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

//go:embed manifest.v2.jsonschema.json
var ManifestV2JSONSchema []byte
