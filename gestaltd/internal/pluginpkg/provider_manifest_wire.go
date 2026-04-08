package pluginpkg

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/valon-technologies/gestalt/server/internal/config"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

type providerManifestWireRoot struct {
	Source      string                          `json:"source,omitempty" yaml:"source,omitempty"`
	Version     string                          `json:"version" yaml:"version"`
	DisplayName string                          `json:"display_name,omitempty" yaml:"display_name,omitempty"`
	Description string                          `json:"description,omitempty" yaml:"description,omitempty"`
	IconFile    string                          `json:"icon_file,omitempty" yaml:"icon_file,omitempty"`
	Kinds       []string                        `json:"kinds,omitempty" yaml:"kinds,omitempty"`
	Provider    *providerManifestWire           `json:"provider,omitempty" yaml:"provider,omitempty"`
	WebUI       *pluginmanifestv1.WebUIMetadata `json:"webui,omitempty" yaml:"webui,omitempty"`
	Artifacts   []pluginmanifestv1.Artifact     `json:"artifacts,omitempty" yaml:"artifacts,omitempty"`
}

type providerManifestWire struct {
	ConfigSchemaPath  string                                                 `json:"config_schema_path,omitempty" yaml:"config_schema_path,omitempty"`
	Exec              *providerExecWire                                      `json:"exec,omitempty" yaml:"exec,omitempty"`
	Connections       map[string]*providerManifestConnectionWire             `json:"connections,omitempty" yaml:"connections,omitempty"`
	Headers           map[string]string                                      `json:"headers,omitempty" yaml:"headers,omitempty"`
	ManagedParameters []pluginmanifestv1.ManagedParameter                    `json:"managed_parameters,omitempty" yaml:"managed_parameters,omitempty"`
	ResponseMapping   *pluginmanifestv1.ManifestResponseMapping              `json:"response_mapping,omitempty" yaml:"response_mapping,omitempty"`
	Pagination        *pluginmanifestv1.ManifestPaginationConfig             `json:"pagination,omitempty" yaml:"pagination,omitempty"`
	AllowedOperations map[string]*pluginmanifestv1.ManifestOperationOverride `json:"allowed_operations,omitempty" yaml:"allowed_operations,omitempty"`
	Surfaces          providerManifestSurfacesWire                           `json:"surfaces,omitempty" yaml:"surfaces,omitempty"`
	MCP               *providerManifestMCPWire                               `json:"mcp,omitempty" yaml:"mcp,omitempty"`
}

type providerExecWire struct {
	ArtifactPath string   `json:"artifact_path" yaml:"artifact_path"`
	Args         []string `json:"args,omitempty" yaml:"args,omitempty"`
}

type providerManifestConnectionWire struct {
	Mode      string                                              `json:"mode,omitempty" yaml:"mode,omitempty"`
	Auth      *pluginmanifestv1.ProviderAuth                      `json:"auth,omitempty" yaml:"auth,omitempty"`
	Params    map[string]pluginmanifestv1.ProviderConnectionParam `json:"params,omitempty" yaml:"params,omitempty"`
	Discovery *providerManifestDiscoveryWire                      `json:"discovery,omitempty" yaml:"discovery,omitempty"`
}

type providerManifestDiscoveryWire struct {
	URL      string            `json:"url" yaml:"url"`
	IDPath   string            `json:"id_path,omitempty" yaml:"id_path,omitempty"`
	NamePath string            `json:"name_path,omitempty" yaml:"name_path,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

type providerManifestSurfacesWire struct {
	REST    *providerManifestRESTSurfaceWire    `json:"rest,omitempty" yaml:"rest,omitempty"`
	OpenAPI *providerManifestOpenAPISurfaceWire `json:"openapi,omitempty" yaml:"openapi,omitempty"`
	GraphQL *providerManifestGraphQLSurfaceWire `json:"graphql,omitempty" yaml:"graphql,omitempty"`
	MCP     *providerManifestMCPSurfaceWire     `json:"mcp,omitempty" yaml:"mcp,omitempty"`
}

type providerManifestRESTSurfaceWire struct {
	Connection string                               `json:"connection,omitempty" yaml:"connection,omitempty"`
	BaseURL    string                               `json:"base_url" yaml:"base_url"`
	Operations []pluginmanifestv1.ProviderOperation `json:"operations" yaml:"operations"`
}

type providerManifestOpenAPISurfaceWire struct {
	Connection string `json:"connection,omitempty" yaml:"connection,omitempty"`
	Document   string `json:"document" yaml:"document"`
	BaseURL    string `json:"base_url,omitempty" yaml:"base_url,omitempty"`
}

type providerManifestGraphQLSurfaceWire struct {
	Connection string `json:"connection,omitempty" yaml:"connection,omitempty"`
	URL        string `json:"url" yaml:"url"`
}

type providerManifestMCPSurfaceWire struct {
	Connection string `json:"connection,omitempty" yaml:"connection,omitempty"`
	URL        string `json:"url" yaml:"url"`
}

type providerManifestMCPWire struct {
	Enabled bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
}

func decodeProviderManifestWire(data []byte, format string) (*pluginmanifestv1.Manifest, error) {
	var wire providerManifestWireRoot
	switch format {
	case ManifestFormatJSON:
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&wire); err != nil {
			return nil, fmt.Errorf("parse manifest JSON: %w", err)
		}
	case ManifestFormatYAML:
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(&wire); err != nil {
			return nil, fmt.Errorf("parse manifest YAML: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported manifest format %q", format)
	}
	return providerWireToInternal(&wire), nil
}

func providerWireToInternal(wire *providerManifestWireRoot) *pluginmanifestv1.Manifest {
	if wire == nil {
		return nil
	}

	manifest := &pluginmanifestv1.Manifest{
		Source:      wire.Source,
		Version:     wire.Version,
		DisplayName: wire.DisplayName,
		Description: wire.Description,
		IconFile:    wire.IconFile,
		Kinds:       append([]string(nil), wire.Kinds...),
		WebUI:       wire.WebUI,
		Artifacts:   append([]pluginmanifestv1.Artifact(nil), wire.Artifacts...),
	}

	if wire.Provider != nil {
		manifest.Plugin = &pluginmanifestv1.Plugin{
			ConfigSchemaPath:  wire.Provider.ConfigSchemaPath,
			Headers:           wire.Provider.Headers,
			ManagedParameters: wire.Provider.ManagedParameters,
			ResponseMapping:   wire.Provider.ResponseMapping,
			Pagination:        wire.Provider.Pagination,
			AllowedOperations: wire.Provider.AllowedOperations,
		}
		if len(manifest.Kinds) == 0 {
			manifest.Kinds = append(manifest.Kinds, pluginmanifestv1.KindPlugin)
		}
		if wire.Provider.MCP != nil {
			manifest.Plugin.MCP = wire.Provider.MCP.Enabled
		}
		if wire.Provider.Exec != nil {
			manifest.Entrypoints.Provider = &pluginmanifestv1.Entrypoint{
				ArtifactPath: wire.Provider.Exec.ArtifactPath,
				Args:         append([]string(nil), wire.Provider.Exec.Args...),
			}
		}
		if defaultConn, ok := wire.Provider.Connections["default"]; ok && defaultConn != nil {
			manifest.Plugin.Auth = defaultConn.Auth
			manifest.Plugin.ConnectionMode = defaultConn.Mode
			manifest.Plugin.ConnectionParams = defaultConn.Params
			manifest.Plugin.PostConnectDiscovery = defaultConn.toInternalDiscovery()
		}
		if len(wire.Provider.Connections) > 0 {
			manifest.Plugin.Connections = make(map[string]*pluginmanifestv1.ManifestConnectionDef, len(wire.Provider.Connections))
			for name, conn := range wire.Provider.Connections {
				if name == "default" {
					continue
				}
				if conn == nil {
					manifest.Plugin.Connections[name] = nil
					continue
				}
				manifest.Plugin.Connections[name] = &pluginmanifestv1.ManifestConnectionDef{
					Mode: conn.Mode,
					Auth: conn.Auth,
				}
			}
			if len(manifest.Plugin.Connections) == 0 {
				manifest.Plugin.Connections = nil
			}
		}
		if s := wire.Provider.Surfaces.REST; s != nil {
			manifest.Plugin.BaseURL = s.BaseURL
			manifest.Plugin.Operations = s.Operations
			manifest.Plugin.DefaultConnection = remapManifestWireConnectionName(s.Connection)
		}
		if s := wire.Provider.Surfaces.OpenAPI; s != nil {
			manifest.Plugin.OpenAPI = s.Document
			if s.BaseURL != "" {
				manifest.Plugin.BaseURL = s.BaseURL
			}
			manifest.Plugin.OpenAPIConnection = remapManifestWireConnectionName(s.Connection)
		}
		if s := wire.Provider.Surfaces.GraphQL; s != nil {
			manifest.Plugin.GraphQLURL = s.URL
			manifest.Plugin.GraphQLConnection = remapManifestWireConnectionName(s.Connection)
		}
		if s := wire.Provider.Surfaces.MCP; s != nil {
			manifest.Plugin.MCPURL = s.URL
			manifest.Plugin.MCPConnection = remapManifestWireConnectionName(s.Connection)
		}
	}
	if wire.WebUI != nil && len(manifest.Kinds) == 0 {
		manifest.Kinds = append(manifest.Kinds, pluginmanifestv1.KindWebUI)
	}
	return manifest
}

func encodeProviderManifestWire(manifest *pluginmanifestv1.Manifest, format string) ([]byte, error) {
	wire := internalProviderManifestToWire(manifest)
	switch format {
	case ManifestFormatJSON:
		data, err := json.MarshalIndent(wire, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal manifest: %w", err)
		}
		return append(data, '\n'), nil
	case ManifestFormatYAML:
		data, err := yaml.Marshal(wire)
		if err != nil {
			return nil, fmt.Errorf("marshal manifest YAML: %w", err)
		}
		return data, nil
	default:
		return nil, fmt.Errorf("unsupported manifest format %q", format)
	}
}

func internalProviderManifestToWire(manifest *pluginmanifestv1.Manifest) *providerManifestWireRoot {
	if manifest == nil {
		return nil
	}
	wire := &providerManifestWireRoot{
		Source:      manifest.Source,
		Version:     manifest.Version,
		DisplayName: manifest.DisplayName,
		Description: manifest.Description,
		IconFile:    manifest.IconFile,
		WebUI:       manifest.WebUI,
		Artifacts:   append([]pluginmanifestv1.Artifact(nil), manifest.Artifacts...),
	}
	if manifest.Plugin == nil {
		return wire
	}

	provider := &providerManifestWire{
		ConfigSchemaPath:  manifest.Plugin.ConfigSchemaPath,
		Headers:           manifest.Plugin.Headers,
		ManagedParameters: manifest.Plugin.ManagedParameters,
		ResponseMapping:   manifest.Plugin.ResponseMapping,
		Pagination:        manifest.Plugin.Pagination,
		AllowedOperations: manifest.Plugin.AllowedOperations,
	}
	if manifest.Entrypoints.Provider != nil {
		provider.Exec = &providerExecWire{
			ArtifactPath: manifest.Entrypoints.Provider.ArtifactPath,
			Args:         append([]string(nil), manifest.Entrypoints.Provider.Args...),
		}
	}
	if manifest.Plugin.MCP {
		provider.MCP = &providerManifestMCPWire{Enabled: true}
	}
	if manifest.Plugin.ConnectionMode != "" || manifest.Plugin.Auth != nil || len(manifest.Plugin.ConnectionParams) > 0 || manifest.Plugin.PostConnectDiscovery != nil {
		provider.Connections = map[string]*providerManifestConnectionWire{
			"default": {
				Mode:      manifest.Plugin.ConnectionMode,
				Auth:      manifest.Plugin.Auth,
				Params:    manifest.Plugin.ConnectionParams,
				Discovery: internalDiscoveryToWire(manifest.Plugin.PostConnectDiscovery),
			},
		}
	}
	if len(manifest.Plugin.Connections) > 0 {
		if provider.Connections == nil {
			provider.Connections = make(map[string]*providerManifestConnectionWire, len(manifest.Plugin.Connections))
		}
		for name, conn := range manifest.Plugin.Connections {
			if conn == nil {
				provider.Connections[name] = nil
				continue
			}
			provider.Connections[name] = &providerManifestConnectionWire{
				Mode: conn.Mode,
				Auth: conn.Auth,
			}
		}
	}
	switch {
	case manifest.Plugin.IsDeclarative():
		provider.Surfaces.REST = &providerManifestRESTSurfaceWire{
			Connection: manifestDefaultConnectionToWire(manifest.Plugin.DefaultConnection),
			BaseURL:    manifest.Plugin.BaseURL,
			Operations: manifest.Plugin.Operations,
		}
	case manifest.Plugin.OpenAPI != "":
		provider.Surfaces.OpenAPI = &providerManifestOpenAPISurfaceWire{
			Connection: manifestDefaultConnectionToWire(manifest.Plugin.OpenAPIConnection),
			Document:   manifest.Plugin.OpenAPI,
			BaseURL:    manifest.Plugin.BaseURL,
		}
	case manifest.Plugin.GraphQLURL != "":
		provider.Surfaces.GraphQL = &providerManifestGraphQLSurfaceWire{
			Connection: manifestDefaultConnectionToWire(manifest.Plugin.GraphQLConnection),
			URL:        manifest.Plugin.GraphQLURL,
		}
	}
	if manifest.Plugin.MCPURL != "" {
		provider.Surfaces.MCP = &providerManifestMCPSurfaceWire{
			Connection: manifestDefaultConnectionToWire(manifest.Plugin.MCPConnection),
			URL:        manifest.Plugin.MCPURL,
		}
	}
	wire.Provider = provider
	return wire
}

func (w *providerManifestConnectionWire) toInternalDiscovery() *pluginmanifestv1.ProviderPostConnectDiscovery {
	if w == nil || w.Discovery == nil {
		return nil
	}
	return &pluginmanifestv1.ProviderPostConnectDiscovery{
		URL:             w.Discovery.URL,
		IDPath:          w.Discovery.IDPath,
		NamePath:        w.Discovery.NamePath,
		MetadataMapping: w.Discovery.Metadata,
	}
}

func internalDiscoveryToWire(discovery *pluginmanifestv1.ProviderPostConnectDiscovery) *providerManifestDiscoveryWire {
	if discovery == nil {
		return nil
	}
	return &providerManifestDiscoveryWire{
		URL:      discovery.URL,
		IDPath:   discovery.IDPath,
		NamePath: discovery.NamePath,
		Metadata: discovery.MetadataMapping,
	}
}

func remapManifestWireConnectionName(name string) string {
	switch name {
	case "", "default":
		return ""
	default:
		return name
	}
}

func manifestDefaultConnectionToWire(name string) string {
	switch name {
	case "", config.PluginConnectionAlias, config.PluginConnectionName:
		return ""
	default:
		return name
	}
}
