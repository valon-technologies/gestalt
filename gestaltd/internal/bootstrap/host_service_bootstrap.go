package bootstrap

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	s3sdk "github.com/valon-technologies/gestalt/sdk/go/s3"
	"github.com/valon-technologies/gestalt/server/core"
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	authorizationservice "github.com/valon-technologies/gestalt/server/services/authorization"
	cacheservice "github.com/valon-technologies/gestalt/server/services/cache"
	externalcredentialsservice "github.com/valon-technologies/gestalt/server/services/externalcredentials"
	indexeddbservice "github.com/valon-technologies/gestalt/server/services/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimeprovider"
	"github.com/valon-technologies/gestalt/server/services/s3"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
	"google.golang.org/grpc"
)

const (
	runtimeHostServiceRelayTokenTTL = 30 * 24 * time.Hour
	hostServiceTLSCAFileEnv         = "GESTALT_HOST_SERVICE_TLS_CA_FILE"
	hostServiceTLSCAPEMEnv          = "GESTALT_HOST_SERVICE_TLS_CA_PEM"
)

func buildProviderHostServices(name string, deps Deps, extraHostServices ...runtimehost.HostService) ([]runtimehost.HostService, *appaccessservice.InvocationTokenManager, error) {
	invTokens, err := appaccessservice.NewInvocationTokenManager(deps.EncryptionKey)
	if err != nil {
		return nil, nil, err
	}

	var hostServices []runtimehost.HostService
	hostServices = append(hostServices, indexedDBServicesFromDeps(deps, indexeddbservice.ServerOptions{}, name)...)
	if cacheHostService, ok := cacheServiceFromDeps(name, deps); ok {
		hostServices = append(hostServices, cacheHostService)
	}
	if s3HostService, ok, err := s3ServiceFromDeps(name, deps); err != nil {
		return nil, nil, err
	} else if ok {
		hostServices = append(hostServices, s3HostService)
	}
	hostServices = append(hostServices,
		buildAppInvocationHostService(deps, invTokens),
		buildWorkflowProviderHostService(name, deps, invTokens),
		buildPluginAgentProviderHostService(name, deps, invTokens),
	)
	if deps.Services != nil && !core.ExternalCredentialProviderMissing(deps.Services.ExternalCredentials) {
		hostServices = append(hostServices, buildPluginExternalCredentialsHostService(deps.Services.ExternalCredentials))
	}
	if deps.AuthorizationProvider != nil {
		hostServices = append(hostServices, buildPluginAuthorizationHostService(deps.AuthorizationProvider))
	}
	hostServices = append(hostServices, extraHostServices...)
	return hostServices, invTokens, nil
}

func appProviderHostServiceDeps(entry *config.ProviderEntry, deps Deps) Deps {
	if entry == nil {
		return deps
	}
	deps.Caches = scopedCacheBindings(entry.Cache, deps.Caches)
	deps.S3 = scopedS3Bindings(entry.S3, deps.S3)
	return deps
}

func scopedCacheBindings(names []string, bindings map[string]corecache.Cache) map[string]corecache.Cache {
	if len(names) == 0 || len(bindings) == 0 {
		return nil
	}
	scoped := map[string]corecache.Cache{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if binding := bindings[name]; binding != nil {
			scoped[name] = binding
		}
	}
	if len(scoped) == 0 {
		return nil
	}
	return scoped
}

func scopedS3Bindings(names []string, bindings map[string]s3sdk.S3) map[string]s3sdk.S3 {
	if len(names) == 0 || len(bindings) == 0 {
		return nil
	}
	scoped := map[string]s3sdk.S3{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if binding := bindings[name]; binding != nil {
			scoped[name] = binding
		}
	}
	if len(scoped) == 0 {
		return nil
	}
	return scoped
}

func appendRuntimeLogHostService(hostServices []runtimehost.HostService, runtimeConfig config.EffectiveRuntimePlacement, deps Deps, runtimePlan RuntimePlacementPlan) []runtimehost.HostService {
	if deps.Services == nil || deps.Services.RuntimeSessionLogs == nil {
		return hostServices
	}
	runtimeProviderName := runtimeSessionLogProviderName(runtimeConfig)
	return append(hostServices, runtimehost.HostService{
		Name:           "runtime_log_host",
		MethodPrefixes: []string{grpcMethodPrefix(proto.RuntimeLogHost_ServiceDesc.ServiceName)},
		Register: func(srv *grpc.Server) {
			runtimehost.RegisterRuntimeLogHostServer(srv, runtimeProviderName, deps.Services.RuntimeSessionLogs.AppendSessionLogs)
		},
	})
}

func runtimeSessionLogProviderName(runtimeConfig config.EffectiveRuntimePlacement) string {
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

func grpcMethodPrefix(serviceName string) string {
	return "/" + serviceName + "/"
}

func mergeHostedRuntimeHostServiceRelayEnv(providerName, sessionID string, hostServices []runtimehost.HostService, deps Deps) (map[string]string, string, error) {
	if len(hostServices) == 0 {
		return nil, "", nil
	}
	for _, hostService := range hostServices {
		if strings.TrimSpace(hostService.Name) == "" || len(hostService.MethodPrefixes) == 0 {
			return nil, "", fmt.Errorf("host service %q requires public host service relay support", hostService.Name)
		}
	}
	serviceLabel := strings.ReplaceAll(strings.TrimSpace(hostServices[0].Name), "_", " ")
	relayDialTarget, relayEnv, relayHost, ok, err := buildHostedRuntimePublicHostServiceRelay(
		providerName,
		sessionID,
		deps,
		serviceLabel,
	)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, "", fmt.Errorf("provider %q requires server.baseURL and server.encryptionKey to relay host services through the public host service relay", providerName)
	}
	relayEnv[runtimehost.HostServiceSocketEnv] = relayDialTarget
	return relayEnv, relayHost, nil
}

func applyHostedRuntimeHostServiceRelayEnv(providerName, sessionID string, hostServices []runtimehost.HostService, runtimePlan RuntimePlacementPlan, deps Deps, env map[string]string, allowedHosts []string) (map[string]string, []string, error) {
	bindingEnv, relayHost, err := mergeHostedRuntimeHostServiceRelayEnv(providerName, sessionID, hostServices, deps)
	if err != nil {
		return nil, nil, err
	}
	if len(bindingEnv) > 0 {
		if env == nil {
			env = make(map[string]string, len(bindingEnv))
		}
		maps.Copy(env, bindingEnv)
	}
	if runtimePlan.RequiresHostnameEgress {
		allowedHosts = appendAllowedHost(allowedHosts, relayHost)
	}
	return env, allowedHosts, nil
}

type runtimeHostServiceSessionVerifier struct {
	providerName string
	provider     runtimeprovider.Provider
}

func (v runtimeHostServiceSessionVerifier) VerifyHostServiceSession(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("runtime session id is required")
	}
	if v.provider == nil {
		return fmt.Errorf("runtime provider is not configured")
	}
	session, err := v.provider.GetSession(ctx, &proto.GetRuntimeSessionRequest{SessionId: sessionID})
	if err != nil {
		return err
	}
	if session == nil {
		return fmt.Errorf("runtime session %q was not found", sessionID)
	}
	if expected := strings.TrimSpace(v.providerName); expected != "" {
		if got := strings.TrimSpace(session.GetMetadata()["provider_name"]); got != "" && got != expected {
			return fmt.Errorf("runtime session %q belongs to provider %q", sessionID, got)
		}
	}
	if session.GetLifecycle().GetExpiresAt() != nil {
		expiresAt := session.GetLifecycle().GetExpiresAt().AsTime().UTC()
		if !time.Now().UTC().Before(expiresAt) {
			return fmt.Errorf("runtime session %q expired at %s", sessionID, expiresAt.Format(time.RFC3339Nano))
		}
	}
	switch session.GetState() {
	case runtimeprovider.SessionStatePending, runtimeprovider.SessionStateReady, runtimeprovider.SessionStateRunning:
		return nil
	default:
		return fmt.Errorf("runtime session %q is %s", sessionID, session.GetState())
	}
}

func registerPublicRuntimeHostServices(providerName string, hostServices []runtimehost.HostService, deps Deps, runtimeProvider runtimeprovider.Provider) (func(), error) {
	return registerVerifiedPublicHostServices(providerName, hostServices, deps, runtimeHostServiceSessionVerifier{
		providerName: providerName,
		provider:     runtimeProvider,
	}, false)
}

type workflowProviderHostServiceSessionVerifier struct{}

func (workflowProviderHostServiceSessionVerifier) VerifyHostServiceSession(context.Context, string) error {
	// Non-runtime workflow providers do not allocate runtime sessions; the
	// signed relay token is scoped by provider, service, and method.
	return nil
}

func registerPublicWorkflowProviderHostServices(providerName string, hostServices []runtimehost.HostService, deps Deps) (func(), error) {
	if !hostCanRelayRuntimeHostServices(deps) {
		return nil, nil
	}
	return registerVerifiedPublicHostServices(providerName, hostServices, deps, workflowProviderHostServiceSessionVerifier{}, true)
}

func registerVerifiedPublicHostServices(providerName string, hostServices []runtimehost.HostService, deps Deps, verifier runtimehost.PublicHostServiceSessionVerifier, skipNilRegister bool) (func(), error) {
	if deps.PublicHostServices == nil {
		return nil, nil
	}
	registerHostServices := make([]runtimehost.HostService, 0, len(hostServices))
	for _, hostService := range hostServices {
		if skipNilRegister && hostService.Register == nil {
			continue
		}
		if strings.TrimSpace(hostService.Name) == "" {
			return nil, fmt.Errorf("host service %q requires a service name for public relay", hostService.Name)
		}
		registerHostServices = append(registerHostServices, hostService)
	}
	if len(registerHostServices) == 0 {
		return nil, nil
	}
	registration := deps.PublicHostServices.RegisterVerified(providerName, verifier, registerHostServices...)
	return func() {
		registration.Unregister()
	}, nil
}

func buildHostedRuntimePublicHostServiceRelay(providerName, sessionID string, deps Deps, serviceLabel string) (string, map[string]string, string, bool, error) {
	baseURL, explicitRelayBaseURL := hostedRuntimeRelayBaseURL(deps)
	if baseURL == "" || len(deps.EncryptionKey) == 0 {
		return "", nil, "", false, nil
	}
	dialTarget, relayHost, err := runtimePublicRelayTarget(baseURL, explicitRelayBaseURL)
	if err != nil {
		return "", nil, "", false, err
	}
	tokenManager, err := runtimehost.NewHostServiceRelayTokenManager(deps.EncryptionKey)
	if err != nil {
		return "", nil, "", false, fmt.Errorf("init host service relay tokens: %w", err)
	}
	token, err := tokenManager.MintToken(runtimehost.HostServiceRelayTokenRequest{
		AppName:   providerName,
		SessionID: sessionID,
		Service:   "host_service",
		// MethodPrefix "/" scopes the relay to the unified host-service surface.
		// Per-RPC access is enforced by AllowsMethod and session verification, not
		// by narrowing the token to individual gRPC service prefixes.
		MethodPrefix: "/",
		TTL:          runtimeHostServiceRelayTokenTTL,
	})
	if err != nil {
		return "", nil, "", false, fmt.Errorf("mint %s host service relay token: %w", serviceLabel, err)
	}
	return dialTarget, map[string]string{
		runtimehost.HostServiceTokenEnv: token,
	}, relayHost, true, nil
}
func indexedDBBindingsFromInstances(instances map[string]indexeddb.IndexedDB) map[string]indexeddb.IndexedDB {
	bindings := make(map[string]indexeddb.IndexedDB, len(instances))
	for _, name := range slices.Sorted(maps.Keys(instances)) {
		if ds := instances[name]; ds != nil {
			bindings[name] = ds
		}
	}
	if len(bindings) == 0 {
		return nil
	}
	return bindings
}

func indexedDBService(defaultBinding string, bindings map[string]indexeddb.IndexedDB, opts indexeddbservice.ServerOptions, pluginName string) runtimehost.HostService {
	if strings.TrimSpace(pluginName) == "" {
		pluginName = strings.TrimSpace(defaultBinding)
	}
	return runtimehost.HostService{
		Name:           "indexeddb",
		MethodPrefixes: []string{grpcMethodPrefix(proto.IndexedDB_ServiceDesc.ServiceName)},
		Register: func(srv *grpc.Server) {
			proto.RegisterIndexedDBServer(srv, registerIndexedDBServer(bindings, defaultBinding, pluginName, opts))
		},
	}
}

func indexedDBServicesFromDeps(deps Deps, opts indexeddbservice.ServerOptions, pluginName string) []runtimehost.HostService {
	bindings := indexedDBBindingsFromInstances(deps.IndexedDBs)
	if len(bindings) == 0 {
		return nil
	}
	return []runtimehost.HostService{indexedDBService(deps.SelectedIndexedDBName, bindings, opts, pluginName)}
}

func cacheServiceFromDeps(pluginName string, deps Deps) (runtimehost.HostService, bool) {
	if len(deps.Caches) == 0 {
		return runtimehost.HostService{}, false
	}
	defaultBinding := ""
	if len(deps.Caches) == 1 {
		for name := range deps.Caches {
			defaultBinding = name
		}
	}
	bindings := maps.Clone(deps.Caches)
	return runtimehost.HostService{
		Name:           "cache",
		MethodPrefixes: []string{grpcMethodPrefix(proto.Cache_ServiceDesc.ServiceName)},
		Register: func(srv *grpc.Server) {
			proto.RegisterCacheServer(srv, registerCacheServer(bindings, defaultBinding, pluginName))
		},
	}, true
}

func s3ServiceFromDeps(pluginName string, deps Deps) (runtimehost.HostService, bool, error) {
	if len(deps.S3) == 0 {
		return runtimehost.HostService{}, false, nil
	}

	var accessURLs *s3.ObjectAccessURLManager
	if len(deps.EncryptionKey) != 0 {
		var err error
		accessURLs, err = s3.NewObjectAccessURLManager(deps.EncryptionKey, deps.BaseURL)
		if err != nil {
			return runtimehost.HostService{}, false, fmt.Errorf("s3 object access URLs: %w", err)
		}
	}

	defaultBinding := ""
	if len(deps.S3) == 1 {
		for name := range deps.S3 {
			defaultBinding = name
		}
	}
	bindings := maps.Clone(deps.S3)
	return runtimehost.HostService{
		Name: "s3",
		MethodPrefixes: []string{
			grpcMethodPrefix(proto.S3_ServiceDesc.ServiceName),
			grpcMethodPrefix(proto.S3ObjectAccess_ServiceDesc.ServiceName),
		},
		Register: func(srv *grpc.Server) {
			s3Server, objectAccessServer := registerS3Servers(bindings, defaultBinding, pluginName, accessURLs)
			proto.RegisterS3Server(srv, s3Server)
			proto.RegisterS3ObjectAccessServer(srv, objectAccessServer)
		},
	}, true, nil
}

func registerIndexedDBServer(bindings map[string]indexeddb.IndexedDB, defaultBinding, pluginName string, opts indexeddbservice.ServerOptions) proto.IndexedDBServer {
	if len(bindings) == 1 {
		for _, ds := range bindings {
			return indexeddbservice.NewServer(ds, pluginName, opts)
		}
	}
	return indexeddbservice.NewRoutingServer(bindings, defaultBinding, pluginName, opts)
}

func registerCacheServer(bindings map[string]corecache.Cache, defaultBinding, pluginName string) proto.CacheServer {
	if len(bindings) == 1 {
		for _, cache := range bindings {
			return cacheservice.NewServer(cache, pluginName)
		}
	}
	return cacheservice.NewRoutingServer(bindings, defaultBinding, pluginName)
}

func registerS3Servers(bindings map[string]s3sdk.S3, defaultBinding, pluginName string, accessURLs *s3.ObjectAccessURLManager) (proto.S3Server, proto.S3ObjectAccessServer) {
	if len(bindings) == 1 {
		for bindingName, client := range bindings {
			return s3.NewServer(client, pluginName), s3.NewObjectAccessServer(accessURLs, pluginName, bindingName)
		}
	}
	return s3.NewRoutingServers(bindings, defaultBinding, pluginName, accessURLs)
}

func buildWorkflowProviderHostService(appName string, deps Deps, tokens *appaccessservice.InvocationTokenManager) runtimehost.HostService {
	manager := deps.WorkflowManager
	if manager == nil {
		manager = unavailableWorkflowManager{}
	}
	return runtimehost.HostService{
		Name:           "workflow_provider",
		MethodPrefixes: []string{grpcMethodPrefix(proto.WorkflowProvider_ServiceDesc.ServiceName)},
		Register: func(srv *grpc.Server) {
			proto.RegisterWorkflowProviderServer(srv, workflowservice.NewProviderServer(appName, manager, tokens))
		},
	}
}

func buildPluginAgentProviderHostService(pluginName string, deps Deps, tokens *appaccessservice.InvocationTokenManager) runtimehost.HostService {
	manager := deps.AgentManager
	if manager == nil {
		manager = unavailableAgentManager{}
	}
	workflowRuns := workflowRunResolverFromDeps(deps)
	return runtimehost.HostService{
		Name:           "agent_provider",
		MethodPrefixes: []string{grpcMethodPrefix(proto.AgentProvider_ServiceDesc.ServiceName)},
		Register: func(srv *grpc.Server) {
			proto.RegisterAgentProviderServer(srv, agentservice.NewProviderServer(
				pluginName,
				manager,
				tokens,
				agentservice.WithWorkflowRunResolver(workflowRuns),
			))
		},
	}
}

func buildPluginAuthorizationHostService(provider core.AuthorizationProvider) runtimehost.HostService {
	return runtimehost.HostService{
		Name:           "authorization",
		MethodPrefixes: []string{grpcMethodPrefix(proto.AuthorizationProvider_ServiceDesc.ServiceName)},
		Register: func(srv *grpc.Server) {
			proto.RegisterAuthorizationProviderServer(srv, authorizationservice.NewProviderServer(provider))
		},
	}
}

func buildPluginExternalCredentialsHostService(provider core.ExternalCredentialProvider) runtimehost.HostService {
	return runtimehost.HostService{
		Name:           "external_credentials",
		MethodPrefixes: []string{grpcMethodPrefix(proto.ExternalCredentialProvider_ServiceDesc.ServiceName)},
		Register: func(srv *grpc.Server) {
			proto.RegisterExternalCredentialProviderServer(srv, externalcredentialsservice.NewProviderServer(provider))
		},
	}
}

func buildAppInvocationHostService(deps Deps, tokens *appaccessservice.InvocationTokenManager) runtimehost.HostService {
	invoker := deps.AppInvocation
	if invoker == nil {
		invoker = unavailableAppInvocation{}
	}
	workflowRuns := workflowRunResolverFromDeps(deps)
	return runtimehost.HostService{
		Name:           "app",
		MethodPrefixes: []string{grpcMethodPrefix(proto.App_ServiceDesc.ServiceName)},
		Register: func(srv *grpc.Server) {
			proto.RegisterAppServer(srv, appaccessservice.NewServer(
				invoker,
				tokens,
				appaccessservice.WithWorkflowRunResolver(workflowRuns),
				appaccessservice.WithWorkflowAppInvocationGrants(deps.WorkflowAppInvocationGrants),
			))
		},
	}
}

type workflowRunResolver struct {
	runtime *workflowRuntime
}

func workflowRunResolverFromDeps(deps Deps) appaccessservice.WorkflowRunResolver {
	if deps.WorkflowRuntime == nil {
		return nil
	}
	return workflowRunResolver{runtime: deps.WorkflowRuntime}
}

func (r workflowRunResolver) ResolveWorkflowRun(ctx context.Context, providerName, runID string) (*coreworkflow.Run, error) {
	if r.runtime == nil {
		return nil, fmt.Errorf("workflow runtime is not configured")
	}
	_, provider, err := r.runtime.ResolveProvider(ctx, providerName)
	if err != nil {
		return nil, err
	}
	runProto, err := provider.GetRun(ctx, &proto.GetWorkflowProviderRunRequest{RunId: strings.TrimSpace(runID)})
	if err != nil {
		return nil, err
	}
	return workflowwire.RunFromProto(runProto)
}
