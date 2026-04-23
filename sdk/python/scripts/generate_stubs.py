import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

PROTO_MODULES = (
    "agent",
    "authentication",
    "cache",
    "datastore",
    "plugin",
    "runtime",
    "s3",
    "secrets",
    "workflow",
)
GRPC_RUNTIME_IMPORT_PREFIX = "from v1 import "
GRPC_RUNTIME_IMPORT_REPLACEMENT_PREFIX = "from . import "


def grpc_pb2_import_module(module_name: str) -> str:
    return module_name


def grpc_runtime_header(module_name: str) -> str:
    pb2_module = grpc_pb2_import_module(module_name)
    return f"""import grpc
import warnings

from google.protobuf import empty_pb2 as google_dot_protobuf_dot_empty__pb2
from . import {pb2_module}_pb2 as v1_dot_{pb2_module}__pb2

GRPC_GENERATED_VERSION = '1.80.0'
GRPC_VERSION = grpc.__version__
_version_not_supported = False

try:
    from grpc._utilities import first_version_is_lower
    _version_not_supported = first_version_is_lower(GRPC_VERSION, GRPC_GENERATED_VERSION)
except ImportError:
    _version_not_supported = True

if _version_not_supported:
    raise RuntimeError(
        f'The grpc package installed is at version {{GRPC_VERSION}},'
        + ' but the generated code in v1/{module_name}_pb2_grpc.py depends on'
        + f' grpcio>={{GRPC_GENERATED_VERSION}}.'
        + f' Please upgrade your grpc module to grpcio>={{GRPC_GENERATED_VERSION}}'
        + f' or downgrade your generated code using grpcio-tools<={{GRPC_VERSION}}.'
    )
"""


def main() -> int:
    repo_root = Path(__file__).resolve().parents[3]
    proto_dir = repo_root / "sdk/proto"
    template_path = proto_dir / "buf.python.gen.yaml"
    target_dir = repo_root / "sdk/python/gestalt/gen/v1"

    if shutil.which("buf") is None:
        print("buf is required to regenerate Python protobuf stubs", file=sys.stderr)
        return 1

    with tempfile.TemporaryDirectory(prefix="gestalt-python-stubs-") as work_dir:
        work_path = Path(work_dir)
        subprocess.run(
            [
                "buf",
                "generate",
                "--template",
                str(template_path),
                "--output",
                str(work_path),
            ],
            cwd=proto_dir,
            check=True,
        )

        generated_dir = work_path / "gen/v1"
        target_dir.mkdir(parents=True, exist_ok=True)
        for module_name in PROTO_MODULES:
            pb2_path = generated_dir / f"{module_name}_pb2.py"
            pb2_grpc_path = generated_dir / f"{module_name}_pb2_grpc.py"

            pb2_source = pb2_path.read_text(encoding="utf-8")
            if "Protobuf Python Version: 6.33.1" not in pb2_source:
                raise RuntimeError(
                    f"buf generated {module_name}_pb2.py without the expected protobuf 6.33.1 runtime floor"
                )

            pb2_grpc_source = pb2_grpc_path.read_text(encoding="utf-8")
            pb2_import_module = grpc_pb2_import_module(module_name)
            expected_import = (
                f"{GRPC_RUNTIME_IMPORT_PREFIX}{pb2_import_module}_pb2 as v1_dot_{pb2_import_module}__pb2\n"
            )
            if expected_import not in pb2_grpc_source:
                raise RuntimeError(
                    f"unexpected grpc Python import layout in generated {module_name} stub"
                )

            # Buf's grpc Python plugin emits a top-level import, but these stubs
            # are vendored under gestalt.gen.v1 and need package-relative imports.
            pb2_grpc_source = pb2_grpc_source.replace(
                expected_import,
                f"{GRPC_RUNTIME_IMPORT_REPLACEMENT_PREFIX}{pb2_import_module}_pb2 as v1_dot_{pb2_import_module}__pb2\n",
                1,
            )
            pb2_grpc_source = pb2_grpc_source.replace(
                "import grpc\n\n"
                "from google.protobuf import empty_pb2 as google_dot_protobuf_dot_empty__pb2\n"
                f"from . import {pb2_import_module}_pb2 as v1_dot_{pb2_import_module}__pb2\n",
                grpc_runtime_header(module_name),
                1,
            )

            (target_dir / f"{module_name}_pb2.py").write_text(pb2_source, encoding="utf-8")
            (target_dir / f"{module_name}_pb2_grpc.py").write_text(
                pb2_grpc_source,
                encoding="utf-8",
            )

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
