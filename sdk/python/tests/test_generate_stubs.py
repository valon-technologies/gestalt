from __future__ import annotations

import importlib.util
import sys
import unittest
from pathlib import Path


def _load_generate_stubs_module():
    script_path = Path(__file__).resolve().parents[1] / "scripts" / "generate_stubs.py"
    spec = importlib.util.spec_from_file_location("generate_stubs", script_path)
    if spec is None or spec.loader is None:
        raise RuntimeError("could not load generate_stubs.py for testing")

    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


generate_stubs = _load_generate_stubs_module()


class GenerateStubsTests(unittest.TestCase):
    def test_validate_pb2_source_rejects_unexpected_version(self) -> None:
        expected_message = (
            f"expected protobuf {generate_stubs.EXPECTED_PROTOBUF_VERSION} runtime floor"
        )

        with self.assertRaisesRegex(RuntimeError, expected_message):
            generate_stubs._validate_pb2_source("# Protobuf Python Version: 7.34.1\n")

    def test_rewrite_pb2_grpc_source_rewrites_import_and_injects_runtime_header(self) -> None:
        source = (
            "import grpc\n\n"
            "from google.protobuf import empty_pb2 as google_dot_protobuf_dot_empty__pb2\n"
            "from v1 import plugin_pb2 as v1_dot_plugin__pb2\n\n"
            "class ProviderPluginStub:\n"
            "    pass\n"
        )

        rewritten = generate_stubs._rewrite_pb2_grpc_source(source)

        self.assertIn(generate_stubs.RELATIVE_GRPC_IMPORT, rewritten)
        self.assertNotIn(generate_stubs.EXPECTED_GRPC_IMPORT, rewritten)
        self.assertIn(
            f"GRPC_GENERATED_VERSION = '{generate_stubs.EXPECTED_GRPC_VERSION}'",
            rewritten,
        )
        self.assertIn("class ProviderPluginStub:", rewritten)

    def test_rewrite_pb2_grpc_source_rejects_unexpected_header(self) -> None:
        source = "from v1 import plugin_pb2 as v1_dot_plugin__pb2\n"

        with self.assertRaisesRegex(RuntimeError, "unexpected grpc Python header"):
            generate_stubs._rewrite_pb2_grpc_source(source)


if __name__ == "__main__":
    unittest.main()
