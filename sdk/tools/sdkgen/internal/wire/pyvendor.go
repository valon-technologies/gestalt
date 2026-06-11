package wire

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

// This file is a faithful port of sdk/python/scripts/generate_stubs.py: the
// Python wire stubs are generated into a scratch directory and vendored under
// gestalt/_gen with package-relative imports and a grpc version guard. The
// byte-compare in check mode against the checked-in stubs holds the port and
// the script to identical output. Keep the two in sync until the script is
// retired in favor of sdkgen.

// pyVendorRel is the vendored stub root, relative to the repo root.
const pyVendorRel = "sdk/python/gestalt/_gen"

const (
	pyTemplate         = "buf.python.gen.yaml"
	pyProtobufFloor    = "Protobuf Python Version: 6.33.1"
	pyImportPrefix     = "from v1 import "
	pyImportReplace    = "from . import "
	pyGRPCVersionFloor = "1.80.0"
)

var pyProtoModules = []string{
	"agent",
	"authentication",
	"authorization",
	"cache",
	"indexeddb",
	"app",
	"runtime_provider",
	"runtime",
	"s3",
	"secrets",
	"test",
	"workflow",
}

var pyTopLevelImportMarkers = []string{"from v1 import ", "import v1."}

// renderPython generates and vendors the Python wire stubs into outDir.
func renderPython(bufTool *toolchain.Tool, protoDir, outDir string) error {
	stubs, err := pythonVendoredStubs(bufTool, protoDir)
	if err != nil {
		return err
	}
	for rel, content := range stubs {
		abs := filepath.Join(outDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(abs, content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// pythonVendoredStubs generates the Python wire stubs and applies the
// vendoring transform, returning content keyed by path relative to
// sdk/python/gestalt/_gen.
func pythonVendoredStubs(bufTool *toolchain.Tool, protoDir string) (map[string][]byte, error) {
	work, err := os.MkdirTemp("", "sdkgen-python-stubs-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(work) }()

	if err := bufTool.Run(protoDir, "generate", "--template", pyTemplate, "--output", work); err != nil {
		return nil, err
	}
	genDir := filepath.Join(work, "gen", "v1")

	out := map[string][]byte{}
	for _, module := range pyProtoModules {
		pb2, err := os.ReadFile(filepath.Join(genDir, module+"_pb2.py"))
		if err != nil {
			return nil, err
		}
		pb2Source := string(pb2)
		if !strings.Contains(pb2Source, pyProtobufFloor) {
			return nil, fmt.Errorf("buf generated %s_pb2.py without the expected protobuf runtime floor (%s)", module, pyProtobufFloor)
		}
		pb2Source = strings.ReplaceAll(pb2Source, pyImportPrefix, pyImportReplace)
		if err := assertNoTopLevelV1Imports(module+"_pb2.py", pb2Source); err != nil {
			return nil, err
		}
		out["v1/"+module+"_pb2.py"] = []byte(pb2Source)

		pyi, err := os.ReadFile(filepath.Join(genDir, module+"_pb2.pyi"))
		if err != nil {
			return nil, fmt.Errorf("buf did not generate %s_pb2.pyi: %w", module, err)
		}
		pyiSource := strings.ReplaceAll(string(pyi), pyImportPrefix, pyImportReplace)
		if err := assertNoTopLevelV1Imports(module+"_pb2.pyi", pyiSource); err != nil {
			return nil, err
		}
		out["v1/"+module+"_pb2.pyi"] = []byte(pyiSource)

		grpcStub, err := os.ReadFile(filepath.Join(genDir, module+"_pb2_grpc.py"))
		if err != nil {
			return nil, err
		}
		grpcSource, err := vendorGRPCStub(module, string(grpcStub))
		if err != nil {
			return nil, err
		}
		out["v1/"+module+"_pb2_grpc.py"] = []byte(grpcSource)
	}
	return out, nil
}

func assertNoTopLevelV1Imports(name, source string) error {
	for _, marker := range pyTopLevelImportMarkers {
		if strings.Contains(source, marker) {
			return fmt.Errorf("generated stub %s still imports top-level v1", name)
		}
	}
	return nil
}

func vendorGRPCStub(module, source string) (string, error) {
	alias := strings.ReplaceAll(module, "_", "__")
	expectedImport := pyImportPrefix + module + "_pb2 as v1_dot_" + alias + "__pb2\n"
	if !strings.Contains(source, expectedImport) {
		return "", fmt.Errorf("unexpected grpc Python import layout in generated %s stub", module)
	}
	relativeImport := pyImportReplace + module + "_pb2 as v1_dot_" + alias + "__pb2\n"
	source = strings.Replace(source, expectedImport, relativeImport, 1)
	source = strings.Replace(source,
		"import grpc\n\n"+
			"from google.protobuf import empty_pb2 as google_dot_protobuf_dot_empty__pb2\n"+
			relativeImport,
		grpcRuntimeHeader(module, true),
		1,
	)
	source = strings.Replace(source,
		"import grpc\n\n"+relativeImport,
		grpcRuntimeHeader(module, false),
		1,
	)
	return source, nil
}

func grpcRuntimeHeader(module string, includeEmptyImport bool) string {
	emptyImport := ""
	if includeEmptyImport {
		emptyImport = "from google.protobuf import empty_pb2 as google_dot_protobuf_dot_empty__pb2\n"
	}
	alias := strings.ReplaceAll(module, "_", "__")
	return "import grpc\n" +
		"import warnings\n" +
		"\n" +
		emptyImport +
		"from . import " + module + "_pb2 as v1_dot_" + alias + "__pb2\n" +
		"\n" +
		"GRPC_GENERATED_VERSION = '" + pyGRPCVersionFloor + "'\n" +
		"GRPC_VERSION = grpc.__version__\n" +
		"_version_not_supported = False\n" +
		"\n" +
		"try:\n" +
		"    from grpc._utilities import first_version_is_lower\n" +
		"    _version_not_supported = first_version_is_lower(GRPC_VERSION, GRPC_GENERATED_VERSION)\n" +
		"except ImportError:\n" +
		"    _version_not_supported = True\n" +
		"\n" +
		"if _version_not_supported:\n" +
		"    raise RuntimeError(\n" +
		"        f'The grpc package installed is at version {GRPC_VERSION},'\n" +
		"        + ' but the generated code in v1/" + module + "_pb2_grpc.py depends on'\n" +
		"        + f' grpcio>={GRPC_GENERATED_VERSION}.'\n" +
		"        + f' Please upgrade your grpc module to grpcio>={GRPC_GENERATED_VERSION}'\n" +
		"        + f' or downgrade your generated code using grpcio-tools<={GRPC_VERSION}.'\n" +
		"    )\n"
}

// isPythonStub scopes sync and stale detection to vendored stub files; the
// package __init__.py and caches in gestalt/_gen are out of scope.
func isPythonStub(rel string) bool {
	return strings.HasSuffix(rel, "_pb2.py") ||
		strings.HasSuffix(rel, "_pb2.pyi") ||
		strings.HasSuffix(rel, "_pb2_grpc.py")
}
