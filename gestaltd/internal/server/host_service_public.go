package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
)

type hostServiceHandlerKey struct {
	pluginName string
	service    string
	envVar     string
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
		pluginName: strings.TrimSpace(service.PluginName),
		service:    strings.TrimSpace(service.Service.Name),
		envVar:     strings.TrimSpace(service.Service.EnvVar),
	}
	if key.pluginName == "" || key.service == "" || key.envVar == "" || service.Service.Register == nil {
		return hostServiceHandlerKey{}, false
	}
	return key, true
}

func (k hostServiceHandlerKey) String() string {
	return strings.Join([]string{k.pluginName, k.service, k.envVar}, "/")
}

func (s *Server) hostServiceHandler(ctx context.Context, target runtimehost.HostServiceRelayTarget) (http.Handler, error) {
	if s == nil {
		return nil, nil
	}
	key := hostServiceHandlerKey{
		pluginName: strings.TrimSpace(target.PluginName),
		service:    strings.TrimSpace(target.Service),
		envVar:     strings.TrimSpace(target.EnvVar),
	}

	entry, _, ok, err := s.hostServiceHandlerEntry(ctx, key, target.SessionID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return entry.handler, nil
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
		if service.SessionVerifier == nil {
			return hostServiceHandlerEntry{}, true, false, fmt.Errorf("public host service %s requires a session verifier", key.String())
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
