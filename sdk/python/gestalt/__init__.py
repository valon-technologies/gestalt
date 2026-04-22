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

from importlib import import_module

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
from ._authorization import ENV_AUTHORIZATION_SOCKET, Authorization, AuthorizationClient
from ._http_subject import (
    HTTPSubjectRequest,
    HTTPSubjectResolutionError,
    http_subject_error,
)
from ._manifest_metadata import (
    HTTPAck,
    HTTPBinding,
    HTTPMediaType,
    HTTPRequestBody,
    HTTPSecretRef,
    HTTPSecurityScheme,
    PluginManifestMetadata,
)

_LAZY_EXPORTS = {
    "AlreadyExistsError": ("._indexeddb", "AlreadyExistsError"),
    "AuthenticatedUser": ("._providers", "AuthenticatedUser"),
    "AuthenticationProvider": ("._providers", "AuthenticationProvider"),
    "BeginLoginRequest": ("._providers", "BeginLoginRequest"),
    "BeginLoginResponse": ("._providers", "BeginLoginResponse"),
    "ByteRange": ("._s3", "ByteRange"),
    "CURSOR_NEXT": ("._indexeddb", "CURSOR_NEXT"),
    "CURSOR_NEXT_UNIQUE": ("._indexeddb", "CURSOR_NEXT_UNIQUE"),
    "CURSOR_PREV": ("._indexeddb", "CURSOR_PREV"),
    "CURSOR_PREV_UNIQUE": ("._indexeddb", "CURSOR_PREV_UNIQUE"),
    "Cache": ("._cache", "Cache"),
    "CacheEntry": ("._cache", "CacheEntry"),
    "CacheProvider": ("._providers", "CacheProvider"),
    "ENV_CACHE_SOCKET_TOKEN": ("._cache", "ENV_CACHE_SOCKET_TOKEN"),
    "Catalog": ("._catalog", "Catalog"),
    "CatalogOperation": ("._catalog", "CatalogOperation"),
    "CatalogParameter": ("._catalog", "CatalogParameter"),
    "Closer": ("._providers", "Closer"),
    "CompleteLoginRequest": ("._providers", "CompleteLoginRequest"),
    "CopyOptions": ("._s3", "CopyOptions"),
    "Cursor": ("._indexeddb", "Cursor"),
    "http_subject": ("._plugin", "http_subject"),
    "ENV_PLUGIN_INVOKER_SOCKET": ("._invoker", "ENV_PLUGIN_INVOKER_SOCKET"),
    "ENV_PLUGIN_INVOKER_SOCKET_TOKEN": ("._invoker", "ENV_PLUGIN_INVOKER_SOCKET_TOKEN"),
    "ENV_S3_SOCKET": ("._s3", "ENV_S3_SOCKET"),
    "ENV_WORKFLOW_HOST_SOCKET": ("._workflow", "ENV_WORKFLOW_HOST_SOCKET"),
    "ExternalTokenValidator": ("._providers", "ExternalTokenValidator"),
    "HealthChecker": ("._providers", "HealthChecker"),
    "Index": ("._indexeddb", "Index"),
    "IndexedDB": ("._indexeddb", "IndexedDB"),
    "IndexSchema": ("._indexeddb", "IndexSchema"),
    "KeyRange": ("._indexeddb", "KeyRange"),
    "ListOptions": ("._s3", "ListOptions"),
    "ListPage": ("._s3", "ListPage"),
    "MetadataProvider": ("._providers", "MetadataProvider"),
    "NotFoundError": ("._indexeddb", "NotFoundError"),
    "ObjectMeta": ("._s3", "ObjectMeta"),
    "ObjectRef": ("._s3", "ObjectRef"),
    "ObjectStore": ("._indexeddb", "ObjectStore"),
    "ObjectStoreSchema": ("._indexeddb", "ObjectStoreSchema"),
    "OperationAnnotations": ("._catalog", "OperationAnnotations"),
    "Plugin": ("._plugin", "Plugin"),
    "PluginInvoker": ("._invoker", "PluginInvoker"),
    "PluginProvider": ("._providers", "PluginProvider"),
    "PluginProviderAdapter": ("._providers", "PluginProviderAdapter"),
    "PresignMethod": ("._s3", "PresignMethod"),
    "PresignOptions": ("._s3", "PresignOptions"),
    "PresignResult": ("._s3", "PresignResult"),
    "ProviderKind": ("._providers", "ProviderKind"),
    "ProviderMetadata": ("._providers", "ProviderMetadata"),
    "ReadOptions": ("._s3", "ReadOptions"),
    "S3": ("._s3", "S3"),
    "S3InvalidRangeError": ("._s3", "S3InvalidRangeError"),
    "S3NotFoundError": ("._s3", "S3NotFoundError"),
    "S3Object": ("._s3", "S3Object"),
    "S3PreconditionFailedError": ("._s3", "S3PreconditionFailedError"),
    "S3Provider": ("._providers", "S3Provider"),
    "S3ReadStream": ("._s3", "S3ReadStream"),
    "SecretsProvider": ("._providers", "SecretsProvider"),
    "SessionCatalogProvider": ("._catalog", "SessionCatalogProvider"),
    "SessionTTLProvider": ("._providers", "SessionTTLProvider"),
    "WorkflowHost": ("._workflow", "WorkflowHost"),
    "WarningsProvider": ("._providers", "WarningsProvider"),
    "WorkflowProvider": ("._providers", "WorkflowProvider"),
    "WriteOptions": ("._s3", "WriteOptions"),
    "cache_socket_env": ("._cache", "cache_socket_env"),
    "cache_socket_token_env": ("._cache", "cache_socket_token_env"),
    "indexeddb_socket_env": ("._indexeddb", "indexeddb_socket_env"),
    "indexeddb_socket_token_env": ("._indexeddb", "indexeddb_socket_token_env"),
    "operation": ("._plugin", "operation"),
    "s3_socket_env": ("._s3", "s3_socket_env"),
    "session_catalog": ("._plugin", "session_catalog"),
}


def __getattr__(name: str):
    export = _LAZY_EXPORTS.get(name)
    if export is None:
        raise AttributeError(f"module {__name__!r} has no attribute {name!r}")
    module_name, attr_name = export
    value = getattr(import_module(module_name, __name__), attr_name)
    globals()[name] = value
    return value

__all__ = [
    "AlreadyExistsError",
    "AuthenticationProvider",
    "Authorization",
    "AuthorizationClient",
    "AuthenticatedUser",
    "Cache",
    "CacheEntry",
    "CacheProvider",
    "ENV_CACHE_SOCKET_TOKEN",
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
    "ENV_AUTHORIZATION_SOCKET",
    "Error",
    "ENV_S3_SOCKET",
    "ENV_PLUGIN_INVOKER_SOCKET",
    "ENV_PLUGIN_INVOKER_SOCKET_TOKEN",
    "ENV_WORKFLOW_HOST_SOCKET",
    "ExternalTokenValidator",
    "HealthChecker",
    "HTTPAck",
    "HTTPBinding",
    "HTTPMediaType",
    "HTTPRequestBody",
    "HTTPSecretRef",
    "HTTPSecurityScheme",
    "HTTPSubjectRequest",
    "HTTPSubjectResolutionError",
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
    "PluginManifestMetadata",
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
    "cache_socket_token_env",
    "WriteOptions",
    "ByteRange",
    "CopyOptions",
    "field",
    "http_subject",
    "http_subject_error",
    "indexeddb_socket_env",
    "indexeddb_socket_token_env",
    "operation",
    "s3_socket_env",
    "session_catalog",
]
