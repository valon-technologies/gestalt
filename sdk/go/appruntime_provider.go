package gestalt

import (
	"context"
	"time"
)

type AppRuntimeEgressMode string

const (
	AppRuntimeEgressModeNone     AppRuntimeEgressMode = "none"
	AppRuntimeEgressModeCIDR     AppRuntimeEgressMode = "cidr"
	AppRuntimeEgressModeHostname AppRuntimeEgressMode = "hostname"
)

type AppRuntimeSupport struct {
	CanHostApps           bool
	EgressMode               AppRuntimeEgressMode
	SupportsPrepareWorkspace bool
}

type AppRuntimeSessionLifecycle struct {
	StartedAt          *time.Time
	RecommendedDrainAt *time.Time
	ExpiresAt          *time.Time
}

type AppRuntimeSession struct {
	ID           string
	State        string
	Metadata     map[string]string
	Lifecycle    AppRuntimeSessionLifecycle
	StateReason  string
	StateMessage string
}

type AppRuntimeImagePullAuth struct {
	DockerConfigJSON string
}

type StartAppRuntimeSessionRequest struct {
	AppName    string
	Template      string
	Image         string
	Metadata      map[string]string
	ImagePullAuth *AppRuntimeImagePullAuth
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

type PrepareAppRuntimeWorkspaceRequest struct {
	SessionID      string
	AgentSessionID string
	Workspace      *AgentWorkspace
}

type PrepareAppRuntimeWorkspaceResponse struct {
	Workspace *PreparedAgentWorkspace
}

type RemoveAppRuntimeWorkspaceRequest struct {
	SessionID      string
	AgentSessionID string
}

type StartHostedAppRequest struct {
	SessionID     string
	AppName    string
	Command       string
	Args          []string
	Env           map[string]string
	AllowedHosts  []string
	DefaultAction string
	HostBinary    string
	Workdir       string
}

type HostedApp struct {
	ID         string
	SessionID  string
	AppName string
	DialTarget string
}

// AppRuntimeProvider is implemented by providers that manage hosted
// executable-plugin runtime sessions over gRPC.
type AppRuntimeProvider interface {
	Provider
	GetSupport(ctx context.Context) (AppRuntimeSupport, error)
	StartSession(ctx context.Context, req StartAppRuntimeSessionRequest) (AppRuntimeSession, error)
	GetSession(ctx context.Context, sessionID string) (AppRuntimeSession, error)
	ListSessions(ctx context.Context) ([]AppRuntimeSession, error)
	StopSession(ctx context.Context, sessionID string) error
	StartApp(ctx context.Context, req StartHostedAppRequest) (HostedApp, error)
}

// AppRuntimeWorkspaceProvider can be implemented by runtime providers that
// prepare per-agent workspaces before a hosted agent provider session starts.
type AppRuntimeWorkspaceProvider interface {
	PrepareWorkspace(ctx context.Context, req PrepareAppRuntimeWorkspaceRequest) (PrepareAppRuntimeWorkspaceResponse, error)
	RemoveWorkspace(ctx context.Context, req RemoveAppRuntimeWorkspaceRequest) error
}
