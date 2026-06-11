import datetime

from google.protobuf import duration_pb2 as _duration_pb2
from google.protobuf import empty_pb2 as _empty_pb2
from . import annotations_pb2 as _annotations_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class CacheSetEntry(_message.Message):
    __slots__ = ()
    KEY_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    key: str
    value: bytes
    def __init__(self, key: _Optional[str] = ..., value: _Optional[bytes] = ...) -> None: ...

class CacheResult(_message.Message):
    __slots__ = ()
    KEY_FIELD_NUMBER: _ClassVar[int]
    FOUND_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    key: str
    found: bool
    value: bytes
    def __init__(self, key: _Optional[str] = ..., found: _Optional[bool] = ..., value: _Optional[bytes] = ...) -> None: ...

class CacheGetRequest(_message.Message):
    __slots__ = ()
    KEY_FIELD_NUMBER: _ClassVar[int]
    key: str
    def __init__(self, key: _Optional[str] = ...) -> None: ...

class CacheGetResponse(_message.Message):
    __slots__ = ()
    FOUND_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    found: bool
    value: bytes
    def __init__(self, found: _Optional[bool] = ..., value: _Optional[bytes] = ...) -> None: ...

class CacheGetManyRequest(_message.Message):
    __slots__ = ()
    KEYS_FIELD_NUMBER: _ClassVar[int]
    keys: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, keys: _Optional[_Iterable[str]] = ...) -> None: ...

class CacheGetManyResponse(_message.Message):
    __slots__ = ()
    ENTRIES_FIELD_NUMBER: _ClassVar[int]
    entries: _containers.RepeatedCompositeFieldContainer[CacheResult]
    def __init__(self, entries: _Optional[_Iterable[_Union[CacheResult, _Mapping]]] = ...) -> None: ...

class CacheSetRequest(_message.Message):
    __slots__ = ()
    KEY_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    TTL_FIELD_NUMBER: _ClassVar[int]
    key: str
    value: bytes
    ttl: _duration_pb2.Duration
    def __init__(self, key: _Optional[str] = ..., value: _Optional[bytes] = ..., ttl: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class CacheSetManyRequest(_message.Message):
    __slots__ = ()
    ENTRIES_FIELD_NUMBER: _ClassVar[int]
    TTL_FIELD_NUMBER: _ClassVar[int]
    entries: _containers.RepeatedCompositeFieldContainer[CacheSetEntry]
    ttl: _duration_pb2.Duration
    def __init__(self, entries: _Optional[_Iterable[_Union[CacheSetEntry, _Mapping]]] = ..., ttl: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class CacheDeleteRequest(_message.Message):
    __slots__ = ()
    KEY_FIELD_NUMBER: _ClassVar[int]
    key: str
    def __init__(self, key: _Optional[str] = ...) -> None: ...

class CacheDeleteResponse(_message.Message):
    __slots__ = ()
    DELETED_FIELD_NUMBER: _ClassVar[int]
    deleted: bool
    def __init__(self, deleted: _Optional[bool] = ...) -> None: ...

class CacheDeleteManyRequest(_message.Message):
    __slots__ = ()
    KEYS_FIELD_NUMBER: _ClassVar[int]
    keys: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, keys: _Optional[_Iterable[str]] = ...) -> None: ...

class CacheDeleteManyResponse(_message.Message):
    __slots__ = ()
    DELETED_FIELD_NUMBER: _ClassVar[int]
    deleted: int
    def __init__(self, deleted: _Optional[int] = ...) -> None: ...

class CacheTouchRequest(_message.Message):
    __slots__ = ()
    KEY_FIELD_NUMBER: _ClassVar[int]
    TTL_FIELD_NUMBER: _ClassVar[int]
    key: str
    ttl: _duration_pb2.Duration
    def __init__(self, key: _Optional[str] = ..., ttl: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class CacheTouchResponse(_message.Message):
    __slots__ = ()
    TOUCHED_FIELD_NUMBER: _ClassVar[int]
    touched: bool
    def __init__(self, touched: _Optional[bool] = ...) -> None: ...
