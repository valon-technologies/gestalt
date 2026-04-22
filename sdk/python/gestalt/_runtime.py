from __future__ import annotations

import datetime as dt
import functools
import importlib
import json
import os
import pathlib
import signal
import sys
import traceback
from concurrent import futures
from dataclasses import dataclass
from enum import Enum
from http import HTTPStatus
from typing import TYPE_CHECKING, Any, Final, cast

from ._api import Access, Credential, Request, Subject
from ._bootstrap import parse_plugin_target, read_bundled_plugin_config
from ._catalog import catalog_to_proto
from ._http_subject import HTTPSubjectRequest, HTTPSubjectResolutionError
from ._operations import INTERNAL_ERROR_MESSAGE
from ._plugin import ConnectedToken, Plugin, _module_plugin

if TYPE_CHECKING:
    from ._providers import (
        AgentProvider,
        AuthenticationProvider,
        CacheProvider,
        Closer,
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
else:
    try:
        from ._providers import (
            AgentProvider,
            AuthenticationProvider,
            CacheProvider,
            Closer,
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
    except ModuleNotFoundError:
        class ProviderKind(str, Enum):
            INTEGRATION = "integration"
            AUTHENTICATION = "authentication"
            CACHE = "cache"
            S3 = "s3"
            AGENT = "agent"
            WORKFLOW = "workflow"
            SECRETS = "secrets"
            TELEMETRY = "telemetry"

        class PluginProvider:
            def configure(self, name: str, config: dict[str, Any]) -> None:
                pass

        class PluginProviderAdapter:
            def __init__(
                self,
                *,
                kind: ProviderKind,
                provider: PluginProvider,
                register_services: Any,
            ) -> None:
                self.kind = kind
                self.provider = provider
                self.register_services = register_services

        class ProviderMetadata:
            def __init__(
                self,
                kind: ProviderKind | str,
                name: str = "",
                display_name: str = "",
                description: str = "",
                version: str = "",
            ) -> None:
                self.kind = kind
                self.name = name
                self.display_name = display_name
                self.description = description
                self.version = version

        class AuthenticationProvider(PluginProvider):
            def begin_login(self, request: Any) -> Any:
                raise NotImplementedError

            def complete_login(self, request: Any) -> Any:
                raise NotImplementedError

        class CacheProvider(PluginProvider):
            def get(self, key: str) -> bytes | None:
                raise NotImplementedError

            def get_many(self, keys: list[str]) -> dict[str, bytes]:
                raise NotImplementedError

            def set(
                self, key: str, value: bytes, ttl: dt.timedelta | None = None
            ) -> None:
                raise NotImplementedError

            def set_many(
                self, entries: list[Any], ttl: dt.timedelta | None = None
            ) -> None:
                raise NotImplementedError

            def delete(self, key: str) -> bool:
                raise NotImplementedError

            def delete_many(self, keys: list[str]) -> int:
                raise NotImplementedError

            def touch(self, key: str, ttl: dt.timedelta) -> bool:
                raise NotImplementedError

        class S3Provider(PluginProvider):
            pass

        class AgentProvider(PluginProvider):
            pass

        class SecretsProvider(PluginProvider):
            def get_secret(self, name: str) -> str:
                raise NotImplementedError

        class WorkflowProvider(PluginProvider):
            pass

        class Closer:
            def close(self) -> None:
                raise NotImplementedError

        class ExternalTokenValidator:
            def validate_external_token(self, token: str) -> Any:
                raise NotImplementedError

        class HealthChecker:
            def health_check(self) -> None:
                raise NotImplementedError

        class MetadataProvider:
            def metadata(self) -> ProviderMetadata:
                raise NotImplementedError

        class SessionTTLProvider:
            def session_ttl(self) -> dt.timedelta:
                raise NotImplementedError

        class WarningsProvider:
            def warnings(self) -> list[str]:
                raise NotImplementedError
from ._serialization import json_body

json_format: Any = cast(Any, None)
try:
    from google.protobuf import json_format as _json_format
except ModuleNotFoundError:
    pass
else:
    json_format = _json_format

grpc: Any = cast(Any, None)
empty_pb2: Any = cast(Any, None)
duration_pb2: Any = cast(Any, None)
plugin_pb2: Any = cast(Any, None)
plugin_pb2_grpc: Any = cast(Any, None)
runtime_pb2: Any = cast(Any, None)
runtime_pb2_grpc: Any = cast(Any, None)
authentication_pb2: Any = cast(Any, None)
authentication_pb2_grpc: Any = cast(Any, None)
cache_pb2: Any = cast(Any, None)
cache_pb2_grpc: Any = cast(Any, None)
s3_pb2_grpc: Any = cast(Any, None)
secrets_pb2: Any = cast(Any, None)
secrets_pb2_grpc: Any = cast(Any, None)
agent_pb2_grpc: Any = cast(Any, None)
workflow_pb2: Any = cast(Any, None)
workflow_pb2_grpc: Any = cast(Any, None)
authorization_pb2: Any = cast(Any, None)
authorization_pb2_grpc: Any = cast(Any, None)

ENV_PROVIDER_SOCKET: Final[str] = "GESTALT_PLUGIN_SOCKET"
ENV_WRITE_CATALOG: Final[str] = "GESTALT_PLUGIN_WRITE_CATALOG"
ENV_WRITE_MANIFEST_METADATA: Final[str] = "GESTALT_PLUGIN_WRITE_MANIFEST_METADATA"
CURRENT_PROTOCOL_VERSION: Final[int] = 3
GRPC_SERVER_MAX_WORKERS: Final[int] = 4
GRPC_SHUTDOWN_GRACE_SECONDS: Final[int] = 2
USAGE: Final[str] = (
    "usage: python -m gestalt._runtime ROOT MODULE[:ATTRIBUTE] [RUNTIME_KIND]"
)


def _ensure_grpc_runtime() -> None:
    global json_format
    global authentication_pb2
    global authentication_pb2_grpc
    global authorization_pb2
    global authorization_pb2_grpc
    global cache_pb2
    global cache_pb2_grpc
    global duration_pb2
    global empty_pb2
    global grpc
    global plugin_pb2
    global plugin_pb2_grpc
    global runtime_pb2
    global runtime_pb2_grpc
    global s3_pb2_grpc
    global secrets_pb2
    global secrets_pb2_grpc
    global agent_pb2_grpc
    global workflow_pb2
    global workflow_pb2_grpc

    if grpc is not None:
        return

    import grpc as _grpc
    from google.protobuf import duration_pb2 as _duration_pb2
    from google.protobuf import empty_pb2 as _empty_pb2
    from google.protobuf import json_format as _json_format

    from .gen.v1 import agent_pb2_grpc as _agent_pb2_grpc
    from .gen.v1 import authentication_pb2 as _authentication_pb2
    from .gen.v1 import authentication_pb2_grpc as _authentication_pb2_grpc
    from .gen.v1 import authorization_pb2 as _authorization_pb2
    from .gen.v1 import authorization_pb2_grpc as _authorization_pb2_grpc
    from .gen.v1 import cache_pb2 as _cache_pb2
    from .gen.v1 import cache_pb2_grpc as _cache_pb2_grpc
    from .gen.v1 import plugin_pb2 as _plugin_pb2
    from .gen.v1 import plugin_pb2_grpc as _plugin_pb2_grpc
    from .gen.v1 import runtime_pb2 as _runtime_pb2
    from .gen.v1 import runtime_pb2_grpc as _runtime_pb2_grpc
    from .gen.v1 import s3_pb2_grpc as _s3_pb2_grpc
    from .gen.v1 import secrets_pb2 as _secrets_pb2
    from .gen.v1 import secrets_pb2_grpc as _secrets_pb2_grpc
    from .gen.v1 import workflow_pb2 as _workflow_pb2
    from .gen.v1 import workflow_pb2_grpc as _workflow_pb2_grpc

    grpc = _grpc
    json_format = _json_format
    duration_pb2 = _duration_pb2
    empty_pb2 = _empty_pb2
    plugin_pb2 = _plugin_pb2
    plugin_pb2_grpc = _plugin_pb2_grpc
    runtime_pb2 = _runtime_pb2
    runtime_pb2_grpc = _runtime_pb2_grpc
    authentication_pb2 = _authentication_pb2
    authentication_pb2_grpc = _authentication_pb2_grpc
    authorization_pb2 = _authorization_pb2
    authorization_pb2_grpc = _authorization_pb2_grpc
    cache_pb2 = _cache_pb2
    cache_pb2_grpc = _cache_pb2_grpc
    s3_pb2_grpc = _s3_pb2_grpc
    secrets_pb2 = _secrets_pb2
    secrets_pb2_grpc = _secrets_pb2_grpc
    agent_pb2_grpc = _agent_pb2_grpc
    workflow_pb2 = _workflow_pb2
    workflow_pb2_grpc = _workflow_pb2_grpc


@dataclass(frozen=True)
class RuntimeArgs:
    target: str
    root: pathlib.Path | None = None
    plugin_name: str | None = None
    runtime_kind: str | None = None


def _grpc_handler(label: str):
    def decorator(fn):
        @functools.wraps(fn)
        def wrapper(self, request, context):
            _ensure_grpc_runtime()
            try:
                return fn(self, request, context)
            except Exception as error:
                if context.code() is not None:
                    raise
                traceback.print_exception(error)
                context.abort(grpc.StatusCode.UNKNOWN, f"{label}: {error}")

        return wrapper

    return decorator


def _abort_if_protocol_version_mismatch(
    protocol_version: int,
    context: Any,
) -> bool:
    _ensure_grpc_runtime()
    if protocol_version == CURRENT_PROTOCOL_VERSION:
        return False
    context.abort(
        grpc.StatusCode.FAILED_PRECONDITION,
        f"host requested protocol version {protocol_version}, provider requires {CURRENT_PROTOCOL_VERSION}",
    )
    return True


def serve(
    target: Plugin | PluginProviderAdapter | PluginProvider,
    *,
    runtime_kind: ProviderKind | str | None = None,
) -> None:
    _ensure_grpc_runtime()
    socket_path = _socket_path_from_env()
    _remove_stale_socket(socket_path)

    server = grpc.server(
        futures.ThreadPoolExecutor(max_workers=GRPC_SERVER_MAX_WORKERS)
    )
    servable = _servable_target(target, runtime_kind=runtime_kind)
    _register_services(server=server, servable=servable)
    server.add_insecure_port(f"unix:{socket_path}")
    close_provider = _close_once_callable(servable)
    server.start()
    _register_shutdown_handlers(server, close_provider)
    try:
        server.wait_for_termination()
    finally:
        close_provider()


def main(argv: list[str] | None = None) -> int:
    runtime_args = _parse_runtime_args(sys.argv[1:] if argv is None else argv)
    if runtime_args is None:
        print(USAGE, file=sys.stderr)
        return 2

    target = _load_target(runtime_args)
    if runtime_args.plugin_name and isinstance(target, Plugin):
        target.name = runtime_args.plugin_name

    catalog_path = os.environ.get(ENV_WRITE_CATALOG)
    manifest_metadata_path = os.environ.get(ENV_WRITE_MANIFEST_METADATA)
    if catalog_path or manifest_metadata_path:
        if not isinstance(target, Plugin):
            raise RuntimeError(
                "catalog and manifest metadata export are only supported for integration plugins"
            )
        if catalog_path:
            target.write_catalog(catalog_path)
        if manifest_metadata_path:
            target.write_manifest_metadata(manifest_metadata_path)
        return 0

    serve(target, runtime_kind=runtime_args.runtime_kind)
    return 0


def _parse_runtime_args(args: list[str]) -> RuntimeArgs | None:
    if args:
        if len(args) not in (2, 3):
            return None

        root, target = args[:2]
        runtime_kind = _normalized_runtime_kind(
            args[2] if len(args) == 3 else ProviderKind.INTEGRATION.value
        ).value
        return RuntimeArgs(
            target=target,
            root=pathlib.Path(root),
            runtime_kind=runtime_kind,
        )

    bundled_config = read_bundled_plugin_config(
        bundle_root=pathlib.Path(
            getattr(sys, "_MEIPASS", pathlib.Path(__file__).resolve().parent)
        )
    )
    if bundled_config is None:
        return None

    return RuntimeArgs(
        target=bundled_config.target,
        plugin_name=bundled_config.plugin_name,
        runtime_kind=_normalized_runtime_kind(
            bundled_config.runtime_kind or ProviderKind.INTEGRATION.value
        ).value,
    )


def _load_target(args: RuntimeArgs) -> Plugin | PluginProviderAdapter | PluginProvider:
    if args.root is not None:
        root = str(args.root)
        if root not in sys.path:
            sys.path.insert(0, root)

    plugin_target = parse_plugin_target(args.target)
    module = importlib.import_module(plugin_target.module_name)
    resolved_kind = _normalized_runtime_kind(args.runtime_kind)
    if plugin_target.attribute_name is None:
        target = _module_target(module, resolved_kind)
    else:
        target = getattr(module, plugin_target.attribute_name, None)

    if isinstance(target, (Plugin, PluginProviderAdapter)):
        return target

    if resolved_kind == ProviderKind.AUTHENTICATION and isinstance(
        target, AuthenticationProvider
    ):
        return _authentication_runtime_plugin(target)
    if resolved_kind == ProviderKind.CACHE and isinstance(target, CacheProvider):
        return _cache_runtime_plugin(target)
    if resolved_kind == ProviderKind.S3 and isinstance(target, S3Provider):
        return _s3_runtime_plugin(target)
    if resolved_kind == ProviderKind.AGENT and isinstance(target, AgentProvider):
        return _agent_runtime_plugin(target)
    if resolved_kind == ProviderKind.WORKFLOW and isinstance(target, WorkflowProvider):
        return _workflow_runtime_plugin(target)
    if resolved_kind == ProviderKind.SECRETS and isinstance(target, SecretsProvider):
        return _secrets_runtime_plugin(target)
    if isinstance(target, PluginProvider):
        raise RuntimeError(
            "providers must be wrapped in gestalt.PluginProviderAdapter unless runtime_kind is authentication, cache, s3, agent, workflow, or secrets"
        )
    raise RuntimeError(f"{args.target} did not resolve to a supported gestalt target")


def _module_target(
    module: Any,
    runtime_kind: ProviderKind,
) -> Plugin | PluginProviderAdapter | PluginProvider | Any:
    if runtime_kind == ProviderKind.INTEGRATION:
        return _module_plugin(module)

    for attribute_name in (runtime_kind.value, "provider", "plugin"):
        value = getattr(module, attribute_name, None)
        if value is not None:
            return value
    return None


def _socket_path_from_env() -> pathlib.Path:
    socket_path = os.environ.get(ENV_PROVIDER_SOCKET)
    if not socket_path:
        raise RuntimeError(f"{ENV_PROVIDER_SOCKET} is required")
    return pathlib.Path(socket_path)


def _remove_stale_socket(socket_path: pathlib.Path) -> None:
    if socket_path.exists():
        socket_path.unlink()


def _register_shutdown_handlers(server: Any, close_provider: Any) -> None:
    def _shutdown(_signum: int, _frame: Any) -> None:
        server.stop(grace=GRPC_SHUTDOWN_GRACE_SECONDS)
        close_provider()

    signal.signal(signal.SIGTERM, _shutdown)
    signal.signal(signal.SIGINT, _shutdown)


def _register_services(
    *, server: Any, servable: Plugin | PluginProviderAdapter
) -> None:
    _ensure_grpc_runtime()
    if isinstance(servable, Plugin):
        plugin_pb2_grpc.add_IntegrationProviderServicer_to_server(
            _provider_servicer(plugin=servable),
            server,
        )
        return

    servable.register_services(server, servable.provider)


def _close_once_callable(target: Plugin | PluginProviderAdapter) -> Any:
    provider = target.provider if isinstance(target, PluginProviderAdapter) else target
    closed = False

    def _close() -> None:
        nonlocal closed
        if closed:
            return
        closed = True
        if isinstance(provider, Closer):
            provider.close()

    return _close


def _servable_target(
    target: Plugin | PluginProviderAdapter | PluginProvider,
    *,
    runtime_kind: ProviderKind | str | None,
) -> Plugin | PluginProviderAdapter:
    if isinstance(target, (Plugin, PluginProviderAdapter)):
        return target

    kind = _normalized_runtime_kind(runtime_kind)
    if kind == ProviderKind.AUTHENTICATION and isinstance(
        target, AuthenticationProvider
    ):
        return _authentication_runtime_plugin(target)
    if kind == ProviderKind.CACHE and isinstance(target, CacheProvider):
        return _cache_runtime_plugin(target)
    if kind == ProviderKind.S3 and isinstance(target, S3Provider):
        return _s3_runtime_plugin(target)
    if kind == ProviderKind.AGENT and isinstance(target, AgentProvider):
        return _agent_runtime_plugin(target)
    if kind == ProviderKind.WORKFLOW and isinstance(target, WorkflowProvider):
        return _workflow_runtime_plugin(target)
    if kind == ProviderKind.SECRETS and isinstance(target, SecretsProvider):
        return _secrets_runtime_plugin(target)
    raise RuntimeError("unsupported runtime target")


def _authentication_runtime_plugin(
    provider: AuthenticationProvider,
) -> PluginProviderAdapter:
    return PluginProviderAdapter(
        kind=ProviderKind.AUTHENTICATION,
        provider=provider,
        register_services=_register_authentication_services,
    )


def _register_authentication_services(server: Any, provider: PluginProvider) -> None:
    _ensure_grpc_runtime()
    runtime_pb2_grpc.add_ProviderLifecycleServicer_to_server(
        _runtime_servicer(provider=provider, kind=ProviderKind.AUTHENTICATION),
        server,
    )
    authentication_pb2_grpc.add_AuthenticationProviderServicer_to_server(
        _authentication_servicer(provider=provider),
        server,
    )


def _s3_runtime_plugin(provider: S3Provider) -> PluginProviderAdapter:
    return PluginProviderAdapter(
        kind=ProviderKind.S3,
        provider=provider,
        register_services=_register_s3_services,
    )


def _register_s3_services(server: Any, provider: PluginProvider) -> None:
    _ensure_grpc_runtime()
    runtime_pb2_grpc.add_ProviderLifecycleServicer_to_server(
        _runtime_servicer(provider=provider, kind=ProviderKind.S3),
        server,
    )
    s3_pb2_grpc.add_S3Servicer_to_server(provider, server)


def _agent_runtime_plugin(provider: AgentProvider) -> PluginProviderAdapter:
    return PluginProviderAdapter(
        kind=ProviderKind.AGENT,
        provider=provider,
        register_services=_register_agent_services,
    )


def _register_agent_services(server: Any, provider: PluginProvider) -> None:
    _ensure_grpc_runtime()
    runtime_pb2_grpc.add_ProviderLifecycleServicer_to_server(
        _runtime_servicer(provider=provider, kind=ProviderKind.AGENT),
        server,
    )
    agent_pb2_grpc.add_AgentProviderServicer_to_server(provider, server)


def _workflow_runtime_plugin(provider: WorkflowProvider) -> PluginProviderAdapter:
    return PluginProviderAdapter(
        kind=ProviderKind.WORKFLOW,
        provider=provider,
        register_services=_register_workflow_services,
    )


def _register_workflow_services(server: Any, provider: PluginProvider) -> None:
    _ensure_grpc_runtime()
    runtime_pb2_grpc.add_ProviderLifecycleServicer_to_server(
        _runtime_servicer(provider=provider, kind=ProviderKind.WORKFLOW),
        server,
    )
    workflow_pb2_grpc.add_WorkflowProviderServicer_to_server(provider, server)


def _secrets_runtime_plugin(provider: SecretsProvider) -> PluginProviderAdapter:
    return PluginProviderAdapter(
        kind=ProviderKind.SECRETS,
        provider=provider,
        register_services=_register_secrets_services,
    )


def _register_secrets_services(server: Any, provider: PluginProvider) -> None:
    _ensure_grpc_runtime()
    runtime_pb2_grpc.add_ProviderLifecycleServicer_to_server(
        _runtime_servicer(provider=provider, kind=ProviderKind.SECRETS),
        server,
    )
    secrets_pb2_grpc.add_SecretsProviderServicer_to_server(
        _secrets_servicer(provider=provider),
        server,
    )


def _cache_runtime_plugin(provider: CacheProvider) -> PluginProviderAdapter:
    return PluginProviderAdapter(
        kind=ProviderKind.CACHE,
        provider=provider,
        register_services=_register_cache_services,
    )


def _register_cache_services(server: Any, provider: PluginProvider) -> None:
    _ensure_grpc_runtime()
    runtime_pb2_grpc.add_ProviderLifecycleServicer_to_server(
        _runtime_servicer(provider=provider, kind=ProviderKind.CACHE),
        server,
    )
    cache_pb2_grpc.add_CacheServicer_to_server(
        _cache_servicer(provider=provider),
        server,
    )


def _provider_servicer(*, plugin: Plugin) -> Any:
    _ensure_grpc_runtime()
    class ProviderServicer(plugin_pb2_grpc.IntegrationProviderServicer):
        def GetMetadata(self, _request: Any, _context: Any) -> Any:
            return plugin_pb2.ProviderMetadata(
                supports_session_catalog=plugin.supports_session_catalog(),
                supports_post_connect=plugin.supports_post_connect(),
                min_protocol_version=CURRENT_PROTOCOL_VERSION,
                max_protocol_version=CURRENT_PROTOCOL_VERSION,
            )

        @_grpc_handler("configure provider")
        def StartProvider(self, request: Any, context: Any) -> Any:
            if _abort_if_protocol_version_mismatch(request.protocol_version, context):
                return None
            plugin.configure_provider(
                request.name,
                _message_to_dict(
                    field_name="config",
                    message=request.config,
                    request=request,
                ),
            )
            return plugin_pb2.StartProviderResponse(
                protocol_version=CURRENT_PROTOCOL_VERSION
            )

        def Execute(self, request: Any, _context: Any) -> Any:
            try:
                result = plugin.execute(
                    request.operation,
                    _message_to_dict(
                        field_name="params",
                        message=request.params,
                        request=request,
                    ),
                    _plugin_request(request),
                )
            except Exception as error:
                traceback.print_exception(error)
                status = HTTPStatus.INTERNAL_SERVER_ERROR
                body = json_body({"error": INTERNAL_ERROR_MESSAGE})
                return plugin_pb2.OperationResult(status=status, body=body)
            return plugin_pb2.OperationResult(status=result.status, body=result.body)

        def ResolveHTTPSubject(self, request: Any, context: Any) -> Any:
            if not plugin.supports_http_subject():
                return plugin_pb2.ResolveHTTPSubjectResponse()

            try:
                subject = plugin.resolve_http_subject(
                    _http_subject_request(request.request),
                    _plugin_request(request),
                )
            except HTTPSubjectResolutionError as error:
                return plugin_pb2.ResolveHTTPSubjectResponse(
                    reject_status=error.status,
                    reject_message=error.message,
                )
            except Exception as error:
                traceback.print_exception(error)
                return context.abort(
                    grpc.StatusCode.UNKNOWN,
                    f"resolve http subject: {error}",
                )

            if subject is None:
                return plugin_pb2.ResolveHTTPSubjectResponse()

            return plugin_pb2.ResolveHTTPSubjectResponse(
                subject=plugin_pb2.SubjectContext(
                    id=subject.id,
                    kind=subject.kind,
                    display_name=subject.display_name,
                    auth_source=subject.auth_source,
                )
            )

        def GetSessionCatalog(self, request: Any, context: Any) -> Any:
            if not plugin.supports_session_catalog():
                return context.abort(
                    grpc.StatusCode.UNIMPLEMENTED,
                    "provider does not support session catalogs",
                )

            try:
                catalog = plugin.catalog_for_request(_plugin_request(request))
            except Exception as error:
                return context.abort(
                    grpc.StatusCode.UNKNOWN,
                    f"session catalog: {error}",
                )

            try:
                proto_catalog = catalog_to_proto(catalog)
            except Exception as error:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    f"encode session catalog: {error}",
                )

            return plugin_pb2.GetSessionCatalogResponse(catalog=proto_catalog)

        def PostConnect(self, request: Any, context: Any) -> Any:
            if not plugin.supports_post_connect():
                return context.abort(
                    grpc.StatusCode.UNIMPLEMENTED,
                    "provider does not support post connect",
                )

            try:
                metadata = plugin.post_connect_metadata(_connected_token(request.token))
            except Exception as error:
                return context.abort(
                    grpc.StatusCode.UNKNOWN,
                    f"post connect: {error}",
                )

            return plugin_pb2.PostConnectResponse(metadata=metadata or {})

    return ProviderServicer()


def _runtime_servicer(*, provider: PluginProvider, kind: ProviderKind) -> Any:
    _ensure_grpc_runtime()
    class RuntimeServicer(runtime_pb2_grpc.ProviderLifecycleServicer):
        def GetProviderIdentity(self, _request: Any, _context: Any) -> Any:
            metadata = _provider_metadata(provider=provider, kind=kind)
            return runtime_pb2.ProviderIdentity(
                kind=_provider_kind_to_proto(metadata.kind),
                name=metadata.name,
                display_name=metadata.display_name,
                description=metadata.description,
                version=metadata.version,
                warnings=_provider_warnings(provider),
                min_protocol_version=CURRENT_PROTOCOL_VERSION,
                max_protocol_version=CURRENT_PROTOCOL_VERSION,
            )

        @_grpc_handler("configure provider")
        def ConfigureProvider(self, request: Any, context: Any) -> Any:
            if _abort_if_protocol_version_mismatch(request.protocol_version, context):
                return None
            config = _message_to_dict(
                field_name="config",
                message=request.config,
                request=request,
            )
            provider.configure(request.name, config)
            return runtime_pb2.ConfigureProviderResponse(
                protocol_version=CURRENT_PROTOCOL_VERSION
            )

        def HealthCheck(self, _request: Any, _context: Any) -> Any:
            if isinstance(provider, HealthChecker):
                try:
                    provider.health_check()
                except Exception as error:
                    return runtime_pb2.HealthCheckResponse(
                        ready=False,
                        message=str(error),
                    )
                return runtime_pb2.HealthCheckResponse(ready=True)
            return runtime_pb2.HealthCheckResponse(ready=True)

    return RuntimeServicer()


def _authentication_servicer(*, provider: PluginProvider) -> Any:
    _ensure_grpc_runtime()
    auth_provider = cast(AuthenticationProvider, provider)

    class AuthenticationServicer(authentication_pb2_grpc.AuthenticationProviderServicer):
        @_grpc_handler("begin login")
        def BeginLogin(self, request: Any, context: Any) -> Any:
            response = auth_provider.begin_login(request)
            if response is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "authentication provider returned nil response",
                )
            return response

        @_grpc_handler("complete login")
        def CompleteLogin(self, request: Any, context: Any) -> Any:
            user = auth_provider.complete_login(request)
            if user is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "authentication provider returned nil user",
                )
            return user

        def ValidateExternalToken(self, request: Any, context: Any) -> Any:
            if not isinstance(auth_provider, ExternalTokenValidator):
                return context.abort(
                    grpc.StatusCode.UNIMPLEMENTED,
                    "authentication provider does not support external token validation",
                )
            try:
                user = auth_provider.validate_external_token(request.token)
            except Exception as error:
                traceback.print_exception(error)
                return context.abort(
                    grpc.StatusCode.UNKNOWN,
                    f"validate external token: {error}",
                )
            if user is None:
                return context.abort(
                    grpc.StatusCode.NOT_FOUND,
                    "token not recognized",
                )
            return user

        def GetSessionSettings(self, request: Any, context: Any) -> Any:
            if not isinstance(auth_provider, SessionTTLProvider):
                return context.abort(
                    grpc.StatusCode.UNIMPLEMENTED,
                    "authentication provider does not expose session settings",
                )
            ttl = auth_provider.session_ttl()
            seconds = int(ttl.total_seconds())
            if seconds < 0:
                seconds = 0
            return authentication_pb2.AuthSessionSettings(session_ttl_seconds=seconds)

    return AuthenticationServicer()


def _secrets_servicer(*, provider: PluginProvider) -> Any:
    _ensure_grpc_runtime()
    secrets_provider = cast(SecretsProvider, provider)

    class SecretsServicer(secrets_pb2_grpc.SecretsProviderServicer):
        @_grpc_handler("get secret")
        def GetSecret(self, request: Any, context: Any) -> Any:
            value = secrets_provider.get_secret(request.name)
            return secrets_pb2.GetSecretResponse(value=value)

    return SecretsServicer()


def _cache_servicer(*, provider: PluginProvider) -> Any:
    _ensure_grpc_runtime()
    from ._cache import CacheEntry

    cache_provider = cast(CacheProvider, provider)

    class CacheServicer(cache_pb2_grpc.CacheServicer):
        @_grpc_handler("cache get")
        def Get(self, request: Any, _context: Any) -> Any:
            value = cache_provider.get(request.key)
            if value is None:
                return cache_pb2.CacheGetResponse(found=False, value=b"")
            return cache_pb2.CacheGetResponse(found=True, value=bytes(value))

        @_grpc_handler("cache get_many")
        def GetMany(self, request: Any, _context: Any) -> Any:
            values = cache_provider.get_many(list(request.keys))
            entries = []
            for key in request.keys:
                value = values.get(key)
                if value is None:
                    entries.append(cache_pb2.CacheResult(key=key, found=False, value=b""))
                else:
                    entries.append(
                        cache_pb2.CacheResult(key=key, found=True, value=bytes(value))
                    )
            return cache_pb2.CacheGetManyResponse(entries=entries)

        @_grpc_handler("cache set")
        def Set(self, request: Any, _context: Any) -> Any:
            cache_provider.set(
                request.key,
                bytes(request.value),
                _duration_to_timedelta(request.ttl),
            )
            return empty_pb2.Empty()

        @_grpc_handler("cache set_many")
        def SetMany(self, request: Any, _context: Any) -> Any:
            cache_provider.set_many(
                [CacheEntry(key=entry.key, value=bytes(entry.value)) for entry in request.entries],
                _duration_to_timedelta(request.ttl),
            )
            return empty_pb2.Empty()

        @_grpc_handler("cache delete")
        def Delete(self, request: Any, _context: Any) -> Any:
            return cache_pb2.CacheDeleteResponse(
                deleted=bool(cache_provider.delete(request.key))
            )

        @_grpc_handler("cache delete_many")
        def DeleteMany(self, request: Any, _context: Any) -> Any:
            return cache_pb2.CacheDeleteManyResponse(
                deleted=int(cache_provider.delete_many(list(request.keys)))
            )

        @_grpc_handler("cache touch")
        def Touch(self, request: Any, _context: Any) -> Any:
            return cache_pb2.CacheTouchResponse(
                touched=bool(
                    cache_provider.touch(
                        request.key,
                        _duration_to_timedelta(request.ttl) or dt.timedelta(),
                    )
                )
            )

    return CacheServicer()


def _plugin_request(request: Any) -> Request:
    return Request(
        token=getattr(request, "token", ""),
        connection_params=dict(getattr(request, "connection_params", {})),
        subject=_subject_from_proto(getattr(request, "context", None)),
        credential=_credential_from_proto(getattr(request, "context", None)),
        access=_access_from_proto(getattr(request, "context", None)),
        workflow=_workflow_from_proto(getattr(request, "context", None)),
        invocation_token=getattr(request, "invocation_token", ""),
    )


def _http_subject_request(request: Any) -> HTTPSubjectRequest:
    if request is None:
        return HTTPSubjectRequest()
    return HTTPSubjectRequest(
        binding=getattr(request, "binding", ""),
        method=getattr(request, "method", ""),
        path=getattr(request, "path", ""),
        content_type=getattr(request, "content_type", ""),
        headers=_string_lists_from_proto_map(getattr(request, "headers", {})),
        query=_string_lists_from_proto_map(getattr(request, "query", {})),
        params=_message_to_dict(
            field_name="params",
            message=getattr(request, "params", None),
            request=request,
        ),
        raw_body=bytes(getattr(request, "raw_body", b"")),
        security_scheme=getattr(request, "security_scheme", ""),
        verified_subject=getattr(request, "verified_subject", ""),
        verified_claims=dict(getattr(request, "verified_claims", {})),
    )


def _subject_from_proto(request_context: Any) -> Subject:
    if request_context is None:
        return Subject()
    subject = getattr(request_context, "subject", None)
    if subject is None:
        return Subject()
    return Subject(
        id=getattr(subject, "id", ""),
        kind=getattr(subject, "kind", ""),
        display_name=getattr(subject, "display_name", ""),
        auth_source=getattr(subject, "auth_source", ""),
    )


def _credential_from_proto(request_context: Any) -> Credential:
    if request_context is None:
        return Credential()
    credential = getattr(request_context, "credential", None)
    if credential is None:
        return Credential()
    return Credential(
        mode=getattr(credential, "mode", ""),
        subject_id=getattr(credential, "subject_id", ""),
        connection=getattr(credential, "connection", ""),
        instance=getattr(credential, "instance", ""),
    )


def _access_from_proto(request_context: Any) -> Access:
    if request_context is None:
        return Access()
    access = getattr(request_context, "access", None)
    if access is None:
        return Access()
    return Access(
        policy=getattr(access, "policy", ""),
        role=getattr(access, "role", ""),
    )


def _workflow_from_proto(request_context: Any) -> dict[str, Any]:
    if request_context is None:
        return {}
    if hasattr(request_context, "HasField") and not request_context.HasField("workflow"):
        return {}
    workflow = getattr(request_context, "workflow", None)
    if workflow is None:
        return {}
    return cast(
        dict[str, Any],
        json_format.MessageToDict(
            workflow,
            preserving_proto_field_name=True,
        ),
    )


def _string_lists_from_proto_map(values: Any) -> dict[str, list[str]]:
    return {
        str(key): list(getattr(value, "values", ()))
        for key, value in dict(values or {}).items()
    }


def _message_to_dict(
    *,
    field_name: str,
    message: Any,
    request: Any,
) -> dict[str, Any]:
    if not request.HasField(field_name):
        return {}

    return json_format.MessageToDict(
        message,
        preserving_proto_field_name=True,
    )


def _provider_metadata(
    *, provider: PluginProvider, kind: ProviderKind
) -> ProviderMetadata:
    if isinstance(provider, MetadataProvider):
        metadata = provider.metadata()
        if isinstance(metadata, ProviderMetadata):
            return metadata
    return ProviderMetadata(kind=kind)


def _provider_warnings(provider: PluginProvider) -> list[str]:
    if isinstance(provider, WarningsProvider):
        return list(provider.warnings())
    return []


def _provider_kind_to_proto(kind: ProviderKind | str) -> Any:
    _ensure_grpc_runtime()
    normalized = _normalized_runtime_kind(kind)
    return {
        ProviderKind.INTEGRATION: runtime_pb2.ProviderKind.PROVIDER_KIND_INTEGRATION,
        ProviderKind.AUTHENTICATION: runtime_pb2.ProviderKind.PROVIDER_KIND_AUTHENTICATION,
        ProviderKind.CACHE: runtime_pb2.ProviderKind.PROVIDER_KIND_CACHE,
        ProviderKind.S3: runtime_pb2.ProviderKind.PROVIDER_KIND_S3,
        ProviderKind.AGENT: runtime_pb2.ProviderKind.PROVIDER_KIND_AGENT,
        ProviderKind.WORKFLOW: runtime_pb2.ProviderKind.PROVIDER_KIND_WORKFLOW,
        ProviderKind.SECRETS: runtime_pb2.ProviderKind.PROVIDER_KIND_SECRETS,
        ProviderKind.TELEMETRY: runtime_pb2.ProviderKind.PROVIDER_KIND_TELEMETRY,
    }.get(normalized, runtime_pb2.ProviderKind.PROVIDER_KIND_UNSPECIFIED)


def _normalized_runtime_kind(kind: object | None) -> ProviderKind:
    if kind is None:
        return ProviderKind.INTEGRATION
    if isinstance(kind, ProviderKind):
        return kind
    if isinstance(kind, str):
        normalized = kind.strip().lower()
        if normalized == "":
            return ProviderKind.INTEGRATION
        if normalized == "auth":
            return ProviderKind.AUTHENTICATION
        try:
            return ProviderKind(normalized)
        except ValueError as exc:
            raise ValueError(f"unsupported runtime kind: {kind!r}") from exc
    raise TypeError(f"unsupported runtime kind: {kind!r}")


def _duration_to_timedelta(duration: Any) -> dt.timedelta | None:
    if duration is None:
        return None
    seconds = getattr(duration, "seconds", 0)
    nanos = getattr(duration, "nanos", 0)
    if seconds == 0 and nanos == 0:
        return None
    return dt.timedelta(seconds=seconds, microseconds=nanos // 1000)


def _connected_token(token: Any) -> ConnectedToken:
    metadata_json = getattr(token, "metadata_json", "") or ""
    metadata: dict[str, str] = {}
    if metadata_json:
        try:
            parsed = json.loads(metadata_json)
        except json.JSONDecodeError:
            metadata = {}
        else:
            if isinstance(parsed, dict):
                metadata = {str(key): str(value) for key, value in parsed.items()}
    return ConnectedToken(
        id=getattr(token, "id", ""),
        subject_id=getattr(token, "user_id", ""),
        integration=getattr(token, "integration", ""),
        connection=getattr(token, "connection", ""),
        instance=getattr(token, "instance", ""),
        access_token=getattr(token, "access_token", ""),
        refresh_token=getattr(token, "refresh_token", ""),
        scopes=getattr(token, "scopes", ""),
        expires_at=_timestamp_to_datetime(getattr(token, "expires_at", None)),
        last_refreshed_at=_timestamp_to_datetime(getattr(token, "last_refreshed_at", None)),
        refresh_error_count=int(getattr(token, "refresh_error_count", 0) or 0),
        metadata_json=metadata_json,
        metadata=metadata,
        created_at=_timestamp_to_datetime(getattr(token, "created_at", None)),
        updated_at=_timestamp_to_datetime(getattr(token, "updated_at", None)),
    )


def _timestamp_to_datetime(value: Any) -> dt.datetime | None:
    if value is None:
        return None
    seconds = getattr(value, "seconds", 0)
    nanos = getattr(value, "nanos", 0)
    if seconds == 0 and nanos == 0:
        return None
    if hasattr(value, "ToDatetime"):
        converted = value.ToDatetime()
        if converted.tzinfo is None:
            return converted.replace(tzinfo=dt.timezone.utc)
        return converted.astimezone(dt.timezone.utc)
    return dt.datetime.fromtimestamp(
        seconds + (nanos / 1_000_000_000),
        tz=dt.timezone.utc,
    )


if __name__ == "__main__":
    raise SystemExit(main())
