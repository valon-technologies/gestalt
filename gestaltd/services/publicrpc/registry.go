package publicrpc

import (
	"fmt"
	"slices"
	"strings"

	"google.golang.org/genproto/googleapis/api/visibility"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	protov1 "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

const providerPackage = "gestalt.provider.v1"

// PublicMethodPolicy describes one publicly exposed gRPC method and how
// external requests should be adapted before dispatch.
type PublicMethodPolicy struct {
	FullMethod string
	Service    string
	Method     string
	Fill       []string
	Reject     []string
}

// PublicMethodRegistry looks up public method policies by full gRPC method name.
type PublicMethodRegistry interface {
	Lookup(fullMethod string) (PublicMethodPolicy, bool)
}

// Registry indexes public method policies discovered from protobuf descriptors.
type Registry struct {
	methods map[string]PublicMethodPolicy
}

// NewRegistry scans every method in files and builds a lookup table for methods
// marked public via google.api.method_visibility.
func NewRegistry(files *protoregistry.Files) (*Registry, error) {
	if files == nil {
		return nil, fmt.Errorf("publicrpc: files registry is required")
	}

	registry := &Registry{methods: make(map[string]PublicMethodPolicy)}
	var errs []string

	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if string(fd.Package()) != providerPackage {
			return true
		}
		services := fd.Services()
		for i := 0; i < services.Len(); i++ {
			sd := services.Get(i)
			methods := sd.Methods()
			for j := 0; j < methods.Len(); j++ {
				md := methods.Get(j)
				if err := registry.indexMethod(md, &errs); err != nil {
					errs = append(errs, err.Error())
				}
			}
		}
		return true
	})

	if len(errs) > 0 {
		return nil, fmt.Errorf("publicrpc: %s", strings.Join(errs, "; "))
	}
	return registry, nil
}

func (r *Registry) indexMethod(md protoreflect.MethodDescriptor, errs *[]string) error {
	policy, public, err := policyForMethod(md)
	if err != nil {
		return err
	}
	if !public {
		if proto.HasExtension(md.Options(), protov1.E_Public) {
			fullMethod := fullGRPCMethod(md)
			*errs = append(*errs, fmt.Sprintf(
				"method %s has option (public) but is not marked PUBLIC via google.api.method_visibility",
				fullMethod,
			))
		}
		return nil
	}
	if _, exists := r.methods[policy.FullMethod]; exists {
		return fmt.Errorf("duplicate public method %s", policy.FullMethod)
	}
	r.methods[policy.FullMethod] = policy
	return nil
}

// Lookup returns the public policy for a full gRPC method name such as
// "/gestalt.provider.v1.App/Invoke".
func (r *Registry) Lookup(fullMethod string) (PublicMethodPolicy, bool) {
	if r == nil {
		return PublicMethodPolicy{}, false
	}
	policy, ok := r.methods[fullMethod]
	return policy, ok
}

func policyForMethod(md protoreflect.MethodDescriptor) (PublicMethodPolicy, bool, error) {
	if !isPublicMethod(md) {
		return PublicMethodPolicy{}, false, nil
	}

	policy := PublicMethodPolicy{
		FullMethod: fullGRPCMethod(md),
		Service:    string(md.Parent().FullName()),
		Method:     string(md.Name()),
	}

	if proto.HasExtension(md.Options(), protov1.E_Public) {
		publicPolicy, ok := proto.GetExtension(md.Options(), protov1.E_Public).(*protov1.PublicPolicy)
		if !ok || publicPolicy == nil {
			return PublicMethodPolicy{}, false, fmt.Errorf("method %s: invalid option (public)", policy.FullMethod)
		}
		fill, err := normalizePolicyFields("fill", publicPolicy.GetFill())
		if err != nil {
			return PublicMethodPolicy{}, false, fmt.Errorf("method %s: %w", policy.FullMethod, err)
		}
		reject, err := normalizePolicyFields("reject", publicPolicy.GetReject())
		if err != nil {
			return PublicMethodPolicy{}, false, fmt.Errorf("method %s: %w", policy.FullMethod, err)
		}
		if overlap := intersect(fill, reject); len(overlap) > 0 {
			return PublicMethodPolicy{}, false, fmt.Errorf(
				"method %s: fields cannot appear in both fill and reject: %s",
				policy.FullMethod,
				strings.Join(overlap, ", "),
			)
		}
		if err := validatePolicyFields(md.Input(), fill, reject); err != nil {
			return PublicMethodPolicy{}, false, fmt.Errorf("method %s: %w", policy.FullMethod, err)
		}
		policy.Fill = fill
		policy.Reject = reject
	}

	return policy, true, nil
}

func isPublicMethod(md protoreflect.MethodDescriptor) bool {
	if !proto.HasExtension(md.Options(), visibility.E_MethodVisibility) {
		return false
	}
	rule, ok := proto.GetExtension(md.Options(), visibility.E_MethodVisibility).(*visibility.VisibilityRule)
	if !ok || rule == nil {
		return false
	}
	return hasRestriction(rule.GetRestriction(), "PUBLIC")
}

func hasRestriction(restriction string, want string) bool {
	for part := range strings.SplitSeq(restriction, ",") {
		if strings.TrimSpace(part) == want {
			return true
		}
	}
	return false
}

func fullGRPCMethod(md protoreflect.MethodDescriptor) string {
	return "/" + string(md.Parent().FullName()) + "/" + string(md.Name())
}

func normalizePolicyFields(kind string, fields []string) ([]string, error) {
	if len(fields) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(fields))
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			return nil, fmt.Errorf("%s contains an empty field name", kind)
		}
		if _, ok := seen[field]; ok {
			return nil, fmt.Errorf("%s contains duplicate field %q", kind, field)
		}
		seen[field] = struct{}{}
		out = append(out, field)
	}
	return out, nil
}

func validatePolicyFields(
	input protoreflect.MessageDescriptor,
	fill []string,
	reject []string,
) error {
	for _, field := range append(slices.Clone(fill), reject...) {
		if input.Fields().ByName(protoreflect.Name(field)) == nil {
			return fmt.Errorf("unknown %s field %q on request message %s", policyFieldKind(fill, reject, field), field, input.FullName())
		}
	}
	return nil
}

func policyFieldKind(fill []string, reject []string, field string) string {
	if slices.Contains(fill, field) {
		return "fill"
	}
	return "reject"
}

func intersect(a []string, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(b))
	for _, item := range b {
		set[item] = struct{}{}
	}
	var out []string
	for _, item := range a {
		if _, ok := set[item]; ok {
			out = append(out, item)
		}
	}
	slices.Sort(out)
	return out
}
