package emit_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/golang"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/publicrpc"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/python"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/rust"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/ts"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/pipeline"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

type manifestSymbol struct {
	Service string `json:"service"`
	Method  string `json:"method"`
}

type manifestSymbols struct {
	Go         manifestSymbol `json:"go"`
	Python     manifestSymbol `json:"python"`
	Rust       manifestSymbol `json:"rust"`
	TypeScript manifestSymbol `json:"typescript"`
}

type manifestFile struct {
	Languages       []string `json:"languages"`
	GRPCMethodCount int      `json:"grpcMethodCount"`
	RESTMethodCount int      `json:"restMethodCount"`
	Methods         []struct {
		Service         string          `json:"service"`
		Method          string          `json:"method"`
		StreamingKind   string          `json:"streamingKind"`
		RESTVerb        string          `json:"restVerb,omitempty"`
		RESTPath        string          `json:"restPath,omitempty"`
		RESTBody        string          `json:"restBody,omitempty"`
		RESTPathFields  []struct {
			Name     string `json:"name"`
			JSONName string `json:"jsonName"`
		} `json:"restPathFields,omitempty"`
		RESTQueryFields []struct {
			Name     string `json:"name"`
			JSONName string `json:"jsonName"`
		} `json:"restQueryFields,omitempty"`
		GRPCPath string          `json:"grpcPath"`
		Symbols  manifestSymbols `json:"symbols"`
	} `json:"methods"`
}

func loadManifestGolden(t *testing.T) manifestFile {
	t.Helper()
	root, err := pipeline.FindRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "sdk", "testdata", "public_surface", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest manifestFile
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func realSchema(t *testing.T) *model.Schema {
	t.Helper()
	bufTool := toolchain.Buf()
	if err := bufTool.Verify(); err != nil {
		t.Skipf("skipping: %v", err)
	}
	root, err := pipeline.FindRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := pipeline.BuildSchema(bufTool, root, t.TempDir())
	if err != nil {
		t.Fatalf("build schema: %v", err)
	}
	return schema
}

func fileSetText(set *fileset.FileSet) string {
	var b strings.Builder
	files := set.Files()
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	for _, f := range files {
		fmt.Fprintf(&b, "\n// %s\n", f.Path)
		b.Write(f.Content)
	}
	return b.String()
}

func serviceClientFile(output, service string) string {
	marker := fmt.Sprintf("// %s_client.", strings.ToLower(service))
	idx := strings.Index(output, marker)
	if idx < 0 {
		return output
	}
	return output[idx:]
}

func restSurfaceFile(output, lang, service string) string {
	clientName := service + "Client"
	switch lang {
	case "go":
		marker := fmt.Sprintf("type %sREST interface", clientName)
		idx := strings.Index(output, marker)
		if idx < 0 {
			return ""
		}
		end := strings.Index(output[idx:], "\n\n")
		if end < 0 {
			return output[idx:]
		}
		return output[idx : idx+end]
	case "python":
		marker := fmt.Sprintf("class %sREST(Protocol):", clientName)
		idx := strings.Index(output, marker)
		if idx < 0 {
			return ""
		}
		end := strings.Index(output[idx:], "\n\nclass ")
		if end < 0 {
			end = strings.Index(output[idx:], "\n\n")
		}
		if end < 0 {
			return output[idx:]
		}
		return output[idx : idx+end]
	case "rust":
		marker := fmt.Sprintf("impl<T: UnaryTransport> %s<T> {\n    pub async fn", clientName)
		idx := strings.Index(output, marker)
		if idx < 0 {
			return ""
		}
		// Stop at the next impl block (SyncUnaryTransport, GrpcCapable, etc.)
		// so gRPC-only methods are not included in the REST surface.
		rest := output[idx:]
		nextImpl := strings.Index(rest[1:], "\nimpl<")
		if nextImpl >= 0 {
			return rest[:1+nextImpl]
		}
		return rest
	case "ts":
		marker := fmt.Sprintf("export interface %sREST {", clientName)
		idx := strings.Index(output, marker)
		if idx < 0 {
			return ""
		}
		end := strings.Index(output[idx:], "\n}\n")
		if end < 0 {
			return output[idx:]
		}
		return output[idx : idx+end+2]
	default:
		return ""
	}
}

type langCase struct {
	name string
	emit func(*model.Schema) (*fileset.FileSet, error)
	meta func(grpcPath string) string
	call func(symbol manifestSymbol) string
	rest func(symbol manifestSymbol) string
	noRest func(symbol manifestSymbol) string
}

func manifestLangCases() []langCase {
	return []langCase{
		{
			name: "go",
			emit: golang.EmitPublic,
			meta: func(grpcPath string) string { return fmt.Sprintf("%q", grpcPath) },
			call: func(symbol manifestSymbol) string {
				return fmt.Sprintf("func (c *%s) %s(", symbol.Service, symbol.Method)
			},
			rest: func(symbol manifestSymbol) string {
				return symbol.Method + "("
			},
			noRest: func(symbol manifestSymbol) string {
				return fmt.Sprintf("func (c *%s) %s(", symbol.Service, symbol.Method)
			},
		},
		{
			name: "python",
			emit: python.EmitPublic,
			meta: func(grpcPath string) string { return fmt.Sprintf("full_method=%q", grpcPath) },
			call: func(symbol manifestSymbol) string {
				return fmt.Sprintf("def %s(", symbol.Method)
			},
			rest: func(symbol manifestSymbol) string {
				return "def " + symbol.Method + "("
			},
			noRest: func(symbol manifestSymbol) string {
				return fmt.Sprintf("def %s(", symbol.Method)
			},
		},
		{
			name: "rust",
			emit: rust.EmitPublic,
			meta: func(grpcPath string) string { return fmt.Sprintf("full_method: %q", grpcPath) },
			call: func(symbol manifestSymbol) string {
				return fmt.Sprintf("pub async fn %s(", symbol.Method)
			},
			rest: func(symbol manifestSymbol) string {
				return "pub async fn " + symbol.Method + "("
			},
			noRest: func(symbol manifestSymbol) string {
				return fmt.Sprintf("pub async fn %s(", symbol.Method)
			},
		},
		{
			name: "ts",
			emit: func(schema *model.Schema) (*fileset.FileSet, error) {
				return ts.EmitPublic(schema, ts.ServerPublicImports())
			},
			meta: func(grpcPath string) string { return fmt.Sprintf("grpcPath: %q", grpcPath) },
			call: func(symbol manifestSymbol) string {
				if symbol.Method == "invoke" {
					return "invoke<T"
				}
				return fmt.Sprintf("async %s(", symbol.Method)
			},
			rest: func(symbol manifestSymbol) string {
				if symbol.Method == "invoke" {
					return "invoke<T"
				}
				return symbol.Method + "("
			},
			noRest: func(symbol manifestSymbol) string {
				return fmt.Sprintf("async %s(", symbol.Method)
			},
		},
	}
}

func publicSnake(name string) string {
	switch name {
	case "InvokeGraphQL":
		return "invoke_graphql"
	default:
		var b strings.Builder
		for i, r := range name {
			if i > 0 && r >= 'A' && r <= 'Z' {
				b.WriteByte('_')
			}
			if r >= 'A' && r <= 'Z' {
				b.WriteByte(byte(r - 'A' + 'a'))
			} else {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func TestManifestGoldenShape(t *testing.T) {
	t.Parallel()
	manifest := loadManifestGolden(t)
	wantLangs := []string{"go", "python", "rust", "typescript"}
	if len(manifest.Languages) != len(wantLangs) {
		t.Fatalf("languages = %#v, want %#v", manifest.Languages, wantLangs)
	}
	for i, lang := range wantLangs {
		if manifest.Languages[i] != lang {
			t.Fatalf("languages[%d] = %q, want %q", i, manifest.Languages[i], lang)
		}
	}
	if manifest.GRPCMethodCount != len(manifest.Methods) {
		t.Fatalf("grpcMethodCount = %d, want %d", manifest.GRPCMethodCount, len(manifest.Methods))
	}
	restCount := 0
	for _, m := range manifest.Methods {
		if m.RESTVerb != "" {
			restCount++
			if m.RESTPath == "" {
				t.Fatalf("%s.%s missing restPath", m.Service, m.Method)
			}
		}
	}
	if manifest.RESTMethodCount != restCount {
		t.Fatalf("restMethodCount = %d, want %d", manifest.RESTMethodCount, restCount)
	}
}

func manifestSymbolForLang(symbols manifestSymbols, lang string) manifestSymbol {
	switch lang {
	case "go":
		return symbols.Go
	case "python":
		return symbols.Python
	case "rust":
		return symbols.Rust
	case "ts":
		return symbols.TypeScript
	default:
		return manifestSymbol{}
	}
}

func TestManifestMethodsEmittedPerLanguage(t *testing.T) {
	t.Parallel()

	manifest := loadManifestGolden(t)
	schema := realSchema(t)

	for _, tc := range manifestLangCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			set, err := tc.emit(schema)
			if err != nil {
				t.Fatalf("emit: %v", err)
			}
			output := fileSetText(set)
			for _, m := range manifest.Methods {
				needle := tc.meta(m.GRPCPath)
				if !strings.Contains(output, needle) {
					t.Fatalf("missing %s.%s metadata marker %q", m.Service, m.Method, needle)
				}
				symbol := manifestSymbolForLang(m.Symbols, tc.name)
				if symbol.Service == "" || symbol.Method == "" {
					t.Fatalf("missing manifest symbols for %s.%s (%s)", m.Service, m.Method, tc.name)
				}
				clientFile := serviceClientFile(output, m.Service)
				callNeedle := tc.call(symbol)
				if !strings.Contains(clientFile, callNeedle) {
					t.Fatalf("missing callable %s.%s marker %q", m.Service, m.Method, callNeedle)
				}

				restSurface := restSurfaceFile(output, tc.name, m.Service)
				if m.RESTVerb == "" {
					if restSurface != "" && strings.Contains(restSurface, tc.noRest(symbol)) {
						t.Fatalf("gRPC-only %s.%s must not appear on REST surface", m.Service, m.Method)
					}
					continue
				}
				if restSurface == "" {
					t.Fatalf("missing REST surface for %s", m.Service)
				}
				if !strings.Contains(restSurface, tc.rest(symbol)) {
					t.Fatalf("missing REST-projected %s.%s marker %q", m.Service, m.Method, tc.rest(symbol))
				}
			}
		})
	}
}

func TestRESTGatewayDerivedFromManifest(t *testing.T) {
	t.Parallel()
	manifest := loadManifestGolden(t)
	schema := realSchema(t)
	content, err := publicrpc.EmitRESTGateway(schema)
	if err != nil {
		t.Fatal(err)
	}
	restServices := map[string]struct{}{}
	for _, m := range manifest.Methods {
		if m.RESTVerb != "" {
			restServices[grpcServiceName(m.GRPCPath)] = struct{}{}
		}
	}
	for service := range restServices {
		if !strings.Contains(content, fmt.Sprintf("%q: {}", service)) {
			t.Fatalf("missing REST gateway service %q", service)
		}
	}
	for _, m := range manifest.Methods {
		if m.RESTVerb != "" {
			continue
		}
		service := grpcServiceName(m.GRPCPath)
		if _, ok := restServices[service]; ok {
			continue
		}
		if strings.Contains(content, fmt.Sprintf("%q: {}", service)) {
			t.Fatalf("gRPC-only service %q must not be in REST gateway map", service)
		}
	}
}

func grpcServiceName(grpcPath string) string {
	trimmed := strings.TrimPrefix(grpcPath, "/")
	if i := strings.Index(trimmed, "/"); i >= 0 {
		return trimmed[:i]
	}
	return trimmed
}

func TestLiveManifestMatchesGolden(t *testing.T) {
	t.Parallel()
	schema := realSchema(t)
	plan, err := publicsurface.PrepareEmit(schema)
	if err != nil {
		t.Fatal(err)
	}
	live := publicsurface.BuildManifest(plan.View, plan.Methods)
	golden := loadManifestGolden(t)
	if live.GRPCMethodCount != golden.GRPCMethodCount || live.RESTMethodCount != golden.RESTMethodCount {
		t.Fatalf("live counts grpc=%d rest=%d golden grpc=%d rest=%d",
			live.GRPCMethodCount, live.RESTMethodCount, golden.GRPCMethodCount, golden.RESTMethodCount)
	}
	if len(live.Methods) != len(golden.Methods) {
		t.Fatalf("live method count %d != golden %d", len(live.Methods), len(golden.Methods))
	}
}
