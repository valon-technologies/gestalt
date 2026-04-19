package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	authorizationservice "github.com/valon-technologies/gestalt/server/services/authorization"
	cacheservice "github.com/valon-technologies/gestalt/server/services/cache"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/egressproxy"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	indexeddbservice "github.com/valon-technologies/gestalt/server/services/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/pluginruntime"
	"github.com/valon-technologies/gestalt/server/services/s3"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"google.golang.org/grpc"
)

func buildPluginRuntimeHostServices(name string, entry *config.ProviderEntry, deps Deps) ([]runtimehost.HostService, *plugininvokerservice.InvocationTokenManager, func(), error) {
	var (
		hostServices []runtimehost.HostService
		cleanup      func()
		invTokens    *plugininvokerservice.InvocationTokenManager
	)
	fail := func(err error) ([]runtimehost.HostService, *plugininvokerservice.InvocationTokenManager, func(), error) {
		if cleanup != nil {
			cleanup()
			cleanup = nil
		}
		return nil, nil, nil, err
	}

	effectiveIndexedDB, err := config.ResolveEffectivePluginIndexedDB(name, entry, deps.SelectedIndexedDBName, deps.IndexedDBDefs)
	if err != nil {
		return fail(err)
	}
	if effectiveIndexedDB.Enabled {
		services, indexedDBCleanup, err := buildPluginIndexedDBHostServices(name, effectiveIndexedDB, deps)
		if err != nil {
			return fail(err)
		}
		hostServices = append(hostServices, services...)
		cleanup = chainCleanup(cleanup, indexedDBCleanup)
	}
	if len(entry.Cache) > 0 {
		services, cacheCleanup, err := buildPluginCacheHostServices(name, entry, deps)
		if err != nil {
			return fail(err)
		}
		hostServices = append(hostServices, services...)
		cleanup = chainCleanup(cleanup, cacheCleanup)
	}
	if len(entry.S3) > 0 {
		services, err := buildPluginS3HostServices(name, entry, deps)
		if err != nil {
			return fail(err)
		}
		hostServices = append(hostServices, services...)
	}
	includeWorkflowManager := deps.WorkflowManager != nil || (deps.WorkflowRuntime != nil && deps.WorkflowRuntime.HasConfiguredProviders())
	includeAgentManager := deps.AgentManager != nil || deps.AgentRuntime != nil
	needInvocationTokens := len(entry.Invokes) > 0
	if includeWorkflowManager || includeAgentManager {
		needInvocationTokens = true
	}
	if needInvocationTokens {
		invTokens, err = plugininvokerservice.NewInvocationTokenManager(deps.EncryptionKey)
		if err != nil {
			return fail(err)
		}
	}
	if includeWorkflowManager {
		hostServices = append(hostServices, buildPluginWorkflowManagerHostService(name, deps, invTokens))
	}
	if includeAgentManager {
		hostServices = append(hostServices, buildPluginAgentManagerHostService(name, deps, invTokens))
	}
	if deps.AuthorizationProvider != nil && len(entry.EffectiveHTTPBindings()) > 0 {
		hostServices = append(hostServices, buildPluginAuthorizationHostService(deps.AuthorizationProvider))
	}
	if len(entry.Invokes) > 0 {
		hostServices = append(hostServices, buildPluginInvokerHostService(name, entry, deps, invTokens))
	}
	return hostServices, invTokens, cleanup, nil
}

func appendRuntimeLogHostService(hostServices []runtimehost.HostService, runtimeConfig config.EffectiveHostedRuntime, deps Deps, runtimePlan HostedRuntimePlan) []runtimehost.HostService {
	if deps.Services == nil || deps.Services.RuntimeSessionLogs == nil || runtimePlan.Resolved.HostServiceAccess == RuntimeHostServiceAccessNone {
		return hostServices
	}
	runtimeProviderName := runtimeSessionLogProviderName(runtimeConfig)
	return append(hostServices, runtimehost.HostService{
		Name:   "runtime_log_host",
		EnvVar: runtimehost.DefaultRuntimeLogHostSocketEnv,
		Register: func(srv *grpc.Server) {
			runtimehost.RegisterRuntimeLogHostServer(srv, runtimeProviderName, deps.Services.RuntimeSessionLogs.AppendSessionLogs)
		},
	})
}

func runtimeSessionLogProviderName(runtimeConfig config.EffectiveHostedRuntime) string {
	if name := strings.TrimSpace(runtimeConfig.ProviderName); name != "" {
		return name
	}
	return "local"
}

func withRuntimeSessionEnv(env map[string]string, sessionID string) map[string]string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return env
	}
	if env == nil {
		env = map[string]string{}
	}
	env[runtimehost.DefaultRuntimeSessionIDEnv] = sessionID
	return env
}

func withHostServiceTLSCAEnv(env map[string]string, deps Deps) map[string]string {
	caPEM := strings.TrimSpace(deps.HostServiceTLSCAPEM)
	caFile := strings.TrimSpace(deps.HostServiceTLSCAFile)
	if caPEM == "" && caFile == "" {
		return env
	}
	if env == nil {
		env = map[string]string{}
	}
	if caPEM != "" {
		env[hostServiceTLSCAPEMEnv] = caPEM
	} else {
		env[hostServiceTLSCAFileEnv] = caFile
	}
	return env
}

type hostServiceBindingDescriptor struct {
	Name   string
	EnvVar string
}

func hostServiceBindingDescriptorFromConfigured(hostService runtimehost.HostService) hostServiceBindingDescriptor {
	return hostServiceBindingDescriptor{
		Name:   strings.TrimSpace(hostService.Name),
		EnvVar: strings.TrimSpace(hostService.EnvVar),
	}
}

func hostServiceBindingDescriptorsFromConfigured(hostServices []runtimehost.HostService) []hostServiceBindingDescriptor {
	if len(hostServices) == 0 {
		return nil
	}
	out := make([]hostServiceBindingDescriptor, 0, len(hostServices))
	for _, hostService := range hostServices {
		out = append(out, hostServiceBindingDescriptorFromConfigured(hostService))
	}
	return out
}

func buildHostedRuntimeHostServiceEnv(providerName, sessionID string, hostService hostServiceBindingDescriptor, deps Deps) (map[string]string, string, error) {
	var (
		serviceKey   string
		serviceLabel string
		methodPrefix string
	)
	switch {
	case isIndexedDBHostServiceEnv(hostService.EnvVar):
		serviceKey = "indexeddb"
		serviceLabel = "IndexedDB"
		methodPrefix = "/" + proto.IndexedDB_ServiceDesc.ServiceName + "/"
	case isCacheHostServiceEnv(hostService.EnvVar):
		serviceKey = "cache"
		serviceLabel = "cache"
		methodPrefix = "/" + proto.Cache_ServiceDesc.ServiceName + "/"
	case isS3HostServiceEnv(hostService.EnvVar):
		serviceKey = "s3"
		serviceLabel = "S3"
		methodPrefix = "/" + proto.S3_ServiceDesc.ServiceName + "/"
	case hostService.EnvVar == workflowservice.DefaultManagerSocketEnv:
		serviceKey = "workflow_manager"
		serviceLabel = "workflow manager"
		methodPrefix = "/" + proto.WorkflowManagerHost_ServiceDesc.ServiceName + "/"
	case hostService.EnvVar == agentservice.DefaultHostSocketEnv:
		serviceKey = "agent_host"
		serviceLabel = "agent host"
		methodPrefix = "/" + proto.AgentHost_ServiceDesc.ServiceName + "/"
	case hostService.EnvVar == agentservice.DefaultManagerSocketEnv:
		serviceKey = "agent_manager"
		serviceLabel = "agent manager"
		methodPrefix = "/" + proto.AgentManagerHost_ServiceDesc.ServiceName + "/"
	case hostService.EnvVar == authorizationservice.DefaultSocketEnv:
		serviceKey = "authorization"
		serviceLabel = "authorization"
		methodPrefix = "/" + proto.AuthorizationProvider_ServiceDesc.ServiceName + "/"
	case hostService.EnvVar == plugininvokerservice.DefaultSocketEnv:
		serviceKey = "plugin_invoker"
		serviceLabel = "plugin invoker"
		methodPrefix = "/" + proto.PluginInvoker_ServiceDesc.ServiceName + "/"
	case hostService.EnvVar == runtimehost.DefaultRuntimeLogHostSocketEnv:
		serviceKey = "runtime_log_host"
		serviceLabel = "runtime log host"
		methodPrefix = "/" + proto.PluginRuntimeLogHost_ServiceDesc.ServiceName + "/"
	default:
		return nil, "", fmt.Errorf("host service %q requires public host service relay support", hostService.EnvVar)
	}
	relayDialTarget, relayEnv, relayHost, ok, err := buildHostedRuntimePublicHostServiceRelay(
		providerName,
		sessionID,
		hostService,
		deps,
		serviceKey,
		serviceLabel,
		methodPrefix,
	)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, "", fmt.Errorf("provider %q requires server.baseURL and server.encryptionKey to relay %s through the public host service relay", providerName, serviceLabel)
	}
	relayEnv[hostService.EnvVar] = relayDialTarget
	return relayEnv, relayHost, nil
}

type runtimeHostServiceSessionVerifier struct {
	providerName string
	provider     pluginruntime.Provider
}

func (v runtimeHostServiceSessionVerifier) VerifyHostServiceSession(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("runtime session id is required")
	}
	if v.provider == nil {
		return fmt.Errorf("plugin runtime provider is not configured")
	}
	session, err := v.provider.GetSession(ctx, pluginruntime.GetSessionRequest{SessionID: sessionID})
	if err != nil {
		return err
	}
	if session == nil {
		return fmt.Errorf("plugin runtime session %q was not found", sessionID)
	}
	if expected := strings.TrimSpace(v.providerName); expected != "" {
		if got := strings.TrimSpace(session.Metadata["provider_name"]); got != "" && got != expected {
			return fmt.Errorf("plugin runtime session %q belongs to provider %q", sessionID, got)
		}
	}
	if session.Lifecycle != nil && session.Lifecycle.ExpiresAt != nil {
		expiresAt := session.Lifecycle.ExpiresAt.UTC()
		if !time.Now().UTC().Before(expiresAt) {
			return fmt.Errorf("plugin runtime session %q expired at %s", sessionID, expiresAt.Format(time.RFC3339Nano))
		}
	}
	switch session.State {
	case pluginruntime.SessionStatePending, pluginruntime.SessionStateReady, pluginruntime.SessionStateRunning:
		return nil
	default:
		return fmt.Errorf("plugin runtime session %q is %s", sessionID, session.State)
	}
}

func registerPublicRuntimeHostServices(providerName string, hostServices []runtimehost.HostService, deps Deps, runtimePlan HostedRuntimePlan, runtimeProvider pluginruntime.Provider) (func(), error) {
	if runtimePlan.Resolved.HostServiceAccess != RuntimeHostServiceAccessRelay || deps.PublicHostServices == nil {
		return nil, nil
	}
	registerHostServices := publicRuntimeRegistryHostServices(hostServices)
	if len(registerHostServices) == 0 {
		return nil, nil
	}
	for _, hostService := range registerHostServices {
		if strings.TrimSpace(hostService.Name) == "" {
			return nil, fmt.Errorf("host service %q requires a service name for public relay", hostService.EnvVar)
		}
	}
	registration := deps.PublicHostServices.RegisterVerified(providerName, runtimeHostServiceSessionVerifier{
		providerName: providerName,
		provider:     runtimeProvider,
	}, registerHostServices...)
	return func() {
		registration.Unregister()
	}, nil
}

func buildHostedRuntimePublicEgressProxy(providerName, sessionID string, allowedHosts []string, defaultAction egress.PolicyAction, deps Deps) (map[string]string, error) {
	baseURL, explicitRelayBaseURL := hostedRuntimeRelayBaseURL(deps)
	if baseURL == "" || len(deps.EncryptionKey) == 0 {
		return nil, fmt.Errorf("provider %q requires server.baseURL and server.encryptionKey to enforce hostname-based egress for hosted runtimes", providerName)
	}
	proxyBaseURL, _, err := pluginRuntimePublicProxyBaseURL(baseURL, explicitRelayBaseURL)
	if err != nil {
		return nil, err
	}
	tokenManager, err := egressproxy.NewTokenManager(deps.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("init egress proxy tokens: %w", err)
	}
	token, err := tokenManager.MintToken(egressproxy.TokenRequest{
		PluginName:    providerName,
		SessionID:     sessionID,
		AllowedHosts:  slices.Clone(allowedHosts),
		DefaultAction: defaultAction,
		TTL:           pluginRuntimeEgressProxyTokenTTL,
	})
	if err != nil {
		return nil, fmt.Errorf("mint public egress proxy token: %w", err)
	}
	proxyURL := *proxyBaseURL
	proxyURL.User = url.UserPassword("gestalt-egress-proxy", token)
	return map[string]string{
		"HTTP_PROXY":  proxyURL.String(),
		"HTTPS_PROXY": proxyURL.String(),
	}, nil
}

func publicRuntimeRegistryHostServices(hostServices []runtimehost.HostService) []runtimehost.HostService {
	if len(hostServices) == 0 {
		return nil
	}
	return append([]runtimehost.HostService(nil), hostServices...)
}

func buildHostedRuntimePublicHostServiceRelay(providerName, sessionID string, hostService hostServiceBindingDescriptor, deps Deps, serviceKey, serviceLabel, methodPrefix string) (string, map[string]string, string, bool, error) {
	baseURL, explicitRelayBaseURL := hostedRuntimeRelayBaseURL(deps)
	if baseURL == "" || len(deps.EncryptionKey) == 0 {
		return "", nil, "", false, nil
	}
	dialTarget, relayHost, err := pluginRuntimePublicRelayTarget(baseURL, explicitRelayBaseURL)
	if err != nil {
		return "", nil, "", false, err
	}
	tokenManager, err := runtimehost.NewHostServiceRelayTokenManager(deps.EncryptionKey)
	if err != nil {
		return "", nil, "", false, fmt.Errorf("init host service relay tokens: %w", err)
	}
	token, err := tokenManager.MintToken(runtimehost.HostServiceRelayTokenRequest{
		PluginName:   providerName,
		SessionID:    sessionID,
		Service:      serviceKey,
		EnvVar:       hostService.EnvVar,
		MethodPrefix: methodPrefix,
		TTL:          pluginRuntimeHostServiceRelayTokenTTL,
	})
	if err != nil {
		return "", nil, "", false, fmt.Errorf("mint %s host service relay token: %w", serviceLabel, err)
	}
	return dialTarget, map[string]string{
		hostService.EnvVar + "_TOKEN": token,
	}, relayHost, true, nil
}

func pluginRuntimePublicRelayTarget(baseURL string, allowInsecureHTTP bool) (string, string, error) {
	parsed, host, err := pluginRuntimePublicProxyBaseURL(baseURL, allowInsecureHTTP)
	if err != nil {
		return "", "", err
	}
	port := parsed.Port()
	if port == "" {
		if strings.EqualFold(parsed.Scheme, "http") {
			port = "80"
		} else {
			port = "443"
		}
	}
	target := net.JoinHostPort(host, port)
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return "tls://" + target, host, nil
	case "http":
		return "tcp://" + target, host, nil
	default:
		return "", "", fmt.Errorf("server.baseURL %q has unsupported public runtime relay scheme %q", baseURL, parsed.Scheme)
	}
}

func pluginRuntimePublicProxyBaseURL(baseURL string, allowInsecureHTTP bool) (*url.URL, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, "", fmt.Errorf("parse server.baseURL for public runtime relay: %w", err)
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return nil, "", fmt.Errorf("server.baseURL %q is missing a hostname", baseURL)
	}
	if path := strings.TrimSpace(parsed.EscapedPath()); path != "" && path != "/" {
		return nil, "", fmt.Errorf("server.baseURL %q must not include a path for public runtime relay", baseURL)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, "", fmt.Errorf("server.baseURL %q must not include a query or fragment for public runtime relay", baseURL)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
	case "http":
		if !allowInsecureHTTP && !isLoopbackAllowedHost(host) {
			return nil, "", fmt.Errorf("server.baseURL %q must use https for public runtime relay unless it targets loopback", baseURL)
		}
	default:
		return nil, "", fmt.Errorf("server.baseURL %q must use https for public runtime relay", baseURL)
	}
	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, host, nil
}

func isIndexedDBHostServiceEnv(envVar string) bool {
	envVar = strings.TrimSpace(envVar)
	return envVar == indexeddbservice.DefaultSocketEnv || strings.HasPrefix(envVar, indexeddbservice.DefaultSocketEnv+"_")
}

func isCacheHostServiceEnv(envVar string) bool {
	envVar = strings.TrimSpace(envVar)
	return envVar == cacheservice.DefaultSocketEnv || strings.HasPrefix(envVar, cacheservice.DefaultSocketEnv+"_")
}

func isS3HostServiceEnv(envVar string) bool {
	envVar = strings.TrimSpace(envVar)
	return envVar == s3.DefaultSocketEnv || strings.HasPrefix(envVar, s3.DefaultSocketEnv+"_")
}

func appendAllowedHost(allowedHosts []string, host string) []string {
	host = strings.TrimSpace(host)
	if host == "" {
		return allowedHosts
	}
	for _, allowed := range allowedHosts {
		if strings.EqualFold(strings.TrimSpace(allowed), host) {
			return allowedHosts
		}
	}
	return append(allowedHosts, host)
}

func hostedAgentAllowedHosts(allowedHosts []string, runtimePlan HostedRuntimePlan) []string {
	cloned := slices.Clone(allowedHosts)
	if runtimePlan.Resolved.HostServiceAccess != RuntimeHostServiceAccessRelay || runtimePlan.RequiresHostnameEgress {
		return cloned
	}
	// Hosted agent bundles include loopback host allowances for local SDK
	// transports. Once the agent host is exposed over the public relay, those
	// loopback hosts are no longer relevant and can spuriously force hosted
	// runtimes into proxy-enforced egress mode.
	out := cloned[:0]
	for _, host := range cloned {
		if isLoopbackAllowedHost(host) {
			continue
		}
		out = append(out, host)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isLoopbackAllowedHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func buildPluginIndexedDBHostServices(pluginName string, effective config.EffectiveHostIndexedDBBinding, deps Deps) ([]runtimehost.HostService, func(), error) {
	if deps.IndexedDBFactory == nil || len(deps.IndexedDBDefs) == 0 {
		return nil, nil, fmt.Errorf("indexeddb host services are not available")
	}

	ds, err := buildPluginScopedIndexedDB(pluginName, effective, deps)
	if err != nil {
		return nil, nil, err
	}

	hostServices := []runtimehost.HostService{{
		Name:   "indexeddb",
		EnvVar: indexeddbservice.DefaultSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterIndexedDBServer(srv, indexeddbservice.NewServer(ds, pluginName, indexeddbservice.ServerOptions{
				AllowedStores: effective.ObjectStores,
			}))
		},
	}}
	return hostServices, func() {
		_ = closeIndexedDBs(ds)
	}, nil
}

func buildPluginCacheHostServices(pluginName string, entry *config.ProviderEntry, deps Deps) ([]runtimehost.HostService, func(), error) {
	if deps.CacheFactory == nil || len(deps.CacheDefs) == 0 {
		return nil, nil, fmt.Errorf("cache host services are not available")
	}

	hostServices := make([]runtimehost.HostService, 0, len(entry.Cache)+1)
	boundCaches := make([]corecache.Cache, 0, len(entry.Cache))
	for _, bindingName := range entry.Cache {
		def, ok := deps.CacheDefs[bindingName]
		if !ok || def == nil {
			_ = closeCaches(boundCaches...)
			return nil, nil, fmt.Errorf("cache %q is not available", bindingName)
		}
		value, err := buildCache(def, &FactoryRegistry{Cache: deps.CacheFactory})
		if err != nil {
			_ = closeCaches(boundCaches...)
			return nil, nil, fmt.Errorf("cache %q: %w", bindingName, err)
		}
		boundCaches = append(boundCaches, value)
		hostServices = append(hostServices, runtimehost.HostService{
			Name:   "cache",
			EnvVar: cacheservice.SocketEnv(bindingName),
			Register: func(cacheValue corecache.Cache) func(*grpc.Server) {
				return func(srv *grpc.Server) {
					proto.RegisterCacheServer(srv, cacheservice.NewServer(cacheValue, pluginName))
				}
			}(value),
		})
	}
	if len(boundCaches) == 1 {
		value := boundCaches[0]
		hostServices = append(hostServices, runtimehost.HostService{
			Name:   "cache",
			EnvVar: cacheservice.DefaultSocketEnv,
			Register: func(srv *grpc.Server) {
				proto.RegisterCacheServer(srv, cacheservice.NewServer(value, pluginName))
			},
		})
	}
	return hostServices, func() {
		_ = closeCaches(boundCaches...)
	}, nil
}

func buildPluginS3HostServices(pluginName string, entry *config.ProviderEntry, deps Deps) ([]runtimehost.HostService, error) {
	if len(deps.S3) == 0 {
		return nil, fmt.Errorf("s3 host services are not available")
	}

	var accessURLs *s3.ObjectAccessURLManager
	if len(deps.EncryptionKey) != 0 {
		var err error
		accessURLs, err = s3.NewObjectAccessURLManager(deps.EncryptionKey, deps.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("s3 object access URLs: %w", err)
		}
	}

	hostServices := make([]runtimehost.HostService, 0, len(entry.S3)+1)
	for _, binding := range entry.S3 {
		client, ok := deps.S3[binding]
		if !ok || client == nil {
			return nil, fmt.Errorf("s3 %q is not available", binding)
		}
		hostServices = append(hostServices, runtimehost.HostService{
			Name:   "s3",
			EnvVar: s3.SocketEnv(binding),
			Register: func(client s3store.Client, binding string) func(*grpc.Server) {
				return func(srv *grpc.Server) {
					proto.RegisterS3Server(srv, s3.NewServerWithOptions(client, pluginName, s3.ServerOptions{
						BindingName: binding,
						AccessURLs:  accessURLs,
					}))
					proto.RegisterS3ObjectAccessServer(srv, s3.NewObjectAccessServer(accessURLs, pluginName, binding))
				}
			}(client, binding),
		})
	}
	if len(entry.S3) == 1 {
		binding := entry.S3[0]
		client := deps.S3[binding]
		hostServices = append(hostServices, runtimehost.HostService{
			Name:   "s3",
			EnvVar: s3.DefaultSocketEnv,
			Register: func(srv *grpc.Server) {
				proto.RegisterS3Server(srv, s3.NewServerWithOptions(client, pluginName, s3.ServerOptions{
					BindingName: binding,
					AccessURLs:  accessURLs,
				}))
				proto.RegisterS3ObjectAccessServer(srv, s3.NewObjectAccessServer(accessURLs, pluginName, binding))
			},
		})
	}
	return hostServices, nil
}

func buildWorkflowIndexedDBHostServices(name string, effective config.EffectiveHostIndexedDBBinding, deps Deps) ([]runtimehost.HostService, func(), error) {
	if deps.IndexedDBFactory == nil || len(deps.IndexedDBDefs) == 0 {
		return nil, nil, fmt.Errorf("indexeddb host services are not available")
	}

	ds, err := buildWorkflowScopedIndexedDB(name, effective, deps)
	if err != nil {
		return nil, nil, err
	}

	hostServices := []runtimehost.HostService{{
		Name:   "indexeddb",
		EnvVar: indexeddbservice.DefaultSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterIndexedDBServer(srv, indexeddbservice.NewServer(ds, name, indexeddbservice.ServerOptions{
				AllowedStores: effective.ObjectStores,
			}))
		},
	}}
	return hostServices, func() {
		_ = closeIndexedDBs(ds)
	}, nil
}

func buildAgentIndexedDBHostServices(name string, effective config.EffectiveHostIndexedDBBinding, deps Deps) ([]runtimehost.HostService, func(), error) {
	if deps.IndexedDBFactory == nil || len(deps.IndexedDBDefs) == 0 {
		return nil, nil, fmt.Errorf("indexeddb host services are not available")
	}

	ds, err := buildAgentScopedIndexedDB(name, effective, deps)
	if err != nil {
		return nil, nil, err
	}

	hostServices := []runtimehost.HostService{{
		Name:   "indexeddb",
		EnvVar: indexeddbservice.DefaultSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterIndexedDBServer(srv, indexeddbservice.NewServer(ds, name, indexeddbservice.ServerOptions{
				AllowedStores: effective.ObjectStores,
			}))
		},
	}}
	return hostServices, func() {
		_ = closeIndexedDBs(ds)
	}, nil
}

func buildPluginWorkflowManagerHostService(pluginName string, deps Deps, tokens *plugininvokerservice.InvocationTokenManager) runtimehost.HostService {
	manager := deps.WorkflowManager
	if manager == nil {
		manager = unavailableWorkflowManager{}
	}
	return runtimehost.HostService{
		Name:   "workflow_manager",
		EnvVar: workflowservice.DefaultManagerSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterWorkflowManagerHostServer(srv, workflowservice.NewManagerServer(pluginName, manager, tokens))
		},
	}
}

func buildPluginAgentManagerHostService(pluginName string, deps Deps, tokens *plugininvokerservice.InvocationTokenManager) runtimehost.HostService {
	manager := deps.AgentManager
	if manager == nil {
		manager = unavailableAgentManager{}
	}
	return runtimehost.HostService{
		Name:   "agent_manager",
		EnvVar: agentservice.DefaultManagerSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterAgentManagerHostServer(srv, agentservice.NewManagerServer(pluginName, manager, tokens))
		},
	}
}

func buildPluginAuthorizationHostService(provider core.AuthorizationProvider) runtimehost.HostService {
	return runtimehost.HostService{
		Name:   "authorization",
		EnvVar: authorizationservice.DefaultSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterAuthorizationProviderServer(srv, authorizationservice.NewProviderServer(provider))
		},
	}
}

func buildPluginInvokerHostService(pluginName string, entry *config.ProviderEntry, deps Deps, tokens *plugininvokerservice.InvocationTokenManager) runtimehost.HostService {
	invoker := deps.PluginInvoker
	if invoker == nil {
		invoker = unavailablePluginInvoker{}
	}
	return runtimehost.HostService{
		Name:   "plugin_invoker",
		EnvVar: plugininvokerservice.DefaultSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterPluginInvokerServer(srv, plugininvokerservice.NewServer(pluginName, pluginInvocationDependencies(entry.Invokes), invoker, tokens))
		},
	}
}

type unavailablePluginInvoker struct{}

func (unavailablePluginInvoker) Invoke(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
	return nil, fmt.Errorf("plugin invoker is not available")
}

func (unavailablePluginInvoker) InvokeGraphQL(context.Context, *principal.Principal, string, string, invocation.GraphQLRequest) (*core.OperationResult, error) {
	return nil, fmt.Errorf("plugin invoker is not available")
}

type unavailableWorkflowManager struct{}

func (unavailableWorkflowManager) ListSchedules(context.Context, *principal.Principal) ([]*workflowmanager.ManagedSchedule, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) CreateSchedule(context.Context, *principal.Principal, workflowmanager.ScheduleUpsert) (*workflowmanager.ManagedSchedule, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) GetSchedule(context.Context, *principal.Principal, string) (*workflowmanager.ManagedSchedule, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) UpdateSchedule(context.Context, *principal.Principal, string, workflowmanager.ScheduleUpsert) (*workflowmanager.ManagedSchedule, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) DeleteSchedule(context.Context, *principal.Principal, string) error {
	return fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) PauseSchedule(context.Context, *principal.Principal, string) (*workflowmanager.ManagedSchedule, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) ResumeSchedule(context.Context, *principal.Principal, string) (*workflowmanager.ManagedSchedule, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) ListEventTriggers(context.Context, *principal.Principal) ([]*workflowmanager.ManagedEventTrigger, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) CreateEventTrigger(context.Context, *principal.Principal, workflowmanager.EventTriggerUpsert) (*workflowmanager.ManagedEventTrigger, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) GetEventTrigger(context.Context, *principal.Principal, string) (*workflowmanager.ManagedEventTrigger, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) UpdateEventTrigger(context.Context, *principal.Principal, string, workflowmanager.EventTriggerUpsert) (*workflowmanager.ManagedEventTrigger, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) DeleteEventTrigger(context.Context, *principal.Principal, string) error {
	return fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) PauseEventTrigger(context.Context, *principal.Principal, string) (*workflowmanager.ManagedEventTrigger, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) ResumeEventTrigger(context.Context, *principal.Principal, string) (*workflowmanager.ManagedEventTrigger, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) ListRuns(context.Context, *principal.Principal) ([]*workflowmanager.ManagedRun, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) StartRun(context.Context, *principal.Principal, workflowmanager.RunStart) (*workflowmanager.ManagedRun, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) GetRun(context.Context, *principal.Principal, string) (*workflowmanager.ManagedRun, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) CancelRun(context.Context, *principal.Principal, string, string) (*workflowmanager.ManagedRun, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) SignalRun(context.Context, *principal.Principal, workflowmanager.RunSignal) (*workflowmanager.ManagedRunSignal, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) SignalOrStartRun(context.Context, *principal.Principal, workflowmanager.RunSignalOrStart) (*workflowmanager.ManagedRunSignal, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) PublishEvent(context.Context, *principal.Principal, string, coreworkflow.Event) (coreworkflow.Event, error) {
	return coreworkflow.Event{}, fmt.Errorf("workflow manager is not available")
}

type unavailableAgentManager struct{}

func (unavailableAgentManager) Available() bool {
	return false
}

func (unavailableAgentManager) ResolveTool(context.Context, *principal.Principal, coreagent.ToolRef) (coreagent.Tool, error) {
	return coreagent.Tool{}, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ResolveTools(context.Context, *principal.Principal, coreagent.ResolveToolsRequest) ([]coreagent.Tool, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ListTools(context.Context, *principal.Principal, coreagent.ListToolsRequest) (*coreagent.ListToolsResponse, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) CreateSession(context.Context, *principal.Principal, coreagent.ManagerCreateSessionRequest) (*coreagent.Session, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) GetSession(context.Context, *principal.Principal, string) (*coreagent.Session, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ListSessions(context.Context, *principal.Principal, coreagent.ManagerListSessionsRequest) ([]*coreagent.Session, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) UpdateSession(context.Context, *principal.Principal, coreagent.ManagerUpdateSessionRequest) (*coreagent.Session, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) CreateTurn(context.Context, *principal.Principal, coreagent.ManagerCreateTurnRequest) (*coreagent.Turn, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) GetTurn(context.Context, *principal.Principal, string) (*coreagent.Turn, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ListTurns(context.Context, *principal.Principal, coreagent.ManagerListTurnsRequest) ([]*coreagent.Turn, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) CancelTurn(context.Context, *principal.Principal, string, string) (*coreagent.Turn, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ListTurnEvents(context.Context, *principal.Principal, string, int64, int) ([]*coreagent.TurnEvent, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ListInteractions(context.Context, *principal.Principal, string) ([]*coreagent.Interaction, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ResolveInteraction(context.Context, *principal.Principal, string, string, map[string]any) (*coreagent.Interaction, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func chainCleanup(cleanups ...func()) func() {
	var combined []func()
	for _, cleanup := range cleanups {
		if cleanup != nil {
			combined = append(combined, cleanup)
		}
	}
	if len(combined) == 0 {
		return nil
	}
	return func() {
		for i := len(combined) - 1; i >= 0; i-- {
			combined[i]()
		}
	}
}

func closeCaches(values ...corecache.Cache) error {
	var errs []error
	for _, value := range values {
		if value == nil {
			continue
		}
		if err := value.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
