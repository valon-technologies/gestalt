"""Public authoring surface for Gestalt Python providers.

The package is published as ``gestalt-sdk`` and imported as ``gestalt``.
Provider authors typically build integrations around the re-exported symbols
documented in the Sphinx reference:

.. code-block:: python

    from gestalt import Model, Plugin, operation

    class SearchInput(Model):
        query: str

    plugin = Plugin("search")

    @plugin.operation(title="Search")
    def search(params: SearchInput):
        return {"query": params.query}

Generated protobuf bindings remain available under :mod:`gestalt.gen`, but the
authored reference documentation focuses on the handwritten SDK surface.
"""

from ._api import (
    OK,
    Access,
    Credential,
    Error,
    Model,
    Request,
    Response,
    Subject,
    field,
)
from ._cache import Cache, CacheEntry, cache_socket_env
from ._catalog import (
    Catalog,
    CatalogOperation,
    CatalogParameter,
    OperationAnnotations,
    SessionCatalogProvider,
)
from ._indexeddb import (
    CURSOR_NEXT,
    CURSOR_NEXT_UNIQUE,
    CURSOR_PREV,
    CURSOR_PREV_UNIQUE,
    AlreadyExistsError,
    Cursor,
    Index,
    IndexedDB,
    IndexSchema,
    KeyRange,
    NotFoundError,
    ObjectStore,
    ObjectStoreSchema,
    indexeddb_socket_env,
)
from ._invoker import ENV_PLUGIN_INVOKER_SOCKET, PluginInvoker
from ._plugin import Plugin, operation, session_catalog
from ._providers import (
    AuthenticatedUser,
    AuthenticationProvider,
    BeginLoginRequest,
    BeginLoginResponse,
    CacheProvider,
    Closer,
    CompleteLoginRequest,
    ExternalTokenValidator,
    HealthChecker,
    MetadataProvider,
    PluginProvider,
    PluginProviderAdapter,
    ProviderKind,
    ProviderMetadata,
    S3Provider,
    SecretsProvider,
    SessionTTLProvider,
    WarningsProvider,
    WorkflowProvider,
)
from ._s3 import (
    ENV_S3_SOCKET,
    S3,
    ByteRange,
    CopyOptions,
    ListOptions,
    ListPage,
    ObjectMeta,
    ObjectRef,
    PresignMethod,
    PresignOptions,
    PresignResult,
    ReadOptions,
    S3InvalidRangeError,
    S3NotFoundError,
    S3Object,
    S3PreconditionFailedError,
    S3ReadStream,
    WriteOptions,
    s3_socket_env,
)
from ._workflow import (
    ENV_WORKFLOW_HOST_SOCKET,
    WorkflowHost,
)

__all__ = [
    "AlreadyExistsError",
    "AuthenticationProvider",
    "AuthenticatedUser",
    "Cache",
    "CacheEntry",
    "CacheProvider",
    "Access",
    "BeginLoginRequest",
    "BeginLoginResponse",
    "CURSOR_NEXT",
    "CURSOR_NEXT_UNIQUE",
    "CURSOR_PREV",
    "CURSOR_PREV_UNIQUE",
    "Catalog",
    "CatalogOperation",
    "CatalogParameter",
    "Credential",
    "Closer",
    "CompleteLoginRequest",
    "Cursor",
    "Error",
    "ENV_S3_SOCKET",
    "ENV_PLUGIN_INVOKER_SOCKET",
    "ENV_WORKFLOW_HOST_SOCKET",
    "ExternalTokenValidator",
    "HealthChecker",
    "Index",
    "IndexedDB",
    "IndexSchema",
    "KeyRange",
    "ListOptions",
    "ListPage",
    "MetadataProvider",
    "Model",
    "NotFoundError",
    "OK",
    "ObjectMeta",
    "ObjectRef",
    "ObjectStore",
    "ObjectStoreSchema",
    "OperationAnnotations",
    "Plugin",
    "PluginInvoker",
    "PluginProvider",
    "PluginProviderAdapter",
    "PresignMethod",
    "PresignOptions",
    "PresignResult",
    "ProviderKind",
    "ProviderMetadata",
    "Request",
    "Response",
    "ReadOptions",
    "S3",
    "S3InvalidRangeError",
    "S3NotFoundError",
    "S3Object",
    "S3PreconditionFailedError",
    "S3Provider",
    "S3ReadStream",
    "SecretsProvider",
    "Subject",
    "SessionCatalogProvider",
    "SessionTTLProvider",
    "WorkflowProvider",
    "WorkflowHost",
    "WarningsProvider",
    "cache_socket_env",
    "WriteOptions",
    "ByteRange",
    "CopyOptions",
    "field",
    "indexeddb_socket_env",
    "operation",
    "s3_socket_env",
    "session_catalog",
]
