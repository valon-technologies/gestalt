package gestalt

import (
	"context"
	"time"
)

// RuntimeEgressMode is the native message type for gestalt.provider.v1.RuntimeEgressMode.
type RuntimeEgressMode string

// The runtime egress modes.
const (
	RuntimeEgressModeNone     RuntimeEgressMode = "none"
	RuntimeEgressModeCIDR     RuntimeEgressMode = "cidr"
	RuntimeEgressModeHostname RuntimeEgressMode = "hostname"
)

// RuntimeSupport is the native message type for gestalt.provider.v1.RuntimeSupport.
type RuntimeSupport struct {
	CanHostApps              bool
	EgressMode               RuntimeEgressMode
	SupportsPrepareWorkspace bool
}

// RuntimeSessionLifecycle is the native message type for gestalt.provider.v1.RuntimeSessionLifecycle.
type RuntimeSessionLifecycle struct {
	StartedAt          *time.Time
	RecommendedDrainAt *time.Time
	ExpiresAt          *time.Time
}

// RuntimeSession is the native message type for gestalt.provider.v1.RuntimeSession.
type RuntimeSession struct {
	ID           string
	State        string
	Metadata     map[string]string
	Lifecycle    RuntimeSessionLifecycle
	StateReason  string
	StateMessage string
}

// ListRuntimeSessionsRequest is the native message type for gestalt.provider.v1.ListRuntimeSessionsRequest.
type ListRuntimeSessionsRequest struct {
	PageSize  int
	PageToken string
}

// ListRuntimeSessionsResponse is the native message type for gestalt.provider.v1.ListRuntimeSessionsResponse.
type ListRuntimeSessionsResponse struct {
	Sessions      []RuntimeSession
	NextPageToken string
}

// RuntimeImagePullAuth is the native message type for gestalt.provider.v1.RuntimeImagePullAuth.
type RuntimeImagePullAuth struct {
	DockerConfigJSON string
}

// StartRuntimeSessionRequest is the native message type for gestalt.provider.v1.StartRuntimeSessionRequest.
type StartRuntimeSessionRequest struct {
	AppName       string
	Template      string
	Image         string
	Metadata      map[string]string
	ImagePullAuth *RuntimeImagePullAuth
}

// AgentWorkspace is the native message type for gestalt.provider.v1.AgentWorkspace.
type AgentWorkspace struct {
	Checkouts []AgentWorkspaceGitCheckout
	CWD       string
}

// AgentWorkspaceGitCheckout is the native message type for gestalt.provider.v1.AgentWorkspaceGitCheckout.
type AgentWorkspaceGitCheckout struct {
	URL  string
	Ref  string
	Path string
}

// PreparedAgentWorkspace is the native message type for gestalt.provider.v1.PreparedAgentWorkspace.
type PreparedAgentWorkspace struct {
	Root string
	CWD  string
}

// PrepareRuntimeWorkspaceRequest is the native message type for gestalt.provider.v1.PrepareRuntimeWorkspaceRequest.
type PrepareRuntimeWorkspaceRequest struct {
	SessionID      string
	AgentSessionID string
	Workspace      *AgentWorkspace
}

// PrepareRuntimeWorkspaceResponse is the native message type for gestalt.provider.v1.PrepareRuntimeWorkspaceResponse.
type PrepareRuntimeWorkspaceResponse struct {
	Workspace *PreparedAgentWorkspace
}

// RemoveRuntimeWorkspaceRequest is the native message type for gestalt.provider.v1.RemoveRuntimeWorkspaceRequest.
type RemoveRuntimeWorkspaceRequest struct {
	SessionID      string
	AgentSessionID string
}

// StartHostedAppRequest is the native message type for gestalt.provider.v1.StartHostedAppRequest.
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

// HostedApp is the native message type for gestalt.provider.v1.HostedApp.
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
