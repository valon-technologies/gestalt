from __future__ import annotations

import logging
import os
from datetime import datetime, timezone
from typing import Any, cast

import grpc
from google.protobuf import timestamp_pb2 as _timestamp_pb2

from ._gen.v1 import pluginruntime_pb2 as _pb
from ._gen.v1 import pluginruntime_pb2_grpc as _pb_grpc
from ._grpc_transport import host_service_channel

pb: Any = _pb
pb_grpc: Any = _pb_grpc
timestamp_pb2: Any = cast(Any, _timestamp_pb2)

ENV_RUNTIME_LOG_HOST_SOCKET = "GESTALT_RUNTIME_LOG_SOCKET"
ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN = f"{ENV_RUNTIME_LOG_HOST_SOCKET}_TOKEN"
ENV_RUNTIME_SESSION_ID = "GESTALT_RUNTIME_SESSION_ID"

_STREAMS = {
    "stdout": pb.PLUGIN_RUNTIME_LOG_STREAM_STDOUT,
    "stderr": pb.PLUGIN_RUNTIME_LOG_STREAM_STDERR,
    "runtime": pb.PLUGIN_RUNTIME_LOG_STREAM_RUNTIME,
}


class RuntimeLogHost:
    """Client for appending runtime logs to the host log stream.

    ``RuntimeLogHost`` reads ``GESTALT_RUNTIME_LOG_SOCKET`` and its optional
    relay token from the environment. Use it directly for structured log
    entries, or create a :class:`RuntimeLogWriter`/:class:`RuntimeLogHandler`
    when redirecting standard streams or Python logging output.
    """

    def __init__(self) -> None:
        target = os.environ.get(ENV_RUNTIME_LOG_HOST_SOCKET, "").strip()
        if not target:
            raise RuntimeError(
                f"runtime log host: {ENV_RUNTIME_LOG_HOST_SOCKET} is not set"
            )
        relay_token = os.environ.get(ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN, "").strip()
        self._channel = host_service_channel(
            "runtime log host",
            target,
            token=relay_token,
        )
        self._stub = pb_grpc.PluginRuntimeLogHostStub(self._channel)
        self._source_seq = 0

    def close(self) -> None:
        """Close the underlying gRPC channel."""

        self._channel.close()

    def append_logs(self, request: Any) -> Any:
        """Append logs using a raw protocol request message."""

        return _grpc_call(self._stub.AppendLogs, request)

    def append(
        self,
        session_id: str,
        message: str | bytes | None = None,
        *,
        stream: str | int = "runtime",
        observed_at: Any = None,
        source_seq: int | None = None,
    ) -> Any:
        """Append one log entry.

        When ``message`` is omitted, the first positional argument is treated as
        the message and the session id is read from ``GESTALT_RUNTIME_SESSION_ID``.
        """

        if message is None:
            message = session_id
            session_id = _runtime_session_id()
        else:
            session_id = _runtime_session_id(session_id)
        if source_seq is None:
            self._source_seq += 1
            source_seq = self._source_seq
        else:
            self._source_seq = max(self._source_seq, source_seq)
        entry = pb.PluginRuntimeLogEntry(
            stream=_stream_value(stream),
            message=_message_value(message),
            observed_at=_timestamp_value(observed_at),
            source_seq=source_seq,
        )
        return self.append_logs(
            pb.AppendPluginRuntimeLogsRequest(session_id=session_id, logs=[entry])
        )

    def writer(
        self,
        session_id: str | None = None,
        *,
        stream: str | int = "stdout",
        source_seq_start: int = 0,
    ) -> RuntimeLogWriter:
        """Return a file-like writer that appends data to a runtime log stream."""

        return RuntimeLogWriter(
            self,
            _runtime_session_id(session_id),
            stream=stream,
            source_seq_start=source_seq_start,
        )

    def __enter__(self) -> RuntimeLogHost:
        """Return the client for ``with`` statements."""

        return self

    def __exit__(self, *args: Any) -> None:
        """Close the client at the end of a context manager block."""

        self.close()


class RuntimeLogWriter:
    """File-like object that forwards writes to :class:`RuntimeLogHost`."""

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
        """Append ``data`` to the configured runtime log stream."""

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
        """Flush buffered data.

        Runtime log writes are sent eagerly, so this method is a no-op for
        file-like compatibility.
        """

        return None

    def close(self) -> None:
        """Mark the writer closed."""

        self.closed = True


class RuntimeLogHandler(logging.Handler):
    """Python logging handler that forwards records to runtime logs."""

    terminator = "\n"

    def __init__(
        self,
        session_id: str | None = None,
        *,
        host: RuntimeLogHost | None = None,
        stream: str | int = "runtime",
        level: int = logging.NOTSET,
    ) -> None:
        super().__init__(level=level)
        self._host = host or RuntimeLogHost()
        self._owns_host = host is None
        self._session_id = _runtime_session_id(session_id)
        self._stream = stream
        self._source_seq = 0

    def emit(self, record: logging.LogRecord) -> None:
        """Format and append one Python logging record."""

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
        """Close the owned runtime-log host, if this handler created it."""

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


def _runtime_session_id(session_id: str | None = None) -> str:
    value = (
        session_id
        if session_id is not None
        else os.environ.get(ENV_RUNTIME_SESSION_ID, "")
    ).strip()
    if not value:
        raise RuntimeError(f"runtime session: {ENV_RUNTIME_SESSION_ID} is not set")
    return value


def _message_value(message: str | bytes) -> str:
    if isinstance(message, bytes):
        return message.decode("utf-8", errors="replace")
    return message


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
