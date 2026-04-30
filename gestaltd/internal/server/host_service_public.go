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
	sessionID  string
	service    string
	envVar     string
}

type hostServiceHandlerEntry struct {
	handler  http.Handler
	verifier runtimehost.PublicHostServiceSessionVerifier
}

func newPublicHostServiceHandlers(services []runtimehost.PublicHostService) (map[hostServiceHandlerKey]hostServiceHandlerEntry, error) {
	if len(services) == 0 {
		return nil, nil
	}
	handlers := make(map[hostServiceHandlerKey]hostServiceHandlerEntry, len(services))
	for _, service := range services {
		key, entry, ok := newPublicHostServiceHandlerEntry(service)
		if !ok {
			continue
		}
		if _, ok := handlers[key]; ok {
			return nil, fmt.Errorf("duplicate public host service %s", key.String())
		}
		handlers[key] = entry
	}
	if len(handlers) == 0 {
		return nil, nil
	}
	return handlers, nil
}

func newPublicHostServiceHandlerEntry(service runtimehost.PublicHostService) (hostServiceHandlerKey, hostServiceHandlerEntry, bool) {
	key := hostServiceHandlerKey{
		pluginName: strings.TrimSpace(service.PluginName),
		sessionID:  strings.TrimSpace(service.SessionID),
		service:    strings.TrimSpace(service.Service.Name),
		envVar:     strings.TrimSpace(service.Service.EnvVar),
	}
	if key.pluginName == "" || key.service == "" || key.envVar == "" || service.Service.Register == nil {
		return hostServiceHandlerKey{}, hostServiceHandlerEntry{}, false
	}
	srv := grpc.NewServer()
	service.Service.Register(srv)
	return key, hostServiceHandlerEntry{
		handler:  http.HandlerFunc(srv.ServeHTTP),
		verifier: service.SessionVerifier,
	}, true
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
	exactKey := hostServiceHandlerKey{
		pluginName: strings.TrimSpace(target.PluginName),
		sessionID:  strings.TrimSpace(target.SessionID),
		service:    strings.TrimSpace(target.Service),
		envVar:     strings.TrimSpace(target.EnvVar),
	}
	providerKey := exactKey
	providerKey.sessionID = ""

	entry, ok, err := s.hostServiceHandlerEntry(exactKey)
	if err != nil {
		return nil, err
	}
	if !ok && exactKey.sessionID != "" {
		entry, ok, err = s.hostServiceHandlerEntry(providerKey)
		if err != nil {
			return nil, err
		}
	}
	if !ok {
		return nil, nil
	}
	if entry.verifier != nil {
		if err := entry.verifier.VerifyHostServiceSession(ctx, exactKey.sessionID); err != nil {
			return nil, err
		}
	}
	return entry.handler, nil
}

func (s *Server) hostServiceHandlerEntry(key hostServiceHandlerKey) (hostServiceHandlerEntry, bool, error) {
	if key.pluginName == "" || key.service == "" || key.envVar == "" {
		return hostServiceHandlerEntry{}, false, nil
	}

	for {
		s.hostServiceMu.Lock()
		s.refreshHostServiceHandlerCacheLocked()
		if entry, ok := s.hostServiceHandlers[key]; ok {
			s.hostServiceMu.Unlock()
			return entry, true, nil
		}
		s.hostServiceMu.Unlock()

		services, snapshotVersion := s.publicHostServices.Snapshot()
		var found *runtimehost.PublicHostService
		for _, service := range services {
			serviceKey := hostServiceHandlerKey{
				pluginName: strings.TrimSpace(service.PluginName),
				sessionID:  strings.TrimSpace(service.SessionID),
				service:    strings.TrimSpace(service.Service.Name),
				envVar:     strings.TrimSpace(service.Service.EnvVar),
			}
			if serviceKey != key || service.Service.Register == nil {
				continue
			}
			if found != nil {
				return hostServiceHandlerEntry{}, false, fmt.Errorf("duplicate public host service %s", key.String())
			}
			serviceCopy := service
			found = &serviceCopy
		}
		if found == nil {
			return hostServiceHandlerEntry{}, false, nil
		}
		entryKey, entry, ok := newPublicHostServiceHandlerEntry(*found)
		if !ok || entryKey != key {
			return hostServiceHandlerEntry{}, false, nil
		}

		s.hostServiceMu.Lock()
		currentVersion := s.publicHostServices.Version()
		if currentVersion != snapshotVersion {
			s.hostServiceHandlers = nil
			s.hostServiceVersion = currentVersion
			s.hostServiceMu.Unlock()
			continue
		}
		s.hostServiceVersion = snapshotVersion
		if existing, ok := s.hostServiceHandlers[key]; ok {
			s.hostServiceMu.Unlock()
			return existing, true, nil
		}
		if s.hostServiceHandlers == nil {
			s.hostServiceHandlers = make(map[hostServiceHandlerKey]hostServiceHandlerEntry)
		}
		s.hostServiceHandlers[key] = entry
		s.hostServiceMu.Unlock()
		return entry, true, nil
	}
}

func (s *Server) refreshHostServiceHandlerCacheLocked() {
	version := s.publicHostServices.Version()
	if version != s.hostServiceVersion {
		s.hostServiceHandlers = nil
		s.hostServiceVersion = version
	}
}
