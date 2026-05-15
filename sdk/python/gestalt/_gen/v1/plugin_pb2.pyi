import datetime

from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ConnectionMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CONNECTION_MODE_UNSPECIFIED: _ClassVar[ConnectionMode]
    CONNECTION_MODE_NONE: _ClassVar[ConnectionMode]
    CONNECTION_MODE_USER: _ClassVar[ConnectionMode]
CONNECTION_MODE_UNSPECIFIED: ConnectionMode
CONNECTION_MODE_NONE: ConnectionMode
CONNECTION_MODE_USER: ConnectionMode

class CatalogParameter(_message.Message):
    __slots__ = ()
    NAME_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    REQUIRED_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_FIELD_NUMBER: _ClassVar[int]
    name: str
    type: str
    description: str
    required: bool
    default: _struct_pb2.Value
    def __init__(self, name: _Optional[str] = ..., type: _Optional[str] = ..., description: _Optional[str] = ..., required: _Optional[bool] = ..., default: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ...) -> None: ...

class OperationAnnotations(_message.Message):
    __slots__ = ()
    READ_ONLY_HINT_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENT_HINT_FIELD_NUMBER: _ClassVar[int]
    DESTRUCTIVE_HINT_FIELD_NUMBER: _ClassVar[int]
    OPEN_WORLD_HINT_FIELD_NUMBER: _ClassVar[int]
    read_only_hint: bool
    idempotent_hint: bool
    destructive_hint: bool
    open_world_hint: bool
    def __init__(self, read_only_hint: _Optional[bool] = ..., idempotent_hint: _Optional[bool] = ..., destructive_hint: _Optional[bool] = ..., open_world_hint: _Optional[bool] = ...) -> None: ...

class CatalogOperation(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    METHOD_FIELD_NUMBER: _ClassVar[int]
    TITLE_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    INPUT_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    ANNOTATIONS_FIELD_NUMBER: _ClassVar[int]
    PARAMETERS_FIELD_NUMBER: _ClassVar[int]
    REQUIRED_SCOPES_FIELD_NUMBER: _ClassVar[int]
    TAGS_FIELD_NUMBER: _ClassVar[int]
    READ_ONLY_FIELD_NUMBER: _ClassVar[int]
    VISIBLE_FIELD_NUMBER: _ClassVar[int]
    TRANSPORT_FIELD_NUMBER: _ClassVar[int]
    ALLOWED_ROLES_FIELD_NUMBER: _ClassVar[int]
    id: str
    method: str
    title: str
    description: str
    input_schema: str
    output_schema: str
    annotations: OperationAnnotations
    parameters: _containers.RepeatedCompositeFieldContainer[CatalogParameter]
    required_scopes: _containers.RepeatedScalarFieldContainer[str]
    tags: _containers.RepeatedScalarFieldContainer[str]
    read_only: bool
    visible: bool
    transport: str
    allowed_roles: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, id: _Optional[str] = ..., method: _Optional[str] = ..., title: _Optional[str] = ..., description: _Optional[str] = ..., input_schema: _Optional[str] = ..., output_schema: _Optional[str] = ..., annotations: _Optional[_Union[OperationAnnotations, _Mapping]] = ..., parameters: _Optional[_Iterable[_Union[CatalogParameter, _Mapping]]] = ..., required_scopes: _Optional[_Iterable[str]] = ..., tags: _Optional[_Iterable[str]] = ..., read_only: _Optional[bool] = ..., visible: _Optional[bool] = ..., transport: _Optional[str] = ..., allowed_roles: _Optional[_Iterable[str]] = ...) -> None: ...

class Catalog(_message.Message):
    __slots__ = ()
    NAME_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_NAME_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    ICON_SVG_FIELD_NUMBER: _ClassVar[int]
    OPERATIONS_FIELD_NUMBER: _ClassVar[int]
    name: str
    display_name: str
    description: str
    icon_svg: str
    operations: _containers.RepeatedCompositeFieldContainer[CatalogOperation]
    def __init__(self, name: _Optional[str] = ..., display_name: _Optional[str] = ..., description: _Optional[str] = ..., icon_svg: _Optional[str] = ..., operations: _Optional[_Iterable[_Union[CatalogOperation, _Mapping]]] = ...) -> None: ...

class ConnectionParamDef(_message.Message):
    __slots__ = ()
    REQUIRED_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_VALUE_FIELD_NUMBER: _ClassVar[int]
    FROM_FIELD_NUMBER: _ClassVar[int]
    FIELD_FIELD_NUMBER: _ClassVar[int]
    required: bool
    description: str
    default_value: str
    field: str
    def __init__(self, required: _Optional[bool] = ..., description: _Optional[str] = ..., default_value: _Optional[str] = ..., field: _Optional[str] = ..., **kwargs) -> None: ...

class ProviderMetadata(_message.Message):
    __slots__ = ()
    class ConnectionParamsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: ConnectionParamDef
        def __init__(self, key: _Optional[str] = ..., value: _Optional[_Union[ConnectionParamDef, _Mapping]] = ...) -> None: ...
    NAME_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_NAME_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_MODE_FIELD_NUMBER: _ClassVar[int]
    AUTH_TYPES_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_PARAMS_FIELD_NUMBER: _ClassVar[int]
    STATIC_CATALOG_FIELD_NUMBER: _ClassVar[int]
    SUPPORTS_SESSION_CATALOG_FIELD_NUMBER: _ClassVar[int]
    SUPPORTS_POST_CONNECT_FIELD_NUMBER: _ClassVar[int]
    MIN_PROTOCOL_VERSION_FIELD_NUMBER: _ClassVar[int]
    MAX_PROTOCOL_VERSION_FIELD_NUMBER: _ClassVar[int]
    name: str
    display_name: str
    description: str
    connection_mode: ConnectionMode
    auth_types: _containers.RepeatedScalarFieldContainer[str]
    connection_params: _containers.MessageMap[str, ConnectionParamDef]
    static_catalog: Catalog
    supports_session_catalog: bool
    supports_post_connect: bool
    min_protocol_version: int
    max_protocol_version: int
    def __init__(self, name: _Optional[str] = ..., display_name: _Optional[str] = ..., description: _Optional[str] = ..., connection_mode: _Optional[_Union[ConnectionMode, str]] = ..., auth_types: _Optional[_Iterable[str]] = ..., connection_params: _Optional[_Mapping[str, ConnectionParamDef]] = ..., static_catalog: _Optional[_Union[Catalog, _Mapping]] = ..., supports_session_catalog: _Optional[bool] = ..., supports_post_connect: _Optional[bool] = ..., min_protocol_version: _Optional[int] = ..., max_protocol_version: _Optional[int] = ...) -> None: ...

class OperationResult(_message.Message):
    __slots__ = ()
    STATUS_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    status: int
    body: str
    def __init__(self, status: _Optional[int] = ..., body: _Optional[str] = ...) -> None: ...

class PluginInvocationGrant(_message.Message):
    __slots__ = ()
    PLUGIN_FIELD_NUMBER: _ClassVar[int]
    OPERATIONS_FIELD_NUMBER: _ClassVar[int]
    SURFACES_FIELD_NUMBER: _ClassVar[int]
    ALL_OPERATIONS_FIELD_NUMBER: _ClassVar[int]
    plugin: str
    operations: _containers.RepeatedScalarFieldContainer[str]
    surfaces: _containers.RepeatedScalarFieldContainer[str]
    all_operations: bool
    def __init__(self, plugin: _Optional[str] = ..., operations: _Optional[_Iterable[str]] = ..., surfaces: _Optional[_Iterable[str]] = ..., all_operations: _Optional[bool] = ...) -> None: ...

class ExchangeInvocationTokenRequest(_message.Message):
    __slots__ = ()
    PARENT_INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    GRANTS_FIELD_NUMBER: _ClassVar[int]
    TTL_SECONDS_FIELD_NUMBER: _ClassVar[int]
    parent_invocation_token: str
    grants: _containers.RepeatedCompositeFieldContainer[PluginInvocationGrant]
    ttl_seconds: int
    def __init__(self, parent_invocation_token: _Optional[str] = ..., grants: _Optional[_Iterable[_Union[PluginInvocationGrant, _Mapping]]] = ..., ttl_seconds: _Optional[int] = ...) -> None: ...

class ExchangeInvocationTokenResponse(_message.Message):
    __slots__ = ()
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    invocation_token: str
    def __init__(self, invocation_token: _Optional[str] = ...) -> None: ...

class PluginInvokeRequest(_message.Message):
    __slots__ = ()
    PLUGIN_FIELD_NUMBER: _ClassVar[int]
    OPERATION_FIELD_NUMBER: _ClassVar[int]
    PARAMS_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_FIELD_NUMBER: _ClassVar[int]
    INSTANCE_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    plugin: str
    operation: str
    params: _struct_pb2.Struct
    connection: str
    instance: str
    invocation_token: str
    idempotency_key: str
    def __init__(self, plugin: _Optional[str] = ..., operation: _Optional[str] = ..., params: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., connection: _Optional[str] = ..., instance: _Optional[str] = ..., invocation_token: _Optional[str] = ..., idempotency_key: _Optional[str] = ...) -> None: ...

class PluginInvokeGraphQLRequest(_message.Message):
    __slots__ = ()
    PLUGIN_FIELD_NUMBER: _ClassVar[int]
    DOCUMENT_FIELD_NUMBER: _ClassVar[int]
    VARIABLES_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_FIELD_NUMBER: _ClassVar[int]
    INSTANCE_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    plugin: str
    document: str
    variables: _struct_pb2.Struct
    connection: str
    instance: str
    invocation_token: str
    idempotency_key: str
    def __init__(self, plugin: _Optional[str] = ..., document: _Optional[str] = ..., variables: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., connection: _Optional[str] = ..., instance: _Optional[str] = ..., invocation_token: _Optional[str] = ..., idempotency_key: _Optional[str] = ...) -> None: ...

class PostConnectCredential(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    INTEGRATION_FIELD_NUMBER: _ClassVar[int]
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
    CONNECTION_FIELD_NUMBER: _ClassVar[int]
    id: str
    subject_id: str
    integration: str
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
    connection: str
    def __init__(self, id: _Optional[str] = ..., subject_id: _Optional[str] = ..., integration: _Optional[str] = ..., instance: _Optional[str] = ..., access_token: _Optional[str] = ..., refresh_token: _Optional[str] = ..., scopes: _Optional[str] = ..., expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., last_refreshed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., refresh_error_count: _Optional[int] = ..., metadata_json: _Optional[str] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., connection: _Optional[str] = ...) -> None: ...

class SubjectContext(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_NAME_FIELD_NUMBER: _ClassVar[int]
    AUTH_SOURCE_FIELD_NUMBER: _ClassVar[int]
    EMAIL_FIELD_NUMBER: _ClassVar[int]
    id: str
    kind: str
    display_name: str
    auth_source: str
    email: str
    def __init__(self, id: _Optional[str] = ..., kind: _Optional[str] = ..., display_name: _Optional[str] = ..., auth_source: _Optional[str] = ..., email: _Optional[str] = ...) -> None: ...

class ExternalIdentityContext(_message.Message):
    __slots__ = ()
    TYPE_FIELD_NUMBER: _ClassVar[int]
    ID_FIELD_NUMBER: _ClassVar[int]
    type: str
    id: str
    def __init__(self, type: _Optional[str] = ..., id: _Optional[str] = ...) -> None: ...

class AgentSubjectContext(_message.Message):
    __slots__ = ()
    SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_KIND_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_NAME_FIELD_NUMBER: _ClassVar[int]
    AUTH_SOURCE_FIELD_NUMBER: _ClassVar[int]
    subject_id: str
    subject_kind: str
    credential_subject_id: str
    display_name: str
    auth_source: str
    def __init__(self, subject_id: _Optional[str] = ..., subject_kind: _Optional[str] = ..., credential_subject_id: _Optional[str] = ..., display_name: _Optional[str] = ..., auth_source: _Optional[str] = ...) -> None: ...

class AgentToolRef(_message.Message):
    __slots__ = ()
    PLUGIN_FIELD_NUMBER: _ClassVar[int]
    OPERATION_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_FIELD_NUMBER: _ClassVar[int]
    INSTANCE_FIELD_NUMBER: _ClassVar[int]
    TITLE_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    SYSTEM_FIELD_NUMBER: _ClassVar[int]
    RUN_AS_FIELD_NUMBER: _ClassVar[int]
    RUN_AS_EXTERNAL_IDENTITY_FIELD_NUMBER: _ClassVar[int]
    plugin: str
    operation: str
    connection: str
    instance: str
    title: str
    description: str
    system: str
    run_as: AgentSubjectContext
    run_as_external_identity: ExternalIdentityContext
    def __init__(self, plugin: _Optional[str] = ..., operation: _Optional[str] = ..., connection: _Optional[str] = ..., instance: _Optional[str] = ..., title: _Optional[str] = ..., description: _Optional[str] = ..., system: _Optional[str] = ..., run_as: _Optional[_Union[AgentSubjectContext, _Mapping]] = ..., run_as_external_identity: _Optional[_Union[ExternalIdentityContext, _Mapping]] = ...) -> None: ...

class StringList(_message.Message):
    __slots__ = ()
    VALUES_FIELD_NUMBER: _ClassVar[int]
    values: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, values: _Optional[_Iterable[str]] = ...) -> None: ...

class CredentialContext(_message.Message):
    __slots__ = ()
    MODE_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_FIELD_NUMBER: _ClassVar[int]
    INSTANCE_FIELD_NUMBER: _ClassVar[int]
    mode: str
    subject_id: str
    connection: str
    instance: str
    def __init__(self, mode: _Optional[str] = ..., subject_id: _Optional[str] = ..., connection: _Optional[str] = ..., instance: _Optional[str] = ...) -> None: ...

class AccessContext(_message.Message):
    __slots__ = ()
    POLICY_FIELD_NUMBER: _ClassVar[int]
    ROLE_FIELD_NUMBER: _ClassVar[int]
    policy: str
    role: str
    def __init__(self, policy: _Optional[str] = ..., role: _Optional[str] = ...) -> None: ...

class HostContext(_message.Message):
    __slots__ = ()
    PUBLIC_BASE_URL_FIELD_NUMBER: _ClassVar[int]
    public_base_url: str
    def __init__(self, public_base_url: _Optional[str] = ...) -> None: ...

class RequestContext(_message.Message):
    __slots__ = ()
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_FIELD_NUMBER: _ClassVar[int]
    ACCESS_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_FIELD_NUMBER: _ClassVar[int]
    HOST_FIELD_NUMBER: _ClassVar[int]
    AGENT_SUBJECT_FIELD_NUMBER: _ClassVar[int]
    AGENT_EXTERNAL_IDENTITY_FIELD_NUMBER: _ClassVar[int]
    EXTERNAL_IDENTITY_FIELD_NUMBER: _ClassVar[int]
    TOOL_REFS_FIELD_NUMBER: _ClassVar[int]
    TOOL_REFS_SET_FIELD_NUMBER: _ClassVar[int]
    subject: SubjectContext
    credential: CredentialContext
    access: AccessContext
    workflow: _struct_pb2.Struct
    host: HostContext
    agent_subject: SubjectContext
    agent_external_identity: ExternalIdentityContext
    external_identity: ExternalIdentityContext
    tool_refs: _containers.RepeatedCompositeFieldContainer[AgentToolRef]
    tool_refs_set: bool
    def __init__(self, subject: _Optional[_Union[SubjectContext, _Mapping]] = ..., credential: _Optional[_Union[CredentialContext, _Mapping]] = ..., access: _Optional[_Union[AccessContext, _Mapping]] = ..., workflow: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., host: _Optional[_Union[HostContext, _Mapping]] = ..., agent_subject: _Optional[_Union[SubjectContext, _Mapping]] = ..., agent_external_identity: _Optional[_Union[ExternalIdentityContext, _Mapping]] = ..., external_identity: _Optional[_Union[ExternalIdentityContext, _Mapping]] = ..., tool_refs: _Optional[_Iterable[_Union[AgentToolRef, _Mapping]]] = ..., tool_refs_set: _Optional[bool] = ...) -> None: ...

class HTTPSubjectRequest(_message.Message):
    __slots__ = ()
    class HeadersEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: StringList
        def __init__(self, key: _Optional[str] = ..., value: _Optional[_Union[StringList, _Mapping]] = ...) -> None: ...
    class QueryEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: StringList
        def __init__(self, key: _Optional[str] = ..., value: _Optional[_Union[StringList, _Mapping]] = ...) -> None: ...
    class VerifiedClaimsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    BINDING_FIELD_NUMBER: _ClassVar[int]
    METHOD_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    CONTENT_TYPE_FIELD_NUMBER: _ClassVar[int]
    HEADERS_FIELD_NUMBER: _ClassVar[int]
    QUERY_FIELD_NUMBER: _ClassVar[int]
    PARAMS_FIELD_NUMBER: _ClassVar[int]
    RAW_BODY_FIELD_NUMBER: _ClassVar[int]
    SECURITY_SCHEME_FIELD_NUMBER: _ClassVar[int]
    VERIFIED_SUBJECT_FIELD_NUMBER: _ClassVar[int]
    VERIFIED_CLAIMS_FIELD_NUMBER: _ClassVar[int]
    binding: str
    method: str
    path: str
    content_type: str
    headers: _containers.MessageMap[str, StringList]
    query: _containers.MessageMap[str, StringList]
    params: _struct_pb2.Struct
    raw_body: bytes
    security_scheme: str
    verified_subject: str
    verified_claims: _containers.ScalarMap[str, str]
    def __init__(self, binding: _Optional[str] = ..., method: _Optional[str] = ..., path: _Optional[str] = ..., content_type: _Optional[str] = ..., headers: _Optional[_Mapping[str, StringList]] = ..., query: _Optional[_Mapping[str, StringList]] = ..., params: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., raw_body: _Optional[bytes] = ..., security_scheme: _Optional[str] = ..., verified_subject: _Optional[str] = ..., verified_claims: _Optional[_Mapping[str, str]] = ...) -> None: ...

class ResolveHTTPSubjectRequest(_message.Message):
    __slots__ = ()
    REQUEST_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    request: HTTPSubjectRequest
    context: RequestContext
    def __init__(self, request: _Optional[_Union[HTTPSubjectRequest, _Mapping]] = ..., context: _Optional[_Union[RequestContext, _Mapping]] = ...) -> None: ...

class ResolveHTTPSubjectResponse(_message.Message):
    __slots__ = ()
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    REJECT_STATUS_FIELD_NUMBER: _ClassVar[int]
    REJECT_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    subject: SubjectContext
    reject_status: int
    reject_message: str
    def __init__(self, subject: _Optional[_Union[SubjectContext, _Mapping]] = ..., reject_status: _Optional[int] = ..., reject_message: _Optional[str] = ...) -> None: ...

class ExecuteRequest(_message.Message):
    __slots__ = ()
    class ConnectionParamsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    OPERATION_FIELD_NUMBER: _ClassVar[int]
    PARAMS_FIELD_NUMBER: _ClassVar[int]
    TOKEN_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_PARAMS_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    operation: str
    params: _struct_pb2.Struct
    token: str
    connection_params: _containers.ScalarMap[str, str]
    invocation_id: str
    context: RequestContext
    invocation_token: str
    idempotency_key: str
    def __init__(self, operation: _Optional[str] = ..., params: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., token: _Optional[str] = ..., connection_params: _Optional[_Mapping[str, str]] = ..., invocation_id: _Optional[str] = ..., context: _Optional[_Union[RequestContext, _Mapping]] = ..., invocation_token: _Optional[str] = ..., idempotency_key: _Optional[str] = ...) -> None: ...

class GetSessionCatalogRequest(_message.Message):
    __slots__ = ()
    class ConnectionParamsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    TOKEN_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_PARAMS_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    token: str
    connection_params: _containers.ScalarMap[str, str]
    invocation_id: str
    context: RequestContext
    def __init__(self, token: _Optional[str] = ..., connection_params: _Optional[_Mapping[str, str]] = ..., invocation_id: _Optional[str] = ..., context: _Optional[_Union[RequestContext, _Mapping]] = ...) -> None: ...

class GetSessionCatalogResponse(_message.Message):
    __slots__ = ()
    CATALOG_FIELD_NUMBER: _ClassVar[int]
    catalog: Catalog
    def __init__(self, catalog: _Optional[_Union[Catalog, _Mapping]] = ...) -> None: ...

class PostConnectRequest(_message.Message):
    __slots__ = ()
    TOKEN_FIELD_NUMBER: _ClassVar[int]
    token: PostConnectCredential
    def __init__(self, token: _Optional[_Union[PostConnectCredential, _Mapping]] = ...) -> None: ...

class PostConnectResponse(_message.Message):
    __slots__ = ()
    class MetadataEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    METADATA_FIELD_NUMBER: _ClassVar[int]
    metadata: _containers.ScalarMap[str, str]
    def __init__(self, metadata: _Optional[_Mapping[str, str]] = ...) -> None: ...

class StartProviderRequest(_message.Message):
    __slots__ = ()
    NAME_FIELD_NUMBER: _ClassVar[int]
    CONFIG_FIELD_NUMBER: _ClassVar[int]
    PROTOCOL_VERSION_FIELD_NUMBER: _ClassVar[int]
    name: str
    config: _struct_pb2.Struct
    protocol_version: int
    def __init__(self, name: _Optional[str] = ..., config: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., protocol_version: _Optional[int] = ...) -> None: ...

class StartProviderResponse(_message.Message):
    __slots__ = ()
    PROTOCOL_VERSION_FIELD_NUMBER: _ClassVar[int]
    protocol_version: int
    def __init__(self, protocol_version: _Optional[int] = ...) -> None: ...
