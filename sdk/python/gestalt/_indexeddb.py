"""IndexedDB-style client helpers for provider processes."""

from __future__ import annotations

import datetime as _dt
import os
import queue
from dataclasses import dataclass, field
from typing import Any, Iterator, Protocol, cast
from urllib import parse as _urlparse

import grpc as _grpc
from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2

from ._gen.v1 import datastore_pb2 as _pb
from ._gen.v1 import datastore_pb2_grpc as _pb_grpc
from ._grpc_transport import (
    insecure_internal_channel,
    internal_channel_target,
    secure_internal_channel,
)

grpc: Any = cast(Any, _grpc)
pb: Any = cast(Any, _pb)
pb_grpc: Any = cast(Any, _pb_grpc)
struct_pb2: Any = cast(Any, _struct_pb2)
timestamp_pb2: Any = cast(Any, _timestamp_pb2)

ENV_INDEXEDDB_SOCKET = "GESTALT_INDEXEDDB_SOCKET"
_INDEXEDDB_SOCKET_TOKEN_SUFFIX = "_TOKEN"
_INDEXEDDB_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token"

#: Iterate in ascending key order.
CURSOR_NEXT = 0
#: Iterate in ascending key order while collapsing duplicate index keys.
CURSOR_NEXT_UNIQUE = 1
#: Iterate in descending key order.
CURSOR_PREV = 2
#: Iterate in descending key order while collapsing duplicate index keys.
CURSOR_PREV_UNIQUE = 3


def indexeddb_socket_env(name: str | None = None) -> str:
    """Return the environment variable name for an IndexedDB socket binding."""

    trimmed = (name or "").strip()
    if not trimmed:
        return ENV_INDEXEDDB_SOCKET
    normalized = "".join(ch.upper() if ch.isalnum() else "_" for ch in trimmed)
    return f"{ENV_INDEXEDDB_SOCKET}_{normalized}"


def indexeddb_socket_token_env(name: str | None = None) -> str:
    """Return the environment variable name for an IndexedDB relay token."""

    return f"{indexeddb_socket_env(name)}{_INDEXEDDB_SOCKET_TOKEN_SUFFIX}"


class NotFoundError(Exception):
    """Raised when an IndexedDB record, store, or cursor target is missing."""

    pass


class AlreadyExistsError(Exception):
    """Raised when an IndexedDB object already exists."""

    pass


class TransactionError(Exception):
    """Raised when a transaction has failed or already finished."""

    pass


@dataclass
class KeyRange:
    """Lower and upper bounds for a cursor or range query."""

    lower: Any = None
    upper: Any = None
    lower_open: bool = False
    upper_open: bool = False


@dataclass
class IndexSchema:
    """Definition for an index within an object store."""

    name: str
    key_path: list[str] = field(default_factory=list)
    unique: bool = False


@dataclass
class ObjectStoreSchema:
    """Schema definition for an object store."""

    indexes: list[IndexSchema] = field(default_factory=list)


class IndexedDB:
    """Client for a host-provided IndexedDB-compatible store."""

    def __init__(self, name: str | None = None) -> None:
        env_name = indexeddb_socket_env(name)
        target = os.environ.get(env_name, "")
        if not target:
            raise RuntimeError(f"{env_name} is not set")
        token = os.environ.get(indexeddb_socket_token_env(name), "")
        self._channel = _indexeddb_channel(target, token=token)
        self._stub = pb_grpc.IndexedDBStub(self._channel)

    def close(self) -> None:
        """Close the underlying gRPC channel."""

        self._channel.close()

    def create_object_store(
        self, name: str, schema: ObjectStoreSchema | None = None
    ) -> None:
        """Create an object store with an optional schema."""

        pb_schema = pb.ObjectStoreSchema()
        if schema:
            for idx in schema.indexes:
                pb_schema.indexes.append(
                    pb.IndexSchema(
                        name=idx.name, key_path=idx.key_path, unique=idx.unique
                    )
                )
        _grpc_call(
            self._stub.CreateObjectStore,
            pb.CreateObjectStoreRequest(name=name, schema=pb_schema),
        )

    def delete_object_store(self, name: str) -> None:
        """Delete an object store by name."""

        _grpc_call(self._stub.DeleteObjectStore, pb.DeleteObjectStoreRequest(name=name))

    def object_store(self, name: str) -> ObjectStore:
        """Return a client bound to an object store."""

        return ObjectStore(self._stub, name)

    def transaction(
        self,
        stores: list[str],
        mode: str = "readonly",
        *,
        durability_hint: str = "default",
    ) -> Transaction:
        """Start an explicit IndexedDB transaction."""

        return Transaction(self._stub, stores, mode, durability_hint=durability_hint)

    def __enter__(self) -> IndexedDB:
        """Return the client for ``with`` statements."""

        return self

    def __exit__(self, *args: Any) -> None:
        """Close the client at the end of a context manager block."""

        self.close()


class ObjectStore:
    """Client bound to a single IndexedDB object store."""

    def __init__(self, stub: Any, store: str) -> None:
        self._stub = stub
        self._store = store

    def get(self, id: str) -> dict[str, Any]:
        """Fetch a record by primary key."""

        resp = _grpc_call(
            self._stub.Get, pb.ObjectStoreRequest(store=self._store, id=id)
        )
        return _record_to_dict(resp.record)

    def get_key(self, id: str) -> str:
        """Return the canonical key for a primary key lookup."""

        resp = _grpc_call(
            self._stub.GetKey, pb.ObjectStoreRequest(store=self._store, id=id)
        )
        return resp.key

    def add(self, record: dict[str, Any]) -> None:
        """Insert a new record."""

        _grpc_call(
            self._stub.Add,
            pb.RecordRequest(store=self._store, record=_dict_to_record(record)),
        )

    def put(self, record: dict[str, Any]) -> None:
        """Insert or replace a record."""

        _grpc_call(
            self._stub.Put,
            pb.RecordRequest(store=self._store, record=_dict_to_record(record)),
        )

    def delete(self, id: str) -> None:
        """Delete a record by primary key."""

        _grpc_call(self._stub.Delete, pb.ObjectStoreRequest(store=self._store, id=id))

    def clear(self) -> None:
        """Delete every record in the store."""

        _grpc_call(self._stub.Clear, pb.ObjectStoreNameRequest(store=self._store))

    def get_all(self, key_range: KeyRange | None = None) -> list[dict[str, Any]]:
        """Return all records that fall within ``key_range``."""

        resp = _grpc_call(
            self._stub.GetAll,
            pb.ObjectStoreRangeRequest(
                store=self._store, range=_kr_to_proto(key_range)
            ),
        )
        return [_record_to_dict(r) for r in resp.records]

    def get_all_keys(self, key_range: KeyRange | None = None) -> list[str]:
        """Return all primary keys that fall within ``key_range``."""

        resp = _grpc_call(
            self._stub.GetAllKeys,
            pb.ObjectStoreRangeRequest(
                store=self._store, range=_kr_to_proto(key_range)
            ),
        )
        return list(resp.keys)

    def count(self, key_range: KeyRange | None = None) -> int:
        """Return the number of matching records."""

        resp = _grpc_call(
            self._stub.Count,
            pb.ObjectStoreRangeRequest(
                store=self._store, range=_kr_to_proto(key_range)
            ),
        )
        return resp.count

    def delete_range(self, key_range: KeyRange) -> int:
        """Delete all records within ``key_range``."""

        resp = _grpc_call(
            self._stub.DeleteRange,
            pb.ObjectStoreRangeRequest(
                store=self._store, range=_kr_to_proto(key_range)
            ),
        )
        return resp.deleted

    def open_cursor(
        self,
        key_range: KeyRange | None = None,
        direction: int = CURSOR_NEXT,
    ) -> Cursor:
        """Open a record cursor over the store."""

        return Cursor(self._stub, self._store, key_range=key_range, direction=direction)

    def open_key_cursor(
        self,
        key_range: KeyRange | None = None,
        direction: int = CURSOR_NEXT,
    ) -> Cursor:
        """Open a key-only cursor over the store."""

        return Cursor(
            self._stub,
            self._store,
            key_range=key_range,
            direction=direction,
            keys_only=True,
        )

    def index(self, name: str) -> Index:
        """Return a client for a named index on this store."""

        return Index(self._stub, self._store, name)


class Index:
    """Client bound to a secondary index on an object store."""

    def __init__(self, stub: Any, store: str, index: str) -> None:
        self._stub = stub
        self._store = store
        self._index = index

    def get(self, *values: Any) -> dict[str, Any]:
        """Fetch the first matching record for the indexed values."""

        resp = _grpc_call(self._stub.IndexGet, self._req(values))
        return _record_to_dict(resp.record)

    def get_key(self, *values: Any) -> str:
        """Fetch the first matching primary key for the indexed values."""

        resp = _grpc_call(self._stub.IndexGetKey, self._req(values))
        return resp.key

    def get_all(
        self, *values: Any, key_range: KeyRange | None = None
    ) -> list[dict[str, Any]]:
        """Return all records matching the indexed values and key range."""

        resp = _grpc_call(self._stub.IndexGetAll, self._req(values, key_range))
        return [_record_to_dict(r) for r in resp.records]

    def get_all_keys(
        self, *values: Any, key_range: KeyRange | None = None
    ) -> list[str]:
        """Return all primary keys matching the indexed values and key range."""

        resp = _grpc_call(self._stub.IndexGetAllKeys, self._req(values, key_range))
        return list(resp.keys)

    def count(self, *values: Any, key_range: KeyRange | None = None) -> int:
        """Return the number of records matching the indexed values."""

        resp = _grpc_call(self._stub.IndexCount, self._req(values, key_range))
        return resp.count

    def delete(self, *values: Any) -> int:
        """Delete records matching the indexed values."""

        resp = _grpc_call(self._stub.IndexDelete, self._req(values))
        return resp.deleted

    def open_cursor(
        self,
        *values: Any,
        key_range: KeyRange | None = None,
        direction: int = CURSOR_NEXT,
    ) -> Cursor:
        """Open a record cursor over the indexed results."""

        return Cursor(
            self._stub,
            self._store,
            key_range=key_range,
            direction=direction,
            index=self._index,
            values=values,
        )

    def open_key_cursor(
        self,
        *values: Any,
        key_range: KeyRange | None = None,
        direction: int = CURSOR_NEXT,
    ) -> Cursor:
        """Open a key-only cursor over the indexed results."""

        return Cursor(
            self._stub,
            self._store,
            key_range=key_range,
            direction=direction,
            keys_only=True,
            index=self._index,
            values=values,
        )

    def _req(self, values: tuple[Any, ...], key_range: KeyRange | None = None) -> Any:
        return pb.IndexQueryRequest(
            store=self._store,
            index=self._index,
            values=[_to_typed_value(v) for v in values],
            range=_kr_to_proto(key_range),
        )


class Transaction:
    """Explicit IndexedDB transaction over a fixed object-store scope."""

    def __init__(
        self,
        stub: Any,
        stores: list[str],
        mode: str = "readonly",
        *,
        durability_hint: str = "default",
    ) -> None:
        self._stub = stub
        self._closed = False
        self._request_id = 0
        self._request_iter = _RequestIterator()
        self._request_iter.send(
            pb.TransactionClientMessage(
                begin=pb.BeginTransactionRequest(
                    stores=stores,
                    mode=_transaction_mode_to_proto(mode),
                    durability_hint=_durability_hint_to_proto(durability_hint),
                )
            )
        )
        self._response_iter = stub.Transaction(iter(self._request_iter))
        try:
            resp = next(self._response_iter)
        except StopIteration:
            self._closed = True
            self._request_iter.close()
            raise TransactionError("transaction stream ended during begin") from None
        except grpc.RpcError as e:
            self._closed = True
            self._request_iter.close()
            _raise_grpc_error(e)
        if resp.WhichOneof("msg") != "begin":
            self._closed = True
            self._request_iter.close()
            raise TransactionError("expected transaction begin response")

    def object_store(self, name: str) -> TransactionObjectStore:
        """Return a transaction-scoped object store."""

        return TransactionObjectStore(self, name)

    def commit(self) -> None:
        """Commit the transaction."""

        self._ensure_open()
        self._closed = True
        self._request_iter.send(
            pb.TransactionClientMessage(commit=pb.TransactionCommitRequest())
        )
        try:
            resp = next(self._response_iter)
        except StopIteration:
            self._request_iter.close()
            raise TransactionError("transaction stream ended during commit") from None
        except grpc.RpcError as e:
            self._request_iter.close()
            _raise_grpc_error(e)
        self._request_iter.close()
        if resp.WhichOneof("msg") != "commit":
            raise TransactionError("expected transaction commit response")
        _raise_rpc_status(resp.commit.error)

    def abort(self) -> None:
        """Abort the transaction."""

        if self._closed:
            return
        self._closed = True
        self._request_iter.send(
            pb.TransactionClientMessage(abort=pb.TransactionAbortRequest())
        )
        try:
            resp = next(self._response_iter)
        except StopIteration:
            self._request_iter.close()
            raise TransactionError("transaction stream ended during abort") from None
        except grpc.RpcError as e:
            self._request_iter.close()
            _raise_grpc_error(e)
        self._request_iter.close()
        if resp.WhichOneof("msg") != "abort":
            raise TransactionError("expected transaction abort response")
        _raise_rpc_status(resp.abort.error)

    def _send_operation(self, operation: Any) -> Any:
        self._ensure_open()
        self._request_id += 1
        operation.request_id = self._request_id
        self._request_iter.send(pb.TransactionClientMessage(operation=operation))
        try:
            resp = next(self._response_iter)
        except StopIteration:
            self._closed = True
            self._request_iter.close()
            raise TransactionError(
                "transaction stream ended during operation"
            ) from None
        except grpc.RpcError as e:
            self._closed = True
            self._request_iter.close()
            _raise_grpc_error(e)
        if resp.WhichOneof("msg") != "operation":
            self._closed = True
            self._request_iter.close()
            raise TransactionError("expected transaction operation response")
        op_resp = resp.operation
        if op_resp.request_id != operation.request_id:
            self._closed = True
            self._request_iter.close()
            raise TransactionError("transaction response request_id mismatch")
        try:
            _raise_rpc_status(op_resp.error)
        except Exception:
            self._closed = True
            self._request_iter.close()
            raise
        return op_resp

    def _ensure_open(self) -> None:
        if self._closed:
            raise TransactionError("transaction is already finished")

    def __enter__(self) -> Transaction:
        return self

    def __exit__(self, exc_type: Any, _exc: Any, _tb: Any) -> None:
        if exc_type is None:
            self.commit()
        else:
            self.abort()


class TransactionObjectStore:
    """Transaction-scoped object store."""

    def __init__(self, tx: Transaction, store: str) -> None:
        self._tx = tx
        self._store = store

    def get(self, id: str) -> dict[str, Any]:
        resp = self._tx._send_operation(
            pb.TransactionOperation(get=pb.ObjectStoreRequest(store=self._store, id=id))
        )
        return _record_to_dict(resp.record.record)

    def get_key(self, id: str) -> str:
        resp = self._tx._send_operation(
            pb.TransactionOperation(
                get_key=pb.ObjectStoreRequest(store=self._store, id=id)
            )
        )
        return resp.key.key

    def add(self, record: dict[str, Any]) -> None:
        self._tx._send_operation(
            pb.TransactionOperation(
                add=pb.RecordRequest(store=self._store, record=_dict_to_record(record))
            )
        )

    def put(self, record: dict[str, Any]) -> None:
        self._tx._send_operation(
            pb.TransactionOperation(
                put=pb.RecordRequest(store=self._store, record=_dict_to_record(record))
            )
        )

    def delete(self, id: str) -> None:
        self._tx._send_operation(
            pb.TransactionOperation(
                delete=pb.ObjectStoreRequest(store=self._store, id=id)
            )
        )

    def clear(self) -> None:
        self._tx._send_operation(
            pb.TransactionOperation(clear=pb.ObjectStoreNameRequest(store=self._store))
        )

    def get_all(self, key_range: KeyRange | None = None) -> list[dict[str, Any]]:
        resp = self._tx._send_operation(
            pb.TransactionOperation(
                get_all=pb.ObjectStoreRangeRequest(
                    store=self._store, range=_kr_to_proto(key_range)
                )
            )
        )
        return [_record_to_dict(r) for r in resp.records.records]

    def get_all_keys(self, key_range: KeyRange | None = None) -> list[str]:
        resp = self._tx._send_operation(
            pb.TransactionOperation(
                get_all_keys=pb.ObjectStoreRangeRequest(
                    store=self._store, range=_kr_to_proto(key_range)
                )
            )
        )
        return list(resp.keys.keys)

    def count(self, key_range: KeyRange | None = None) -> int:
        resp = self._tx._send_operation(
            pb.TransactionOperation(
                count=pb.ObjectStoreRangeRequest(
                    store=self._store, range=_kr_to_proto(key_range)
                )
            )
        )
        return int(resp.count.count)

    def delete_range(self, key_range: KeyRange) -> int:
        resp = self._tx._send_operation(
            pb.TransactionOperation(
                delete_range=pb.ObjectStoreRangeRequest(
                    store=self._store, range=_kr_to_proto(key_range)
                )
            )
        )
        return int(resp.delete.deleted)

    def index(self, name: str) -> TransactionIndex:
        return TransactionIndex(self._tx, self._store, name)


class TransactionIndex:
    """Transaction-scoped secondary index."""

    def __init__(self, tx: Transaction, store: str, index: str) -> None:
        self._tx = tx
        self._store = store
        self._index = index

    def get(self, *values: Any) -> dict[str, Any]:
        resp = self._tx._send_operation(
            pb.TransactionOperation(index_get=self._req(values))
        )
        return _record_to_dict(resp.record.record)

    def get_key(self, *values: Any) -> str:
        resp = self._tx._send_operation(
            pb.TransactionOperation(index_get_key=self._req(values))
        )
        return resp.key.key

    def get_all(
        self, *values: Any, key_range: KeyRange | None = None
    ) -> list[dict[str, Any]]:
        resp = self._tx._send_operation(
            pb.TransactionOperation(index_get_all=self._req(values, key_range))
        )
        return [_record_to_dict(r) for r in resp.records.records]

    def get_all_keys(
        self, *values: Any, key_range: KeyRange | None = None
    ) -> list[str]:
        resp = self._tx._send_operation(
            pb.TransactionOperation(index_get_all_keys=self._req(values, key_range))
        )
        return list(resp.keys.keys)

    def count(self, *values: Any, key_range: KeyRange | None = None) -> int:
        resp = self._tx._send_operation(
            pb.TransactionOperation(index_count=self._req(values, key_range))
        )
        return int(resp.count.count)

    def delete(self, *values: Any) -> int:
        resp = self._tx._send_operation(
            pb.TransactionOperation(index_delete=self._req(values))
        )
        return int(resp.delete.deleted)

    def _req(self, values: tuple[Any, ...], key_range: KeyRange | None = None) -> Any:
        return pb.IndexQueryRequest(
            store=self._store,
            index=self._index,
            values=[_to_typed_value(v) for v in values],
            range=_kr_to_proto(key_range),
        )


def _indexeddb_channel(raw_target: str, *, token: str = "") -> grpc.Channel:
    target = raw_target.strip()
    if not target:
        raise RuntimeError("IndexedDB transport target is required")
    if target.startswith("tcp://"):
        address = target[len("tcp://") :].strip()
        if not address:
            raise RuntimeError(
                f"IndexedDB tcp target {raw_target!r} is missing host:port"
            )
        return _with_indexeddb_relay_token(
            insecure_internal_channel(internal_channel_target("tcp", address)),
            token,
        )
    if target.startswith("tls://"):
        address = target[len("tls://") :].strip()
        if not address:
            raise RuntimeError(
                f"IndexedDB tls target {raw_target!r} is missing host:port"
            )
        return _with_indexeddb_relay_token(
            secure_internal_channel(internal_channel_target("tls", address)),
            token,
        )
    if target.startswith("unix://"):
        socket_path = target[len("unix://") :].strip()
        if not socket_path:
            raise RuntimeError(
                f"IndexedDB unix target {raw_target!r} is missing a socket path"
            )
        return _with_indexeddb_relay_token(
            insecure_internal_channel(internal_channel_target("unix", socket_path)),
            token,
        )
    if "://" in target:
        parsed = _urlparse.urlparse(target)
        raise RuntimeError(f"unsupported IndexedDB target scheme {parsed.scheme!r}")
    return _with_indexeddb_relay_token(
        insecure_internal_channel(internal_channel_target("unix", target)),
        token,
    )


def _with_indexeddb_relay_token(channel: grpc.Channel, token: str) -> grpc.Channel:
    token = token.strip()
    if not token:
        return channel

    class _ClientCallDetails(grpc.ClientCallDetails):
        def __init__(
            self,
            method: str,
            timeout: float | None,
            metadata: Any,
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

    class _RelayTokenInterceptor(
        grpc.UnaryUnaryClientInterceptor,
        grpc.StreamStreamClientInterceptor,
    ):
        def __init__(self, token: str) -> None:
            self._token = token

        def _details(
            self, client_call_details: grpc.ClientCallDetails
        ) -> grpc.ClientCallDetails:
            details = cast(_ClientCallDetailsFields, client_call_details)
            metadata = list(details.metadata or [])
            metadata.append((_INDEXEDDB_RELAY_TOKEN_HEADER, self._token))
            return _ClientCallDetails(
                details.method,
                details.timeout,
                metadata,
                details.credentials,
                details.wait_for_ready,
                details.compression,
            )

        def intercept_unary_unary(
            self,
            continuation: Any,
            client_call_details: grpc.ClientCallDetails,
            request: Any,
        ) -> Any:
            return continuation(self._details(client_call_details), request)

        def intercept_stream_stream(
            self,
            continuation: Any,
            client_call_details: grpc.ClientCallDetails,
            request_iterator: Any,
        ) -> Any:
            return continuation(self._details(client_call_details), request_iterator)

    return grpc.intercept_channel(channel, _RelayTokenInterceptor(token))


class _ClientCallDetailsFields(Protocol):
    method: str
    timeout: float | None
    metadata: Any
    credentials: Any
    wait_for_ready: bool | None
    compression: Any


class _RequestIterator:
    def __init__(self) -> None:
        self._q: queue.Queue[Any | None] = queue.Queue()

    def send(self, msg: Any) -> None:
        self._q.put(msg)

    def close(self) -> None:
        self._q.put(None)

    def __iter__(self) -> Iterator[Any]:
        return self

    def __next__(self) -> Any:
        item = self._q.get()
        if item is None:
            raise StopIteration
        return item


class Cursor:
    """Stateful cursor over object store or index results."""

    def __init__(
        self,
        stub: Any,
        store: str,
        *,
        key_range: KeyRange | None = None,
        direction: int = CURSOR_NEXT,
        keys_only: bool = False,
        index: str = "",
        values: tuple[Any, ...] = (),
    ) -> None:
        self._keys_only = keys_only
        self._closed = False
        self._exhausted = False
        self._index_cursor = bool(index)
        self._key: Any = None
        self._primary_key: str | None = None
        self._record: dict[str, Any] | None = None

        self._request_iter = _RequestIterator()

        open_req = pb.OpenCursorRequest(
            store=store,
            range=_kr_to_proto(key_range),
            direction=direction,
            keys_only=keys_only,
            index=index,
            values=[_to_typed_value(v) for v in values],
        )
        self._request_iter.send(pb.CursorClientMessage(open=open_req))

        self._response_iter = stub.OpenCursor(iter(self._request_iter))
        # Read the open ack to surface creation errors synchronously.
        try:
            next(self._response_iter)
        except grpc.RpcError as e:
            self._closed = True
            self._request_iter.close()
            code = e.code()
            details = e.details()
            if code == grpc.StatusCode.NOT_FOUND:
                raise NotFoundError(details) from e
            if code == grpc.StatusCode.ALREADY_EXISTS:
                raise AlreadyExistsError(details) from e
            raise

    def _send_command(self, **kwargs: Any) -> Any:
        cmd = pb.CursorCommand(**kwargs)
        self._request_iter.send(pb.CursorClientMessage(command=cmd))

    def _advance_to_next(self) -> bool:
        try:
            resp = next(self._response_iter)
        except StopIteration:
            self._closed = True
            self._request_iter.close()
            return False
        except grpc.RpcError as e:
            self._closed = True
            self._request_iter.close()
            code = e.code()
            details = e.details()
            if code == grpc.StatusCode.NOT_FOUND:
                raise NotFoundError(details) from e
            if code == grpc.StatusCode.ALREADY_EXISTS:
                raise AlreadyExistsError(details) from e
            raise

        result = resp.WhichOneof("result")
        if result == "done":
            self._key = None
            self._primary_key = None
            self._record = None
            self._exhausted = True
            return False

        entry = resp.entry
        keys = list(entry.key)
        if not self._index_cursor and len(keys) == 1:
            self._key = _key_value_to_python(keys[0])
        elif len(keys) > 0:
            self._key = [_key_value_to_python(k) for k in keys]
        else:
            self._key = None
        self._primary_key = entry.primary_key
        if not self._keys_only:
            self._record = _record_to_dict(entry.record)
        return True

    def continue_(self) -> bool:
        """Advance to the next matching cursor entry."""

        if self._closed or self._exhausted:
            return False
        self._send_command(next=True)
        return self._advance_to_next()

    def continue_to_key(self, key: Any) -> bool:
        """Advance the cursor to ``key`` or the next greater entry."""

        if self._closed or self._exhausted:
            return False
        self._send_command(
            continue_to_key=pb.CursorKeyTarget(
                key=_cursor_key_to_proto(key, self._index_cursor),
            )
        )
        return self._advance_to_next()

    def advance(self, count: int) -> bool:
        """Skip forward by ``count`` entries."""

        if self._closed or self._exhausted:
            return False
        self._send_command(advance=count)
        return self._advance_to_next()

    @property
    def key(self) -> Any:
        """Current key for the cursor entry."""

        return self._key

    @property
    def primary_key(self) -> str | None:
        """Current primary key for the cursor entry."""

        return self._primary_key

    @property
    def value(self) -> dict[str, Any]:
        """Current record value for the cursor entry."""

        if self._keys_only:
            raise TypeError("cursor opened with keys_only=True has no value")
        if self._record is None:
            raise TypeError("cursor is exhausted")
        return self._record

    def _refresh_from_entry(self, entry: Any) -> None:
        keys = list(entry.key)
        if not self._index_cursor and len(keys) == 1:
            self._key = _key_value_to_python(keys[0])
        elif len(keys) > 0:
            self._key = [_key_value_to_python(k) for k in keys]
        else:
            self._key = None
        self._primary_key = entry.primary_key
        if not self._keys_only:
            self._record = _record_to_dict(entry.record)

    def _recv_mutation_ack(self) -> None:
        try:
            resp = next(self._response_iter)
        except StopIteration:
            self._closed = True
            self._request_iter.close()
            raise TypeError("cursor stream ended during mutation") from None
        except grpc.RpcError as e:
            self._closed = True
            self._request_iter.close()
            code = e.code()
            details = e.details()
            if code == grpc.StatusCode.NOT_FOUND:
                raise NotFoundError(details) from e
            if code == grpc.StatusCode.ALREADY_EXISTS:
                raise AlreadyExistsError(details) from e
            raise
        result = resp.WhichOneof("result")
        if result == "entry":
            self._refresh_from_entry(resp.entry)

    def delete(self) -> None:
        """Delete the current cursor entry."""

        if self._exhausted:
            raise NotFoundError("cursor is exhausted")
        if self._closed:
            raise TypeError("cursor is closed")
        self._send_command(delete=True)
        self._recv_mutation_ack()

    def update(self, value: dict[str, Any]) -> None:
        """Replace the current cursor entry with ``value``."""

        if self._exhausted:
            raise NotFoundError("cursor is exhausted")
        if self._closed:
            raise TypeError("cursor is closed")
        self._send_command(update=_dict_to_record(value))
        self._recv_mutation_ack()

    def close(self) -> None:
        """Close the cursor stream."""

        if self._closed:
            return
        self._closed = True
        self._key = None
        self._primary_key = None
        self._record = None
        try:
            self._send_command(close=True)
            self._request_iter.close()
        except Exception:
            pass

    def __enter__(self) -> Cursor:
        """Return the cursor for ``with`` statements."""

        return self

    def __exit__(self, *args: Any) -> None:
        """Close the cursor at the end of a context manager block."""

        self.close()


def _grpc_call(method: Any, request: Any) -> Any:
    try:
        return method(request)
    except grpc.RpcError as e:
        _raise_grpc_error(e)


def _raise_grpc_error(err: grpc.RpcError) -> None:
    code = err.code()
    details = err.details()
    if code == grpc.StatusCode.NOT_FOUND:
        raise NotFoundError(details) from err
    if code == grpc.StatusCode.ALREADY_EXISTS:
        raise AlreadyExistsError(details) from err
    if code in (grpc.StatusCode.FAILED_PRECONDITION, grpc.StatusCode.INVALID_ARGUMENT):
        raise TransactionError(details) from err
    raise err


def _raise_rpc_status(status: Any) -> None:
    if status is None or status.code == 0:
        return
    if status.code == 5:
        raise NotFoundError(status.message)
    if status.code == 6:
        raise AlreadyExistsError(status.message)
    raise TransactionError(status.message)


def _transaction_mode_to_proto(mode: str) -> int:
    normalized = mode.replace("-", "").replace("_", "").lower()
    if normalized == "readonly":
        return pb.TRANSACTION_READONLY
    if normalized == "readwrite":
        return pb.TRANSACTION_READWRITE
    raise ValueError(f"unsupported transaction mode: {mode!r}")


def _durability_hint_to_proto(hint: str) -> int:
    normalized = hint.replace("-", "").replace("_", "").lower()
    if normalized == "default":
        return pb.TRANSACTION_DURABILITY_DEFAULT
    if normalized == "strict":
        return pb.TRANSACTION_DURABILITY_STRICT
    if normalized == "relaxed":
        return pb.TRANSACTION_DURABILITY_RELAXED
    raise ValueError(f"unsupported transaction durability hint: {hint!r}")


def _dict_to_record(d: dict[str, Any]) -> Any:
    record = pb.Record()
    for key, value in d.items():
        record.fields[key].CopyFrom(_to_typed_value(value))
    return record


def _record_to_dict(record: Any) -> dict[str, Any]:
    return {key: _typed_value_to_python(value) for key, value in record.fields.items()}


def _to_typed_value(v: Any) -> Any:
    val = pb.TypedValue()
    if v is None:
        val.null_value = 0
    elif isinstance(v, bool):
        val.bool_value = v
    elif isinstance(v, int) and not isinstance(v, bool):
        val.int_value = v
    elif isinstance(v, float):
        val.float_value = v
    elif isinstance(v, str):
        val.string_value = v
    elif isinstance(v, (bytes, bytearray, memoryview)):
        val.bytes_value = bytes(v)
    elif isinstance(v, _dt.datetime):
        timestamp = timestamp_pb2.Timestamp()
        dt = v if v.tzinfo is not None else v.replace(tzinfo=_dt.timezone.utc)
        timestamp.FromDatetime(dt.astimezone(_dt.timezone.utc))
        val.time_value.CopyFrom(timestamp)
    else:
        val.json_value.CopyFrom(_to_json_value(v))
    return val


def _typed_value_to_python(v: Any) -> Any:
    kind = v.WhichOneof("kind")
    if kind in (None, "null_value"):
        return None
    if kind == "string_value":
        return v.string_value
    if kind == "int_value":
        return v.int_value
    if kind == "float_value":
        return v.float_value
    if kind == "bool_value":
        return v.bool_value
    if kind == "time_value":
        return _dt.datetime.fromtimestamp(
            v.time_value.seconds + (v.time_value.nanos / 1_000_000_000),
            tz=_dt.timezone.utc,
        )
    if kind == "bytes_value":
        return bytes(v.bytes_value)
    if kind == "json_value":
        return _json_value_to_python(v.json_value)
    raise TypeError(f"unsupported typed value kind: {kind}")


def _to_json_value(v: Any) -> Any:
    value = struct_pb2.Value()
    if v is None:
        value.null_value = 0
    elif isinstance(v, bool):
        value.bool_value = v
    elif isinstance(v, (int, float)) and not isinstance(v, bool):
        value.number_value = float(v)
    elif isinstance(v, str):
        value.string_value = v
    elif isinstance(v, dict):
        struct = struct_pb2.Struct()
        for key, inner in v.items():
            struct.fields[key].CopyFrom(_to_json_value(inner))
        value.struct_value.CopyFrom(struct)
    elif isinstance(v, (list, tuple)):
        list_value = struct_pb2.ListValue()
        for inner in v:
            list_value.values.append(_to_json_value(inner))
        value.list_value.CopyFrom(list_value)
    else:
        raise TypeError(f"unsupported JSON value type: {type(v)!r}")
    return value


def _json_value_to_python(v: Any) -> Any:
    kind = v.WhichOneof("kind")
    if kind in (None, "null_value"):
        return None
    if kind == "number_value":
        return v.number_value
    if kind == "string_value":
        return v.string_value
    if kind == "bool_value":
        return v.bool_value
    if kind == "struct_value":
        return {
            key: _json_value_to_python(value)
            for key, value in v.struct_value.fields.items()
        }
    if kind == "list_value":
        return [_json_value_to_python(value) for value in v.list_value.values]
    raise TypeError(f"unsupported JSON value kind: {kind}")


def _key_value_to_python(kv: Any) -> Any:
    kind = kv.WhichOneof("kind")
    if kind == "scalar":
        return _typed_value_to_python(kv.scalar)
    if kind == "array":
        return [_key_value_to_python(elem) for elem in kv.array.elements]
    return None


def _python_to_key_value(v: Any) -> Any:
    if isinstance(v, (list, tuple)):
        return pb.KeyValue(
            array=pb.KeyValueArray(elements=[_python_to_key_value(elem) for elem in v])
        )
    return pb.KeyValue(scalar=_to_typed_value(v))


def _cursor_key_to_proto(key: Any, index_cursor: bool) -> list[Any]:
    if index_cursor and isinstance(key, (list, tuple)):
        return [_python_to_key_value(part) for part in key]
    return [_python_to_key_value(key)]


def _kr_to_proto(kr: KeyRange | None) -> Any:
    if kr is None:
        return None
    return pb.KeyRange(
        lower=_to_typed_value(kr.lower) if kr.lower is not None else None,
        upper=_to_typed_value(kr.upper) if kr.upper is not None else None,
        lower_open=kr.lower_open,
        upper_open=kr.upper_open,
    )
