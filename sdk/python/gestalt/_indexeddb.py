from __future__ import annotations

import datetime as _dt
import os
import queue
from dataclasses import dataclass, field
from typing import Any, Iterator

import grpc
from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2

from .gen.v1 import datastore_pb2 as _pb
from .gen.v1 import datastore_pb2_grpc as _pb_grpc

pb: Any = _pb
pb_grpc: Any = _pb_grpc
struct_pb2: Any = _struct_pb2
timestamp_pb2: Any = _timestamp_pb2

ENV_INDEXEDDB_SOCKET = "GESTALT_INDEXEDDB_SOCKET"

CURSOR_NEXT = 0
CURSOR_NEXT_UNIQUE = 1
CURSOR_PREV = 2
CURSOR_PREV_UNIQUE = 3


class NotFoundError(Exception):
    pass


class AlreadyExistsError(Exception):
    pass


@dataclass
class KeyRange:
    lower: Any = None
    upper: Any = None
    lower_open: bool = False
    upper_open: bool = False


@dataclass
class IndexSchema:
    name: str
    key_path: list[str] = field(default_factory=list)
    unique: bool = False


@dataclass
class ObjectStoreSchema:
    indexes: list[IndexSchema] = field(default_factory=list)


class IndexedDB:
    def __init__(self) -> None:
        socket_path = os.environ.get(ENV_INDEXEDDB_SOCKET, "")
        if not socket_path:
            raise RuntimeError(f"{ENV_INDEXEDDB_SOCKET} is not set")
        self._channel = grpc.insecure_channel(f"unix:{socket_path}")
        self._stub = pb_grpc.IndexedDBStub(self._channel)

    def close(self) -> None:
        self._channel.close()

    def create_object_store(self, name: str, schema: ObjectStoreSchema | None = None) -> None:
        pb_schema = pb.ObjectStoreSchema()
        if schema:
            for idx in schema.indexes:
                pb_schema.indexes.append(
                    pb.IndexSchema(name=idx.name, key_path=idx.key_path, unique=idx.unique)
                )
        _grpc_call(self._stub.CreateObjectStore, pb.CreateObjectStoreRequest(name=name, schema=pb_schema))

    def delete_object_store(self, name: str) -> None:
        _grpc_call(self._stub.DeleteObjectStore, pb.DeleteObjectStoreRequest(name=name))

    def object_store(self, name: str) -> ObjectStore:
        return ObjectStore(self._stub, name)

    def __enter__(self) -> IndexedDB:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


class ObjectStore:
    def __init__(self, stub: Any, store: str) -> None:
        self._stub = stub
        self._store = store

    def get(self, id: str) -> dict[str, Any]:
        resp = _grpc_call(self._stub.Get, pb.ObjectStoreRequest(store=self._store, id=id))
        return _record_to_dict(resp.record)

    def get_key(self, id: str) -> str:
        resp = _grpc_call(self._stub.GetKey, pb.ObjectStoreRequest(store=self._store, id=id))
        return resp.key

    def add(self, record: dict[str, Any]) -> None:
        _grpc_call(self._stub.Add, pb.RecordRequest(store=self._store, record=_dict_to_record(record)))

    def put(self, record: dict[str, Any]) -> None:
        _grpc_call(self._stub.Put, pb.RecordRequest(store=self._store, record=_dict_to_record(record)))

    def delete(self, id: str) -> None:
        _grpc_call(self._stub.Delete, pb.ObjectStoreRequest(store=self._store, id=id))

    def clear(self) -> None:
        _grpc_call(self._stub.Clear, pb.ObjectStoreNameRequest(store=self._store))

    def get_all(self, key_range: KeyRange | None = None) -> list[dict[str, Any]]:
        resp = _grpc_call(
            self._stub.GetAll,
            pb.ObjectStoreRangeRequest(store=self._store, range=_kr_to_proto(key_range)),
        )
        return [_record_to_dict(r) for r in resp.records]

    def get_all_keys(self, key_range: KeyRange | None = None) -> list[str]:
        resp = _grpc_call(
            self._stub.GetAllKeys,
            pb.ObjectStoreRangeRequest(store=self._store, range=_kr_to_proto(key_range)),
        )
        return list(resp.keys)

    def count(self, key_range: KeyRange | None = None) -> int:
        resp = _grpc_call(
            self._stub.Count,
            pb.ObjectStoreRangeRequest(store=self._store, range=_kr_to_proto(key_range)),
        )
        return resp.count

    def delete_range(self, key_range: KeyRange) -> int:
        resp = _grpc_call(
            self._stub.DeleteRange,
            pb.ObjectStoreRangeRequest(store=self._store, range=_kr_to_proto(key_range)),
        )
        return resp.deleted

    def open_cursor(
        self,
        key_range: KeyRange | None = None,
        direction: int = CURSOR_NEXT,
    ) -> Cursor:
        return Cursor(self._stub, self._store, key_range=key_range, direction=direction)

    def open_key_cursor(
        self,
        key_range: KeyRange | None = None,
        direction: int = CURSOR_NEXT,
    ) -> Cursor:
        return Cursor(
            self._stub, self._store, key_range=key_range, direction=direction, keys_only=True
        )

    def index(self, name: str) -> Index:
        return Index(self._stub, self._store, name)


class Index:
    def __init__(self, stub: Any, store: str, index: str) -> None:
        self._stub = stub
        self._store = store
        self._index = index

    def get(self, *values: Any) -> dict[str, Any]:
        resp = _grpc_call(self._stub.IndexGet, self._req(values))
        return _record_to_dict(resp.record)

    def get_key(self, *values: Any) -> str:
        resp = _grpc_call(self._stub.IndexGetKey, self._req(values))
        return resp.key

    def get_all(self, *values: Any, key_range: KeyRange | None = None) -> list[dict[str, Any]]:
        resp = _grpc_call(self._stub.IndexGetAll, self._req(values, key_range))
        return [_record_to_dict(r) for r in resp.records]

    def get_all_keys(self, *values: Any, key_range: KeyRange | None = None) -> list[str]:
        resp = _grpc_call(self._stub.IndexGetAllKeys, self._req(values, key_range))
        return list(resp.keys)

    def count(self, *values: Any, key_range: KeyRange | None = None) -> int:
        resp = _grpc_call(self._stub.IndexCount, self._req(values, key_range))
        return resp.count

    def delete(self, *values: Any) -> int:
        resp = _grpc_call(self._stub.IndexDelete, self._req(values))
        return resp.deleted

    def open_cursor(
        self,
        *values: Any,
        key_range: KeyRange | None = None,
        direction: int = CURSOR_NEXT,
    ) -> Cursor:
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
        return Cursor(
            self._stub,
            self._store,
            key_range=key_range,
            direction=direction,
            keys_only=True,
            index=self._index,
            values=values,
        )

    def _req(
        self, values: tuple[Any, ...], key_range: KeyRange | None = None
    ) -> Any:
        return pb.IndexQueryRequest(
            store=self._store,
            index=self._index,
            values=[_to_typed_value(v) for v in values],
            range=_kr_to_proto(key_range),
        )


class _RequestIterator:
    def __init__(self) -> None:
        self._q: queue.Queue[pb.CursorClientMessage | None] = queue.Queue()

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
            code = e.code()  # ty: ignore[unresolved-attribute]
            details = e.details()  # ty: ignore[unresolved-attribute]
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
            code = e.code()  # ty: ignore[unresolved-attribute]
            details = e.details()  # ty: ignore[unresolved-attribute]
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
        if self._closed or self._exhausted:
            return False
        self._send_command(next=True)
        return self._advance_to_next()

    def continue_to_key(self, key: Any) -> bool:
        if self._closed or self._exhausted:
            return False
        kv = _python_to_key_value(key)
        self._send_command(continue_to_key=pb.CursorKeyTarget(key=[kv]))
        return self._advance_to_next()

    def advance(self, count: int) -> bool:
        if self._closed or self._exhausted:
            return False
        self._send_command(advance=count)
        return self._advance_to_next()

    @property
    def key(self) -> Any:
        return self._key

    @property
    def primary_key(self) -> str | None:
        return self._primary_key

    @property
    def value(self) -> dict[str, Any]:
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
            code = e.code()  # ty: ignore[unresolved-attribute]
            details = e.details()  # ty: ignore[unresolved-attribute]
            if code == grpc.StatusCode.NOT_FOUND:
                raise NotFoundError(details) from e
            if code == grpc.StatusCode.ALREADY_EXISTS:
                raise AlreadyExistsError(details) from e
            raise
        result = resp.WhichOneof("result")
        if result == "entry":
            self._refresh_from_entry(resp.entry)

    def delete(self) -> None:
        if self._exhausted:
            raise NotFoundError("cursor is exhausted")
        if self._closed:
            raise TypeError("cursor is closed")
        self._send_command(delete=True)
        self._recv_mutation_ack()

    def update(self, value: dict[str, Any]) -> None:
        if self._exhausted:
            raise NotFoundError("cursor is exhausted")
        if self._closed:
            raise TypeError("cursor is closed")
        self._send_command(update=_dict_to_record(value))
        self._recv_mutation_ack()

    def close(self) -> None:
        if self._closed:
            return
        self._closed = True
        try:
            self._send_command(close=True)
            self._request_iter.close()
        except Exception:
            pass

    def __enter__(self) -> Cursor:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


def _grpc_call(method: Any, request: Any) -> Any:
    try:
        return method(request)
    except grpc.RpcError as e:
        code = e.code()  # ty: ignore[unresolved-attribute]
        details = e.details()  # ty: ignore[unresolved-attribute]
        if code == grpc.StatusCode.NOT_FOUND:
            raise NotFoundError(details) from e
        if code == grpc.StatusCode.ALREADY_EXISTS:
            raise AlreadyExistsError(details) from e
        raise


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
        return {key: _json_value_to_python(value) for key, value in v.struct_value.fields.items()}
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
        return pb.KeyValue(array=pb.KeyValueArray(elements=[_python_to_key_value(elem) for elem in v]))
    return pb.KeyValue(scalar=_to_typed_value(v))


def _kr_to_proto(kr: KeyRange | None) -> Any:
    if kr is None:
        return None
    return pb.KeyRange(
        lower=_to_typed_value(kr.lower) if kr.lower is not None else None,
        upper=_to_typed_value(kr.upper) if kr.upper is not None else None,
        lower_open=kr.lower_open,
        upper_open=kr.upper_open,
    )
