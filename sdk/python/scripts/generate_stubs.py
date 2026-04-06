from __future__ import annotations

import shutil
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path


EXPECTED_PROTOBUF_VERSION = "6.33.1"
EXPECTED_GRPC_VERSION = "1.80.0"
EXPECTED_GRPC_IMPORT = "from v1 import plugin_pb2 as v1_dot_plugin__pb2\n"
RELATIVE_GRPC_IMPORT = "from . import plugin_pb2 as v1_dot_plugin__pb2\n"
GENERATED_GRPC_HEADER = (
    "import grpc\n\n"
    "from google.protobuf import empty_pb2 as google_dot_protobuf_dot_empty__pb2\n"
    "from . import plugin_pb2 as v1_dot_plugin__pb2\n"
)
GRPC_RUNTIME_HEADER = f"""import grpc
import warnings

from google.protobuf import empty_pb2 as google_dot_protobuf_dot_empty__pb2
from . import plugin_pb2 as v1_dot_plugin__pb2

GRPC_GENERATED_VERSION = '{EXPECTED_GRPC_VERSION}'
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
        + ' but the generated code in v1/plugin_pb2_grpc.py depends on'
        + f' grpcio>={{GRPC_GENERATED_VERSION}}.'
        + f' Please upgrade your grpc module to grpcio>={{GRPC_GENERATED_VERSION}}'
        + f' or downgrade your generated code using grpcio-tools<={{GRPC_VERSION}}.'
    )
"""


@dataclass(frozen=True)
class StubGenerationPaths:
    proto_dir: Path
    target_dir: Path
    template_path: Path


@dataclass(frozen=True)
class GeneratedSources:
    pb2_source: str
    pb2_grpc_source: str


def main() -> int:
    paths = _stub_generation_paths(Path(__file__).resolve())

    if not _buf_is_available():
        print("buf is required to regenerate Python protobuf stubs", file=sys.stderr)
        return 1

    with tempfile.TemporaryDirectory(prefix="gestalt-python-stubs-") as work_dir:
        generated_sources = _generate_sources(
            output_dir=Path(work_dir),
            paths=paths,
        )
        _write_vendored_sources(
            generated_sources=generated_sources,
            target_dir=paths.target_dir,
        )

    return 0


def _stub_generation_paths(script_path: Path) -> StubGenerationPaths:
    repo_root = script_path.parents[3]
    proto_dir = repo_root / "sdk/proto"
    return StubGenerationPaths(
        proto_dir=proto_dir,
        target_dir=repo_root / "sdk/python/gestalt/gen/v1",
        template_path=proto_dir / "buf.python.gen.yaml",
    )


def _buf_is_available() -> bool:
    return shutil.which("buf") is not None


def _generate_sources(*, output_dir: Path, paths: StubGenerationPaths) -> GeneratedSources:
    _run_buf_generate(
        output_dir=output_dir,
        proto_dir=paths.proto_dir,
        template_path=paths.template_path,
    )
    generated_dir = output_dir / "gen/v1"
    pb2_source = _read_source(generated_dir / "plugin_pb2.py")
    _validate_pb2_source(pb2_source)
    pb2_grpc_source = _rewrite_pb2_grpc_source(
        _read_source(generated_dir / "plugin_pb2_grpc.py")
    )
    return GeneratedSources(
        pb2_source=pb2_source,
        pb2_grpc_source=pb2_grpc_source,
    )


def _run_buf_generate(*, output_dir: Path, proto_dir: Path, template_path: Path) -> None:
    subprocess.run(
        [
            "buf",
            "generate",
            "--template",
            str(template_path),
            "--output",
            str(output_dir),
        ],
        cwd=proto_dir,
        check=True,
    )


def _read_source(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def _validate_pb2_source(pb2_source: str) -> None:
    version_marker = f"Protobuf Python Version: {EXPECTED_PROTOBUF_VERSION}"
    if version_marker not in pb2_source:
        raise RuntimeError(
            "buf generated plugin_pb2.py without the expected "
            f"protobuf {EXPECTED_PROTOBUF_VERSION} runtime floor"
        )


def _rewrite_pb2_grpc_source(pb2_grpc_source: str) -> str:
    if EXPECTED_GRPC_IMPORT not in pb2_grpc_source:
        raise RuntimeError("unexpected grpc Python import layout in generated stub")

    # Buf's grpc Python plugin emits a top-level import, but these stubs are
    # vendored under gestalt.gen.v1 and need a package-relative import plus a
    # runtime version guard.
    rewritten_source = pb2_grpc_source.replace(
        EXPECTED_GRPC_IMPORT,
        RELATIVE_GRPC_IMPORT,
        1,
    )
    if GENERATED_GRPC_HEADER not in rewritten_source:
        raise RuntimeError("unexpected grpc Python header in generated stub")
    return rewritten_source.replace(
        GENERATED_GRPC_HEADER,
        GRPC_RUNTIME_HEADER,
        1,
    )


def _write_vendored_sources(*, generated_sources: GeneratedSources, target_dir: Path) -> None:
    target_dir.mkdir(parents=True, exist_ok=True)
    (target_dir / "plugin_pb2.py").write_text(
        generated_sources.pb2_source,
        encoding="utf-8",
    )
    (target_dir / "plugin_pb2_grpc.py").write_text(
        generated_sources.pb2_grpc_source,
        encoding="utf-8",
    )


if __name__ == "__main__":
    raise SystemExit(main())
