package scim

import (
	"context"
	"net/http"
	"sync"
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

// Runtime owns the active SCIM handler. Configuration updates replace the
// entire snapshot atomically, so requests never observe a partial update.
type Runtime struct {
	inner     *Service
	cfg       config.ServerSCIMConfig
	handler   atomic.Pointer[leanHandler]
	managedMu sync.RWMutex
	managed   map[string]struct{}
}

func NewRuntime(s *Service, cfg config.ServerSCIMConfig) *Runtime {
	r := &Runtime{inner: s, cfg: cfg}
	handler := &leanHandler{}
	if s != nil {
		handler = &leanHandler{s: s.compact}
	}
	r.handler.Store(handler)
	r.managed = config.ManagedGroupIDs(cfg)
	return r
}

func (r *Runtime) Handler() http.Handler { return r }

func (r *Runtime) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if r == nil {
		(&leanHandler{}).ServeHTTP(w, request)
		return
	}
	if handler := r.handler.Load(); handler != nil {
		handler.ServeHTTP(w, request)
		return
	}
	(&leanHandler{}).ServeHTTP(w, request)
}

// Apply atomically replaces the active service. A nil service disables SCIM.
func (r *Runtime) Apply(s *Service, cfg config.ServerSCIMConfig) {
	if r == nil {
		return
	}
	r.inner = s
	r.cfg = cfg
	handler := &leanHandler{}
	if s != nil {
		handler = &leanHandler{s: s.compact}
	}
	r.handler.Store(handler)
	managed := config.ManagedGroupIDs(cfg)
	r.managedMu.Lock()
	r.managed = managed
	r.managedMu.Unlock()
}

func (r *Runtime) Config() config.ServerSCIMConfig {
	if r == nil {
		return config.ServerSCIMConfig{}
	}
	return r.cfg
}

func (r *Runtime) Service() *Service {
	if r == nil {
		return nil
	}
	return r.inner
}

func (r *Runtime) ManagedGroupIDs() map[string]struct{} {
	if r == nil {
		return nil
	}
	r.managedMu.RLock()
	defer r.managedMu.RUnlock()
	if r.managed == nil {
		return nil
	}
	out := make(map[string]struct{}, len(r.managed))
	for id := range r.managed {
		out[id] = struct{}{}
	}
	return out
}

// ResolveAuthorizationResourceDisplayName keeps display-name resolution stable
// across configuration updates even when the latest service has no clients.
func (r *Runtime) ResolveAuthorizationResourceDisplayName(ctx context.Context, resource *proto.Resource) (string, error) {
	if r == nil || r.inner == nil {
		return "", nil
	}
	return r.inner.ResolveAuthorizationResourceDisplayName(ctx, resource)
}

func ManagedGroupIDs(cfg config.ServerSCIMConfig) map[string]struct{} {
	out := map[string]struct{}{}
	for _, client := range cfg.Clients {
		for _, projection := range client.ActiveUserRelationships {
			if projection.Resource.Type == "group" {
				out[projection.Resource.ID] = struct{}{}
			}
		}
	}
	return out
}
