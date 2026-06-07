from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import struct_pb2 as _struct_pb2
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
    CONNECTION_MODE_SUBJECT: _ClassVar[ConnectionMode]
CONNECTION_MODE_UNSPECIFIED: ConnectionMode
CONNECTION_MODE_NONE: ConnectionMode
CONNECTION_MODE_SUBJECT: ConnectionMode

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
    min_protocol_version: int
    max_protocol_version: int
    def __init__(self, name: _Optional[str] = ..., display_name: _Optional[str] = ..., description: _Optional[str] = ..., connection_mode: _Optional[_Union[ConnectionMode, str]] = ..., auth_types: _Optional[_Iterable[str]] = ..., connection_params: _Optional[_Mapping[str, ConnectionParamDef]] = ..., static_catalog: _Optional[_Union[Catalog, _Mapping]] = ..., supports_session_catalog: _Optional[bool] = ..., min_protocol_version: _Optional[int] = ..., max_protocol_version: _Optional[int] = ...) -> None: ...

class OperationResult(_message.Message):
    __slots__ = ()
    class HeadersEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: StringList
        def __init__(self, key: _Optional[str] = ..., value: _Optional[_Union[StringList, _Mapping]] = ...) -> None: ...
    STATUS_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    HEADERS_FIELD_NUMBER: _ClassVar[int]
    status: int
    body: str
    headers: _containers.MessageMap[str, StringList]
    def __init__(self, status: _Optional[int] = ..., body: _Optional[str] = ..., headers: _Optional[_Mapping[str, StringList]] = ...) -> None: ...

class AppInvokeRequest(_message.Message):
    __slots__ = ()
    APP_FIELD_NUMBER: _ClassVar[int]
    OPERATION_FIELD_NUMBER: _ClassVar[int]
    PARAMS_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_FIELD_NUMBER: _ClassVar[int]
    INSTANCE_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_MODE_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    app: str
    operation: str
    params: _struct_pb2.Struct
    connection: str
    instance: str
    idempotency_key: str
    credential_mode: str
    context: RequestContext
    def __init__(self, app: _Optional[str] = ..., operation: _Optional[str] = ..., params: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., connection: _Optional[str] = ..., instance: _Optional[str] = ..., idempotency_key: _Optional[str] = ..., credential_mode: _Optional[str] = ..., context: _Optional[_Union[RequestContext, _Mapping]] = ...) -> None: ...

class AppInvokeGraphQLRequest(_message.Message):
    __slots__ = ()
    APP_FIELD_NUMBER: _ClassVar[int]
    DOCUMENT_FIELD_NUMBER: _ClassVar[int]
    VARIABLES_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_FIELD_NUMBER: _ClassVar[int]
    INSTANCE_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    app: str
    document: str
    variables: _struct_pb2.Struct
    connection: str
    instance: str
    idempotency_key: str
    context: RequestContext
    def __init__(self, app: _Optional[str] = ..., document: _Optional[str] = ..., variables: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., connection: _Optional[str] = ..., instance: _Optional[str] = ..., idempotency_key: _Optional[str] = ..., context: _Optional[_Union[RequestContext, _Mapping]] = ...) -> None: ...

class SubjectContext(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    EMAIL_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_NAME_FIELD_NUMBER: _ClassVar[int]
    SCOPES_FIELD_NUMBER: _ClassVar[int]
    PERMISSIONS_FIELD_NUMBER: _ClassVar[int]
    id: str
    credential_subject_id: str
    email: str
    display_name: str
    scopes: _containers.RepeatedScalarFieldContainer[str]
    permissions: _containers.RepeatedCompositeFieldContainer[SubjectPermissionContext]
    def __init__(self, id: _Optional[str] = ..., credential_subject_id: _Optional[str] = ..., email: _Optional[str] = ..., display_name: _Optional[str] = ..., scopes: _Optional[_Iterable[str]] = ..., permissions: _Optional[_Iterable[_Union[SubjectPermissionContext, _Mapping]]] = ...) -> None: ...

class SubjectPermissionContext(_message.Message):
    __slots__ = ()
    APP_FIELD_NUMBER: _ClassVar[int]
    OPERATIONS_FIELD_NUMBER: _ClassVar[int]
    ALL_OPERATIONS_FIELD_NUMBER: _ClassVar[int]
    app: str
    operations: _containers.RepeatedScalarFieldContainer[str]
    all_operations: bool
    def __init__(self, app: _Optional[str] = ..., operations: _Optional[_Iterable[str]] = ..., all_operations: _Optional[bool] = ...) -> None: ...

class AgentToolRef(_message.Message):
    __slots__ = ()
    APP_FIELD_NUMBER: _ClassVar[int]
    OPERATION_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_FIELD_NUMBER: _ClassVar[int]
    INSTANCE_FIELD_NUMBER: _ClassVar[int]
    TITLE_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_MODE_FIELD_NUMBER: _ClassVar[int]
    SYSTEM_FIELD_NUMBER: _ClassVar[int]
    RUN_AS_FIELD_NUMBER: _ClassVar[int]
    app: str
    operation: str
    connection: str
    instance: str
    title: str
    description: str
    credential_mode: str
    system: str
    run_as: SubjectContext
    def __init__(self, app: _Optional[str] = ..., operation: _Optional[str] = ..., connection: _Optional[str] = ..., instance: _Optional[str] = ..., title: _Optional[str] = ..., description: _Optional[str] = ..., credential_mode: _Optional[str] = ..., system: _Optional[str] = ..., run_as: _Optional[_Union[SubjectContext, _Mapping]] = ...) -> None: ...

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

class ProviderContext(_message.Message):
    __slots__ = ()
    KIND_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    kind: str
    name: str
    def __init__(self, kind: _Optional[str] = ..., name: _Optional[str] = ...) -> None: ...

class InvocationContext(_message.Message):
    __slots__ = ()
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    DEPTH_FIELD_NUMBER: _ClassVar[int]
    CALL_CHAIN_FIELD_NUMBER: _ClassVar[int]
    SURFACE_FIELD_NUMBER: _ClassVar[int]
    INTERNAL_CONNECTION_ACCESS_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_FIELD_NUMBER: _ClassVar[int]
    request_id: str
    depth: int
    call_chain: _containers.RepeatedScalarFieldContainer[str]
    surface: str
    internal_connection_access: bool
    connection: str
    def __init__(self, request_id: _Optional[str] = ..., depth: _Optional[int] = ..., call_chain: _Optional[_Iterable[str]] = ..., surface: _Optional[str] = ..., internal_connection_access: _Optional[bool] = ..., connection: _Optional[str] = ...) -> None: ...

class RequestMetaContext(_message.Message):
    __slots__ = ()
    CLIENT_IP_FIELD_NUMBER: _ClassVar[int]
    REMOTE_ADDR_FIELD_NUMBER: _ClassVar[int]
    USER_AGENT_FIELD_NUMBER: _ClassVar[int]
    client_ip: str
    remote_addr: str
    user_agent: str
    def __init__(self, client_ip: _Optional[str] = ..., remote_addr: _Optional[str] = ..., user_agent: _Optional[str] = ...) -> None: ...

class AgentInvocationContext(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    TURN_ID_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    session_id: str
    turn_id: str
    def __init__(self, provider_name: _Optional[str] = ..., session_id: _Optional[str] = ..., turn_id: _Optional[str] = ...) -> None: ...

class RequestContext(_message.Message):
    __slots__ = ()
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_FIELD_NUMBER: _ClassVar[int]
    ACCESS_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_FIELD_NUMBER: _ClassVar[int]
    HOST_FIELD_NUMBER: _ClassVar[int]
    AGENT_SUBJECT_FIELD_NUMBER: _ClassVar[int]
    CALLER_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_FIELD_NUMBER: _ClassVar[int]
    TOOL_REFS_FIELD_NUMBER: _ClassVar[int]
    TOOL_REFS_SET_FIELD_NUMBER: _ClassVar[int]
    REQUEST_META_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    subject: SubjectContext
    credential: CredentialContext
    access: AccessContext
    workflow: _struct_pb2.Struct
    host: HostContext
    agent_subject: SubjectContext
    caller: ProviderContext
    invocation: InvocationContext
    tool_refs: _containers.RepeatedCompositeFieldContainer[AgentToolRef]
    tool_refs_set: bool
    request_meta: RequestMetaContext
    agent: AgentInvocationContext
    def __init__(self, subject: _Optional[_Union[SubjectContext, _Mapping]] = ..., credential: _Optional[_Union[CredentialContext, _Mapping]] = ..., access: _Optional[_Union[AccessContext, _Mapping]] = ..., workflow: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., host: _Optional[_Union[HostContext, _Mapping]] = ..., agent_subject: _Optional[_Union[SubjectContext, _Mapping]] = ..., caller: _Optional[_Union[ProviderContext, _Mapping]] = ..., invocation: _Optional[_Union[InvocationContext, _Mapping]] = ..., tool_refs: _Optional[_Iterable[_Union[AgentToolRef, _Mapping]]] = ..., tool_refs_set: _Optional[bool] = ..., request_meta: _Optional[_Union[RequestMetaContext, _Mapping]] = ..., agent: _Optional[_Union[AgentInvocationContext, _Mapping]] = ...) -> None: ...

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
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    operation: str
    params: _struct_pb2.Struct
    token: str
    connection_params: _containers.ScalarMap[str, str]
    invocation_id: str
    context: RequestContext
    idempotency_key: str
    def __init__(self, operation: _Optional[str] = ..., params: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., token: _Optional[str] = ..., connection_params: _Optional[_Mapping[str, str]] = ..., invocation_id: _Optional[str] = ..., context: _Optional[_Union[RequestContext, _Mapping]] = ..., idempotency_key: _Optional[str] = ...) -> None: ...

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
