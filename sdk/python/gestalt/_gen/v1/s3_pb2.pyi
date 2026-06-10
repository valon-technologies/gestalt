import datetime

from google.protobuf import empty_pb2 as _empty_pb2
from . import annotations_pb2 as _annotations_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class PresignMethod(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PRESIGN_METHOD_UNSPECIFIED: _ClassVar[PresignMethod]
    PRESIGN_METHOD_GET: _ClassVar[PresignMethod]
    PRESIGN_METHOD_PUT: _ClassVar[PresignMethod]
    PRESIGN_METHOD_DELETE: _ClassVar[PresignMethod]
    PRESIGN_METHOD_HEAD: _ClassVar[PresignMethod]
PRESIGN_METHOD_UNSPECIFIED: PresignMethod
PRESIGN_METHOD_GET: PresignMethod
PRESIGN_METHOD_PUT: PresignMethod
PRESIGN_METHOD_DELETE: PresignMethod
PRESIGN_METHOD_HEAD: PresignMethod

class S3ObjectRef(_message.Message):
    __slots__ = ()
    KEY_FIELD_NUMBER: _ClassVar[int]
    VERSION_ID_FIELD_NUMBER: _ClassVar[int]
    key: str
    version_id: str
    def __init__(self, key: _Optional[str] = ..., version_id: _Optional[str] = ...) -> None: ...

class S3ObjectMeta(_message.Message):
    __slots__ = ()
    class MetadataEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    REF_FIELD_NUMBER: _ClassVar[int]
    ETAG_FIELD_NUMBER: _ClassVar[int]
    SIZE_FIELD_NUMBER: _ClassVar[int]
    CONTENT_TYPE_FIELD_NUMBER: _ClassVar[int]
    LAST_MODIFIED_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    STORAGE_CLASS_FIELD_NUMBER: _ClassVar[int]
    ref: S3ObjectRef
    etag: str
    size: int
    content_type: str
    last_modified: _timestamp_pb2.Timestamp
    metadata: _containers.ScalarMap[str, str]
    storage_class: str
    def __init__(self, ref: _Optional[_Union[S3ObjectRef, _Mapping]] = ..., etag: _Optional[str] = ..., size: _Optional[int] = ..., content_type: _Optional[str] = ..., last_modified: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., metadata: _Optional[_Mapping[str, str]] = ..., storage_class: _Optional[str] = ...) -> None: ...

class ByteRange(_message.Message):
    __slots__ = ()
    START_FIELD_NUMBER: _ClassVar[int]
    END_FIELD_NUMBER: _ClassVar[int]
    start: int
    end: int
    def __init__(self, start: _Optional[int] = ..., end: _Optional[int] = ...) -> None: ...

class HeadObjectRequest(_message.Message):
    __slots__ = ()
    REF_FIELD_NUMBER: _ClassVar[int]
    ref: S3ObjectRef
    def __init__(self, ref: _Optional[_Union[S3ObjectRef, _Mapping]] = ...) -> None: ...

class HeadObjectResponse(_message.Message):
    __slots__ = ()
    META_FIELD_NUMBER: _ClassVar[int]
    meta: S3ObjectMeta
    def __init__(self, meta: _Optional[_Union[S3ObjectMeta, _Mapping]] = ...) -> None: ...

class ReadObjectRequest(_message.Message):
    __slots__ = ()
    REF_FIELD_NUMBER: _ClassVar[int]
    RANGE_FIELD_NUMBER: _ClassVar[int]
    IF_MATCH_FIELD_NUMBER: _ClassVar[int]
    IF_NONE_MATCH_FIELD_NUMBER: _ClassVar[int]
    IF_MODIFIED_SINCE_FIELD_NUMBER: _ClassVar[int]
    IF_UNMODIFIED_SINCE_FIELD_NUMBER: _ClassVar[int]
    ref: S3ObjectRef
    range: ByteRange
    if_match: str
    if_none_match: str
    if_modified_since: _timestamp_pb2.Timestamp
    if_unmodified_since: _timestamp_pb2.Timestamp
    def __init__(self, ref: _Optional[_Union[S3ObjectRef, _Mapping]] = ..., range: _Optional[_Union[ByteRange, _Mapping]] = ..., if_match: _Optional[str] = ..., if_none_match: _Optional[str] = ..., if_modified_since: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., if_unmodified_since: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class ReadObjectChunk(_message.Message):
    __slots__ = ()
    META_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    meta: S3ObjectMeta
    data: bytes
    def __init__(self, meta: _Optional[_Union[S3ObjectMeta, _Mapping]] = ..., data: _Optional[bytes] = ...) -> None: ...

class WriteObjectOpen(_message.Message):
    __slots__ = ()
    class MetadataEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    REF_FIELD_NUMBER: _ClassVar[int]
    CONTENT_TYPE_FIELD_NUMBER: _ClassVar[int]
    CACHE_CONTROL_FIELD_NUMBER: _ClassVar[int]
    CONTENT_DISPOSITION_FIELD_NUMBER: _ClassVar[int]
    CONTENT_ENCODING_FIELD_NUMBER: _ClassVar[int]
    CONTENT_LANGUAGE_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    IF_MATCH_FIELD_NUMBER: _ClassVar[int]
    IF_NONE_MATCH_FIELD_NUMBER: _ClassVar[int]
    ref: S3ObjectRef
    content_type: str
    cache_control: str
    content_disposition: str
    content_encoding: str
    content_language: str
    metadata: _containers.ScalarMap[str, str]
    if_match: str
    if_none_match: str
    def __init__(self, ref: _Optional[_Union[S3ObjectRef, _Mapping]] = ..., content_type: _Optional[str] = ..., cache_control: _Optional[str] = ..., content_disposition: _Optional[str] = ..., content_encoding: _Optional[str] = ..., content_language: _Optional[str] = ..., metadata: _Optional[_Mapping[str, str]] = ..., if_match: _Optional[str] = ..., if_none_match: _Optional[str] = ...) -> None: ...

class WriteObjectRequest(_message.Message):
    __slots__ = ()
    OPEN_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    open: WriteObjectOpen
    data: bytes
    def __init__(self, open: _Optional[_Union[WriteObjectOpen, _Mapping]] = ..., data: _Optional[bytes] = ...) -> None: ...

class WriteObjectResponse(_message.Message):
    __slots__ = ()
    META_FIELD_NUMBER: _ClassVar[int]
    meta: S3ObjectMeta
    def __init__(self, meta: _Optional[_Union[S3ObjectMeta, _Mapping]] = ...) -> None: ...

class DeleteObjectRequest(_message.Message):
    __slots__ = ()
    REF_FIELD_NUMBER: _ClassVar[int]
    ref: S3ObjectRef
    def __init__(self, ref: _Optional[_Union[S3ObjectRef, _Mapping]] = ...) -> None: ...

class ListObjectsRequest(_message.Message):
    __slots__ = ()
    PREFIX_FIELD_NUMBER: _ClassVar[int]
    DELIMITER_FIELD_NUMBER: _ClassVar[int]
    CONTINUATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    START_AFTER_FIELD_NUMBER: _ClassVar[int]
    MAX_KEYS_FIELD_NUMBER: _ClassVar[int]
    prefix: str
    delimiter: str
    continuation_token: str
    start_after: str
    max_keys: int
    def __init__(self, prefix: _Optional[str] = ..., delimiter: _Optional[str] = ..., continuation_token: _Optional[str] = ..., start_after: _Optional[str] = ..., max_keys: _Optional[int] = ...) -> None: ...

class ListObjectsResponse(_message.Message):
    __slots__ = ()
    OBJECTS_FIELD_NUMBER: _ClassVar[int]
    COMMON_PREFIXES_FIELD_NUMBER: _ClassVar[int]
    NEXT_CONTINUATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    HAS_MORE_FIELD_NUMBER: _ClassVar[int]
    objects: _containers.RepeatedCompositeFieldContainer[S3ObjectMeta]
    common_prefixes: _containers.RepeatedScalarFieldContainer[str]
    next_continuation_token: str
    has_more: bool
    def __init__(self, objects: _Optional[_Iterable[_Union[S3ObjectMeta, _Mapping]]] = ..., common_prefixes: _Optional[_Iterable[str]] = ..., next_continuation_token: _Optional[str] = ..., has_more: _Optional[bool] = ...) -> None: ...

class CopyObjectRequest(_message.Message):
    __slots__ = ()
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    DESTINATION_FIELD_NUMBER: _ClassVar[int]
    IF_MATCH_FIELD_NUMBER: _ClassVar[int]
    IF_NONE_MATCH_FIELD_NUMBER: _ClassVar[int]
    source: S3ObjectRef
    destination: S3ObjectRef
    if_match: str
    if_none_match: str
    def __init__(self, source: _Optional[_Union[S3ObjectRef, _Mapping]] = ..., destination: _Optional[_Union[S3ObjectRef, _Mapping]] = ..., if_match: _Optional[str] = ..., if_none_match: _Optional[str] = ...) -> None: ...

class CopyObjectResponse(_message.Message):
    __slots__ = ()
    META_FIELD_NUMBER: _ClassVar[int]
    meta: S3ObjectMeta
    def __init__(self, meta: _Optional[_Union[S3ObjectMeta, _Mapping]] = ...) -> None: ...

class PresignObjectRequest(_message.Message):
    __slots__ = ()
    class HeadersEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    REF_FIELD_NUMBER: _ClassVar[int]
    METHOD_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_SECONDS_FIELD_NUMBER: _ClassVar[int]
    CONTENT_TYPE_FIELD_NUMBER: _ClassVar[int]
    CONTENT_DISPOSITION_FIELD_NUMBER: _ClassVar[int]
    HEADERS_FIELD_NUMBER: _ClassVar[int]
    ref: S3ObjectRef
    method: PresignMethod
    expires_seconds: int
    content_type: str
    content_disposition: str
    headers: _containers.ScalarMap[str, str]
    def __init__(self, ref: _Optional[_Union[S3ObjectRef, _Mapping]] = ..., method: _Optional[_Union[PresignMethod, str]] = ..., expires_seconds: _Optional[int] = ..., content_type: _Optional[str] = ..., content_disposition: _Optional[str] = ..., headers: _Optional[_Mapping[str, str]] = ...) -> None: ...

class PresignObjectResponse(_message.Message):
    __slots__ = ()
    class HeadersEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    URL_FIELD_NUMBER: _ClassVar[int]
    METHOD_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    HEADERS_FIELD_NUMBER: _ClassVar[int]
    url: str
    method: PresignMethod
    expires_at: _timestamp_pb2.Timestamp
    headers: _containers.ScalarMap[str, str]
    def __init__(self, url: _Optional[str] = ..., method: _Optional[_Union[PresignMethod, str]] = ..., expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., headers: _Optional[_Mapping[str, str]] = ...) -> None: ...

class CreateObjectAccessURLRequest(_message.Message):
    __slots__ = ()
    class HeadersEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    REF_FIELD_NUMBER: _ClassVar[int]
    METHOD_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_SECONDS_FIELD_NUMBER: _ClassVar[int]
    CONTENT_TYPE_FIELD_NUMBER: _ClassVar[int]
    CONTENT_DISPOSITION_FIELD_NUMBER: _ClassVar[int]
    HEADERS_FIELD_NUMBER: _ClassVar[int]
    ref: S3ObjectRef
    method: PresignMethod
    expires_seconds: int
    content_type: str
    content_disposition: str
    headers: _containers.ScalarMap[str, str]
    def __init__(self, ref: _Optional[_Union[S3ObjectRef, _Mapping]] = ..., method: _Optional[_Union[PresignMethod, str]] = ..., expires_seconds: _Optional[int] = ..., content_type: _Optional[str] = ..., content_disposition: _Optional[str] = ..., headers: _Optional[_Mapping[str, str]] = ...) -> None: ...

class CreateObjectAccessURLResponse(_message.Message):
    __slots__ = ()
    class HeadersEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    URL_FIELD_NUMBER: _ClassVar[int]
    METHOD_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    HEADERS_FIELD_NUMBER: _ClassVar[int]
    url: str
    method: PresignMethod
    expires_at: _timestamp_pb2.Timestamp
    headers: _containers.ScalarMap[str, str]
    def __init__(self, url: _Optional[str] = ..., method: _Optional[_Union[PresignMethod, str]] = ..., expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., headers: _Optional[_Mapping[str, str]] = ...) -> None: ...
