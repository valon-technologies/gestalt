package scim

import (
	"context"
	"net/http"
	"sync/atomic"

	"github.com/valon-technologies/gestalt/server/internal/config"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type Service struct{ compact *CompactService }

func (s *Service) Enabled() bool { return s != nil && s.compact != nil && s.compact.Enabled() }

func (s *Service) ClientForToken(token string) (string, bool) {
	if s == nil || s.compact == nil {
		return "", false
	}
	return s.compact.ClientForToken(token)
}

func (s *Service) Start(context.Context) {}

func (s *Service) IsEligible(ctx context.Context, coreID, email string) (bool, error) {
	if s == nil || s.compact == nil {
		return true, nil
	}
	return s.compact.IsEligible(ctx, coreID, email)
}

// ResolveAuthorizationResourceDisplayName returns current metadata from the
// resource-owning SCIM store. Authorization relationships intentionally keep
// only stable resource IDs, so renamed groups do not require relationship
// rewrites to remain readable in admin projections.
func (s *Service) ResolveAuthorizationResourceDisplayName(ctx context.Context, resource *proto.Resource) (string, error) {
	if s == nil || s.compact == nil {
		return "", nil
	}
	return s.compact.authorizationResourceDisplayName(ctx, resource)
}

type runtimeSnapshot struct {
	service *Service
	handler *leanHandler
	managed map[string]struct{}
}

// Runtime owns one immutable SCIM snapshot. Consumers see the complete
// handler, eligibility service, and managed-group set from the same snapshot,
// eliminating partial-update windows.
type Runtime struct {
	current atomic.Pointer[runtimeSnapshot]
}

func NewRuntime(s *Service, cfg config.ServerSCIMConfig) *Runtime {
	r := &Runtime{}
	r.Apply(s, cfg)
	return r
}

func newRuntimeSnapshot(s *Service, cfg config.ServerSCIMConfig) *runtimeSnapshot {
	handler := &leanHandler{}
	if s != nil {
		handler = &leanHandler{s: s.compact}
	}
	return &runtimeSnapshot{
		service: s,
		handler: handler,
		managed: config.ManagedGroupIDs(cfg),
	}
}

func (r *Runtime) Handler() http.Handler { return r }

func (r *Runtime) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if r == nil {
		(&leanHandler{}).ServeHTTP(w, request)
		return
	}
	snapshot := r.current.Load()
	if snapshot == nil || snapshot.handler == nil {
		(&leanHandler{}).ServeHTTP(w, request)
		return
	}
	snapshot.handler.ServeHTTP(w, request)
}

// Apply atomically publishes a complete SCIM snapshot. A nil service disables
// authenticated SCIM requests while preserving the runtime container.
func (r *Runtime) Apply(s *Service, cfg config.ServerSCIMConfig) {
	if r == nil {
		return
	}
	r.current.Store(newRuntimeSnapshot(s, cfg))
}

func (r *Runtime) Enabled() bool {
	if snapshot := r.snapshot(); snapshot != nil {
		return snapshot.service.Enabled()
	}
	return false
}

func (r *Runtime) snapshot() *runtimeSnapshot {
	if r == nil {
		return nil
	}
	return r.current.Load()
}

func (r *Runtime) Service() *Service {
	if snapshot := r.snapshot(); snapshot != nil {
		return snapshot.service
	}
	return nil
}

func (r *Runtime) ManagedGroupIDs() map[string]struct{} {
	if snapshot := r.snapshot(); snapshot != nil {
		return cloneGroupIDs(snapshot.managed)
	}
	return nil
}

func (r *Runtime) IsEligible(ctx context.Context, coreID, email string) (bool, error) {
	if snapshot := r.snapshot(); snapshot != nil {
		return snapshot.service.IsEligible(ctx, coreID, email)
	}
	return true, nil
}

func cloneGroupIDs(ids map[string]struct{}) map[string]struct{} {
	if ids == nil {
		return nil
	}
	out := make(map[string]struct{}, len(ids))
	for id := range ids {
		out[id] = struct{}{}
	}
	return out
}
