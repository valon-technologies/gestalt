import datetime

from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.rpc import status_pb2 as _status_pb2
from . import annotations_pb2 as _annotations_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class CursorDirection(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CURSOR_NEXT: _ClassVar[CursorDirection]
    CURSOR_NEXT_UNIQUE: _ClassVar[CursorDirection]
    CURSOR_PREV: _ClassVar[CursorDirection]
    CURSOR_PREV_UNIQUE: _ClassVar[CursorDirection]

class TransactionMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    TRANSACTION_READONLY: _ClassVar[TransactionMode]
    TRANSACTION_READWRITE: _ClassVar[TransactionMode]

class TransactionDurabilityHint(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    TRANSACTION_DURABILITY_DEFAULT: _ClassVar[TransactionDurabilityHint]
    TRANSACTION_DURABILITY_STRICT: _ClassVar[TransactionDurabilityHint]
    TRANSACTION_DURABILITY_RELAXED: _ClassVar[TransactionDurabilityHint]
CURSOR_NEXT: CursorDirection
CURSOR_NEXT_UNIQUE: CursorDirection
CURSOR_PREV: CursorDirection
CURSOR_PREV_UNIQUE: CursorDirection
TRANSACTION_READONLY: TransactionMode
TRANSACTION_READWRITE: TransactionMode
TRANSACTION_DURABILITY_DEFAULT: TransactionDurabilityHint
TRANSACTION_DURABILITY_STRICT: TransactionDurabilityHint
TRANSACTION_DURABILITY_RELAXED: TransactionDurabilityHint

class TypedValue(_message.Message):
    __slots__ = ()
    NULL_VALUE_FIELD_NUMBER: _ClassVar[int]
    STRING_VALUE_FIELD_NUMBER: _ClassVar[int]
    INT_VALUE_FIELD_NUMBER: _ClassVar[int]
    FLOAT_VALUE_FIELD_NUMBER: _ClassVar[int]
    BOOL_VALUE_FIELD_NUMBER: _ClassVar[int]
    TIME_VALUE_FIELD_NUMBER: _ClassVar[int]
    BYTES_VALUE_FIELD_NUMBER: _ClassVar[int]
    JSON_VALUE_FIELD_NUMBER: _ClassVar[int]
    null_value: _struct_pb2.NullValue
    string_value: str
    int_value: int
    float_value: float
    bool_value: bool
    time_value: _timestamp_pb2.Timestamp
    bytes_value: bytes
    json_value: _struct_pb2.Value
    def __init__(self, null_value: _Optional[_Union[_struct_pb2.NullValue, str]] = ..., string_value: _Optional[str] = ..., int_value: _Optional[int] = ..., float_value: _Optional[float] = ..., bool_value: _Optional[bool] = ..., time_value: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., bytes_value: _Optional[bytes] = ..., json_value: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ...) -> None: ...

class Record(_message.Message):
    __slots__ = ()
    class FieldsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: TypedValue
        def __init__(self, key: _Optional[str] = ..., value: _Optional[_Union[TypedValue, _Mapping]] = ...) -> None: ...
    FIELDS_FIELD_NUMBER: _ClassVar[int]
    fields: _containers.MessageMap[str, TypedValue]
    def __init__(self, fields: _Optional[_Mapping[str, TypedValue]] = ...) -> None: ...

class ObjectStoreSchema(_message.Message):
    __slots__ = ()
    INDEXES_FIELD_NUMBER: _ClassVar[int]
    COLUMNS_FIELD_NUMBER: _ClassVar[int]
    indexes: _containers.RepeatedCompositeFieldContainer[IndexSchema]
    columns: _containers.RepeatedCompositeFieldContainer[ColumnDef]
    def __init__(self, indexes: _Optional[_Iterable[_Union[IndexSchema, _Mapping]]] = ..., columns: _Optional[_Iterable[_Union[ColumnDef, _Mapping]]] = ...) -> None: ...

class IndexSchema(_message.Message):
    __slots__ = ()
    NAME_FIELD_NUMBER: _ClassVar[int]
    KEY_PATH_FIELD_NUMBER: _ClassVar[int]
    UNIQUE_FIELD_NUMBER: _ClassVar[int]
    name: str
    key_path: _containers.RepeatedScalarFieldContainer[str]
    unique: bool
    def __init__(self, name: _Optional[str] = ..., key_path: _Optional[_Iterable[str]] = ..., unique: _Optional[bool] = ...) -> None: ...

class ColumnDef(_message.Message):
    __slots__ = ()
    NAME_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    PRIMARY_KEY_FIELD_NUMBER: _ClassVar[int]
    NOT_NULL_FIELD_NUMBER: _ClassVar[int]
    UNIQUE_FIELD_NUMBER: _ClassVar[int]
    name: str
    type: int
    primary_key: bool
    not_null: bool
    unique: bool
    def __init__(self, name: _Optional[str] = ..., type: _Optional[int] = ..., primary_key: _Optional[bool] = ..., not_null: _Optional[bool] = ..., unique: _Optional[bool] = ...) -> None: ...

class KeyValue(_message.Message):
    __slots__ = ()
    SCALAR_FIELD_NUMBER: _ClassVar[int]
    ARRAY_FIELD_NUMBER: _ClassVar[int]
    scalar: TypedValue
    array: KeyValueArray
    def __init__(self, scalar: _Optional[_Union[TypedValue, _Mapping]] = ..., array: _Optional[_Union[KeyValueArray, _Mapping]] = ...) -> None: ...

class KeyValueArray(_message.Message):
    __slots__ = ()
    ELEMENTS_FIELD_NUMBER: _ClassVar[int]
    elements: _containers.RepeatedCompositeFieldContainer[KeyValue]
    def __init__(self, elements: _Optional[_Iterable[_Union[KeyValue, _Mapping]]] = ...) -> None: ...

class KeyRange(_message.Message):
    __slots__ = ()
    LOWER_FIELD_NUMBER: _ClassVar[int]
    UPPER_FIELD_NUMBER: _ClassVar[int]
    LOWER_OPEN_FIELD_NUMBER: _ClassVar[int]
    UPPER_OPEN_FIELD_NUMBER: _ClassVar[int]
    lower: KeyValue
    upper: KeyValue
    lower_open: bool
    upper_open: bool
    def __init__(self, lower: _Optional[_Union[KeyValue, _Mapping]] = ..., upper: _Optional[_Union[KeyValue, _Mapping]] = ..., lower_open: _Optional[bool] = ..., upper_open: _Optional[bool] = ...) -> None: ...

class IndexedDBQuery(_message.Message):
    __slots__ = ()
    KEY_FIELD_NUMBER: _ClassVar[int]
    RANGE_FIELD_NUMBER: _ClassVar[int]
    key: KeyValue
    range: KeyRange
    def __init__(self, key: _Optional[_Union[KeyValue, _Mapping]] = ..., range: _Optional[_Union[KeyRange, _Mapping]] = ...) -> None: ...

class RecordRequest(_message.Message):
    __slots__ = ()
    STORE_FIELD_NUMBER: _ClassVar[int]
    RECORD_FIELD_NUMBER: _ClassVar[int]
    store: str
    record: Record
    def __init__(self, store: _Optional[str] = ..., record: _Optional[_Union[Record, _Mapping]] = ...) -> None: ...

class RecordResponse(_message.Message):
    __slots__ = ()
    RECORD_FIELD_NUMBER: _ClassVar[int]
    record: Record
    def __init__(self, record: _Optional[_Union[Record, _Mapping]] = ...) -> None: ...

class RecordsResponse(_message.Message):
    __slots__ = ()
    RECORDS_FIELD_NUMBER: _ClassVar[int]
    records: _containers.RepeatedCompositeFieldContainer[Record]
    def __init__(self, records: _Optional[_Iterable[_Union[Record, _Mapping]]] = ...) -> None: ...

class KeysResponse(_message.Message):
    __slots__ = ()
    KEYS_FIELD_NUMBER: _ClassVar[int]
    keys: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, keys: _Optional[_Iterable[str]] = ...) -> None: ...

class ObjectStoreRequest(_message.Message):
    __slots__ = ()
    STORE_FIELD_NUMBER: _ClassVar[int]
    ID_FIELD_NUMBER: _ClassVar[int]
    store: str
    id: str
    def __init__(self, store: _Optional[str] = ..., id: _Optional[str] = ...) -> None: ...

class ObjectStoreNameRequest(_message.Message):
    __slots__ = ()
    STORE_FIELD_NUMBER: _ClassVar[int]
    store: str
    def __init__(self, store: _Optional[str] = ...) -> None: ...

class ObjectStoreRangeRequest(_message.Message):
    __slots__ = ()
    STORE_FIELD_NUMBER: _ClassVar[int]
    QUERY_FIELD_NUMBER: _ClassVar[int]
    COUNT_FIELD_NUMBER: _ClassVar[int]
    store: str
    query: IndexedDBQuery
    count: int
    def __init__(self, store: _Optional[str] = ..., query: _Optional[_Union[IndexedDBQuery, _Mapping]] = ..., count: _Optional[int] = ...) -> None: ...

class CreateObjectStoreRequest(_message.Message):
    __slots__ = ()
    NAME_FIELD_NUMBER: _ClassVar[int]
    SCHEMA_FIELD_NUMBER: _ClassVar[int]
    name: str
    schema: ObjectStoreSchema
    def __init__(self, name: _Optional[str] = ..., schema: _Optional[_Union[ObjectStoreSchema, _Mapping]] = ...) -> None: ...

class DeleteObjectStoreRequest(_message.Message):
    __slots__ = ()
    NAME_FIELD_NUMBER: _ClassVar[int]
    name: str
    def __init__(self, name: _Optional[str] = ...) -> None: ...

class CreateIndexRequest(_message.Message):
    __slots__ = ()
    STORE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    KEY_PATH_FIELD_NUMBER: _ClassVar[int]
    UNIQUE_FIELD_NUMBER: _ClassVar[int]
    store: str
    name: str
    key_path: _containers.RepeatedScalarFieldContainer[str]
    unique: bool
    def __init__(self, store: _Optional[str] = ..., name: _Optional[str] = ..., key_path: _Optional[_Iterable[str]] = ..., unique: _Optional[bool] = ...) -> None: ...

class DeleteIndexRequest(_message.Message):
    __slots__ = ()
    STORE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    store: str
    name: str
    def __init__(self, store: _Optional[str] = ..., name: _Optional[str] = ...) -> None: ...

class IndexQueryRequest(_message.Message):
    __slots__ = ()
    STORE_FIELD_NUMBER: _ClassVar[int]
    INDEX_FIELD_NUMBER: _ClassVar[int]
    QUERY_FIELD_NUMBER: _ClassVar[int]
    COUNT_FIELD_NUMBER: _ClassVar[int]
    store: str
    index: str
    query: IndexedDBQuery
    count: int
    def __init__(self, store: _Optional[str] = ..., index: _Optional[str] = ..., query: _Optional[_Union[IndexedDBQuery, _Mapping]] = ..., count: _Optional[int] = ...) -> None: ...

class CountResponse(_message.Message):
    __slots__ = ()
    COUNT_FIELD_NUMBER: _ClassVar[int]
    count: int
    def __init__(self, count: _Optional[int] = ...) -> None: ...

class AcquireLockRequest(_message.Message):
    __slots__ = ()
    KEY_FIELD_NUMBER: _ClassVar[int]
    HOLDER_FIELD_NUMBER: _ClassVar[int]
    TTL_MS_FIELD_NUMBER: _ClassVar[int]
    key: str
    holder: str
    ttl_ms: int
    def __init__(self, key: _Optional[str] = ..., holder: _Optional[str] = ..., ttl_ms: _Optional[int] = ...) -> None: ...

class AcquireLockResponse(_message.Message):
    __slots__ = ()
    ACQUIRED_FIELD_NUMBER: _ClassVar[int]
    HOLDER_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    FENCING_TOKEN_FIELD_NUMBER: _ClassVar[int]
    acquired: bool
    holder: str
    expires_at: _timestamp_pb2.Timestamp
    fencing_token: int
    def __init__(self, acquired: _Optional[bool] = ..., holder: _Optional[str] = ..., expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., fencing_token: _Optional[int] = ...) -> None: ...

class ReleaseLockRequest(_message.Message):
    __slots__ = ()
    KEY_FIELD_NUMBER: _ClassVar[int]
    HOLDER_FIELD_NUMBER: _ClassVar[int]
    key: str
    holder: str
    def __init__(self, key: _Optional[str] = ..., holder: _Optional[str] = ...) -> None: ...

class OpenCursorRequest(_message.Message):
    __slots__ = ()
    STORE_FIELD_NUMBER: _ClassVar[int]
    INDEX_FIELD_NUMBER: _ClassVar[int]
    QUERY_FIELD_NUMBER: _ClassVar[int]
    DIRECTION_FIELD_NUMBER: _ClassVar[int]
    KEYS_ONLY_FIELD_NUMBER: _ClassVar[int]
    store: str
    index: str
    query: IndexedDBQuery
    direction: CursorDirection
    keys_only: bool
    def __init__(self, store: _Optional[str] = ..., index: _Optional[str] = ..., query: _Optional[_Union[IndexedDBQuery, _Mapping]] = ..., direction: _Optional[_Union[CursorDirection, str]] = ..., keys_only: _Optional[bool] = ...) -> None: ...

class CursorKeyTarget(_message.Message):
    __slots__ = ()
    KEY_FIELD_NUMBER: _ClassVar[int]
    key: KeyValue
    def __init__(self, key: _Optional[_Union[KeyValue, _Mapping]] = ...) -> None: ...

class CursorCommand(_message.Message):
    __slots__ = ()
    NEXT_FIELD_NUMBER: _ClassVar[int]
    CONTINUE_TO_KEY_FIELD_NUMBER: _ClassVar[int]
    ADVANCE_FIELD_NUMBER: _ClassVar[int]
    UPDATE_FIELD_NUMBER: _ClassVar[int]
    DELETE_FIELD_NUMBER: _ClassVar[int]
    CLOSE_FIELD_NUMBER: _ClassVar[int]
    next: bool
    continue_to_key: CursorKeyTarget
    advance: int
    update: Record
    delete: bool
    close: bool
    def __init__(self, next: _Optional[bool] = ..., continue_to_key: _Optional[_Union[CursorKeyTarget, _Mapping]] = ..., advance: _Optional[int] = ..., update: _Optional[_Union[Record, _Mapping]] = ..., delete: _Optional[bool] = ..., close: _Optional[bool] = ...) -> None: ...

class CursorClientMessage(_message.Message):
    __slots__ = ()
    OPEN_FIELD_NUMBER: _ClassVar[int]
    COMMAND_FIELD_NUMBER: _ClassVar[int]
    open: OpenCursorRequest
    command: CursorCommand
    def __init__(self, open: _Optional[_Union[OpenCursorRequest, _Mapping]] = ..., command: _Optional[_Union[CursorCommand, _Mapping]] = ...) -> None: ...

class CursorEntry(_message.Message):
    __slots__ = ()
    KEY_FIELD_NUMBER: _ClassVar[int]
    PRIMARY_KEY_FIELD_NUMBER: _ClassVar[int]
    RECORD_FIELD_NUMBER: _ClassVar[int]
    key: KeyValue
    primary_key: str
    record: Record
    def __init__(self, key: _Optional[_Union[KeyValue, _Mapping]] = ..., primary_key: _Optional[str] = ..., record: _Optional[_Union[Record, _Mapping]] = ...) -> None: ...

class CursorResponse(_message.Message):
    __slots__ = ()
    ENTRY_FIELD_NUMBER: _ClassVar[int]
    DONE_FIELD_NUMBER: _ClassVar[int]
    entry: CursorEntry
    done: bool
    def __init__(self, entry: _Optional[_Union[CursorEntry, _Mapping]] = ..., done: _Optional[bool] = ...) -> None: ...

class DeleteResponse(_message.Message):
    __slots__ = ()
    DELETED_FIELD_NUMBER: _ClassVar[int]
    deleted: int
    def __init__(self, deleted: _Optional[int] = ...) -> None: ...

class KeyResponse(_message.Message):
    __slots__ = ()
    KEY_FIELD_NUMBER: _ClassVar[int]
    key: str
    def __init__(self, key: _Optional[str] = ...) -> None: ...

class BeginTransactionRequest(_message.Message):
    __slots__ = ()
    STORES_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    DURABILITY_HINT_FIELD_NUMBER: _ClassVar[int]
    stores: _containers.RepeatedScalarFieldContainer[str]
    mode: TransactionMode
    durability_hint: TransactionDurabilityHint
    def __init__(self, stores: _Optional[_Iterable[str]] = ..., mode: _Optional[_Union[TransactionMode, str]] = ..., durability_hint: _Optional[_Union[TransactionDurabilityHint, str]] = ...) -> None: ...

class TransactionBeginResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class TransactionCommitRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class TransactionCommitResponse(_message.Message):
    __slots__ = ()
    ERROR_FIELD_NUMBER: _ClassVar[int]
    error: _status_pb2.Status
    def __init__(self, error: _Optional[_Union[_status_pb2.Status, _Mapping]] = ...) -> None: ...

class TransactionAbortRequest(_message.Message):
    __slots__ = ()
    REASON_FIELD_NUMBER: _ClassVar[int]
    reason: str
    def __init__(self, reason: _Optional[str] = ...) -> None: ...

class TransactionAbortResponse(_message.Message):
    __slots__ = ()
    ERROR_FIELD_NUMBER: _ClassVar[int]
    error: _status_pb2.Status
    def __init__(self, error: _Optional[_Union[_status_pb2.Status, _Mapping]] = ...) -> None: ...

class TransactionOperation(_message.Message):
    __slots__ = ()
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    GET_FIELD_NUMBER: _ClassVar[int]
    GET_KEY_FIELD_NUMBER: _ClassVar[int]
    ADD_FIELD_NUMBER: _ClassVar[int]
    PUT_FIELD_NUMBER: _ClassVar[int]
    DELETE_FIELD_NUMBER: _ClassVar[int]
    CLEAR_FIELD_NUMBER: _ClassVar[int]
    GET_ALL_FIELD_NUMBER: _ClassVar[int]
    GET_ALL_KEYS_FIELD_NUMBER: _ClassVar[int]
    COUNT_FIELD_NUMBER: _ClassVar[int]
    DELETE_RANGE_FIELD_NUMBER: _ClassVar[int]
    INDEX_GET_FIELD_NUMBER: _ClassVar[int]
    INDEX_GET_KEY_FIELD_NUMBER: _ClassVar[int]
    INDEX_GET_ALL_FIELD_NUMBER: _ClassVar[int]
    INDEX_GET_ALL_KEYS_FIELD_NUMBER: _ClassVar[int]
    INDEX_COUNT_FIELD_NUMBER: _ClassVar[int]
    INDEX_DELETE_FIELD_NUMBER: _ClassVar[int]
    request_id: int
    get: ObjectStoreRequest
    get_key: ObjectStoreRequest
    add: RecordRequest
    put: RecordRequest
    delete: ObjectStoreRequest
    clear: ObjectStoreNameRequest
    get_all: ObjectStoreRangeRequest
    get_all_keys: ObjectStoreRangeRequest
    count: ObjectStoreRangeRequest
    delete_range: ObjectStoreRangeRequest
    index_get: IndexQueryRequest
    index_get_key: IndexQueryRequest
    index_get_all: IndexQueryRequest
    index_get_all_keys: IndexQueryRequest
    index_count: IndexQueryRequest
    index_delete: IndexQueryRequest
    def __init__(self, request_id: _Optional[int] = ..., get: _Optional[_Union[ObjectStoreRequest, _Mapping]] = ..., get_key: _Optional[_Union[ObjectStoreRequest, _Mapping]] = ..., add: _Optional[_Union[RecordRequest, _Mapping]] = ..., put: _Optional[_Union[RecordRequest, _Mapping]] = ..., delete: _Optional[_Union[ObjectStoreRequest, _Mapping]] = ..., clear: _Optional[_Union[ObjectStoreNameRequest, _Mapping]] = ..., get_all: _Optional[_Union[ObjectStoreRangeRequest, _Mapping]] = ..., get_all_keys: _Optional[_Union[ObjectStoreRangeRequest, _Mapping]] = ..., count: _Optional[_Union[ObjectStoreRangeRequest, _Mapping]] = ..., delete_range: _Optional[_Union[ObjectStoreRangeRequest, _Mapping]] = ..., index_get: _Optional[_Union[IndexQueryRequest, _Mapping]] = ..., index_get_key: _Optional[_Union[IndexQueryRequest, _Mapping]] = ..., index_get_all: _Optional[_Union[IndexQueryRequest, _Mapping]] = ..., index_get_all_keys: _Optional[_Union[IndexQueryRequest, _Mapping]] = ..., index_count: _Optional[_Union[IndexQueryRequest, _Mapping]] = ..., index_delete: _Optional[_Union[IndexQueryRequest, _Mapping]] = ...) -> None: ...

class TransactionOperationResponse(_message.Message):
    __slots__ = ()
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    EMPTY_FIELD_NUMBER: _ClassVar[int]
    RECORD_FIELD_NUMBER: _ClassVar[int]
    RECORDS_FIELD_NUMBER: _ClassVar[int]
    KEY_FIELD_NUMBER: _ClassVar[int]
    KEYS_FIELD_NUMBER: _ClassVar[int]
    COUNT_FIELD_NUMBER: _ClassVar[int]
    DELETE_FIELD_NUMBER: _ClassVar[int]
    request_id: int
    error: _status_pb2.Status
    empty: _empty_pb2.Empty
    record: RecordResponse
    records: RecordsResponse
    key: KeyResponse
    keys: KeysResponse
    count: CountResponse
    delete: DeleteResponse
    def __init__(self, request_id: _Optional[int] = ..., error: _Optional[_Union[_status_pb2.Status, _Mapping]] = ..., empty: _Optional[_Union[_empty_pb2.Empty, _Mapping]] = ..., record: _Optional[_Union[RecordResponse, _Mapping]] = ..., records: _Optional[_Union[RecordsResponse, _Mapping]] = ..., key: _Optional[_Union[KeyResponse, _Mapping]] = ..., keys: _Optional[_Union[KeysResponse, _Mapping]] = ..., count: _Optional[_Union[CountResponse, _Mapping]] = ..., delete: _Optional[_Union[DeleteResponse, _Mapping]] = ...) -> None: ...

class TransactionClientMessage(_message.Message):
    __slots__ = ()
    BEGIN_FIELD_NUMBER: _ClassVar[int]
    OPERATION_FIELD_NUMBER: _ClassVar[int]
    COMMIT_FIELD_NUMBER: _ClassVar[int]
    ABORT_FIELD_NUMBER: _ClassVar[int]
    begin: BeginTransactionRequest
    operation: TransactionOperation
    commit: TransactionCommitRequest
    abort: TransactionAbortRequest
    def __init__(self, begin: _Optional[_Union[BeginTransactionRequest, _Mapping]] = ..., operation: _Optional[_Union[TransactionOperation, _Mapping]] = ..., commit: _Optional[_Union[TransactionCommitRequest, _Mapping]] = ..., abort: _Optional[_Union[TransactionAbortRequest, _Mapping]] = ...) -> None: ...

class TransactionServerMessage(_message.Message):
    __slots__ = ()
    BEGIN_FIELD_NUMBER: _ClassVar[int]
    OPERATION_FIELD_NUMBER: _ClassVar[int]
    COMMIT_FIELD_NUMBER: _ClassVar[int]
    ABORT_FIELD_NUMBER: _ClassVar[int]
    begin: TransactionBeginResponse
    operation: TransactionOperationResponse
    commit: TransactionCommitResponse
    abort: TransactionAbortResponse
    def __init__(self, begin: _Optional[_Union[TransactionBeginResponse, _Mapping]] = ..., operation: _Optional[_Union[TransactionOperationResponse, _Mapping]] = ..., commit: _Optional[_Union[TransactionCommitResponse, _Mapping]] = ..., abort: _Optional[_Union[TransactionAbortResponse, _Mapping]] = ...) -> None: ...
