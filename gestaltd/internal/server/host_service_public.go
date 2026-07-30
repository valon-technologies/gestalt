package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
)

type hostServiceHandlerKey struct {
	pluginName string
}

type hostServiceHandlerEntry struct {
	handler  http.Handler
	verifier runtimehost.PublicHostServiceSessionVerifier
}

func validatePublicHostServices(services []runtimehost.PublicHostService) error {
	handlers := make(map[hostServiceHandlerKey][]hostServiceHandlerEntry)
	for _, service := range services {
		key, ok := publicHostServiceHandlerKey(service)
		if !ok {
			continue
		}
		if service.SessionVerifier == nil {
			return fmt.Errorf("public host service %s requires a session verifier", key.String())
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
		pluginName: strings.TrimSpace(service.AppName),
	}
	if key.pluginName == "" || service.Service.Register == nil {
		return hostServiceHandlerKey{}, false
	}
	return key, true
}

func (k hostServiceHandlerKey) String() string {
	return k.pluginName
}

// appInvocationPublicProviderKey is the well-known registration key for the
// single, global app-invocation host service registered by gestaltd bootstrap.
const appInvocationPublicProviderKey = "app"

var appServiceMethodPrefix = "/" + proto.App_ServiceDesc.ServiceName + "/"

// hostServiceRelayPluginName routes App/* RPCs to the single global
// app-invocation host service: the token's AppName is the caller, not the
// serving provider, and caller identity comes from the verified token. All
// other services stay keyed by the caller's provider name.
func hostServiceRelayPluginName(target runtimehost.HostServiceRelayTarget, methodPath string) string {
	if strings.HasPrefix(methodPath, appServiceMethodPrefix) {
		return appInvocationPublicProviderKey
	}
	return strings.TrimSpace(target.AppName)
}

func (s *Server) unifiedHostServiceHandler(ctx context.Context, target runtimehost.HostServiceRelayTarget, methodPath string) (http.Handler, error) {
	pluginName := hostServiceRelayPluginName(target, methodPath)
	if s == nil || pluginName == "" {
		return nil, nil
	}
	var entries []hostServiceHandlerEntry
	services := s.publicHostServices.Snapshot()
	s.prunePublicHostServiceHandlerCache(services)
	for _, service := range services {
		key, ok := publicHostServiceHandlerKey(service)
		if !ok || key.pluginName != pluginName || !service.Service.AllowsMethod(methodPath) {
			continue
		}
		if service.SessionVerifier == nil {
			return nil, fmt.Errorf("public host service %s requires a session verifier", key.String())
		}
		entry, ok := s.publicHostServiceHandlerEntry(service)
		if !ok {
			continue
		}
		if err := checkHostServiceHandlerDuplicate(key, entries, entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, nil
	}
	entry, ok, err := selectHostServiceHandlerEntry(ctx, hostServiceHandlerKey{pluginName: pluginName}, target.SessionID, entries)
	if err != nil || !ok {
		return nil, err
	}
	return entry.handler, nil
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

func (s *Server) hostServiceGRPCOptions(opts ...grpc.ServerOption) []grpc.ServerOption {
	if s != nil && s.hostServiceRelayTokens != nil {
		opts = append(opts, s.hostServiceRelayTokens.HostServiceGRPCServerOptions()...)
	}
	return opts
}

func (s *Server) cachedPublicHostServiceHandler(service runtimehost.PublicHostService) http.Handler {
	registrationID := service.RegistrationID()
	if registrationID == 0 {
		srv := grpc.NewServer(s.hostServiceGRPCOptions()...)
		service.Service.Register(srv)
		return http.HandlerFunc(srv.ServeHTTP)
	}

	s.hostServiceMu.Lock()
	if handler := s.hostServiceHandlers[registrationID]; handler != nil {
		s.hostServiceMu.Unlock()
		return handler
	}
	s.hostServiceMu.Unlock()

	srv := grpc.NewServer(s.hostServiceGRPCOptions()...)
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
			return hostServiceHandlerEntry{}, false, fmt.Errorf("public host service %s requires a session verifier", key.String())
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
