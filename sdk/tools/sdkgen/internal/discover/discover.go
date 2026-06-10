// Package discover finds provider services in a compiled proto schema by
// convention.
package discover

import (
	"sort"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// ProviderPackage is the proto package that holds every provider service.
// Dependency files (googleapis, well-known types) are loaded for type
// resolution but never discovered.
const ProviderPackage = "gestalt.provider.v1"

// Services returns every service in the provider package, sorted by full name
// so downstream output is deterministic.
func Services(files *protoregistry.Files) []protoreflect.ServiceDescriptor {
	var out []protoreflect.ServiceDescriptor
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if string(fd.Package()) != ProviderPackage {
			return true
		}
		services := fd.Services()
		for i := 0; i < services.Len(); i++ {
			out = append(out, services.Get(i))
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].FullName() < out[j].FullName() })
	return out
}
