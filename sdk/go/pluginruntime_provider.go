package gestalt

import (
	"context"
	"time"
)

type PluginRuntimeEgressMode string

const (
	PluginRuntimeEgressModeNone     PluginRuntimeEgressMode = "none"
	PluginRuntimeEgressModeCIDR     PluginRuntimeEgressMode = "cidr"
	PluginRuntimeEgressModeHostname PluginRuntimeEgressMode = "hostname"
)

type PluginRuntimeSupport struct {
	CanHostPlugins bool
	EgressMode     PluginRuntimeEgressMode
}

type PluginRuntimeSessionLifecycle struct {
	StartedAt          *time.Time
	RecommendedDrainAt *time.Time
	ExpiresAt          *time.Time
}

type PluginRuntimeSession struct {
	ID           string
	State        string
	Metadata     map[string]string
	Lifecycle    PluginRuntimeSessionLifecycle
	StateReason  string
	StateMessage string
}

type PluginRuntimeImagePullAuth struct {
	DockerConfigJSON string
}

type StartPluginRuntimeSessionRequest struct {
	PluginName    string
	Template      string
	Image         string
	Metadata      map[string]string
	ImagePullAuth *PluginRuntimeImagePullAuth
}

type StartHostedPluginRequest struct {
	SessionID     string
	PluginName    string
	Command       string
	Args          []string
	Env           map[string]string
	AllowedHosts  []string
	DefaultAction string
	HostBinary    string
}

type HostedPlugin struct {
	ID         string
	SessionID  string
	PluginName string
	DialTarget string
}

// PluginRuntimeProvider is implemented by providers that manage hosted
// executable-plugin runtime sessions over gRPC.
type PluginRuntimeProvider interface {
	Provider
	GetSupport(ctx context.Context) (PluginRuntimeSupport, error)
	StartSession(ctx context.Context, req StartPluginRuntimeSessionRequest) (PluginRuntimeSession, error)
	GetSession(ctx context.Context, sessionID string) (PluginRuntimeSession, error)
	ListSessions(ctx context.Context) ([]PluginRuntimeSession, error)
	StopSession(ctx context.Context, sessionID string) error
	StartPlugin(ctx context.Context, req StartHostedPluginRequest) (HostedPlugin, error)
}
