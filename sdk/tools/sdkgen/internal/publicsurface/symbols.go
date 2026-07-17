package publicsurface

import (
	"fmt"
	"strings"
	"unicode"
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
			Method:  goManifestMethod(pm),
		},
		Python: ManifestSymbol{
			Service: serviceLocal + "Client",
			Method:  pythonManifestMethod(pm),
		},
		Rust: ManifestSymbol{
			Service: serviceLocal + "Client",
			Method:  rustManifestMethod(pm),
		},
		TypeScript: ManifestSymbol{
			Service: typescriptServiceClient(pm.Service),
			Method:  typescriptManifestMethod(pm),
		},
	}
}

func goManifestMethod(pm PublicMethod) string {
	if pm.JSONResult != nil {
		return pm.Method
	}
	return pm.Method
}

func pythonManifestMethod(pm PublicMethod) string {
	if pm.JSONResult != nil {
		return snakeCase(pm.Method)
	}
	if pm.Method == "InvokeGraphQL" {
		return "invoke_graphql"
	}
	return snakeCase(pm.Method)
}

func rustManifestMethod(pm PublicMethod) string {
	if pm.JSONResult != nil {
		return snakeCase(pm.Method)
	}
	if pm.Method == "InvokeGraphQL" {
		return "invoke_graphql"
	}
	return snakeCase(pm.Method)
}

func typescriptManifestMethod(pm PublicMethod) string {
	if pm.JSONResult != nil {
		return lowerFirst(pm.Method)
	}
	return lowerFirst(pm.Method)
}

func typescriptServiceClient(serviceFullName string) string {
	local := ServiceLocalName(serviceFullName)
	switch local {
	case "ExternalCredentials":
		return "ExternalCredentialsClient"
	case "IndexedDB":
		return "IndexedDBClient"
	default:
		return local + "Client"
	}
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func snakeCase(s string) string {
	s = strings.ReplaceAll(s, "GraphQL", "Graphql")
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			prevLower := !unicode.IsUpper(runes[i-1]) && runes[i-1] != '_'
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if prevLower || (unicode.IsUpper(runes[i-1]) && nextLower) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	out := b.String()
	if out == "invoke_graphql" {
		return "invoke_graphql"
	}
	return out
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
