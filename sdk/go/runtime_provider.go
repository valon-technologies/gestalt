package gestalt

import (
	"context"
	"time"
)

type RuntimeEgressMode string

const (
	RuntimeEgressModeNone     RuntimeEgressMode = "none"
	RuntimeEgressModeCIDR     RuntimeEgressMode = "cidr"
	RuntimeEgressModeHostname RuntimeEgressMode = "hostname"
)

type RuntimeSupport struct {
	CanHostApps              bool
	EgressMode               RuntimeEgressMode
	SupportsPrepareWorkspace bool
}

type RuntimeSessionLifecycle struct {
	StartedAt          *time.Time
	RecommendedDrainAt *time.Time
	ExpiresAt          *time.Time
}

type RuntimeSession struct {
	ID           string
	State        string
	Metadata     map[string]string
	Lifecycle    RuntimeSessionLifecycle
	StateReason  string
	StateMessage string
}

type ListRuntimeSessionsRequest struct {
	PageSize  int
	PageToken string
}

type ListRuntimeSessionsResponse struct {
	Sessions      []RuntimeSession
	NextPageToken string
}

type RuntimeImagePullAuth struct {
	DockerConfigJSON string
}

type StartRuntimeSessionRequest struct {
	AppName       string
	Template      string
	Image         string
	Metadata      map[string]string
	ImagePullAuth *RuntimeImagePullAuth
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

type PrepareRuntimeWorkspaceRequest struct {
	SessionID      string
	AgentSessionID string
	Workspace      *AgentWorkspace
}

type PrepareRuntimeWorkspaceResponse struct {
	Workspace *PreparedAgentWorkspace
}

type RemoveRuntimeWorkspaceRequest struct {
	SessionID      string
	AgentSessionID string
}

type StartHostedAppRequest struct {
	SessionID     string
	AppName       string
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
	AppName    string
	DialTarget string
}

// RuntimeProvider is implemented by providers that manage hosted
// executable-runtime sessions over gRPC.
type RuntimeProvider interface {
	Provider
	GetSupport(ctx context.Context) (RuntimeSupport, error)
	StartSession(ctx context.Context, req StartRuntimeSessionRequest) (RuntimeSession, error)
	GetSession(ctx context.Context, sessionID string) (RuntimeSession, error)
	ListSessions(ctx context.Context, req ListRuntimeSessionsRequest) (ListRuntimeSessionsResponse, error)
	StopSession(ctx context.Context, sessionID string) error
	StartApp(ctx context.Context, req StartHostedAppRequest) (HostedApp, error)
}

// RuntimeWorkspaceProvider can be implemented by runtime providers that
// prepare per-agent workspaces before a hosted agent provider session starts.
type RuntimeWorkspaceProvider interface {
	PrepareWorkspace(ctx context.Context, req PrepareRuntimeWorkspaceRequest) (PrepareRuntimeWorkspaceResponse, error)
	RemoveWorkspace(ctx context.Context, req RemoveRuntimeWorkspaceRequest) error
}
