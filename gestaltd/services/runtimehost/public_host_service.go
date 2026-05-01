package runtimehost

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
	SessionVerifier PublicHostServiceSessionVerifier
	Service         HostService
	registrationID  uint64
}

type PublicHostServiceRegistry struct {
	mu       sync.Mutex
	services []PublicHostService
	nextID   uint64
}

type PublicHostServiceRegistration struct {
	registry *PublicHostServiceRegistry
	ids      []uint64
}

func NewPublicHostServiceRegistry() *PublicHostServiceRegistry {
	return &PublicHostServiceRegistry{}
}

func (r *PublicHostServiceRegistry) RegisterVerified(pluginName string, verifier PublicHostServiceSessionVerifier, services ...HostService) PublicHostServiceRegistration {
	if verifier == nil {
		return PublicHostServiceRegistration{}
	}
	return r.register(pluginName, verifier, services...)
}

func (r *PublicHostServiceRegistry) register(pluginName string, verifier PublicHostServiceSessionVerifier, services ...HostService) PublicHostServiceRegistration {
	if r == nil {
		return PublicHostServiceRegistration{}
	}
	pluginName = strings.TrimSpace(pluginName)
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]uint64, 0, len(services))
	for _, service := range services {
		r.nextID++
		ids = append(ids, r.nextID)
		r.services = append(r.services, PublicHostService{
			PluginName:      pluginName,
			SessionVerifier: verifier,
			Service:         service,
			registrationID:  r.nextID,
		})
	}
	return PublicHostServiceRegistration{registry: r, ids: ids}
}

func (r PublicHostServiceRegistration) Unregister() {
	if r.registry == nil || len(r.ids) == 0 {
		return
	}
	r.registry.unregisterIDs(r.ids...)
}

func (r *PublicHostServiceRegistry) unregisterIDs(ids ...uint64) {
	if r == nil || len(ids) == 0 {
		return
	}
	removing := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if id != 0 {
			removing[id] = struct{}{}
		}
	}
	if len(removing) == 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	originalLen := len(r.services)
	filtered := r.services[:0]
	for _, service := range r.services {
		if _, ok := removing[service.registrationID]; ok {
			continue
		}
		filtered = append(filtered, service)
	}
	for i := len(filtered); i < originalLen; i++ {
		r.services[i] = PublicHostService{}
	}
	r.services = filtered
}

func (r *PublicHostServiceRegistry) Snapshot() []PublicHostService {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.services) == 0 {
		return nil
	}
	return append([]PublicHostService(nil), r.services...)
}

func (s PublicHostService) RegistrationID() uint64 {
	return s.registrationID
}
