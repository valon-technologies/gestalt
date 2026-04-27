package pluginruntime

import (
	"context"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

type SessionState string

const (
	HostedPluginBundleRoot              = gestalt.HostedPluginBundleRoot
	SessionStatePending    SessionState = "pending"
	SessionStateReady      SessionState = "ready"
	SessionStateRunning    SessionState = "running"
	SessionStateStopped    SessionState = "stopped"
	SessionStateFailed     SessionState = "failed"
)

// PolicyAction mirrors the host egress default for runtime-launched plugins.
// Runtime backends live outside gestaltd internals, so this contract cannot
// depend on the server's internal egress package directly.
type PolicyAction string

const (
	PolicyAllow PolicyAction = "allow"
	PolicyDeny  PolicyAction = "deny"
)

type HostServiceAccess string

const (
	HostServiceAccessNone   HostServiceAccess = "none"
	HostServiceAccessDirect HostServiceAccess = "direct"
)

type EgressMode string

const (
	EgressModeNone     EgressMode = "none"
	EgressModeCIDR     EgressMode = "cidr"
	EgressModeHostname EgressMode = "hostname"
)

type LaunchMode string

const (
	LaunchModeHostPath LaunchMode = "host_path"
	LaunchModeBundle   LaunchMode = "bundle"
)

type ExecutionTarget struct {
	GOOS   string
	GOARCH string
}

type Support struct {
	CanHostPlugins    bool
	HostServiceAccess HostServiceAccess
	EgressMode        EgressMode
	LaunchMode        LaunchMode
	ExecutionTarget   ExecutionTarget
}

type Session struct {
	ID       string
	State    SessionState
	Metadata map[string]string
}

type StartSessionRequest struct {
	PluginName string
	Template   string
	Image      string
	Metadata   map[string]string
}

type GetSessionRequest struct {
	SessionID string
}

type StopSessionRequest struct {
	SessionID string
}

type HostServiceRelay struct {
	DialTarget string
}

type BindHostServiceRequest struct {
	SessionID string
	EnvVar    string
	Relay     HostServiceRelay
}

type HostServiceBinding struct {
	ID        string
	SessionID string
	EnvVar    string
	Relay     HostServiceRelay
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
	BundleDir  string
	Egress     RuntimeEgressPolicy
	// Deprecated: use Egress.AllowedHosts.
	AllowedHosts []string
	// Deprecated: use Egress.DefaultAction.
	DefaultAction PolicyAction
	HostBinary    string
}

type RuntimeEgressPolicy struct {
	AllowedHosts  []string
	DefaultAction PolicyAction
}

func (r StartPluginRequest) EgressPolicy() RuntimeEgressPolicy {
	if len(r.Egress.AllowedHosts) > 0 || r.Egress.DefaultAction != "" {
		return RuntimeEgressPolicy{
			AllowedHosts:  append([]string(nil), r.Egress.AllowedHosts...),
			DefaultAction: r.Egress.DefaultAction,
		}
	}
	return RuntimeEgressPolicy{
		AllowedHosts:  append([]string(nil), r.AllowedHosts...),
		DefaultAction: r.DefaultAction,
	}
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

type Provider interface {
	Support(ctx context.Context) (Support, error)
	ListSessions(ctx context.Context) ([]Session, error)
	StartSession(ctx context.Context, req StartSessionRequest) (*Session, error)
	GetSession(ctx context.Context, req GetSessionRequest) (*Session, error)
	StopSession(ctx context.Context, req StopSessionRequest) error
	BindHostService(ctx context.Context, req BindHostServiceRequest) (*HostServiceBinding, error)
	StartPlugin(ctx context.Context, req StartPluginRequest) (*HostedPlugin, error)
	Close() error
}
