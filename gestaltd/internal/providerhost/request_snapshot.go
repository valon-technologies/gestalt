package providerhost

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type RequestSnapshotStore struct {
	mu        sync.Mutex
	snapshots map[string]*requestSnapshotEntry
}

type requestSnapshotEntry struct {
	snapshot requestSnapshot
	refs     int
}

type requestSnapshot struct {
	principal   *principal.Principal
	requestMeta invocation.RequestMeta
	credential  invocation.CredentialContext
	invocation  *invocation.InvocationMeta
	webhook     *invocation.WebhookContext
	surface     invocation.InvocationSurface
	connection  string
}

func NewRequestSnapshotStore() *RequestSnapshotStore {
	return &RequestSnapshotStore{
		snapshots: make(map[string]*requestSnapshotEntry),
	}
}

func (s *RequestSnapshotStore) Register(ctx context.Context, handle string) func() {
	if s == nil || handle == "" {
		return func() {}
	}

	snapshot := captureRequestSnapshot(ctx, handle)

	s.mu.Lock()
	entry, ok := s.snapshots[handle]
	if !ok {
		entry = &requestSnapshotEntry{}
		s.snapshots[handle] = entry
	}
	entry.snapshot = snapshot
	entry.refs++
	s.mu.Unlock()

	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		entry, ok := s.snapshots[handle]
		if !ok {
			return
		}
		entry.refs--
		if entry.refs <= 0 {
			delete(s.snapshots, handle)
		}
	}
}

func (s *RequestSnapshotStore) snapshot(handle string) (requestSnapshot, error) {
	if s == nil {
		return requestSnapshot{}, fmt.Errorf("plugin invocation request snapshots are not available")
	}
	if handle == "" {
		return requestSnapshot{}, fmt.Errorf("plugin invocation request handle is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.snapshots[handle]
	if !ok || entry == nil {
		return requestSnapshot{}, fmt.Errorf("plugin invocation request handle %q is invalid or expired", handle)
	}
	return cloneRequestSnapshot(entry.snapshot), nil
}

func captureRequestSnapshot(ctx context.Context, handle string) requestSnapshot {
	meta := invocation.MetaFromContext(ctx)
	if meta == nil {
		meta = &invocation.InvocationMeta{RequestID: handle}
	}

	return requestSnapshot{
		principal:   clonePrincipal(principal.FromContext(ctx)),
		requestMeta: invocation.RequestMetaFromContext(ctx),
		credential:  invocation.CredentialContextFromContext(ctx),
		invocation:  cloneInvocationMeta(meta),
		webhook:     invocation.WebhookContextFromContext(ctx),
		surface:     invocation.InvocationSurfaceFromContext(ctx),
		connection:  invocation.ConnectionFromContext(ctx),
	}
}

func cloneRequestSnapshot(snapshot requestSnapshot) requestSnapshot {
	return requestSnapshot{
		principal:   clonePrincipal(snapshot.principal),
		requestMeta: snapshot.requestMeta,
		credential:  snapshot.credential,
		invocation:  cloneInvocationMeta(snapshot.invocation),
		webhook:     cloneWebhookContext(snapshot.webhook),
		surface:     snapshot.surface,
		connection:  snapshot.connection,
	}
}

func clonePrincipal(src *principal.Principal) *principal.Principal {
	if src == nil {
		return nil
	}

	out := *src
	out.Scopes = append([]string(nil), src.Scopes...)
	if src.Identity != nil {
		identity := *src.Identity
		out.Identity = &identity
	}
	return &out
}

func cloneInvocationMeta(src *invocation.InvocationMeta) *invocation.InvocationMeta {
	if src == nil {
		return nil
	}
	return &invocation.InvocationMeta{
		RequestID: src.RequestID,
		Depth:     src.Depth,
		CallChain: append([]string(nil), src.CallChain...),
	}
}

func cloneWebhookContext(src *invocation.WebhookContext) *invocation.WebhookContext {
	if src == nil {
		return nil
	}
	dst := *src
	if len(src.RawBody) > 0 {
		dst.RawBody = append([]byte(nil), src.RawBody...)
	}
	if len(src.Headers) > 0 {
		dst.Headers = make(map[string][]string, len(src.Headers))
		for key, values := range src.Headers {
			dst.Headers[key] = append([]string(nil), values...)
		}
	}
	if len(src.Claims) > 0 {
		dst.Claims = make(map[string]string, len(src.Claims))
		for key, value := range src.Claims {
			dst.Claims[key] = value
		}
	}
	return &dst
}

func restoreRequestSnapshotContext(ctx context.Context, snapshot requestSnapshot, connectionOverride string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if snapshot.principal != nil {
		ctx = principal.WithPrincipal(ctx, clonePrincipal(snapshot.principal))
	}
	if snapshot.invocation != nil {
		ctx = invocation.ContextWithMeta(ctx, cloneInvocationMeta(snapshot.invocation))
	}
	if snapshot.requestMeta != (invocation.RequestMeta{}) {
		ctx = invocation.WithRequestMeta(ctx, snapshot.requestMeta)
	}
	if snapshot.credential != (invocation.CredentialContext{}) {
		ctx = invocation.WithCredentialContext(ctx, snapshot.credential)
	}
	if snapshot.webhook != nil {
		ctx = invocation.WithWebhookContext(ctx, snapshot.webhook)
	}
	if snapshot.surface != "" {
		ctx = invocation.WithInvocationSurface(ctx, snapshot.surface)
	}

	connection := strings.TrimSpace(connectionOverride)
	if connection == "" {
		connection = snapshot.connection
	}
	if connection == "" && shouldInheritCredentialSelectors(snapshot) {
		connection = snapshot.credential.Connection
	}
	if connection != "" {
		ctx = invocation.WithConnection(ctx, connection)
	}

	return ctx
}

func shouldInheritCredentialSelectors(snapshot requestSnapshot) bool {
	return snapshot.principal == nil || snapshot.principal.Kind != principal.KindWorkload
}
