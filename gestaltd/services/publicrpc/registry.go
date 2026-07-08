package publicrpc

import (
	"fmt"
	"strings"

	gestaltproto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/genproto/googleapis/api/visibility"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

const publicVisibilityRestriction = "PUBLIC"

// Registry indexes public method policies from protobuf descriptors.
type Registry struct {
	byFullMethod map[string]PublicMethodPolicy
}

// NewRegistry builds a policy registry from the supplied file set.
func NewRegistry(files *protoregistry.Files) (*Registry, error) {
	if files == nil {
		return &Registry{byFullMethod: map[string]PublicMethodPolicy{}}, nil
	}
	byFullMethod := map[string]PublicMethodPolicy{}
	var walkErr error
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		for i := 0; i < fd.Services().Len(); i++ {
			sd := fd.Services().Get(i)
			for j := 0; j < sd.Methods().Len(); j++ {
				policy, public, err := policyForMethod(sd.Methods().Get(j))
				if err != nil {
					walkErr = err
					return false
				}
				if !public {
					continue
				}
				byFullMethod[policy.FullMethod] = policy
			}
		}
		return true
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return &Registry{byFullMethod: byFullMethod}, nil
}

// NewMapRegistry returns a registry backed by an explicit policy map.
func NewMapRegistry(policies map[string]PublicMethodPolicy) *Registry {
	byFullMethod := make(map[string]PublicMethodPolicy, len(policies))
	for fullMethod, policy := range policies {
		policy.FullMethod = fullMethod
		if policy.Service == "" || policy.Method == "" {
			service, method := splitFullMethod(fullMethod)
			if policy.Service == "" {
				policy.Service = service
			}
			if policy.Method == "" {
				policy.Method = method
			}
		}
		byFullMethod[fullMethod] = policy
	}
	return &Registry{byFullMethod: byFullMethod}
}

func splitFullMethod(fullMethod string) (service, method string) {
	fullMethod = strings.TrimSpace(fullMethod)
	if !strings.HasPrefix(fullMethod, "/") {
		return "", ""
	}
	service, method, ok := strings.Cut(strings.TrimPrefix(fullMethod, "/"), "/")
	if !ok {
		return "", ""
	}
	return service, method
}

func (r *Registry) Lookup(fullMethod string) (PublicMethodPolicy, bool) {
	if r == nil {
		return PublicMethodPolicy{}, false
	}
	policy, ok := r.byFullMethod[fullMethod]
	return policy, ok
}

func policyForMethod(md protoreflect.MethodDescriptor) (PublicMethodPolicy, bool, error) {
	visibilityRule, _ := proto.GetExtension(md.Options(), visibility.E_MethodVisibility).(*visibility.VisibilityRule)
	if !hasVisibilityRestriction(visibilityRule, publicVisibilityRestriction) {
		if proto.HasExtension(md.Options(), gestaltproto.E_Public) {
			return PublicMethodPolicy{}, false, fmt.Errorf("publicrpc: method %s/%s has (public) policy without PUBLIC visibility", md.Parent().FullName(), md.Name())
		}
		return PublicMethodPolicy{}, false, nil
	}

	policy := PublicMethodPolicy{
		FullMethod: "/" + string(md.Parent().FullName()) + "/" + string(md.Name()),
		Service:    string(md.Parent().FullName()),
		Method:     string(md.Name()),
	}
	if proto.HasExtension(md.Options(), gestaltproto.E_Public) {
		publicPolicy, ok := proto.GetExtension(md.Options(), gestaltproto.E_Public).(*gestaltproto.PublicPolicy)
		if !ok || publicPolicy == nil {
			return PublicMethodPolicy{}, false, fmt.Errorf("publicrpc: invalid (public) extension on %s", policy.FullMethod)
		}
		fill, reject, err := validateFieldPolicy(md.Input(), publicPolicy.GetFill(), publicPolicy.GetReject())
		if err != nil {
			return PublicMethodPolicy{}, false, err
		}
		policy.Fill = fill
		policy.Reject = reject
	}
	return policy, true, nil
}

func hasVisibilityRestriction(rule *visibility.VisibilityRule, restriction string) bool {
	if rule == nil {
		return false
	}
	for _, value := range strings.Split(rule.GetRestriction(), ",") {
		if strings.TrimSpace(value) == restriction {
			return true
		}
	}
	return false
}

func validateFieldPolicy(input protoreflect.MessageDescriptor, fill, reject []string) ([]string, []string, error) {
	if input == nil {
		return nil, nil, fmt.Errorf("publicrpc: request message descriptor is required")
	}
	seen := map[string]string{}
	normalizedFill := make([]string, 0, len(fill))
	for _, name := range fill {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if input.Fields().ByName(protoreflect.Name(name)) == nil {
			return nil, nil, fmt.Errorf("publicrpc: fill field %q does not exist on %s", name, input.FullName())
		}
		if prev, ok := seen[name]; ok && prev != "fill" {
			return nil, nil, fmt.Errorf("publicrpc: field %q appears in both fill and reject", name)
		}
		seen[name] = "fill"
		normalizedFill = append(normalizedFill, name)
	}
	normalizedReject := make([]string, 0, len(reject))
	for _, name := range reject {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if input.Fields().ByName(protoreflect.Name(name)) == nil {
			return nil, nil, fmt.Errorf("publicrpc: reject field %q does not exist on %s", name, input.FullName())
		}
		if prev, ok := seen[name]; ok && prev != "reject" {
			return nil, nil, fmt.Errorf("publicrpc: field %q appears in both fill and reject", name)
		}
		seen[name] = "reject"
		normalizedReject = append(normalizedReject, name)
	}
	return normalizedFill, normalizedReject, nil
}
