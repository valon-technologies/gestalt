from . import annotations_pb2 as _annotations_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class AuthorizeRequest(_message.Message):
    __slots__ = ()
    RESPONSE_TYPE_FIELD_NUMBER: _ClassVar[int]
    CLIENT_ID_FIELD_NUMBER: _ClassVar[int]
    REDIRECT_URI_FIELD_NUMBER: _ClassVar[int]
    SCOPE_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    response_type: str
    client_id: str
    redirect_uri: str
    scope: str
    state: str
    def __init__(self, response_type: _Optional[str] = ..., client_id: _Optional[str] = ..., redirect_uri: _Optional[str] = ..., scope: _Optional[str] = ..., state: _Optional[str] = ...) -> None: ...

class AuthorizeResponse(_message.Message):
    __slots__ = ()
    REDIRECT_URI_FIELD_NUMBER: _ClassVar[int]
    redirect_uri: str
    def __init__(self, redirect_uri: _Optional[str] = ...) -> None: ...

class TokenRequest(_message.Message):
    __slots__ = ()
    GRANT_TYPE_FIELD_NUMBER: _ClassVar[int]
    CODE_FIELD_NUMBER: _ClassVar[int]
    REDIRECT_URI_FIELD_NUMBER: _ClassVar[int]
    CLIENT_ID_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    SCOPE_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_TOKEN_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_TOKEN_TYPE_FIELD_NUMBER: _ClassVar[int]
    grant_type: str
    code: str
    redirect_uri: str
    client_id: str
    state: str
    scope: str
    subject_token: str
    subject_token_type: str
    def __init__(self, grant_type: _Optional[str] = ..., code: _Optional[str] = ..., redirect_uri: _Optional[str] = ..., client_id: _Optional[str] = ..., state: _Optional[str] = ..., scope: _Optional[str] = ..., subject_token: _Optional[str] = ..., subject_token_type: _Optional[str] = ...) -> None: ...

class TokenResponse(_message.Message):
    __slots__ = ()
    ACCESS_TOKEN_FIELD_NUMBER: _ClassVar[int]
    TOKEN_TYPE_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_IN_FIELD_NUMBER: _ClassVar[int]
    REFRESH_TOKEN_FIELD_NUMBER: _ClassVar[int]
    SCOPE_FIELD_NUMBER: _ClassVar[int]
    GRANT_ID_FIELD_NUMBER: _ClassVar[int]
    access_token: str
    token_type: str
    expires_in: int
    refresh_token: str
    scope: str
    grant_id: str
    def __init__(self, access_token: _Optional[str] = ..., token_type: _Optional[str] = ..., expires_in: _Optional[int] = ..., refresh_token: _Optional[str] = ..., scope: _Optional[str] = ..., grant_id: _Optional[str] = ...) -> None: ...

class IntrospectRequest(_message.Message):
    __slots__ = ()
    TOKEN_FIELD_NUMBER: _ClassVar[int]
    TOKEN_TYPE_HINT_FIELD_NUMBER: _ClassVar[int]
    token: str
    token_type_hint: str
    def __init__(self, token: _Optional[str] = ..., token_type_hint: _Optional[str] = ...) -> None: ...

class IntrospectResponse(_message.Message):
    __slots__ = ()
    ACTIVE_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    SCOPE_FIELD_NUMBER: _ClassVar[int]
    CLIENT_ID_FIELD_NUMBER: _ClassVar[int]
    AUDIENCE_FIELD_NUMBER: _ClassVar[int]
    active: bool
    subject: str
    scope: str
    client_id: str
    audience: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, active: _Optional[bool] = ..., subject: _Optional[str] = ..., scope: _Optional[str] = ..., client_id: _Optional[str] = ..., audience: _Optional[_Iterable[str]] = ...) -> None: ...

class ListGrantsRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListGrantsResponse(_message.Message):
    __slots__ = ()
    GRANT_IDS_FIELD_NUMBER: _ClassVar[int]
    grant_ids: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, grant_ids: _Optional[_Iterable[str]] = ...) -> None: ...

class GetGrantRequest(_message.Message):
    __slots__ = ()
    GRANT_ID_FIELD_NUMBER: _ClassVar[int]
    grant_id: str
    def __init__(self, grant_id: _Optional[str] = ...) -> None: ...

class GrantScope(_message.Message):
    __slots__ = ()
    SCOPE_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    scope: str
    resource: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, scope: _Optional[str] = ..., resource: _Optional[_Iterable[str]] = ...) -> None: ...

class GetGrantResponse(_message.Message):
    __slots__ = ()
    SCOPES_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    scopes: _containers.RepeatedCompositeFieldContainer[GrantScope]
    created_at: int
    expires_at: int
    def __init__(self, scopes: _Optional[_Iterable[_Union[GrantScope, _Mapping]]] = ..., created_at: _Optional[int] = ..., expires_at: _Optional[int] = ...) -> None: ...

class RevokeGrantRequest(_message.Message):
    __slots__ = ()
    GRANT_ID_FIELD_NUMBER: _ClassVar[int]
    grant_id: str
    def __init__(self, grant_id: _Optional[str] = ...) -> None: ...

class RevokeGrantResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...
