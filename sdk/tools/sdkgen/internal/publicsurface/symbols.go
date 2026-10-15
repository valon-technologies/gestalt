package publicsurface

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/naming"
)

// ManifestSymbols records the generated client symbol names for one method.
type ManifestSymbols struct {
	Go         ManifestSymbol `json:"go"`
	Python     ManifestSymbol `json:"python"`
	Rust       ManifestSymbol `json:"rust"`
	TypeScript ManifestSymbol `json:"typescript"`
}

// ManifestSymbol names the generated service client type and method symbol.
type ManifestSymbol struct {
	Service string `json:"service"`
	Method  string `json:"method"`
}

func manifestSymbols(pm PublicMethod) ManifestSymbols {
	serviceLocal := ServiceLocalName(pm.Service)
	return ManifestSymbols{
		Go: ManifestSymbol{
			Service: serviceLocal + "Client",
			Method:  pm.Method,
		},
		Python: ManifestSymbol{
			Service: serviceLocal + "Client",
			Method:  naming.SnakeCase(pm.Method),
		},
		Rust: ManifestSymbol{
			Service: serviceLocal + "Client",
			Method:  naming.SnakeCase(pm.Method),
		},
		TypeScript: ManifestSymbol{
			Service: serviceLocal + "Client",
			Method:  naming.LowerFirst(pm.Method),
		},
	}
}

// MarshalAvailabilityDoc renders human-readable API availability from a manifest.
func MarshalAvailabilityDoc(m Manifest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Public API availability\n\n")
	fmt.Fprintf(&b, "Generated from the public surface manifest (%d gRPC methods, %d REST methods).\n\n",
		m.GRPCMethodCount, m.RESTMethodCount)
	fmt.Fprintf(&b, "| Service | Method | REST | Go | Python | Rust | TypeScript |\n")
	fmt.Fprintf(&b, "| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, entry := range m.Methods {
		rest := "gRPC only"
		if entry.RESTVerb != "" {
			rest = entry.RESTVerb + " " + entry.RESTPath
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s.%s | %s.%s | %s.%s | %s.%s |\n",
			entry.Service,
			entry.Method,
			rest,
			entry.Symbols.Go.Service, entry.Symbols.Go.Method,
			entry.Symbols.Python.Service, entry.Symbols.Python.Method,
			entry.Symbols.Rust.Service, entry.Symbols.Rust.Method,
			entry.Symbols.TypeScript.Service, entry.Symbols.TypeScript.Method,
		)
	}
	return b.String()
}
