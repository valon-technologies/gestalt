package providerhost

import (
	"context"
	"strings"
	"sync"
)

type PublicHostServiceSessionVerifier interface {
	VerifyHostServiceSession(context.Context, string) error
}

// PublicHostService describes a host service that can be served by the public
// gestaltd listener after a host-service relay token has authorized the RPC.
type PublicHostService struct {
	PluginName      string
	SessionID       string
	SessionVerifier PublicHostServiceSessionVerifier
	Service         HostService
}

type PublicHostServiceRegistry struct {
	mu       sync.Mutex
	services []PublicHostService
	version  uint64
}

func NewPublicHostServiceRegistry() *PublicHostServiceRegistry {
	return &PublicHostServiceRegistry{}
}

func (r *PublicHostServiceRegistry) Register(pluginName string, services ...HostService) {
	r.RegisterVerified(pluginName, nil, services...)
}

func (r *PublicHostServiceRegistry) RegisterVerified(pluginName string, verifier PublicHostServiceSessionVerifier, services ...HostService) {
	r.register(pluginName, "", verifier, services...)
}

func (r *PublicHostServiceRegistry) RegisterSession(pluginName, sessionID string, services ...HostService) {
	r.register(pluginName, sessionID, nil, services...)
}

func (r *PublicHostServiceRegistry) register(pluginName, sessionID string, verifier PublicHostServiceSessionVerifier, services ...HostService) {
	if r == nil {
		return
	}
	pluginName = strings.TrimSpace(pluginName)
	sessionID = strings.TrimSpace(sessionID)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, service := range services {
		r.services = append(r.services, PublicHostService{
			PluginName:      pluginName,
			SessionID:       sessionID,
			SessionVerifier: verifier,
			Service:         service,
		})
	}
	if len(services) > 0 {
		r.version++
	}
}

func (r *PublicHostServiceRegistry) Unregister(pluginName string, services ...HostService) {
	r.unregister(pluginName, "", services...)
}

func (r *PublicHostServiceRegistry) UnregisterSession(pluginName, sessionID string, services ...HostService) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	r.unregister(pluginName, sessionID, services...)
}

func (r *PublicHostServiceRegistry) unregister(pluginName, sessionID string, services ...HostService) {
	if r == nil {
		return
	}
	pluginName = strings.TrimSpace(pluginName)
	sessionID = strings.TrimSpace(sessionID)
	if pluginName == "" {
		return
	}
	removing := make(map[string]struct{}, len(services))
	for _, service := range services {
		key := publicHostServiceKey(service)
		if key != "" {
			removing[key] = struct{}{}
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	originalLen := len(r.services)
	filtered := r.services[:0]
	for _, service := range r.services {
		if strings.TrimSpace(service.PluginName) == pluginName && strings.TrimSpace(service.SessionID) == sessionID {
			if len(removing) == 0 {
				continue
			}
			if _, ok := removing[publicHostServiceKey(service.Service)]; ok {
				continue
			}
		}
		filtered = append(filtered, service)
	}
	for i := len(filtered); i < originalLen; i++ {
		r.services[i] = PublicHostService{}
	}
	r.services = filtered
	if len(r.services) != originalLen {
		r.version++
	}
}

func (r *PublicHostServiceRegistry) Services() []PublicHostService {
	services, _ := r.Snapshot()
	return services
}

func (r *PublicHostServiceRegistry) Snapshot() ([]PublicHostService, uint64) {
	if r == nil {
		return nil, 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.services) == 0 {
		return nil, r.version
	}
	return append([]PublicHostService(nil), r.services...), r.version
}

func (r *PublicHostServiceRegistry) Version() uint64 {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.version
}

func publicHostServiceKey(service HostService) string {
	name := strings.TrimSpace(service.Name)
	envVar := strings.TrimSpace(service.EnvVar)
	if name == "" || envVar == "" {
		return ""
	}
	return name + "\x00" + envVar
}
