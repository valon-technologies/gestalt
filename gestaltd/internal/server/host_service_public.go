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

func newPublicHostServiceHandlers(services []runtimehost.PublicHostService) (map[hostServiceHandlerKey][]hostServiceHandlerEntry, error) {
	if len(services) == 0 {
		return nil, nil
	}
	handlers := make(map[hostServiceHandlerKey][]hostServiceHandlerEntry, len(services))
	for _, service := range services {
		key, entry, ok := newPublicHostServiceHandlerEntry(service)
		if !ok {
			continue
		}
		if err := appendHostServiceHandlerEntry(handlers, key, entry); err != nil {
			return nil, err
		}
	}
	if len(handlers) == 0 {
		return nil, nil
	}
	return handlers, nil
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

	entry, ok, err := s.hostServiceHandlerEntry(ctx, exactKey, exactKey.sessionID)
	exactErr := err
	if !ok && exactKey.sessionID != "" {
		entry, ok, err = s.hostServiceHandlerEntry(ctx, providerKey, exactKey.sessionID)
		if err != nil {
			return nil, err
		}
	}
	if !ok && exactErr != nil {
		return nil, exactErr
	}
	if !ok {
		return nil, nil
	}
	return entry.handler, nil
}

func (s *Server) hostServiceHandlerEntry(ctx context.Context, key hostServiceHandlerKey, sessionID string) (hostServiceHandlerEntry, bool, error) {
	if key.pluginName == "" || key.service == "" || key.envVar == "" {
		return hostServiceHandlerEntry{}, false, nil
	}

	for {
		s.hostServiceMu.Lock()
		s.refreshHostServiceHandlerCacheLocked()
		if entries, ok := s.hostServiceHandlers[key]; ok {
			s.hostServiceMu.Unlock()
			return selectHostServiceHandlerEntry(ctx, key, sessionID, entries)
		}
		s.hostServiceMu.Unlock()

		services, snapshotVersion := s.publicHostServices.Snapshot()
		var entries []hostServiceHandlerEntry
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
			entryKey, entry, ok := newPublicHostServiceHandlerEntry(service)
			if !ok || entryKey != key {
				continue
			}
			if err := checkHostServiceHandlerDuplicate(key, entries, entry); err != nil {
				return hostServiceHandlerEntry{}, false, err
			}
			entries = append(entries, entry)
		}
		if len(entries) == 0 {
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
			return selectHostServiceHandlerEntry(ctx, key, sessionID, existing)
		}
		if s.hostServiceHandlers == nil {
			s.hostServiceHandlers = make(map[hostServiceHandlerKey][]hostServiceHandlerEntry)
		}
		s.hostServiceHandlers[key] = entries
		s.hostServiceMu.Unlock()
		return selectHostServiceHandlerEntry(ctx, key, sessionID, entries)
	}
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

func (s *Server) refreshHostServiceHandlerCacheLocked() {
	version := s.publicHostServices.Version()
	if version != s.hostServiceVersion {
		s.hostServiceHandlers = nil
		s.hostServiceVersion = version
	}
}
