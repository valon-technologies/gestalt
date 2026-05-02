"""RuntimeLogHost SDK transport tests over a real Unix socket."""

from __future__ import annotations

import logging
import os
import tempfile
import unittest
from concurrent import futures
from datetime import datetime, timezone
from typing import Any

import grpc

from gestalt import (
    ENV_RUNTIME_LOG_HOST_SOCKET,
    ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN,
    ENV_RUNTIME_SESSION_ID,
    RuntimeLogHandler,
    RuntimeLogHost,
)
from gestalt._gen.v1 import pluginruntime_pb2 as _pluginruntime_pb2
from gestalt._gen.v1 import pluginruntime_pb2_grpc as _pluginruntime_pb2_grpc

pluginruntime_pb2: Any = _pluginruntime_pb2
pluginruntime_pb2_grpc: Any = _pluginruntime_pb2_grpc

_server: grpc.Server | None = None
_socket_path = ""
_requests: list[Any] = []
_relay_tokens: list[str] = []


class _RuntimeLogHostServicer(pluginruntime_pb2_grpc.PluginRuntimeLogHostServicer):
    def AppendLogs(self, request: Any, context: grpc.ServicerContext) -> Any:
        _relay_tokens.extend(
            value
            for key, value in context.invocation_metadata()
            if key == "x-gestalt-host-service-relay-token"
        )
        _requests.append(request)
        last_seq = request.logs[-1].source_seq if request.logs else 0
        return pluginruntime_pb2.AppendPluginRuntimeLogsResponse(last_seq=last_seq)


def setUpModule() -> None:
    global _server, _socket_path
    _socket_path = os.path.join(
        tempfile.gettempdir(), f"py-runtime-log-host-test-{os.getpid()}.sock"
    )
    if os.path.exists(_socket_path):
        os.remove(_socket_path)

    _server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
    pluginruntime_pb2_grpc.add_PluginRuntimeLogHostServicer_to_server(
        _RuntimeLogHostServicer(), _server
    )
    _server.add_insecure_port(f"unix:{_socket_path}")
    _server.start()

    os.environ[ENV_RUNTIME_LOG_HOST_SOCKET] = _socket_path
    os.environ[ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN] = "relay-token-py"
    os.environ[ENV_RUNTIME_SESSION_ID] = "runtime-session-1"


def tearDownModule() -> None:
    if _server is not None:
        _server.stop(None)
    if _socket_path and os.path.exists(_socket_path):
        os.remove(_socket_path)


class RuntimeLogHostTransportTests(unittest.TestCase):
    def setUp(self) -> None:
        _requests.clear()
        _relay_tokens.clear()

    def test_runtime_log_host_append_writer_and_logging_handler(self) -> None:
        observed_at = datetime(2026, 4, 30, 12, 0, tzinfo=timezone.utc)

        with RuntimeLogHost() as host:
            response = host.append(
                "runtime boot\n",
                stream="runtime",
                observed_at=observed_at,
                source_seq=7,
            )
            writer = host.writer(stream="stderr", source_seq_start=7)
            self.assertEqual(writer.write(b"stderr line\n"), len(b"stderr line\n"))

            handler = RuntimeLogHandler(host=host)
            handler.setFormatter(logging.Formatter("%(levelname)s:%(message)s"))
            logger = logging.getLogger("gestalt.runtime-log-host-test")
            logger.handlers = []
            logger.propagate = False
            logger.setLevel(logging.INFO)
            logger.addHandler(handler)
            try:
                logger.error("dispatch failed")
            finally:
                logger.removeHandler(handler)
                handler.close()

        self.assertEqual(response.last_seq, 7)
        self.assertEqual(_relay_tokens, ["relay-token-py"] * 3)
        self.assertEqual(
            [req.session_id for req in _requests], ["runtime-session-1"] * 3
        )
        self.assertEqual(_requests[0].logs[0].message, "runtime boot\n")
        self.assertEqual(
            _requests[0].logs[0].stream,
            pluginruntime_pb2.PLUGIN_RUNTIME_LOG_STREAM_RUNTIME,
        )
        self.assertEqual(
            _requests[0].logs[0].observed_at.ToDatetime(tzinfo=timezone.utc),
            observed_at,
        )
        self.assertEqual(_requests[0].logs[0].source_seq, 7)
        self.assertEqual(_requests[1].logs[0].message, "stderr line\n")
        self.assertEqual(
            _requests[1].logs[0].stream,
            pluginruntime_pb2.PLUGIN_RUNTIME_LOG_STREAM_STDERR,
        )
        self.assertEqual(_requests[1].logs[0].source_seq, 8)
        self.assertEqual(_requests[2].logs[0].message, "ERROR:dispatch failed\n")
        self.assertEqual(
            _requests[2].logs[0].stream,
            pluginruntime_pb2.PLUGIN_RUNTIME_LOG_STREAM_RUNTIME,
        )
