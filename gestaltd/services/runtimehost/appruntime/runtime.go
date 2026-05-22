package appruntime

import (
	"context"
	"time"

	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
)

type SessionState string

const (
	SessionStatePending SessionState = "pending"
	SessionStateReady   SessionState = "ready"
	SessionStateRunning SessionState = "running"
	SessionStateStopped SessionState = "stopped"
	SessionStateFailed  SessionState = "failed"
)

// PolicyAction mirrors the host egress default for runtime-launched apps.
// Runtime backends live outside gestaltd internals, so this contract cannot
// depend on the server's internal egress package directly.
type PolicyAction string

const (
	PolicyAllow PolicyAction = "allow"
	PolicyDeny  PolicyAction = "deny"
)

type EgressMode string

const (
	EgressModeNone     EgressMode = "none"
	EgressModeCIDR     EgressMode = "cidr"
	EgressModeHostname EgressMode = "hostname"
)

type Support struct {
	CanHostApps           bool
	EgressMode               EgressMode
	SupportsPrepareWorkspace bool
}

type Session struct {
	ID           string
	State        SessionState
	Metadata     map[string]string
	Lifecycle    *SessionLifecycle
	StateReason  string
	StateMessage string
}

type SessionLifecycle struct {
	StartedAt          *time.Time
	RecommendedDrainAt *time.Time
	ExpiresAt          *time.Time
}

type StartSessionRequest struct {
	AppName    string
	Template      string
	Image         string
	ImagePullAuth *ImagePullAuth
	Metadata      map[string]string
}

type ImagePullAuth struct {
	DockerConfigJSON string
}

type GetSessionRequest struct {
	SessionID string
}

type StopSessionRequest struct {
	SessionID string
}

type Workspace struct {
	Checkouts []WorkspaceGitCheckout
	CWD       string
}

type WorkspaceGitCheckout struct {
	URL  string
	Ref  string
	Path string
}

type PreparedWorkspace struct {
	Root string
	CWD  string
}

type PrepareWorkspaceRequest struct {
	SessionID      string
	AgentSessionID string
	Workspace      *Workspace
}

type RemoveWorkspaceRequest struct {
	SessionID      string
	AgentSessionID string
}

// StartAppRequest describes the app process to launch inside a runtime
// session. Implementations own allocation and injection of the plugin's
// provider listener endpoint and must return a host-reachable dial target in
// HostedApp.DialTarget.
type StartAppRequest struct {
	SessionID  string
	AppName string
	Command    string
	Args       []string
	Workdir    string
	Env        map[string]string
	Egress     RuntimeEgressPolicy
	HostBinary string
}

type RuntimeEgressPolicy struct {
	AllowedHosts  []string
	DefaultAction PolicyAction
}

type HostedApp struct {
	ID         string
	SessionID  string
	AppName string
	DialTarget string
}

type HostedAppConn interface {
	Lifecycle() proto.ProviderLifecycleClient
	Integration() proto.AppProviderClient
	Close() error
}

type HostedAgentConn interface {
	Lifecycle() proto.ProviderLifecycleClient
	Agent() proto.AgentProviderClient
	Close() error
}

type HostedWorkflowConn interface {
	Lifecycle() proto.ProviderLifecycleClient
	Workflow() proto.WorkflowProviderClient
	Close() error
}

type Provider interface {
	Support(ctx context.Context) (Support, error)
	ListSessions(ctx context.Context) ([]Session, error)
	StartSession(ctx context.Context, req StartSessionRequest) (*Session, error)
	GetSession(ctx context.Context, req GetSessionRequest) (*Session, error)
	StopSession(ctx context.Context, req StopSessionRequest) error
	StartApp(ctx context.Context, req StartAppRequest) (*HostedApp, error)
	Close() error
}

type WorkspaceProvider interface {
	PrepareWorkspace(ctx context.Context, req PrepareWorkspaceRequest) (*PreparedWorkspace, error)
	RemoveWorkspace(ctx context.Context, req RemoveWorkspaceRequest) error
}
