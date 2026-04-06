from __future__ import annotations

import shutil
import subprocess
import sys
import tempfile
from pathlib import Path


EXPECTED_GRPC_IMPORT = "from v1 import plugin_pb2 as v1_dot_plugin__pb2\n"
RELATIVE_GRPC_IMPORT = "from . import plugin_pb2 as v1_dot_plugin__pb2\n"


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
        pb2_path = generated_dir / "plugin_pb2.py"
        pb2_grpc_path = generated_dir / "plugin_pb2_grpc.py"

        pb2_source = pb2_path.read_text(encoding="utf-8")
        if "Protobuf Python Version: 7.34.1" not in pb2_source:
            raise RuntimeError(
                "buf generated plugin_pb2.py without the expected protobuf 7.34.1 runtime floor"
            )

        pb2_grpc_source = pb2_grpc_path.read_text(encoding="utf-8")
        if EXPECTED_GRPC_IMPORT not in pb2_grpc_source:
            raise RuntimeError("unexpected grpc Python import layout in generated stub")

        # Buf's grpc Python plugin emits a top-level import, but these stubs are
        # vendored under gestalt.gen.v1 and need a package-relative import.
        pb2_grpc_source = pb2_grpc_source.replace(
            EXPECTED_GRPC_IMPORT,
            RELATIVE_GRPC_IMPORT,
            1,
        )

        target_dir.mkdir(parents=True, exist_ok=True)
        (target_dir / "plugin_pb2.py").write_text(pb2_source, encoding="utf-8")
        (target_dir / "plugin_pb2_grpc.py").write_text(pb2_grpc_source, encoding="utf-8")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
