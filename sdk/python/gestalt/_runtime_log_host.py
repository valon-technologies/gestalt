from __future__ import annotations

import logging
import os
from datetime import datetime, timezone
from typing import Any, cast

import grpc
from google.protobuf import timestamp_pb2 as _timestamp_pb2

from ._grpc_transport import host_service_channel
from .gen.v1 import pluginruntime_pb2 as _pb
from .gen.v1 import pluginruntime_pb2_grpc as _pb_grpc

pb: Any = _pb
pb_grpc: Any = _pb_grpc
timestamp_pb2: Any = cast(Any, _timestamp_pb2)

ENV_RUNTIME_LOG_HOST_SOCKET = "GESTALT_RUNTIME_LOG_SOCKET"
ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN = f"{ENV_RUNTIME_LOG_HOST_SOCKET}_TOKEN"

_STREAMS = {
    "stdout": pb.PLUGIN_RUNTIME_LOG_STREAM_STDOUT,
    "stderr": pb.PLUGIN_RUNTIME_LOG_STREAM_STDERR,
    "runtime": pb.PLUGIN_RUNTIME_LOG_STREAM_RUNTIME,
}


class RuntimeLogHost:
    def __init__(self) -> None:
        target = os.environ.get(ENV_RUNTIME_LOG_HOST_SOCKET, "")
        if not target:
            raise RuntimeError(
                f"runtime log host: {ENV_RUNTIME_LOG_HOST_SOCKET} is not set"
            )
        relay_token = os.environ.get(ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN, "")
        self._channel = host_service_channel(
            "runtime log host",
            target,
            token=relay_token,
        )
        self._stub = pb_grpc.PluginRuntimeLogHostStub(self._channel)

    def close(self) -> None:
        self._channel.close()

    def append_logs(self, request: Any) -> Any:
        return _grpc_call(self._stub.AppendLogs, request)

    def append(
        self,
        session_id: str,
        message: str,
        *,
        stream: str | int = "runtime",
        observed_at: Any = None,
        source_seq: int = 0,
    ) -> Any:
        entry = pb.PluginRuntimeLogEntry(
            stream=_stream_value(stream),
            message=message,
            observed_at=_timestamp_value(observed_at),
            source_seq=source_seq,
        )
        return self.append_logs(
            pb.AppendPluginRuntimeLogsRequest(session_id=session_id, logs=[entry])
        )

    def writer(
        self,
        session_id: str,
        *,
        stream: str | int = "stdout",
        source_seq_start: int = 0,
    ) -> RuntimeLogWriter:
        return RuntimeLogWriter(
            self,
            session_id,
            stream=stream,
            source_seq_start=source_seq_start,
        )

    def __enter__(self) -> RuntimeLogHost:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


class RuntimeLogWriter:
    def __init__(
        self,
        host: RuntimeLogHost,
        session_id: str,
        *,
        stream: str | int = "stdout",
        source_seq_start: int = 0,
    ) -> None:
        self._host = host
        self._session_id = session_id
        self._stream = stream
        self._source_seq = source_seq_start
        self.closed = False

    def write(self, data: str | bytes) -> int:
        if self.closed:
            raise ValueError("I/O operation on closed runtime log writer")
        if isinstance(data, bytes):
            message = data.decode("utf-8", errors="replace")
            written = len(data)
        else:
            message = data
            written = len(data)
        if not message:
            return written
        self._source_seq += 1
        self._host.append(
            self._session_id,
            message,
            stream=self._stream,
            source_seq=self._source_seq,
        )
        return written

    def flush(self) -> None:
        return None

    def close(self) -> None:
        self.closed = True


class RuntimeLogHandler(logging.Handler):
    terminator = "\n"

    def __init__(
        self,
        session_id: str,
        *,
        host: RuntimeLogHost | None = None,
        stream: str | int = "runtime",
        level: int = logging.NOTSET,
    ) -> None:
        super().__init__(level=level)
        self._host = host or RuntimeLogHost()
        self._owns_host = host is None
        self._session_id = session_id
        self._stream = stream
        self._source_seq = 0

    def emit(self, record: logging.LogRecord) -> None:
        try:
            message = self.format(record) + self.terminator
            self._source_seq += 1
            self._host.append(
                self._session_id,
                message,
                stream=self._stream,
                source_seq=self._source_seq,
            )
        except Exception:
            self.handleError(record)

    def close(self) -> None:
        try:
            if self._owns_host:
                self._host.close()
        finally:
            super().close()


def _stream_value(stream: str | int) -> int:
    if isinstance(stream, int):
        return stream
    normalized = stream.strip().lower()
    value = _STREAMS.get(normalized)
    if value is None:
        raise ValueError(f"unsupported runtime log stream {stream!r}")
    return value


def _timestamp_value(observed_at: Any) -> Any:
    if isinstance(observed_at, timestamp_pb2.Timestamp):
        return observed_at
    value = observed_at
    if value is None:
        value = datetime.now(timezone.utc)
    elif value.tzinfo is None:
        value = value.replace(tzinfo=timezone.utc)
    timestamp = timestamp_pb2.Timestamp()
    timestamp.FromDatetime(value)
    return timestamp


def _grpc_call(method: Any, request: Any) -> Any:
    try:
        return method(request)
    except grpc.RpcError:
        raise
