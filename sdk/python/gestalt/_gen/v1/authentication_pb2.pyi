from google.protobuf import empty_pb2 as _empty_pb2
from . import annotations_pb2 as _annotations_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class AuthenticatedUser(_message.Message):
    __slots__ = ()
    class ClaimsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    EMAIL_FIELD_NUMBER: _ClassVar[int]
    EMAIL_VERIFIED_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_NAME_FIELD_NUMBER: _ClassVar[int]
    AVATAR_URL_FIELD_NUMBER: _ClassVar[int]
    CLAIMS_FIELD_NUMBER: _ClassVar[int]
    subject: str
    email: str
    email_verified: bool
    display_name: str
    avatar_url: str
    claims: _containers.ScalarMap[str, str]
    def __init__(self, subject: _Optional[str] = ..., email: _Optional[str] = ..., email_verified: _Optional[bool] = ..., display_name: _Optional[str] = ..., avatar_url: _Optional[str] = ..., claims: _Optional[_Mapping[str, str]] = ...) -> None: ...

class BeginLoginRequest(_message.Message):
    __slots__ = ()
    class OptionsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    CALLBACK_URL_FIELD_NUMBER: _ClassVar[int]
    HOST_STATE_FIELD_NUMBER: _ClassVar[int]
    SCOPES_FIELD_NUMBER: _ClassVar[int]
    OPTIONS_FIELD_NUMBER: _ClassVar[int]
    callback_url: str
    host_state: str
    scopes: _containers.RepeatedScalarFieldContainer[str]
    options: _containers.ScalarMap[str, str]
    def __init__(self, callback_url: _Optional[str] = ..., host_state: _Optional[str] = ..., scopes: _Optional[_Iterable[str]] = ..., options: _Optional[_Mapping[str, str]] = ...) -> None: ...

class BeginLoginResponse(_message.Message):
    __slots__ = ()
    AUTHORIZATION_URL_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_STATE_FIELD_NUMBER: _ClassVar[int]
    authorization_url: str
    provider_state: bytes
    def __init__(self, authorization_url: _Optional[str] = ..., provider_state: _Optional[bytes] = ...) -> None: ...

class CompleteLoginRequest(_message.Message):
    __slots__ = ()
    class QueryEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    QUERY_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_STATE_FIELD_NUMBER: _ClassVar[int]
    CALLBACK_URL_FIELD_NUMBER: _ClassVar[int]
    query: _containers.ScalarMap[str, str]
    provider_state: bytes
    callback_url: str
    def __init__(self, query: _Optional[_Mapping[str, str]] = ..., provider_state: _Optional[bytes] = ..., callback_url: _Optional[str] = ...) -> None: ...

class ValidateExternalTokenRequest(_message.Message):
    __slots__ = ()
    TOKEN_FIELD_NUMBER: _ClassVar[int]
    token: str
    def __init__(self, token: _Optional[str] = ...) -> None: ...

class AuthSessionSettings(_message.Message):
    __slots__ = ()
    SESSION_TTL_SECONDS_FIELD_NUMBER: _ClassVar[int]
    session_ttl_seconds: int
    def __init__(self, session_ttl_seconds: _Optional[int] = ...) -> None: ...
