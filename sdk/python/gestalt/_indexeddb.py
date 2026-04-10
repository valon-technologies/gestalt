from __future__ import annotations

import os
from dataclasses import dataclass, field
from typing import Any

import grpc
from google.protobuf import struct_pb2 as _struct_pb2

from .gen.v1 import datastore_pb2 as _pb
from .gen.v1 import datastore_pb2_grpc as _pb_grpc

pb: Any = _pb
pb_grpc: Any = _pb_grpc
struct_pb2: Any = _struct_pb2

ENV_INDEXEDDB_SOCKET = "GESTALT_INDEXEDDB_SOCKET"


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
        return _struct_to_dict(resp.record)

    def get_key(self, id: str) -> str:
        resp = _grpc_call(self._stub.GetKey, pb.ObjectStoreRequest(store=self._store, id=id))
        return resp.key

    def add(self, record: dict[str, Any]) -> None:
        _grpc_call(self._stub.Add, pb.RecordRequest(store=self._store, record=_dict_to_struct(record)))

    def put(self, record: dict[str, Any]) -> None:
        _grpc_call(self._stub.Put, pb.RecordRequest(store=self._store, record=_dict_to_struct(record)))

    def delete(self, id: str) -> None:
        _grpc_call(self._stub.Delete, pb.ObjectStoreRequest(store=self._store, id=id))

    def clear(self) -> None:
        _grpc_call(self._stub.Clear, pb.ObjectStoreNameRequest(store=self._store))

    def get_all(self, key_range: KeyRange | None = None) -> list[dict[str, Any]]:
        resp = _grpc_call(
            self._stub.GetAll,
            pb.ObjectStoreRangeRequest(store=self._store, range=_kr_to_proto(key_range)),
        )
        return [_struct_to_dict(r) for r in resp.records]

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

    def index(self, name: str) -> Index:
        return Index(self._stub, self._store, name)


class Index:
    def __init__(self, stub: Any, store: str, index: str) -> None:
        self._stub = stub
        self._store = store
        self._index = index

    def get(self, *values: Any) -> dict[str, Any]:
        resp = _grpc_call(self._stub.IndexGet, self._req(values))
        return _struct_to_dict(resp.record)

    def get_key(self, *values: Any) -> str:
        resp = _grpc_call(self._stub.IndexGetKey, self._req(values))
        return resp.key

    def get_all(self, *values: Any, key_range: KeyRange | None = None) -> list[dict[str, Any]]:
        resp = _grpc_call(self._stub.IndexGetAll, self._req(values, key_range))
        return [_struct_to_dict(r) for r in resp.records]

    def get_all_keys(self, *values: Any, key_range: KeyRange | None = None) -> list[str]:
        resp = _grpc_call(self._stub.IndexGetAllKeys, self._req(values, key_range))
        return list(resp.keys)

    def count(self, *values: Any, key_range: KeyRange | None = None) -> int:
        resp = _grpc_call(self._stub.IndexCount, self._req(values, key_range))
        return resp.count

    def delete(self, *values: Any) -> int:
        resp = _grpc_call(self._stub.IndexDelete, self._req(values))
        return resp.deleted

    def _req(
        self, values: tuple[Any, ...], key_range: KeyRange | None = None
    ) -> Any:
        return pb.IndexQueryRequest(
            store=self._store,
            index=self._index,
            values=[_to_proto_value(v) for v in values],
            range=_kr_to_proto(key_range),
        )


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


def _dict_to_struct(d: dict[str, Any]) -> Any:
    s = struct_pb2.Struct()
    s.update(d)
    return s


def _struct_to_dict(s: Any) -> dict[str, Any]:
    return dict(s)


def _to_proto_value(v: Any) -> Any:
    val = struct_pb2.Value()
    if v is None:
        val.null_value = 0
    elif isinstance(v, bool):
        val.bool_value = v
    elif isinstance(v, (int, float)):
        val.number_value = float(v)
    elif isinstance(v, str):
        val.string_value = v
    else:
        val.string_value = str(v)
    return val


def _kr_to_proto(kr: KeyRange | None) -> Any:
    if kr is None:
        return None
    return pb.KeyRange(
        lower=_to_proto_value(kr.lower) if kr.lower is not None else None,
        upper=_to_proto_value(kr.upper) if kr.upper is not None else None,
        lower_open=kr.lower_open,
        upper_open=kr.upper_open,
    )
