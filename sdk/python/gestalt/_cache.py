"""Cache client helpers for provider processes."""

from __future__ import annotations

import datetime as _dt
import os
from dataclasses import dataclass
from typing import Any, Iterable, Protocol

import grpc
from google.protobuf import duration_pb2 as _duration_pb2

from ._gen.v1 import cache_pb2 as _pb
from ._gen.v1 import cache_pb2_grpc as _pb_grpc
from ._grpc_transport import (
    ENV_HOST_SERVICE_SOCKET,
    ENV_HOST_SERVICE_TOKEN,
    host_service_channel,
)

pb: Any = _pb
pb_grpc: Any = _pb_grpc
duration_pb2: Any = _duration_pb2


@dataclass(frozen=True)
class CacheEntry:
    """Cache key and value pair used by batch operations."""

    key: str
    value: bytes


class CacheProtocol(Protocol):
    """Fakeable cache client contract shared by host clients and tests."""

    def get(self, key: str) -> bytes | None:
        """Return the cached value for ``key`` if it exists."""

    def get_many(self, keys: list[str]) -> dict[str, bytes]:
        """Return the subset of ``keys`` that currently exist."""

    def set(
        self,
        key: str,
        value: bytes,
        ttl: _dt.timedelta | None = None,
    ) -> None:
        """Store ``value`` for ``key`` with an optional TTL."""

    def set_many(
        self,
        entries: Iterable[CacheEntry],
        ttl: _dt.timedelta | None = None,
    ) -> None:
        """Store multiple cache entries with one RPC."""

    def delete(self, key: str) -> bool:
        """Delete ``key`` and return whether an entry existed."""

    def delete_many(self, keys: list[str]) -> int:
        """Delete multiple keys and return the number removed."""

    def touch(self, key: str, ttl: _dt.timedelta) -> bool:
        """Refresh the TTL for ``key`` if the entry exists."""


class Cache:
    """Client for a host-provided Gestalt cache provider."""

    def __init__(self, name: str | None = None) -> None:
        target = os.environ.get(ENV_HOST_SERVICE_SOCKET, "")
        if not target:
            raise RuntimeError(f"{ENV_HOST_SERVICE_SOCKET} is not set")
        token = os.environ.get(ENV_HOST_SERVICE_TOKEN, "")
        self._channel = host_service_channel(
            "cache", target, token=token.strip(), binding=(name or "").strip()
        )
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


def _grpc_call(method: Any, request: Any) -> Any:
    try:
        return method(request)
    except grpc.RpcError:
        raise
