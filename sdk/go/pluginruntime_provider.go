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
	CanHostPlugins           bool
	EgressMode               PluginRuntimeEgressMode
	SupportsPrepareWorkspace bool
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

type AgentWorkspace struct {
	Checkouts []AgentWorkspaceGitCheckout
	CWD       string
}

type AgentWorkspaceGitCheckout struct {
	URL  string
	Ref  string
	Path string
}

type PreparedAgentWorkspace struct {
	Root string
	CWD  string
}

type PreparePluginRuntimeWorkspaceRequest struct {
	SessionID      string
	AgentSessionID string
	Workspace      *AgentWorkspace
}

type PreparePluginRuntimeWorkspaceResponse struct {
	Workspace *PreparedAgentWorkspace
}

type RemovePluginRuntimeWorkspaceRequest struct {
	SessionID      string
	AgentSessionID string
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

// PluginRuntimeWorkspaceProvider can be implemented by runtime providers that
// prepare per-agent workspaces before a hosted agent provider session starts.
type PluginRuntimeWorkspaceProvider interface {
	PrepareWorkspace(ctx context.Context, req PreparePluginRuntimeWorkspaceRequest) (PreparePluginRuntimeWorkspaceResponse, error)
	RemoveWorkspace(ctx context.Context, req RemovePluginRuntimeWorkspaceRequest) error
}
