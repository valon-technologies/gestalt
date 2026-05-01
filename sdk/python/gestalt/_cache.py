"""Cache client helpers for provider processes."""

from __future__ import annotations

import datetime as _dt
import os
from dataclasses import dataclass
from typing import Any, Iterable
from urllib import parse as _urlparse

import grpc
from google.protobuf import duration_pb2 as _duration_pb2

from ._gen.v1 import cache_pb2 as _pb
from ._gen.v1 import cache_pb2_grpc as _pb_grpc
from ._grpc_transport import (
    insecure_internal_channel,
    internal_channel_target,
    secure_internal_channel,
)

pb: Any = _pb
pb_grpc: Any = _pb_grpc
duration_pb2: Any = _duration_pb2

ENV_CACHE_SOCKET = "GESTALT_CACHE_SOCKET"
_CACHE_SOCKET_TOKEN_SUFFIX = "_TOKEN"
_CACHE_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token"
ENV_CACHE_SOCKET_TOKEN = f"{ENV_CACHE_SOCKET}{_CACHE_SOCKET_TOKEN_SUFFIX}"


def cache_socket_env(name: str | None = None) -> str:
    """Return the environment variable name for a cache socket binding."""

    trimmed = (name or "").strip()
    if not trimmed:
        return ENV_CACHE_SOCKET
    normalized = "".join(
        ch.upper() if ch.isascii() and ch.isalnum() else "_" for ch in trimmed
    )
    return f"{ENV_CACHE_SOCKET}_{normalized}"


def cache_socket_token_env(name: str | None = None) -> str:
    """Return the environment variable name for a cache relay token."""

    return f"{cache_socket_env(name)}{_CACHE_SOCKET_TOKEN_SUFFIX}"


@dataclass(frozen=True)
class CacheEntry:
    """Cache key and value pair used by batch operations."""

    key: str
    value: bytes


class Cache:
    """Client for a host-provided Gestalt cache provider."""

    def __init__(self, name: str | None = None) -> None:
        env_name = cache_socket_env(name)
        target = os.environ.get(env_name, "")
        if not target:
            raise RuntimeError(f"{env_name} is not set")
        token = os.environ.get(cache_socket_token_env(name), "")
        self._channel = _cache_channel(target, token=token)
        self._stub = pb_grpc.CacheStub(self._channel)

    def close(self) -> None:
        """Close the underlying gRPC channel."""

        self._channel.close()

    def get(self, key: str) -> bytes | None:
        """Return the cached value for ``key`` if it exists."""

        resp = _grpc_call(self._stub.Get, pb.CacheGetRequest(key=key))
        if not resp.found:
            return None
        return bytes(resp.value)

    def get_many(self, keys: list[str]) -> dict[str, bytes]:
        """Return the subset of ``keys`` that currently exist."""

        resp = _grpc_call(self._stub.GetMany, pb.CacheGetManyRequest(keys=keys))
        out: dict[str, bytes] = {}
        for entry in resp.entries:
            if entry.found:
                out[entry.key] = bytes(entry.value)
        return out

    def set(
        self,
        key: str,
        value: bytes,
        ttl: _dt.timedelta | None = None,
    ) -> None:
        """Store ``value`` for ``key`` with an optional TTL."""

        _grpc_call(
            self._stub.Set,
            pb.CacheSetRequest(key=key, value=bytes(value), ttl=_duration_from_ttl(ttl)),
        )

    def set_many(
        self,
        entries: Iterable[CacheEntry],
        ttl: _dt.timedelta | None = None,
    ) -> None:
        """Store multiple cache entries with one RPC."""

        _grpc_call(
            self._stub.SetMany,
            pb.CacheSetManyRequest(
                entries=[
                    pb.CacheSetEntry(key=entry.key, value=bytes(entry.value))
                    for entry in entries
                ],
                ttl=_duration_from_ttl(ttl),
            ),
        )

    def delete(self, key: str) -> bool:
        """Delete ``key`` and return whether an entry existed."""

        resp = _grpc_call(self._stub.Delete, pb.CacheDeleteRequest(key=key))
        return bool(resp.deleted)

    def delete_many(self, keys: list[str]) -> int:
        """Delete multiple keys and return the number removed."""

        resp = _grpc_call(self._stub.DeleteMany, pb.CacheDeleteManyRequest(keys=keys))
        return int(resp.deleted)

    def touch(self, key: str, ttl: _dt.timedelta) -> bool:
        """Refresh the TTL for ``key`` if the entry exists."""

        resp = _grpc_call(
            self._stub.Touch,
            pb.CacheTouchRequest(key=key, ttl=_duration_from_ttl(ttl)),
        )
        return bool(resp.touched)

    def __enter__(self) -> Cache:
        """Return the cache client for ``with`` statements."""

        return self

    def __exit__(self, *args: Any) -> None:
        """Close the cache client at the end of a context manager block."""

        self.close()


def _duration_from_ttl(ttl: _dt.timedelta | None) -> Any:
    if ttl is None:
        return None
    if ttl.total_seconds() <= 0:
        return None
    duration = duration_pb2.Duration()
    duration.FromTimedelta(ttl)
    return duration


class _RelayTokenInterceptor(grpc.UnaryUnaryClientInterceptor):
    def __init__(self, token: str) -> None:
        self._token = token

    def intercept_unary_unary(self, continuation: Any, client_call_details: Any, request: Any) -> Any:
        metadata = list(client_call_details.metadata or [])
        metadata.append((_CACHE_RELAY_TOKEN_HEADER, self._token))
        details = _ClientCallDetails(
            method=client_call_details.method,
            timeout=client_call_details.timeout,
            metadata=metadata,
            credentials=client_call_details.credentials,
            wait_for_ready=client_call_details.wait_for_ready,
            compression=client_call_details.compression,
        )
        return continuation(details, request)


class _ClientCallDetails(grpc.ClientCallDetails):
    def __init__(
        self,
        *,
        method: str,
        timeout: float | None,
        metadata: list[tuple[str, str]],
        credentials: Any,
        wait_for_ready: bool | None,
        compression: Any,
    ) -> None:
        self.method = method
        self.timeout = timeout
        self.metadata = metadata
        self.credentials = credentials
        self.wait_for_ready = wait_for_ready
        self.compression = compression


def _cache_channel(target: str, *, token: str = "") -> Any:
    scheme, address = _parse_cache_target(target)
    if scheme == "unix":
        channel = insecure_internal_channel(internal_channel_target("unix", address))
    elif scheme == "tcp":
        channel = insecure_internal_channel(internal_channel_target("tcp", address))
    elif scheme == "tls":
        channel = secure_internal_channel(internal_channel_target("tls", address))
    else:
        raise RuntimeError(f"unsupported cache transport scheme {scheme!r}")
    if token:
        channel = grpc.intercept_channel(channel, _RelayTokenInterceptor(token))
    return channel


def _parse_cache_target(raw: str) -> tuple[str, str]:
    target = raw.strip()
    if not target:
        raise RuntimeError("cache transport target is required")
    if target.startswith("tcp://"):
        address = target.removeprefix("tcp://").strip()
        if not address:
            raise RuntimeError(f"cache tcp target {raw!r} is missing host:port")
        return "tcp", address
    if target.startswith("tls://"):
        address = target.removeprefix("tls://").strip()
        if not address:
            raise RuntimeError(f"cache tls target {raw!r} is missing host:port")
        return "tls", address
    if target.startswith("unix://"):
        address = target.removeprefix("unix://").strip()
        if not address:
            raise RuntimeError(f"cache unix target {raw!r} is missing a socket path")
        return "unix", address
    if "://" in target:
        parsed = _urlparse.urlparse(target)
        raise RuntimeError(f"unsupported cache target scheme {parsed.scheme!r}")
    return "unix", target


def _grpc_call(method: Any, request: Any) -> Any:
    try:
        return method(request)
    except grpc.RpcError:
        raise
