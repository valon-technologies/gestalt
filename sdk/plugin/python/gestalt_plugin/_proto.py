"""Import helpers for the generated protobuf/gRPC stubs.

The generated code uses ``from v1 import plugin_pb2`` which requires the
``gen/`` directory to be on ``sys.path``. We handle that once here so that
every other module can simply ``from gestalt_plugin._proto import pb2, pb2_grpc``.
"""
from __future__ import annotations

import os
import sys

_gen_dir = os.path.join(os.path.dirname(__file__), os.pardir, "gen")
_gen_dir = os.path.normpath(_gen_dir)
if _gen_dir not in sys.path:
    sys.path.insert(0, _gen_dir)

from v1 import plugin_pb2 as pb2  # noqa: E402
from v1 import plugin_pb2_grpc as pb2_grpc  # noqa: E402

__all__ = ["pb2", "pb2_grpc"]
