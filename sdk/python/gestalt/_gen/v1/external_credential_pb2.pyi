import datetime

from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from . import annotations_pb2 as _annotations_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ExternalCredential(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    INSTANCE_FIELD_NUMBER: _ClassVar[int]
    ACCESS_TOKEN_FIELD_NUMBER: _ClassVar[int]
    REFRESH_TOKEN_FIELD_NUMBER: _ClassVar[int]
    SCOPES_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_REFRESHED_AT_FIELD_NUMBER: _ClassVar[int]
    REFRESH_ERROR_COUNT_FIELD_NUMBER: _ClassVar[int]
    METADATA_JSON_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_ID_FIELD_NUMBER: _ClassVar[int]
    id: str
    subject_id: str
    instance: str
    access_token: str
    refresh_token: str
    scopes: str
    expires_at: _timestamp_pb2.Timestamp
    last_refreshed_at: _timestamp_pb2.Timestamp
    refresh_error_count: int
    metadata_json: str
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    connection_id: str
    def __init__(self, id: _Optional[str] = ..., subject_id: _Optional[str] = ..., instance: _Optional[str] = ..., access_token: _Optional[str] = ..., refresh_token: _Optional[str] = ..., scopes: _Optional[str] = ..., expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., last_refreshed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., refresh_error_count: _Optional[int] = ..., metadata_json: _Optional[str] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., connection_id: _Optional[str] = ...) -> None: ...

class ExternalCredentialLookup(_message.Message):
    __slots__ = ()
    SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    INSTANCE_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_ID_FIELD_NUMBER: _ClassVar[int]
    subject_id: str
    instance: str
    connection_id: str
    def __init__(self, subject_id: _Optional[str] = ..., instance: _Optional[str] = ..., connection_id: _Optional[str] = ...) -> None: ...

class UpsertExternalCredentialRequest(_message.Message):
    __slots__ = ()
    CREDENTIAL_FIELD_NUMBER: _ClassVar[int]
    PRESERVE_TIMESTAMPS_FIELD_NUMBER: _ClassVar[int]
    credential: ExternalCredential
    preserve_timestamps: bool
    def __init__(self, credential: _Optional[_Union[ExternalCredential, _Mapping]] = ..., preserve_timestamps: _Optional[bool] = ...) -> None: ...

class GetExternalCredentialRequest(_message.Message):
    __slots__ = ()
    LOOKUP_FIELD_NUMBER: _ClassVar[int]
    lookup: ExternalCredentialLookup
    def __init__(self, lookup: _Optional[_Union[ExternalCredentialLookup, _Mapping]] = ...) -> None: ...

class ListExternalCredentialsRequest(_message.Message):
    __slots__ = ()
    SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    INSTANCE_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_ID_FIELD_NUMBER: _ClassVar[int]
    subject_id: str
    instance: str
    connection_id: str
    def __init__(self, subject_id: _Optional[str] = ..., instance: _Optional[str] = ..., connection_id: _Optional[str] = ...) -> None: ...

class ListExternalCredentialsResponse(_message.Message):
    __slots__ = ()
    CREDENTIALS_FIELD_NUMBER: _ClassVar[int]
    credentials: _containers.RepeatedCompositeFieldContainer[ExternalCredential]
    def __init__(self, credentials: _Optional[_Iterable[_Union[ExternalCredential, _Mapping]]] = ...) -> None: ...

class DeleteExternalCredentialRequest(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    id: str
    def __init__(self, id: _Optional[str] = ...) -> None: ...

class ExternalCredentialTokenExchangeDriver(_message.Message):
    __slots__ = ()
    class ParamsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    TYPE_FIELD_NUMBER: _ClassVar[int]
    TARGET_PRINCIPAL_FIELD_NUMBER: _ClassVar[int]
    SCOPES_FIELD_NUMBER: _ClassVar[int]
    LIFETIME_SECONDS_FIELD_NUMBER: _ClassVar[int]
    ENDPOINT_FIELD_NUMBER: _ClassVar[int]
    PARAMS_FIELD_NUMBER: _ClassVar[int]
    type: str
    target_principal: str
    scopes: _containers.RepeatedScalarFieldContainer[str]
    lifetime_seconds: int
    endpoint: str
    params: _containers.ScalarMap[str, str]
    def __init__(self, type: _Optional[str] = ..., target_principal: _Optional[str] = ..., scopes: _Optional[_Iterable[str]] = ..., lifetime_seconds: _Optional[int] = ..., endpoint: _Optional[str] = ..., params: _Optional[_Mapping[str, str]] = ...) -> None: ...

class ExternalCredentialAuthConfig(_message.Message):
    __slots__ = ()
    class TokenParamsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    class RefreshParamsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    TYPE_FIELD_NUMBER: _ClassVar[int]
    TOKEN_FIELD_NUMBER: _ClassVar[int]
    TOKEN_PREFIX_FIELD_NUMBER: _ClassVar[int]
    GRANT_TYPE_FIELD_NUMBER: _ClassVar[int]
    TOKEN_URL_FIELD_NUMBER: _ClassVar[int]
    CLIENT_ID_FIELD_NUMBER: _ClassVar[int]
    CLIENT_SECRET_FIELD_NUMBER: _ClassVar[int]
    CLIENT_AUTH_FIELD_NUMBER: _ClassVar[int]
    TOKEN_EXCHANGE_FIELD_NUMBER: _ClassVar[int]
    SCOPES_FIELD_NUMBER: _ClassVar[int]
    SCOPE_PARAM_FIELD_NUMBER: _ClassVar[int]
    SCOPE_SEPARATOR_FIELD_NUMBER: _ClassVar[int]
    TOKEN_PARAMS_FIELD_NUMBER: _ClassVar[int]
    REFRESH_PARAMS_FIELD_NUMBER: _ClassVar[int]
    ACCEPT_HEADER_FIELD_NUMBER: _ClassVar[int]
    ACCESS_TOKEN_PATH_FIELD_NUMBER: _ClassVar[int]
    TOKEN_EXCHANGE_DRIVERS_FIELD_NUMBER: _ClassVar[int]
    REFRESH_TOKEN_FIELD_NUMBER: _ClassVar[int]
    type: str
    token: str
    token_prefix: str
    grant_type: str
    token_url: str
    client_id: str
    client_secret: str
    client_auth: str
    token_exchange: str
    scopes: _containers.RepeatedScalarFieldContainer[str]
    scope_param: str
    scope_separator: str
    token_params: _containers.ScalarMap[str, str]
    refresh_params: _containers.ScalarMap[str, str]
    accept_header: str
    access_token_path: str
    token_exchange_drivers: _containers.RepeatedCompositeFieldContainer[ExternalCredentialTokenExchangeDriver]
    refresh_token: str
    def __init__(self, type: _Optional[str] = ..., token: _Optional[str] = ..., token_prefix: _Optional[str] = ..., grant_type: _Optional[str] = ..., token_url: _Optional[str] = ..., client_id: _Optional[str] = ..., client_secret: _Optional[str] = ..., client_auth: _Optional[str] = ..., token_exchange: _Optional[str] = ..., scopes: _Optional[_Iterable[str]] = ..., scope_param: _Optional[str] = ..., scope_separator: _Optional[str] = ..., token_params: _Optional[_Mapping[str, str]] = ..., refresh_params: _Optional[_Mapping[str, str]] = ..., accept_header: _Optional[str] = ..., access_token_path: _Optional[str] = ..., token_exchange_drivers: _Optional[_Iterable[_Union[ExternalCredentialTokenExchangeDriver, _Mapping]]] = ..., refresh_token: _Optional[str] = ...) -> None: ...

class ValidateExternalCredentialConfigRequest(_message.Message):
    __slots__ = ()
    class ConnectionParamsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_ID_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    AUTH_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_PARAMS_FIELD_NUMBER: _ClassVar[int]
    provider: str
    connection: str
    connection_id: str
    mode: str
    auth: ExternalCredentialAuthConfig
    connection_params: _containers.ScalarMap[str, str]
    def __init__(self, provider: _Optional[str] = ..., connection: _Optional[str] = ..., connection_id: _Optional[str] = ..., mode: _Optional[str] = ..., auth: _Optional[_Union[ExternalCredentialAuthConfig, _Mapping]] = ..., connection_params: _Optional[_Mapping[str, str]] = ...) -> None: ...

class ResolveExternalCredentialRequest(_message.Message):
    __slots__ = ()
    class ConnectionParamsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_ID_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    ACTOR_SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    INSTANCE_FIELD_NUMBER: _ClassVar[int]
    AUTH_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_PARAMS_FIELD_NUMBER: _ClassVar[int]
    provider: str
    connection: str
    connection_id: str
    mode: str
    credential_subject_id: str
    actor_subject_id: str
    instance: str
    auth: ExternalCredentialAuthConfig
    connection_params: _containers.ScalarMap[str, str]
    def __init__(self, provider: _Optional[str] = ..., connection: _Optional[str] = ..., connection_id: _Optional[str] = ..., mode: _Optional[str] = ..., credential_subject_id: _Optional[str] = ..., actor_subject_id: _Optional[str] = ..., instance: _Optional[str] = ..., auth: _Optional[_Union[ExternalCredentialAuthConfig, _Mapping]] = ..., connection_params: _Optional[_Mapping[str, str]] = ...) -> None: ...

class ResolveExternalCredentialResponse(_message.Message):
    __slots__ = ()
    class ParamsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    TOKEN_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    METADATA_JSON_FIELD_NUMBER: _ClassVar[int]
    PARAMS_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_FIELD_NUMBER: _ClassVar[int]
    token: str
    expires_at: _timestamp_pb2.Timestamp
    metadata_json: str
    params: _containers.ScalarMap[str, str]
    credential: ExternalCredential
    def __init__(self, token: _Optional[str] = ..., expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., metadata_json: _Optional[str] = ..., params: _Optional[_Mapping[str, str]] = ..., credential: _Optional[_Union[ExternalCredential, _Mapping]] = ...) -> None: ...

class ExternalCredentialTokenResponse(_message.Message):
    __slots__ = ()
    ACCESS_TOKEN_FIELD_NUMBER: _ClassVar[int]
    REFRESH_TOKEN_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_IN_FIELD_NUMBER: _ClassVar[int]
    TOKEN_TYPE_FIELD_NUMBER: _ClassVar[int]
    EXTRA_JSON_FIELD_NUMBER: _ClassVar[int]
    REFRESH_SOURCE_FIELD_NUMBER: _ClassVar[int]
    access_token: str
    refresh_token: str
    expires_in: int
    token_type: str
    extra_json: str
    refresh_source: str
    def __init__(self, access_token: _Optional[str] = ..., refresh_token: _Optional[str] = ..., expires_in: _Optional[int] = ..., token_type: _Optional[str] = ..., extra_json: _Optional[str] = ..., refresh_source: _Optional[str] = ...) -> None: ...

class ExchangeExternalCredentialRequest(_message.Message):
    __slots__ = ()
    class ConnectionParamsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_ID_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    ACTOR_SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    INSTANCE_FIELD_NUMBER: _ClassVar[int]
    AUTH_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_JSON_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_PARAMS_FIELD_NUMBER: _ClassVar[int]
    provider: str
    connection: str
    connection_id: str
    credential_subject_id: str
    actor_subject_id: str
    instance: str
    auth: ExternalCredentialAuthConfig
    credential_json: str
    connection_params: _containers.ScalarMap[str, str]
    def __init__(self, provider: _Optional[str] = ..., connection: _Optional[str] = ..., connection_id: _Optional[str] = ..., credential_subject_id: _Optional[str] = ..., actor_subject_id: _Optional[str] = ..., instance: _Optional[str] = ..., auth: _Optional[_Union[ExternalCredentialAuthConfig, _Mapping]] = ..., credential_json: _Optional[str] = ..., connection_params: _Optional[_Mapping[str, str]] = ...) -> None: ...

class ExchangeExternalCredentialResponse(_message.Message):
    __slots__ = ()
    TOKEN_RESPONSE_FIELD_NUMBER: _ClassVar[int]
    token_response: ExternalCredentialTokenResponse
    def __init__(self, token_response: _Optional[_Union[ExternalCredentialTokenResponse, _Mapping]] = ...) -> None: ...
