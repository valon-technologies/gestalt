Python API Reference
====================

These pages document the authored Python API that provider authors use to build
Gestalt integrations, authentication providers, caches, and S3 backends.

.. automodule:: gestalt
   :no-members:

.. currentmodule:: gestalt

The supported import surface is the top-level :mod:`gestalt` package:

.. code-block:: python

   from gestalt import Model, Plugin, Cache, IndexedDB, S3

Provider code should prefer the authored SDK helpers documented here. For
low-level fixtures and interoperability tests, :mod:`gestalt.protocol.v1` and
:mod:`gestalt.testing` re-export the checked-in generated protobuf bindings.

Core authoring types
--------------------

.. autosummary::
   :nosignatures:

   Model
   field
   Subject
   Credential
   Access
   Request
   Response
   OK
   Error

.. autoclass:: Model
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autofunction:: field

.. autoclass:: Subject

.. autoclass:: Credential

.. autoclass:: Access

.. autoclass:: Request

.. autoclass:: Response

.. autofunction:: OK

.. autoexception:: Error

Plugin authoring
----------------

.. autosummary::
   :nosignatures:

   Plugin
   operation
   session_catalog
   post_connect
   http_subject
   ConnectedToken
   HTTPSubjectRequest
   HTTPSubjectResolutionError
   http_subject_error
   SessionCatalogProvider
   Catalog
   CatalogOperation
   CatalogParameter
   OperationAnnotations

.. autoclass:: Plugin
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autofunction:: operation

.. autofunction:: session_catalog

.. autofunction:: post_connect

.. autofunction:: http_subject

.. autoclass:: ConnectedToken

.. autoclass:: HTTPSubjectRequest

.. autoexception:: HTTPSubjectResolutionError

.. autofunction:: http_subject_error

.. autoclass:: SessionCatalogProvider
   :members:
   :exclude-members: __dict__, __module__, __weakref__

Catalog protocol types
----------------------

These top-level exports are generated protocol message types that the SDK
re-exports for lower-level catalog integration work.

.. autosummary::
   :nosignatures:

   Catalog
   CatalogOperation
   CatalogParameter
   OperationAnnotations

.. autoclass:: Catalog

.. autoclass:: CatalogOperation

.. autoclass:: CatalogParameter

.. autoclass:: OperationAnnotations

Protocol helpers
----------------

These helpers convert between Python values and protobuf well-known types
without importing generated runtime internals.

.. autosummary::
   :nosignatures:

   struct_from_dict
   struct_to_dict
   value_from_json
   value_to_json
   message_to_dict
   message_from_dict
   timestamp_from_datetime
   datetime_from_timestamp
   has_field
   which_oneof

.. autofunction:: struct_from_dict

.. autofunction:: struct_to_dict

.. autofunction:: value_from_json

.. autofunction:: value_to_json

.. autofunction:: message_to_dict

.. autofunction:: message_from_dict

.. autofunction:: timestamp_from_datetime

.. autofunction:: datetime_from_timestamp

.. autofunction:: has_field

.. autofunction:: which_oneof

Agent protocol helpers
----------------------

These helpers convert common agent protocol messages to and from plain
lower-snake-case dictionaries while preserving protobuf field presence for
optional structured payloads.

.. autosummary::
   :nosignatures:

   agent_actor_to_dict
   agent_actor_from_dict
   agent_subject_context_to_dict
   agent_subject_context_from_dict
   prepared_workspace_to_dict
   prepared_workspace_from_dict
   agent_tool_ref_to_dict
   agent_tool_ref_from_dict
   agent_message_part_to_dict
   agent_message_part_from_dict
   agent_message_to_dict
   agent_message_from_dict
   agent_messages_to_dicts
   agent_messages_from_dicts
   agent_message_to_proto_dict
   agent_message_from_proto_dict
   agent_messages_to_proto_dicts
   agent_messages_from_proto_dicts

.. autofunction:: agent_actor_to_dict

.. autofunction:: agent_actor_from_dict

.. autofunction:: agent_subject_context_to_dict

.. autofunction:: agent_subject_context_from_dict

.. autofunction:: prepared_workspace_to_dict

.. autofunction:: prepared_workspace_from_dict

.. autofunction:: agent_tool_ref_to_dict

.. autofunction:: agent_tool_ref_from_dict

.. autofunction:: agent_message_part_to_dict

.. autofunction:: agent_message_part_from_dict

.. autofunction:: agent_message_to_dict

.. autofunction:: agent_message_from_dict

.. autofunction:: agent_messages_to_dicts

.. autofunction:: agent_messages_from_dicts

.. autofunction:: agent_message_to_proto_dict

.. autofunction:: agent_message_from_proto_dict

.. autofunction:: agent_messages_to_proto_dicts

.. autofunction:: agent_messages_from_proto_dicts

Provider interfaces
-------------------

.. autosummary::
   :nosignatures:

   ProviderKind
   ProviderMetadata
   PluginProvider
   MetadataProvider
   HealthChecker
   Starter
   WarningsProvider
   Closer
   PluginProviderAdapter
   AuthenticationProvider
   ExternalTokenValidator
   SessionTTLProvider
   SecretsProvider
   CacheProvider
   S3Provider
   AgentProvider
   PluginRuntimeProvider
   WorkflowProvider
   AuthenticatedUser
   BeginLoginRequest
   BeginLoginResponse
   CompleteLoginRequest

.. autoclass:: ProviderKind

.. autoclass:: ProviderMetadata

.. autoclass:: PluginProvider
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: MetadataProvider
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: HealthChecker
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: Starter
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: WarningsProvider
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: Closer
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: PluginProviderAdapter
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: AuthenticationProvider
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: ExternalTokenValidator
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: SessionTTLProvider
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: SecretsProvider
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: CacheProvider
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: S3Provider
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: AgentProvider
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: PluginRuntimeProvider
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: WorkflowProvider
   :members:
   :exclude-members: __dict__, __module__, __weakref__

Auth protocol helpers
---------------------

These helpers construct wire-compatible authentication protocol payloads without
requiring provider code to import generated modules.

.. autosummary::
   :nosignatures:

   AuthenticatedUser
   BeginLoginRequest
   BeginLoginResponse
   CompleteLoginRequest

.. autofunction:: AuthenticatedUser

.. autofunction:: BeginLoginRequest

.. autofunction:: BeginLoginResponse

.. autofunction:: CompleteLoginRequest

Provider telemetry
------------------

``gestaltd`` configures OpenTelemetry exporters from the selected
``providers.telemetry`` entry and passes standard ``OTEL_*`` environment into
provider processes. Python providers that run through the SDK runtime get that
setup automatically and can use :mod:`gestalt.telemetry` for
provider-authored GenAI spans and metrics.

.. automodule:: gestalt.telemetry
   :no-members:

.. currentmodule:: gestalt.telemetry

.. autosummary::
   :nosignatures:

   Operation
   configure_from_environment
   shutdown
   model_operation
   agent_invocation
   tool_execution
   record_openai_usage
   record_anthropic_usage

.. autodata:: GENAI_PROVIDER_NAME

.. autodata:: GENAI_OPERATION_CHAT

.. autodata:: GENAI_OPERATION_EXECUTE_TOOL

.. autodata:: GENAI_OPERATION_INVOKE_AGENT

.. autodata:: GENAI_TOOL_TYPE_DATASTORE

.. autodata:: GENAI_TOOL_TYPE_EXTENSION

.. autoclass:: Operation
   :members:
   :special-members: __enter__, __exit__
   :exclude-members: __dict__, __module__, __weakref__

.. autofunction:: configure_from_environment

.. autofunction:: shutdown

.. autofunction:: model_operation

.. autofunction:: agent_invocation

.. autofunction:: tool_execution

.. autofunction:: record_openai_usage

.. autofunction:: record_anthropic_usage

.. currentmodule:: gestalt

Cache client
------------

.. autosummary::
   :nosignatures:

   ENV_CACHE_SOCKET_TOKEN
   CacheEntry
   Cache
   cache_socket_env
   cache_socket_token_env

.. autodata:: ENV_CACHE_SOCKET_TOKEN

.. autoclass:: CacheEntry

.. autoclass:: Cache
   :members:
   :special-members: __enter__, __exit__
   :exclude-members: __dict__, __module__, __weakref__

.. autofunction:: cache_socket_env

.. autofunction:: cache_socket_token_env

IndexedDB client
----------------

.. autosummary::
   :nosignatures:

   CURSOR_NEXT
   CURSOR_NEXT_UNIQUE
   CURSOR_PREV
   CURSOR_PREV_UNIQUE
   indexeddb_socket_env
   indexeddb_socket_token_env
   NotFoundError
   AlreadyExistsError
   TransactionError
   KeyRange
   IndexSchema
   ObjectStoreSchema
   indexeddb_record_to_proto
   indexeddb_record_from_proto
   indexeddb_typed_value_from_python
   indexeddb_typed_value_to_python
   indexeddb_key_value_from_python
   indexeddb_key_value_to_python
   indexeddb_cursor_key_to_proto
   indexeddb_key_range_to_proto
   IndexedDB
   ObjectStore
   Index
   Cursor
   Transaction
   TransactionObjectStore
   TransactionIndex

.. autodata:: CURSOR_NEXT

.. autodata:: CURSOR_NEXT_UNIQUE

.. autodata:: CURSOR_PREV

.. autodata:: CURSOR_PREV_UNIQUE

.. autofunction:: indexeddb_socket_env

.. autofunction:: indexeddb_socket_token_env

.. autofunction:: indexeddb_record_to_proto

.. autofunction:: indexeddb_record_from_proto

.. autofunction:: indexeddb_typed_value_from_python

.. autofunction:: indexeddb_typed_value_to_python

.. autofunction:: indexeddb_key_value_from_python

.. autofunction:: indexeddb_key_value_to_python

.. autofunction:: indexeddb_cursor_key_to_proto

.. autofunction:: indexeddb_key_range_to_proto

.. autoexception:: NotFoundError

.. autoexception:: AlreadyExistsError

.. autoexception:: TransactionError

.. autoclass:: KeyRange

.. autoclass:: IndexSchema

.. autoclass:: ObjectStoreSchema

.. autoclass:: IndexedDB
   :members:
   :special-members: __enter__, __exit__
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: ObjectStore
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: Index
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: Cursor
   :members:
   :special-members: __enter__, __exit__
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: Transaction
   :members:
   :special-members: __enter__, __exit__
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: TransactionObjectStore
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: TransactionIndex
   :members:
   :exclude-members: __dict__, __module__, __weakref__

S3 client
---------

.. autosummary::
   :nosignatures:

   ENV_S3_SOCKET
   ENV_S3_SOCKET_TOKEN
   s3_socket_env
   s3_socket_token_env
   S3NotFoundError
   S3PreconditionFailedError
   S3InvalidRangeError
   ObjectRef
   ObjectMeta
   ByteRange
   ReadOptions
   WriteOptions
   ListOptions
   ListPage
   CopyOptions
   PresignMethod
   PresignOptions
   PresignResult
   ObjectAccessURLOptions
   ObjectAccessURL
   S3ReadStream
   S3
   S3Object

.. autodata:: ENV_S3_SOCKET

.. autodata:: ENV_S3_SOCKET_TOKEN

.. autofunction:: s3_socket_env

.. autofunction:: s3_socket_token_env

.. autoexception:: S3NotFoundError

.. autoexception:: S3PreconditionFailedError

.. autoexception:: S3InvalidRangeError

.. autoclass:: ObjectRef

.. autoclass:: ObjectMeta

.. autoclass:: ByteRange

.. autoclass:: ReadOptions

.. autoclass:: WriteOptions

.. autoclass:: ListOptions

.. autoclass:: ListPage

.. autoclass:: CopyOptions

.. autoclass:: PresignMethod

.. autoclass:: PresignOptions

.. autoclass:: PresignResult

.. autodata:: ObjectAccessURLOptions
   :annotation:

.. autodata:: ObjectAccessURL
   :annotation:

.. autoclass:: S3ReadStream
   :members:
   :special-members: __enter__, __exit__
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: S3
   :members:
   :special-members: __enter__, __exit__
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: S3Object
   :members:
   :exclude-members: __dict__, __module__, __weakref__

Host service clients
--------------------

These clients connect to host services made available to a provider process by
``gestaltd``. They read socket and relay-token locations from environment
variables and add invocation tokens to manager requests when required.

.. autosummary::
   :nosignatures:

   ENV_AGENT_HOST_SOCKET
   ENV_AGENT_HOST_SOCKET_TOKEN
   ENV_AGENT_MANAGER_SOCKET
   ENV_AGENT_MANAGER_SOCKET_TOKEN
   ENV_AUTHORIZATION_SOCKET
   ENV_AUTHORIZATION_SOCKET_TOKEN
   ENV_PLUGIN_INVOKER_SOCKET
   ENV_PLUGIN_INVOKER_SOCKET_TOKEN
   ENV_RUNTIME_LOG_HOST_SOCKET
   ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN
   ENV_RUNTIME_SESSION_ID
   ENV_WORKFLOW_HOST_SOCKET
   ENV_WORKFLOW_HOST_SOCKET_TOKEN
   ENV_WORKFLOW_MANAGER_SOCKET
   ENV_WORKFLOW_MANAGER_SOCKET_TOKEN
   AgentHost
   AgentManager
   Authorization
   AuthorizationClient
   PluginInvoker
   RuntimeLogHost
   RuntimeLogWriter
   RuntimeLogHandler
   WorkflowHost
   WorkflowManager

.. autodata:: ENV_AGENT_HOST_SOCKET

.. autodata:: ENV_AGENT_HOST_SOCKET_TOKEN

.. autodata:: ENV_AGENT_MANAGER_SOCKET

.. autodata:: ENV_AGENT_MANAGER_SOCKET_TOKEN

.. autodata:: ENV_AUTHORIZATION_SOCKET

.. autodata:: ENV_AUTHORIZATION_SOCKET_TOKEN

.. autodata:: ENV_PLUGIN_INVOKER_SOCKET

.. autodata:: ENV_PLUGIN_INVOKER_SOCKET_TOKEN

.. autodata:: ENV_RUNTIME_LOG_HOST_SOCKET

.. autodata:: ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN

.. autodata:: ENV_RUNTIME_SESSION_ID

.. autodata:: ENV_WORKFLOW_HOST_SOCKET

.. autodata:: ENV_WORKFLOW_HOST_SOCKET_TOKEN

.. autodata:: ENV_WORKFLOW_MANAGER_SOCKET

.. autodata:: ENV_WORKFLOW_MANAGER_SOCKET_TOKEN

.. autoclass:: AgentHost
   :members:
   :special-members: __enter__, __exit__
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: AgentManager
   :members:
   :special-members: __enter__, __exit__
   :exclude-members: __dict__, __module__, __weakref__

.. autofunction:: Authorization

.. autoclass:: AuthorizationClient
   :members:
   :special-members: __enter__, __exit__
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: PluginInvoker
   :members:
   :special-members: __enter__, __exit__
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: RuntimeLogHost
   :members:
   :special-members: __enter__, __exit__
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: RuntimeLogWriter
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: RuntimeLogHandler
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: WorkflowHost
   :members:
   :special-members: __enter__, __exit__
   :exclude-members: __dict__, __module__, __weakref__

.. autoclass:: WorkflowManager
   :members:
   :special-members: __enter__, __exit__
   :exclude-members: __dict__, __module__, __weakref__
