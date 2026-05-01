package pluginruntime

import (
	"context"
	"io"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

type SessionState string

const (
	SessionStatePending SessionState = "pending"
	SessionStateReady   SessionState = "ready"
	SessionStateRunning SessionState = "running"
	SessionStateStopped SessionState = "stopped"
	SessionStateFailed  SessionState = "failed"
)

// PolicyAction mirrors the host egress default for runtime-launched plugins.
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
	CanHostPlugins bool
	EgressMode     EgressMode
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
	PluginName    string
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

// StartPluginRequest describes the plugin process to launch inside a runtime
// session. Implementations own allocation and injection of the plugin's
// provider listener endpoint and must return a host-reachable dial target in
// HostedPlugin.DialTarget.
type StartPluginRequest struct {
	SessionID  string
	PluginName string
	Command    string
	Args       []string
	Env        map[string]string
	Egress     RuntimeEgressPolicy
	HostBinary string
}

type RuntimeEgressPolicy struct {
	AllowedHosts  []string
	DefaultAction PolicyAction
}

type HostedPlugin struct {
	ID         string
	SessionID  string
	PluginName string
	DialTarget string
}

type HostedPluginConn interface {
	Lifecycle() proto.ProviderLifecycleClient
	Integration() proto.IntegrationProviderClient
	Close() error
}

type HostedAgentConn interface {
	Lifecycle() proto.ProviderLifecycleClient
	Agent() proto.AgentProviderClient
	Close() error
}

type SupportProvider interface {
	Support(ctx context.Context) (Support, error)
}

type SessionLister interface {
	ListSessions(ctx context.Context) ([]Session, error)
}

type SessionStarter interface {
	StartSession(ctx context.Context, req StartSessionRequest) (*Session, error)
}

type SessionGetter interface {
	GetSession(ctx context.Context, req GetSessionRequest) (*Session, error)
}

type SessionStopper interface {
	StopSession(ctx context.Context, req StopSessionRequest) error
}

type PluginStarter interface {
	StartPlugin(ctx context.Context, req StartPluginRequest) (*HostedPlugin, error)
}

type Provider interface {
	SupportProvider
	SessionLister
	SessionStarter
	SessionGetter
	SessionStopper
	PluginStarter
	io.Closer
}
