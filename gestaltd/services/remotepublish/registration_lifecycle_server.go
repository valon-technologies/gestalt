package remotepublish

import (
	"context"
	"slices"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// registrationLifecycleServer serves RegistrationLifecycle.Check on the tunnel
// listener for one publication group. Check succeeds only when the exact
// provider set named in the request matches the group's immutable provider set
// and every provider is present in the local provider registry.
type registrationLifecycleServer struct {
	proto.UnimplementedRegistrationLifecycleServer
	groupProviders map[string]bool
	registry       ProviderLookup
}

// ProviderLookup checks whether a named provider is registered locally.
type ProviderLookup interface {
	Has(name string) bool
}

func NewRegistrationLifecycleServerForTest(names []string, registry ProviderLookup) proto.RegistrationLifecycleServer {
	pubs := make([]ProviderPublication, 0, len(names))
	for _, n := range names {
		pubs = append(pubs, ProviderPublication{Name: n})
	}
	return newRegistrationLifecycleServer(pubs, registry)
}

func newRegistrationLifecycleServer(groupProviders []ProviderPublication, registry ProviderLookup) *registrationLifecycleServer {
	set := make(map[string]bool, len(groupProviders))
	for _, p := range groupProviders {
		set[p.Name] = true
	}
	return &registrationLifecycleServer{groupProviders: set, registry: registry}
}

func (s *registrationLifecycleServer) Check(_ context.Context, req *proto.RegistrationCheckRequest) (*proto.RegistrationCheckResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	refs := req.GetProviders()
	if len(refs) == 0 {
		return nil, status.Error(codes.InvalidArgument, "providers is required")
	}

	// Every ref must match the group's exact provider set.
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		kind := providermanifestv1.NormalizeKind(ref.GetKind())
		name := strings.TrimSpace(ref.GetName())
		if kind == "" || name == "" {
			return nil, status.Error(codes.InvalidArgument, "provider kind and name are required")
		}
		if kind != providermanifestv1.KindApp {
			return nil, status.Errorf(codes.InvalidArgument, "provider kind %q is not supported", kind)
		}
		if !s.groupProviders[name] {
			return nil, status.Errorf(codes.InvalidArgument, "provider %q is not part of this registration", name)
		}
		if seen[name] {
			return nil, status.Errorf(codes.InvalidArgument, "duplicate provider %q", name)
		}
		seen[name] = true
	}

	// The request set must match the group set exactly (no subset, no superset).
	if len(seen) != len(s.groupProviders) {
		return &proto.RegistrationCheckResponse{
			Ready:   false,
			Message: "provider set does not match registration",
		}, nil
	}

	var missing []string
	for name := range s.groupProviders {
		if !s.registry.Has(name) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return &proto.RegistrationCheckResponse{
			Ready:   false,
			Message: "providers not ready: " + strings.Join(missing, ", "),
		}, nil
	}
	return &proto.RegistrationCheckResponse{Ready: true}, nil
}
