package pluginproc

import (
	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
)

const ProtocolVersion = "gestalt-plugin/1"

const (
	methodInitialize         = "initialize"
	methodProviderExecute    = "provider.execute"
	methodAuthStart          = "auth.start"
	methodAuthExchangeCode   = "auth.exchange_code"
	methodAuthRefreshToken   = "auth.refresh_token"
	methodShutdown           = "shutdown"
	methodCancelRequest      = "$/cancelRequest"
	defaultStartupTimeoutSec = 10
	defaultRequestTimeoutSec = 60

	AuthTypeNone   = "none"
	AuthTypeManual = "manual"
	AuthTypeOAuth2 = "oauth2"
)

type HostInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type IntegrationInfo struct {
	Name   string `json:"name"`
	Config any    `json:"config,omitempty"`
}

type InitializeParams struct {
	ProtocolVersion string          `json:"protocolVersion"`
	HostInfo        HostInfo        `json:"hostInfo"`
	Integration     IntegrationInfo `json:"integration"`
}

type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	PluginInfo      PluginInfo         `json:"pluginInfo"`
	Provider        ProviderManifest   `json:"provider"`
	Capabilities    PluginCapabilities `json:"capabilities,omitempty"`
}

type PluginInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type ProviderManifest struct {
	DisplayName    string              `json:"displayName"`
	Description    string              `json:"description,omitempty"`
	ConnectionMode string              `json:"connectionMode,omitempty"`
	Operations     []core.Operation    `json:"operations"`
	Catalog        *catalog.Catalog    `json:"catalog,omitempty"`
	Auth           *ProviderAuthConfig `json:"auth,omitempty"`
}

type ProviderAuthConfig struct {
	Type string `json:"type,omitempty"`
}

type PluginCapabilities struct {
	Catalog      bool `json:"catalog,omitempty"`
	OAuth        bool `json:"oauth,omitempty"`
	ManualAuth   bool `json:"manualAuth,omitempty"`
	Cancellation bool `json:"cancellation,omitempty"`
}

type ExecuteParams struct {
	Operation string         `json:"operation"`
	Params    map[string]any `json:"params,omitempty"`
	Token     string         `json:"token,omitempty"`
	Meta      *RequestMeta   `json:"meta,omitempty"`
}

type RequestMeta struct {
	RequestID string `json:"requestId,omitempty"`
}

type AuthStartParams struct {
	State  string   `json:"state"`
	Scopes []string `json:"scopes,omitempty"`
}

type AuthStartResult struct {
	AuthURL  string `json:"authUrl"`
	Verifier string `json:"verifier,omitempty"`
}

type AuthExchangeCodeParams struct {
	Code     string `json:"code"`
	Verifier string `json:"verifier,omitempty"`
}

type AuthRefreshTokenParams struct {
	RefreshToken string `json:"refreshToken"`
}

type cancelParams struct {
	ID int64 `json:"id"`
}
