package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
	"google.golang.org/grpc"
)

type hostServiceHandlerKey struct {
	pluginName string
	sessionID  string
	service    string
	envVar     string
}

type hostServiceHandlerEntry struct {
	handler  http.Handler
	verifier runtimehost.PublicHostServiceSessionVerifier
}

type pluginRuntimeSessionVerifier interface {
	VerifyPluginRuntimeSession(context.Context, string, string) error
}

func validatePublicHostServices(services []runtimehost.PublicHostService) error {
	handlers := make(map[hostServiceHandlerKey][]hostServiceHandlerEntry)
	for _, service := range services {
		key, ok := publicHostServiceHandlerKey(service)
		if !ok {
			continue
		}
		entry := hostServiceHandlerEntry{verifier: service.SessionVerifier}
		if err := appendHostServiceHandlerEntry(handlers, key, entry); err != nil {
			return err
		}
	}
	return nil
}

func appendHostServiceHandlerEntry(handlers map[hostServiceHandlerKey][]hostServiceHandlerEntry, key hostServiceHandlerKey, entry hostServiceHandlerEntry) error {
	if handlers == nil {
		return nil
	}
	entries := handlers[key]
	if err := checkHostServiceHandlerDuplicate(key, entries, entry); err != nil {
		return err
	}
	handlers[key] = append(entries, entry)
	return nil
}

func checkHostServiceHandlerDuplicate(key hostServiceHandlerKey, entries []hostServiceHandlerEntry, entry hostServiceHandlerEntry) error {
	for _, existing := range entries {
		if existing.verifier == nil || entry.verifier == nil {
			return fmt.Errorf("duplicate public host service %s", key.String())
		}
	}
	return nil
}

func publicHostServiceHandlerKey(service runtimehost.PublicHostService) (hostServiceHandlerKey, bool) {
	key := hostServiceHandlerKey{
		pluginName: strings.TrimSpace(service.PluginName),
		sessionID:  strings.TrimSpace(service.SessionID),
		service:    strings.TrimSpace(service.Service.Name),
		envVar:     strings.TrimSpace(service.Service.EnvVar),
	}
	if key.pluginName == "" || key.service == "" || key.envVar == "" || service.Service.Register == nil {
		return hostServiceHandlerKey{}, false
	}
	return key, true
}

func (k hostServiceHandlerKey) String() string {
	parts := []string{k.pluginName, k.service, k.envVar}
	if k.sessionID != "" {
		parts = append(parts, "session="+k.sessionID)
	}
	return strings.Join(parts, "/")
}

func (s *Server) hostServiceHandler(ctx context.Context, target runtimehost.HostServiceRelayTarget) (http.Handler, error) {
	if s == nil {
		return nil, nil
	}
	if target.CoreRoutable {
		entry, ok, err := s.coreRoutableHostServiceHandlerEntry(ctx, target)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		return entry.handler, nil
	}

	exactKey := hostServiceHandlerKey{
		pluginName: strings.TrimSpace(target.PluginName),
		sessionID:  strings.TrimSpace(target.SessionID),
		service:    strings.TrimSpace(target.Service),
		envVar:     strings.TrimSpace(target.EnvVar),
	}
	providerKey := exactKey
	providerKey.sessionID = ""

	if exactKey.sessionID != "" {
		entry, found, ok, err := s.hostServiceHandlerEntry(ctx, exactKey, exactKey.sessionID)
		if err != nil {
			return nil, err
		}
		if found {
			if !ok {
				return nil, nil
			}
			return entry.handler, nil
		}
	}

	entry, _, ok, err := s.hostServiceHandlerEntry(ctx, providerKey, exactKey.sessionID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return entry.handler, nil
}

func (s *Server) coreRoutableHostServiceHandlerEntry(ctx context.Context, target runtimehost.HostServiceRelayTarget) (hostServiceHandlerEntry, bool, error) {
	if s == nil || !target.CoreRoutable || s.invocationTokens == nil {
		return hostServiceHandlerEntry{}, false, nil
	}
	pluginName := strings.TrimSpace(target.PluginName)
	if pluginName == "" {
		return hostServiceHandlerEntry{}, false, nil
	}
	key, ok := coreRoutableHostServiceHandlerKey(pluginName, target)
	if !ok {
		return hostServiceHandlerEntry{}, false, nil
	}
	if err := s.verifyCoreRoutableHostServiceSession(ctx, pluginName, target.SessionID); err != nil {
		return hostServiceHandlerEntry{}, false, err
	}
	s.hostServiceMu.Lock()
	if s.coreHostServiceHandlers != nil {
		if entry, ok := s.coreHostServiceHandlers[key]; ok {
			s.hostServiceMu.Unlock()
			return entry, true, nil
		}
	}
	s.hostServiceMu.Unlock()

	entry, ok := s.newCoreRoutableHostServiceHandlerEntry(ctx, pluginName, target)
	if !ok {
		return hostServiceHandlerEntry{}, false, nil
	}
	s.hostServiceMu.Lock()
	if s.coreHostServiceHandlers == nil {
		s.coreHostServiceHandlers = make(map[hostServiceHandlerKey]hostServiceHandlerEntry)
	}
	if existing, ok := s.coreHostServiceHandlers[key]; ok {
		s.hostServiceMu.Unlock()
		return existing, true, nil
	}
	s.coreHostServiceHandlers[key] = entry
	s.hostServiceMu.Unlock()
	return entry, true, nil
}

func (s *Server) verifyCoreRoutableHostServiceSession(ctx context.Context, pluginName, sessionID string) error {
	verifier, ok := s.pluginRuntimes.(pluginRuntimeSessionVerifier)
	if !ok || verifier == nil {
		return fmt.Errorf("plugin runtime session verifier is not configured")
	}
	return verifier.VerifyPluginRuntimeSession(ctx, pluginName, sessionID)
}

func coreRoutableHostServiceHandlerKey(pluginName string, target runtimehost.HostServiceRelayTarget) (hostServiceHandlerKey, bool) {
	methodPrefix := strings.TrimSpace(target.MethodPrefix)
	switch {
	case target.Service == "workflow_manager" &&
		target.EnvVar == workflowservice.DefaultManagerSocketEnv &&
		methodPrefix == "/"+proto.WorkflowManagerHost_ServiceDesc.ServiceName+"/":
	case target.Service == "agent_manager" &&
		target.EnvVar == agentservice.DefaultManagerSocketEnv &&
		methodPrefix == "/"+proto.AgentManagerHost_ServiceDesc.ServiceName+"/":
	case target.Service == "plugin_invoker" &&
		target.EnvVar == plugininvokerservice.DefaultSocketEnv &&
		methodPrefix == "/"+proto.PluginInvoker_ServiceDesc.ServiceName+"/":
	default:
		return hostServiceHandlerKey{}, false
	}
	return hostServiceHandlerKey{
		pluginName: pluginName,
		service:    strings.TrimSpace(target.Service),
		envVar:     strings.TrimSpace(target.EnvVar),
	}, true
}

func (s *Server) newCoreRoutableHostServiceHandlerEntry(ctx context.Context, pluginName string, target runtimehost.HostServiceRelayTarget) (hostServiceHandlerEntry, bool) {
	methodPrefix := strings.TrimSpace(target.MethodPrefix)
	switch {
	case target.Service == "workflow_manager" &&
		target.EnvVar == workflowservice.DefaultManagerSocketEnv &&
		methodPrefix == "/"+proto.WorkflowManagerHost_ServiceDesc.ServiceName+"/":
		if s.workflowSchedules == nil {
			return hostServiceHandlerEntry{}, false
		}
		slog.DebugContext(ctx, "serving core-routable host service relay", "plugin", pluginName, "service", target.Service)
		return hostServiceHandlerEntry{
			handler: newGRPCHostServiceHandler(func(srv *grpc.Server) {
				proto.RegisterWorkflowManagerHostServer(srv, workflowservice.NewManagerServer(pluginName, s.workflowSchedules, s.invocationTokens))
			}),
		}, true
	case target.Service == "agent_manager" &&
		target.EnvVar == agentservice.DefaultManagerSocketEnv &&
		methodPrefix == "/"+proto.AgentManagerHost_ServiceDesc.ServiceName+"/":
		if s.agentRuns == nil {
			return hostServiceHandlerEntry{}, false
		}
		slog.DebugContext(ctx, "serving core-routable host service relay", "plugin", pluginName, "service", target.Service)
		return hostServiceHandlerEntry{
			handler: newGRPCHostServiceHandler(func(srv *grpc.Server) {
				proto.RegisterAgentManagerHostServer(srv, agentservice.NewManagerServer(pluginName, s.agentRuns, s.invocationTokens))
			}),
		}, true
	case target.Service == "plugin_invoker" &&
		target.EnvVar == plugininvokerservice.DefaultSocketEnv &&
		methodPrefix == "/"+proto.PluginInvoker_ServiceDesc.ServiceName+"/":
		if s.pluginInvoker == nil || s.pluginDefs == nil {
			return hostServiceHandlerEntry{}, false
		}
		entry := s.pluginDefs[pluginName]
		if entry == nil {
			return hostServiceHandlerEntry{}, false
		}
		slog.DebugContext(ctx, "serving core-routable host service relay", "plugin", pluginName, "service", target.Service)
		return hostServiceHandlerEntry{
			handler: newGRPCHostServiceHandler(func(srv *grpc.Server) {
				proto.RegisterPluginInvokerServer(srv, plugininvokerservice.NewPluginInvokerServer(pluginName, pluginInvocationDependencies(entry.Invokes), s.pluginInvoker, s.invocationTokens))
			}),
		}, true
	default:
		return hostServiceHandlerEntry{}, false
	}
}

func newGRPCHostServiceHandler(register func(*grpc.Server)) http.Handler {
	srv := grpc.NewServer()
	register(srv)
	return http.HandlerFunc(srv.ServeHTTP)
}

func (s *Server) hostServiceHandlerEntry(ctx context.Context, key hostServiceHandlerKey, sessionID string) (hostServiceHandlerEntry, bool, bool, error) {
	if s == nil || key.pluginName == "" || key.service == "" || key.envVar == "" {
		return hostServiceHandlerEntry{}, false, false, nil
	}

	var entries []hostServiceHandlerEntry
	services := s.publicHostServices.Snapshot()
	s.prunePublicHostServiceHandlerCache(services)
	for _, service := range services {
		serviceKey, ok := publicHostServiceHandlerKey(service)
		if !ok || serviceKey != key {
			continue
		}
		entry, ok := s.publicHostServiceHandlerEntry(service)
		if !ok {
			continue
		}
		if err := checkHostServiceHandlerDuplicate(key, entries, entry); err != nil {
			return hostServiceHandlerEntry{}, true, false, err
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return hostServiceHandlerEntry{}, false, false, nil
	}
	entry, ok, err := selectHostServiceHandlerEntry(ctx, key, sessionID, entries)
	return entry, true, ok, err
}

func (s *Server) publicHostServiceHandlerEntry(service runtimehost.PublicHostService) (hostServiceHandlerEntry, bool) {
	_, ok := publicHostServiceHandlerKey(service)
	if !ok {
		return hostServiceHandlerEntry{}, false
	}
	return hostServiceHandlerEntry{
		handler:  s.cachedPublicHostServiceHandler(service),
		verifier: service.SessionVerifier,
	}, true
}

func (s *Server) cachedPublicHostServiceHandler(service runtimehost.PublicHostService) http.Handler {
	registrationID := service.RegistrationID()
	if registrationID == 0 {
		srv := grpc.NewServer()
		service.Service.Register(srv)
		return http.HandlerFunc(srv.ServeHTTP)
	}

	s.hostServiceMu.Lock()
	if handler := s.hostServiceHandlers[registrationID]; handler != nil {
		s.hostServiceMu.Unlock()
		return handler
	}
	s.hostServiceMu.Unlock()

	srv := grpc.NewServer()
	service.Service.Register(srv)
	handler := http.HandlerFunc(srv.ServeHTTP)

	s.hostServiceMu.Lock()
	if s.hostServiceHandlers == nil {
		s.hostServiceHandlers = make(map[uint64]http.Handler)
	}
	if existing := s.hostServiceHandlers[registrationID]; existing != nil {
		s.hostServiceMu.Unlock()
		return existing
	}
	s.hostServiceHandlers[registrationID] = handler
	s.hostServiceMu.Unlock()
	return handler
}

func (s *Server) prunePublicHostServiceHandlerCache(services []runtimehost.PublicHostService) {
	if s == nil {
		return
	}
	if len(services) == 0 {
		s.hostServiceMu.Lock()
		clear(s.hostServiceHandlers)
		s.hostServiceMu.Unlock()
		return
	}
	active := make(map[uint64]struct{}, len(services))
	for _, service := range services {
		if id := service.RegistrationID(); id != 0 {
			active[id] = struct{}{}
		}
	}
	s.hostServiceMu.Lock()
	for id := range s.hostServiceHandlers {
		if _, ok := active[id]; !ok {
			delete(s.hostServiceHandlers, id)
		}
	}
	s.hostServiceMu.Unlock()
}

func selectHostServiceHandlerEntry(ctx context.Context, key hostServiceHandlerKey, sessionID string, entries []hostServiceHandlerEntry) (hostServiceHandlerEntry, bool, error) {
	if len(entries) == 0 {
		return hostServiceHandlerEntry{}, false, nil
	}
	var lastErr error
	for _, entry := range entries {
		if entry.verifier == nil {
			return entry, true, nil
		}
		if err := entry.verifier.VerifyHostServiceSession(ctx, sessionID); err != nil {
			lastErr = err
			continue
		}
		return entry, true, nil
	}
	if lastErr != nil {
		return hostServiceHandlerEntry{}, false, lastErr
	}
	return hostServiceHandlerEntry{}, false, nil
}
