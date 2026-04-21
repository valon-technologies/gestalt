package pluginruntime

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
)

type SessionState string

const (
	HostedPluginBundleRoot              = "/workspace/plugin"
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

type Capabilities struct {
	HostedPluginRuntime bool
	HostServiceTunnels  bool
	ProviderGRPCTunnel  bool
	HostnameProxyEgress bool
	CIDREgress          bool
	HostPathExecution   bool
	ExecutionGOOS       string
	ExecutionGOARCH     string
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

type BindHostServiceRequest struct {
	SessionID string
	EnvVar    string
	Register  func(*grpc.Server)
}

type HostServiceBinding struct {
	ID         string
	SessionID  string
	EnvVar     string
	SocketPath string
}

// StartPluginRequest describes the plugin process to launch inside a runtime
// session. Implementations own allocation and injection of the plugin's
// provider listener endpoint (for example, GESTALT_PLUGIN_SOCKET); the host
// does not pass an explicit listener path through this contract.
type StartPluginRequest struct {
	SessionID     string
	PluginName    string
	Command       string
	Args          []string
	Env           map[string]string
	BundleDir     string
	AllowedHosts  []string
	DefaultAction PolicyAction
	HostBinary    string
	Cleanup       func()
}

type HostedPlugin struct {
	ID         string
	SessionID  string
	PluginName string
}

type DialPluginRequest struct {
	SessionID string
	PluginID  string
}

type HostedPluginConn interface {
	Lifecycle() proto.ProviderLifecycleClient
	Integration() proto.IntegrationProviderClient
	Close() error
}

type Provider interface {
	Capabilities(ctx context.Context) (Capabilities, error)
	StartSession(ctx context.Context, req StartSessionRequest) (*Session, error)
	GetSession(ctx context.Context, req GetSessionRequest) (*Session, error)
	StopSession(ctx context.Context, req StopSessionRequest) error
	BindHostService(ctx context.Context, req BindHostServiceRequest) (*HostServiceBinding, error)
	// StartPlugin launches a plugin inside the session. Implementations must
	// allocate and inject the plugin listener endpoint before startup and only
	// return success once DialPlugin can connect to the running plugin.
	StartPlugin(ctx context.Context, req StartPluginRequest) (*HostedPlugin, error)
	// DialPlugin connects to the listener established by StartPlugin using the
	// backend-specific transport or tunnel.
	DialPlugin(ctx context.Context, req DialPluginRequest) (HostedPluginConn, error)
	Close() error
}
