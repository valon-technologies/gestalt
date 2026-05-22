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

This reference focuses on provider-facing classes, helpers, clients, and native
input models. Transport serialization details are intentionally omitted.

.. contents:: API sections
   :local:
   :depth: 1

.. _python-core-authoring-types:

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

.. _python-plugin-authoring:

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

.. autoclass:: Catalog

.. autoclass:: CatalogOperation

.. autoclass:: CatalogParameter

.. autoclass:: OperationAnnotations

.. _python-workflow-helpers:

Workflow helpers
----------------

These helpers build workflow values from native Python inputs. Structured
payload fields accept JSON-compatible mappings or dataclass instances.
Timestamp fields accept ``datetime`` values, and copy helpers return
non-aliased message copies.

.. autosummary::
   :nosignatures:

   BoundWorkflowTarget
   WorkflowStep
   WorkflowStepPluginCall
   WorkflowStepAgentTurn
   WorkflowValue
   BoundWorkflowRun
   BoundWorkflowSchedule
   BoundWorkflowEventTrigger
   WorkflowExecutionReference
   bound_workflow_target
   bound_workflow_target_from_target
   workflow_step
   workflow_step_plugin_call
   workflow_step_agent_turn
   workflow_value
   workflow_event
   workflow_event_from_event
   workflow_signal
   workflow_signal_from_signal
   workflow_run_trigger
   workflow_run_trigger_from_trigger
   bound_workflow_run
   bound_workflow_run_from_run
   bound_workflow_schedule
   bound_workflow_schedule_from_schedule
   bound_workflow_event_trigger
   bound_workflow_event_trigger_from_trigger
   workflow_execution_reference
   workflow_execution_reference_from_reference

.. autoclass:: BoundWorkflowTarget

.. autoclass:: WorkflowStep

.. autoclass:: WorkflowStepPluginCall

.. autoclass:: WorkflowStepAgentTurn

.. autoclass:: WorkflowValue

.. autoclass:: BoundWorkflowRun

.. autoclass:: BoundWorkflowSchedule

.. autoclass:: BoundWorkflowEventTrigger

.. autoclass:: WorkflowExecutionReference

.. autofunction:: bound_workflow_target

.. autofunction:: bound_workflow_target_from_target

.. autofunction:: workflow_step

.. autofunction:: workflow_step_plugin_call

.. autofunction:: workflow_step_agent_turn

.. autofunction:: workflow_value

.. autofunction:: workflow_event

.. autofunction:: workflow_event_from_event

.. autofunction:: workflow_signal

.. autofunction:: workflow_signal_from_signal

.. autofunction:: workflow_run_trigger

.. autofunction:: workflow_run_trigger_from_trigger

.. autofunction:: bound_workflow_run

.. autofunction:: bound_workflow_run_from_run

.. autofunction:: bound_workflow_schedule

.. autofunction:: bound_workflow_schedule_from_schedule

.. autofunction:: bound_workflow_event_trigger

.. autofunction:: bound_workflow_event_trigger_from_trigger

.. autofunction:: workflow_execution_reference

.. autofunction:: workflow_execution_reference_from_reference

.. _python-agent-provider-models:

Agent provider models
---------------------

Agent providers receive and return native dataclasses. Structured payload
fields accept JSON-compatible mappings or dataclass instances, and timestamp
fields on sessions, turns, events, interactions, and host connections use
timezone-aware ``datetime`` values. The runtime owns transport serialization.

.. autosummary::
   :nosignatures:

   AgentActor
   AgentPreparedWorkspace
   AgentToolRef
   ResolvedAgentTool
   AgentProviderCapabilities
   AgentSession
   AgentInteraction
   AgentMessage
   AgentMessagePart
   AgentMessagePartToolCall
   AgentMessagePartToolResult
   AgentMessagePartImageRef
   AgentTurn
   AgentTurnDisplay
   AgentTurnEvent
   CreateAgentProviderSessionRequest
   CreateAgentProviderTurnRequest
   ListAgentProviderSessionsResponse
   ListAgentProviderTurnsResponse
   ListAgentProviderTurnEventsResponse
   ListAgentProviderInteractionsResponse
   ExecuteAgentToolResponse
   ListAgentToolsResponse
   ListedAgentTool
   ResolvedAgentConnection

.. autoclass:: AgentActor

.. autoclass:: AgentPreparedWorkspace

.. autoclass:: AgentToolRef

.. autoclass:: ResolvedAgentTool

.. autoclass:: AgentProviderCapabilities

.. autoclass:: AgentSession

.. autoclass:: AgentInteraction

.. autoclass:: AgentMessage

.. autoclass:: AgentMessagePart

.. autoclass:: AgentMessagePartToolCall

.. autoclass:: AgentMessagePartToolResult

.. autoclass:: AgentMessagePartImageRef

.. autoclass:: AgentTurn

.. autoclass:: AgentTurnDisplay

.. autoclass:: AgentTurnEvent

.. autoclass:: CreateAgentProviderSessionRequest

.. autoclass:: CreateAgentProviderTurnRequest

.. autoclass:: ListAgentProviderSessionsResponse

.. autoclass:: ListAgentProviderTurnsResponse

.. autoclass:: ListAgentProviderTurnEventsResponse

.. autoclass:: ListAgentProviderInteractionsResponse

.. autoclass:: ExecuteAgentToolResponse

.. autoclass:: ListAgentToolsResponse

.. autoclass:: ListedAgentTool

.. autoclass:: ResolvedAgentConnection

.. _python-agent-dictionary-helpers:

Agent dictionary helpers
------------------------

These helpers convert native agent dataclasses to and from plain
lower-snake-case dictionaries.

.. autosummary::
   :nosignatures:

   agent_actor_to_dict
   agent_actor_from_dict
   subject_to_dict
   subject_from_dict
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

.. autofunction:: agent_actor_to_dict

.. autofunction:: agent_actor_from_dict

.. autofunction:: subject_to_dict

.. autofunction:: subject_from_dict

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

.. _python-provider-interfaces:

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

.. _python-authentication-payload-models:

Authentication payload models
-----------------------------

These native dataclasses are used by authentication provider handlers. The
runtime owns transport conversion.

.. autosummary::
   :nosignatures:

   AuthenticatedUser
   BeginLoginRequest
   BeginLoginResponse
   CompleteLoginRequest

.. autoclass:: AuthenticatedUser

.. autoclass:: BeginLoginRequest

.. autoclass:: BeginLoginResponse

.. autoclass:: CompleteLoginRequest

.. _python-provider-telemetry:

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

.. _python-storage-and-host-service-clients:

.. _python-cache-client:

Cache client
------------

.. autosummary::
   :nosignatures:

   CacheEntry
   Cache

.. autoclass:: CacheEntry

.. autoclass:: Cache
   :members:
   :special-members: __enter__, __exit__
   :exclude-members: __dict__, __module__, __weakref__

.. _python-indexeddb-client:

IndexedDB client
----------------

.. autosummary::
   :nosignatures:

   CURSOR_NEXT
   CURSOR_NEXT_UNIQUE
   CURSOR_PREV
   CURSOR_PREV_UNIQUE
   NotFoundError
   AlreadyExistsError
   TransactionError
   KeyRange
   IndexSchema
   ObjectStoreSchema
   IndexedDBOpenCursorRequest
   IndexedDBCursorSnapshotEntry
   IndexedDBCursorSnapshot
   new_indexeddb_cursor_snapshot
   indexeddb_range_bounds
   compare_indexeddb_values
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

.. autoexception:: NotFoundError

.. autoexception:: AlreadyExistsError

.. autoexception:: TransactionError

.. autoclass:: KeyRange

.. autoclass:: IndexSchema

.. autoclass:: ObjectStoreSchema

.. autoclass:: IndexedDBOpenCursorRequest

.. autoclass:: IndexedDBCursorSnapshotEntry

.. autoclass:: IndexedDBCursorSnapshot
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autofunction:: new_indexeddb_cursor_snapshot

.. autofunction:: indexeddb_range_bounds

.. autofunction:: compare_indexeddb_values

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

.. _python-s3-client:

S3 client
---------

.. autosummary::
   :nosignatures:

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

.. _python-host-service-clients:

Host service clients
--------------------

These clients connect to host services made available to a provider process by
``gestaltd``.

.. autosummary::
   :nosignatures:

   ENV_RUNTIME_SESSION_ID
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

.. autodata:: ENV_RUNTIME_SESSION_ID

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
