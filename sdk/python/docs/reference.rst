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

Generated protobuf bindings remain available through :mod:`gestalt.gen`, but
the authored reference below focuses on the handwritten SDK API that provider
authors use directly.

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
   Authorization
   AuthorizationClient
   ENV_AUTHORIZATION_SOCKET

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

.. autofunction:: Authorization

.. autoclass:: AuthorizationClient
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autodata:: ENV_AUTHORIZATION_SOCKET

Plugin authoring
----------------

.. autosummary::
   :nosignatures:

   Plugin
   http_subject
   HTTPSubjectRequest
   HTTPSubjectResolutionError
   http_subject_error
   operation
   session_catalog
   SessionCatalogProvider
   Catalog
   CatalogOperation
   CatalogParameter
   OperationAnnotations

.. autoclass:: Plugin
   :members:
   :exclude-members: __dict__, __module__, __weakref__

.. autofunction:: http_subject

.. autoclass:: HTTPSubjectRequest

.. autoexception:: HTTPSubjectResolutionError

.. autofunction:: http_subject_error

.. autofunction:: operation

.. autofunction:: session_catalog

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

Provider interfaces
-------------------

.. autosummary::
   :nosignatures:

   ProviderKind
   ProviderMetadata
   PluginProvider
   MetadataProvider
   HealthChecker
   WarningsProvider
   Closer
   PluginProviderAdapter
   AuthenticationProvider
   ExternalTokenValidator
   SessionTTLProvider
   SecretsProvider
   CacheProvider
   S3Provider
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

Auth protocol types
-------------------

These generated authentication message types are also re-exported from
:mod:`gestalt` so provider code can type or construct lower-level protocol
payloads without reaching into private modules.

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

Cache client
------------

.. autosummary::
   :nosignatures:

   CacheEntry
   Cache
   cache_socket_env

.. autoclass:: CacheEntry

.. autoclass:: Cache
   :members:
   :special-members: __enter__, __exit__
   :exclude-members: __dict__, __module__, __weakref__

.. autofunction:: cache_socket_env

IndexedDB client
----------------

.. autosummary::
   :nosignatures:

   CURSOR_NEXT
   CURSOR_NEXT_UNIQUE
   CURSOR_PREV
   CURSOR_PREV_UNIQUE
   indexeddb_socket_env
   NotFoundError
   AlreadyExistsError
   KeyRange
   IndexSchema
   ObjectStoreSchema
   IndexedDB
   ObjectStore
   Index
   Cursor

.. autodata:: CURSOR_NEXT

.. autodata:: CURSOR_NEXT_UNIQUE

.. autodata:: CURSOR_PREV

.. autodata:: CURSOR_PREV_UNIQUE

.. autofunction:: indexeddb_socket_env

.. autoexception:: NotFoundError

.. autoexception:: AlreadyExistsError

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

S3 client
---------

.. autosummary::
   :nosignatures:

   ENV_S3_SOCKET
   s3_socket_env
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
   S3ReadStream
   S3
   S3Object

.. autodata:: ENV_S3_SOCKET

.. autofunction:: s3_socket_env

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
