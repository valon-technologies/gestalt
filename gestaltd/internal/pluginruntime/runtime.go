package pluginruntime

import public "github.com/valon-technologies/gestalt/server/pluginruntime"

type SessionState = public.SessionState

const (
	HostedPluginBundleRoot = public.HostedPluginBundleRoot
	SessionStatePending    = public.SessionStatePending
	SessionStateReady      = public.SessionStateReady
	SessionStateRunning    = public.SessionStateRunning
	SessionStateStopped    = public.SessionStateStopped
	SessionStateFailed     = public.SessionStateFailed
)

type PolicyAction = public.PolicyAction

const (
	PolicyAllow = public.PolicyAllow
	PolicyDeny  = public.PolicyDeny
)

type Capabilities = public.Capabilities
type Session = public.Session
type StartSessionRequest = public.StartSessionRequest
type GetSessionRequest = public.GetSessionRequest
type StopSessionRequest = public.StopSessionRequest
type BindHostServiceRequest = public.BindHostServiceRequest
type HostServiceBinding = public.HostServiceBinding
type StartPluginRequest = public.StartPluginRequest
type HostedPlugin = public.HostedPlugin
type DialPluginRequest = public.DialPluginRequest
type HostedPluginConn = public.HostedPluginConn
type Provider = public.Provider
