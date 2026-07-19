package providergateway

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

// Routable provider kinds. Authorization is declared in types.go (PG-1); the
// remaining routable kinds are declared here alongside the registry that owns
// them. Deferred kinds (PG-5/PG-6) are intentionally absent and register when
// their migrations activate (issue-148/issue-149).
const (
	ProviderKindApp      ProviderKind = "app"
	ProviderKindWorkflow ProviderKind = "workflow"
	ProviderKindAgent    ProviderKind = "agent"
)

// KindMethod is the registry's resolution of one full method name to its kind,
// generated method descriptor, and local server instance. The direct route
// (PG-1) dispatches from it; the PG-7 wire-server allowlist and the PG-9
// conformance driver read the same value.
type KindMethod struct {
	Kind   ProviderKind
	Method grpc.MethodDesc
	Server any
}

// kindEntry is the per-kind record stored in KindRegistry.
type kindEntry struct {
	desc    grpc.ServiceDesc
	server  any
	methods map[string]grpc.MethodDesc // method name -> descriptor
}

// KindRegistry maps routable provider kinds to their existing generated
// grpc.ServiceDesc values and local server instances. It is the single source
// the direct route (PG-1), the wire-server allowlist (PG-7), and the
// conformance driver (PG-9) read. It is hand-written (decision 5): no
// generator, no gen/ directory, no sdkgen involvement.
type KindRegistry struct {
	mu      sync.RWMutex
	kinds   map[ProviderKind]kindEntry
	methods map[string]ProviderKind // full method name -> kind
}

// NewKindRegistry returns an empty kind registry.
func NewKindRegistry() *KindRegistry {
	return &KindRegistry{
		kinds:   make(map[ProviderKind]kindEntry),
		methods: make(map[string]ProviderKind),
	}
}

// RegisterKind makes a kind routable. desc is the kind's existing generated
// grpc.ServiceDesc; server is the local implementation used by the direct
// route (nil when direct instances resolve elsewhere, e.g. the app provider
// map). Duplicate kind registration, overlapping full-method registration, or
// registration of a core/administration/RemoteManagement service returns a
// named error.
func (r *KindRegistry) RegisterKind(kind ProviderKind, desc grpc.ServiceDesc, server any) error {
	if r == nil {
		return fmt.Errorf("provider gateway: kind registry is required")
	}
	if strings.TrimSpace(string(kind)) == "" {
		return fmt.Errorf("provider gateway: kind is required")
	}
	if desc.ServiceName == "" {
		return fmt.Errorf("provider gateway: service desc is required for kind %q", kind)
	}
	if err := assertServiceRegisterable(desc.ServiceName); err != nil {
		return fmt.Errorf("provider gateway: kind %q: %w", kind, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.kinds[kind]; exists {
		return fmt.Errorf("provider gateway: kind %q already registered", kind)
	}
	methods := make(map[string]grpc.MethodDesc, len(desc.Methods))
	for _, method := range desc.Methods {
		fullMethod := "/" + desc.ServiceName + "/" + method.MethodName
		if owner, ok := r.methods[fullMethod]; ok {
			return fmt.Errorf("provider gateway: method %q already registered by kind %q", fullMethod, owner)
		}
		methods[method.MethodName] = method
		r.methods[fullMethod] = kind
	}
	r.kinds[kind] = kindEntry{desc: desc, server: server, methods: methods}
	return nil
}

// Lookup resolves a canonical full method name ("/" + desc.ServiceName + "/" +
// method.MethodName, matching publicrpc's convention) to its kind, method
// descriptor, and server. The returned Server is the kind's default local
// instance; a per-target instance server (e.g. an app provider) overrides it
// at the direct route's dispatch site.
func (r *KindRegistry) Lookup(fullMethod string) (KindMethod, bool) {
	if r == nil {
		return KindMethod{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	kind, ok := r.methods[fullMethod]
	if !ok {
		return KindMethod{}, false
	}
	entry := r.kinds[kind]
	service, methodName := splitFullMethod(fullMethod)
	if service == "" || methodName == "" {
		return KindMethod{}, false
	}
	method, ok := entry.methods[methodName]
	if !ok {
		return KindMethod{}, false
	}
	return KindMethod{Kind: kind, Method: method, Server: entry.server}, true
}

// Kinds returns the registered kinds in stable (sorted) order. The bootstrap
// exhaustiveness assertion and the PG-9 conformance driver consume this.
func (r *KindRegistry) Kinds() []ProviderKind {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	kinds := make([]ProviderKind, 0, len(r.kinds))
	for kind := range r.kinds {
		kinds = append(kinds, kind)
	}
	r.mu.RUnlock()
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	return kinds
}

// AssertExhaustive fails startup when any kind the route resolver can return is
// not registered. resolvableKinds is the complete set of kinds the resolver may
// hand to the gateway; every entry must appear in Kinds(). The returned error
// names the missing kinds so a misconfigured bootstrap fails loudly.
func (r *KindRegistry) AssertExhaustive(resolvableKinds []ProviderKind) error {
	if r == nil {
		return fmt.Errorf("provider gateway: kind registry is required")
	}
	registered := make(map[ProviderKind]struct{}, len(r.kinds))
	for _, kind := range r.Kinds() {
		registered[kind] = struct{}{}
	}
	var missing []ProviderKind
	for _, kind := range resolvableKinds {
		if _, ok := registered[kind]; !ok {
			missing = append(missing, kind)
		}
	}
	if len(missing) > 0 {
		sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
		return fmt.Errorf("provider gateway: unregistered routable kinds: %v", missing)
	}
	return nil
}

// DefaultKindRegistry builds the bootstrap kind table: only the currently
// routable kinds are registered (decision 6). authorizationServer is the local
// Authorization server instance (nil when authorization resolves elsewhere in
// this process). The app kind's provider-side service is AppProvider; its
// direct instances resolve via the per-target provider map, so its default
// server is nil. Deferred kinds (PG-5/PG-6) register when their migrations
// activate and are intentionally absent here.
func DefaultKindRegistry(authorizationServer any) (*KindRegistry, error) {
	reg := NewKindRegistry()
	if err := reg.RegisterKind(ProviderKindAuthorization, proto.Authorization_ServiceDesc, authorizationServer); err != nil {
		return nil, fmt.Errorf("register %s kind: %w", ProviderKindAuthorization, err)
	}
	if err := reg.RegisterKind(ProviderKindApp, proto.AppProvider_ServiceDesc, nil); err != nil {
		return nil, fmt.Errorf("register %s kind: %w", ProviderKindApp, err)
	}
	if err := reg.RegisterKind(ProviderKindWorkflow, proto.Workflow_ServiceDesc, nil); err != nil {
		return nil, fmt.Errorf("register %s kind: %w", ProviderKindWorkflow, err)
	}
	if err := reg.RegisterKind(ProviderKindAgent, proto.Agent_ServiceDesc, nil); err != nil {
		return nil, fmt.Errorf("register %s kind: %w", ProviderKindAgent, err)
	}
	// Deferred kinds: IndexedDB and S3 land with PG-5 (issue-148); Cache,
	// Secrets, Runtime, and ExternalCredentials land with PG-6 (issue-149).
	// Telemetry and audit are permanently exempt (decision 1).
	return reg, nil
}

// assertServiceRegisterable rejects core, administration, and RemoteManagement
// services by name so the PG-7 wire-server allowlist can never expose them.
// Provider services live under gestalt.provider.v1; the refused categories are
// gestaltd's own internal services, which must never be routable as provider
// kinds.
func assertServiceRegisterable(serviceName string) error {
	switch {
	case strings.HasPrefix(serviceName, "gestalt.core."):
		return fmt.Errorf("core service %q is not a provider kind", serviceName)
	case strings.HasPrefix(serviceName, "gestalt.admin."):
		return fmt.Errorf("administration service %q is not a provider kind", serviceName)
	case strings.Contains(serviceName, "RemoteManagement"):
		return fmt.Errorf("remote management service %q is not a provider kind", serviceName)
	}
	return nil
}
