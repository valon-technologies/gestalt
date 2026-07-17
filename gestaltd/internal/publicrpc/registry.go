package publicrpc

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	"google.golang.org/genproto/googleapis/api/visibility"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	gestaltproto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

const publicVisibilityRestriction = "PUBLIC"

// PublicMethodPolicy describes how a public gRPC method adapts external
// requests.
type PublicMethodPolicy struct {
	FullMethod string
	Fill       []string
	Reject     []string
}

// Registry indexes public gRPC methods and their request adaptation policy.
type Registry struct {
	methods map[string]PublicMethodPolicy
}

// NewRegistry discovers public methods and policies from protobuf descriptors.
func NewRegistry(files *protoregistry.Files) (*Registry, error) {
	if files == nil {
		return nil, fmt.Errorf("publicrpc: file registry is required")
	}

	registry := &Registry{methods: map[string]PublicMethodPolicy{}}
	var errs []error

	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		services := fd.Services()
		for i := 0; i < services.Len(); i++ {
			service := services.Get(i)
			methods := service.Methods()
			for j := 0; j < methods.Len(); j++ {
				md := methods.Get(j)
				policy, public, err := policyForMethod(md)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				if !public {
					continue
				}
				if _, exists := registry.methods[policy.FullMethod]; exists {
					errs = append(errs, fmt.Errorf("publicrpc: duplicate public method %q", policy.FullMethod))
					continue
				}
				registry.methods[policy.FullMethod] = policy
			}
		}
		return true
	})

	if len(errs) > 0 {
		return nil, errorsJoin(errs)
	}
	return registry, nil
}

// NewGeneratedRegistry discovers public methods from the compiled Gestalt
// provider service descriptors.
func NewGeneratedRegistry() (*Registry, error) {
	files, err := GeneratedFiles()
	if err != nil {
		return nil, err
	}
	return NewRegistry(files)
}

// Lookup returns the public policy for a full gRPC method name.
func (r *Registry) Lookup(fullMethod string) (PublicMethodPolicy, bool) {
	if r == nil {
		return PublicMethodPolicy{}, false
	}
	policy, ok := r.methods[fullMethod]
	return policy, ok
}

// Methods returns a sorted copy of every registered public method policy.
func (r *Registry) Methods() []PublicMethodPolicy {
	if r == nil {
		return nil
	}
	methods := make([]PublicMethodPolicy, 0, len(r.methods))
	for _, method := range r.methods {
		method.Fill = slices.Clone(method.Fill)
		method.Reject = slices.Clone(method.Reject)
		methods = append(methods, method)
	}
	slices.SortFunc(methods, func(a, b PublicMethodPolicy) int {
		return cmp.Compare(a.FullMethod, b.FullMethod)
	})
	return methods
}

func policyForMethod(md protoreflect.MethodDescriptor) (PublicMethodPolicy, bool, error) {
	fullMethod := fullGRPCMethod(md)
	opts := md.Options().ProtoReflect()
	hasPublicPolicy := opts.Has(gestaltproto.E_Public.TypeDescriptor())
	isPublic := hasPublicVisibility(opts)

	if hasPublicPolicy && !isPublic {
		return PublicMethodPolicy{}, false, fmt.Errorf(
			"publicrpc: method %q has option (public) but is not marked PUBLIC via google.api.method_visibility",
			fullMethod,
		)
	}
	if !isPublic {
		return PublicMethodPolicy{}, false, nil
	}

	policy := PublicMethodPolicy{FullMethod: fullMethod}
	if hasPublicPolicy {
		fill, reject, err := readPublicPolicy(opts)
		if err != nil {
			return PublicMethodPolicy{}, false, err
		}
		fill, reject, err = normalizePolicyFields(fullMethod, md.Input(), fill, reject)
		if err != nil {
			return PublicMethodPolicy{}, false, err
		}
		policy.Fill = fill
		policy.Reject = reject
	}
	return policy, true, nil
}

func hasPublicVisibility(opts protoreflect.Message) bool {
	ext := visibility.E_MethodVisibility.TypeDescriptor()
	if ext == nil || !opts.Has(ext) {
		return false
	}
	rule := opts.Get(ext).Message()
	restrictionField := rule.Descriptor().Fields().ByName("restriction")
	if restrictionField == nil {
		return false
	}
	for _, restriction := range splitRestrictions(rule.Get(restrictionField).String()) {
		if restriction == publicVisibilityRestriction {
			return true
		}
	}
	return false
}

func readPublicPolicy(opts protoreflect.Message) (fill, reject []string, err error) {
	ext := gestaltproto.E_Public.TypeDescriptor()
	if ext == nil || !opts.Has(ext) {
		return nil, nil, nil
	}
	policy := opts.Get(ext).Message()
	fillField := policy.Descriptor().Fields().ByName("fill")
	rejectField := policy.Descriptor().Fields().ByName("reject")
	if fillField != nil {
		fill = repeatedStringValues(policy.Get(fillField).List())
	}
	if rejectField != nil {
		reject = repeatedStringValues(policy.Get(rejectField).List())
	}
	return fill, reject, nil
}

func repeatedStringValues(list protoreflect.List) []string {
	out := make([]string, 0, list.Len())
	for i := 0; i < list.Len(); i++ {
		out = append(out, list.Get(i).String())
	}
	return out
}

func splitRestrictions(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func normalizePolicyFields(fullMethod string, input protoreflect.MessageDescriptor, fill, reject []string) ([]string, []string, error) {
	normalizedFill, err := normalizeFieldList(fullMethod, input, "fill", fill)
	if err != nil {
		return nil, nil, err
	}
	normalizedReject, err := normalizeFieldList(fullMethod, input, "reject", reject)
	if err != nil {
		return nil, nil, err
	}
	fillSet := make(map[string]struct{}, len(normalizedFill))
	for _, name := range normalizedFill {
		fillSet[name] = struct{}{}
	}
	for _, name := range normalizedReject {
		if _, ok := fillSet[name]; ok {
			return nil, nil, fmt.Errorf("publicrpc: method %q lists field %q in both fill and reject", fullMethod, name)
		}
	}
	return normalizedFill, normalizedReject, nil
}

func normalizeFieldList(fullMethod string, input protoreflect.MessageDescriptor, kind string, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("publicrpc: method %q has empty %s field name", fullMethod, kind)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		if input == nil || input.Fields().ByName(protoreflect.Name(name)) == nil {
			return nil, fmt.Errorf("publicrpc: method %q %s references unknown request field %q", fullMethod, kind, name)
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out, nil
}

func fullGRPCMethod(md protoreflect.MethodDescriptor) string {
	return "/" + string(md.Parent().FullName()) + "/" + string(md.Name())
}

func errorsJoin(errs []error) error {
	return errors.Join(errs...)
}
