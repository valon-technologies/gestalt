import functools
import importlib
import os
import pathlib
import signal
import sys
import traceback
from concurrent import futures
from dataclasses import dataclass
from http import HTTPStatus
from typing import Any, Final, cast

import grpc
from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import json_format

from ._api import Credential, Request, Subject
from ._bootstrap import parse_plugin_target, read_bundled_plugin_config
from ._catalog import catalog_to_proto
from ._plugin import Plugin, _module_plugin
from ._providers import (
    AuthProvider,
    Closer,
    ExternalTokenValidator,
    HealthChecker,
    MetadataProvider,
    PluginProvider,
    PluginProviderAdapter,
    ProviderKind,
    ProviderMetadata,
    SecretsProvider,
    SessionTTLProvider,
    WarningsProvider,
)
from ._serialization import json_body
from .gen.v1 import auth_pb2 as _auth_pb2
from .gen.v1 import auth_pb2_grpc as _auth_pb2_grpc
from .gen.v1 import plugin_pb2 as _plugin_pb2
from .gen.v1 import plugin_pb2_grpc as _plugin_pb2_grpc
from .gen.v1 import runtime_pb2 as _runtime_pb2
from .gen.v1 import runtime_pb2_grpc as _runtime_pb2_grpc
from .gen.v1 import secrets_pb2 as _secrets_pb2
from .gen.v1 import secrets_pb2_grpc as _secrets_pb2_grpc

empty_pb2: Any = _empty_pb2
plugin_pb2: Any = _plugin_pb2
plugin_pb2_grpc: Any = _plugin_pb2_grpc
runtime_pb2: Any = _runtime_pb2
runtime_pb2_grpc: Any = _runtime_pb2_grpc
auth_pb2: Any = _auth_pb2
auth_pb2_grpc: Any = _auth_pb2_grpc
secrets_pb2: Any = _secrets_pb2
secrets_pb2_grpc: Any = _secrets_pb2_grpc

ENV_PROVIDER_SOCKET: Final[str] = "GESTALT_PLUGIN_SOCKET"
ENV_WRITE_CATALOG: Final[str] = "GESTALT_PLUGIN_WRITE_CATALOG"
CURRENT_PROTOCOL_VERSION: Final[int] = 2
GRPC_SERVER_MAX_WORKERS: Final[int] = 4
GRPC_SHUTDOWN_GRACE_SECONDS: Final[int] = 2
USAGE: Final[str] = "usage: python -m gestalt._runtime ROOT MODULE[:ATTRIBUTE] [RUNTIME_KIND]"
INTERNAL_ERROR_MESSAGE: Final[str] = "internal error"


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
            try:
                return fn(self, request, context)
            except Exception as error:
                if context.code() is not None:
                    raise
                traceback.print_exception(error)
                context.abort(grpc.StatusCode.UNKNOWN, f"{label}: {INTERNAL_ERROR_MESSAGE}")
        return wrapper
    return decorator


def serve(
    target: Plugin | PluginProviderAdapter | PluginProvider,
    *,
    runtime_kind: ProviderKind | str | None = None,
) -> None:
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
    if catalog_path:
        if not isinstance(target, Plugin):
            raise RuntimeError("catalog export is only supported for integration plugins")
        target.write_catalog(catalog_path)
        return 0

    serve(target, runtime_kind=runtime_args.runtime_kind)
    return 0


def _parse_runtime_args(args: list[str]) -> RuntimeArgs | None:
    if args:
        if len(args) not in (2, 3):
            return None

        root, target = args[:2]
        runtime_kind = args[2] if len(args) == 3 else ProviderKind.INTEGRATION.value
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
        runtime_kind=bundled_config.runtime_kind or ProviderKind.INTEGRATION.value,
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

    if resolved_kind == ProviderKind.AUTH and isinstance(target, AuthProvider):
        return _auth_runtime_plugin(target)
    if resolved_kind == ProviderKind.SECRETS and isinstance(target, SecretsProvider):
        return _secrets_runtime_plugin(target)
    if isinstance(target, PluginProvider):
        raise RuntimeError(
            "providers must be wrapped in gestalt.PluginProviderAdapter unless runtime_kind is auth or secrets"
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


def _register_services(*, server: Any, servable: Plugin | PluginProviderAdapter) -> None:
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
    if kind == ProviderKind.AUTH and isinstance(target, AuthProvider):
        return _auth_runtime_plugin(target)
    if kind == ProviderKind.SECRETS and isinstance(target, SecretsProvider):
        return _secrets_runtime_plugin(target)
    raise RuntimeError("unsupported runtime target")


def _auth_runtime_plugin(provider: AuthProvider) -> PluginProviderAdapter:
    return PluginProviderAdapter(
        kind=ProviderKind.AUTH,
        provider=provider,
        register_services=_register_auth_services,
    )


def _register_auth_services(server: Any, provider: PluginProvider) -> None:
    runtime_pb2_grpc.add_ProviderLifecycleServicer_to_server(
        _runtime_servicer(provider=provider, kind=ProviderKind.AUTH),
        server,
    )
    auth_pb2_grpc.add_AuthProviderServicer_to_server(
        _auth_servicer(provider=provider),
        server,
    )


def _secrets_runtime_plugin(provider: SecretsProvider) -> PluginProviderAdapter:
    return PluginProviderAdapter(
        kind=ProviderKind.SECRETS,
        provider=provider,
        register_services=_register_secrets_services,
    )


def _register_secrets_services(server: Any, provider: PluginProvider) -> None:
    runtime_pb2_grpc.add_ProviderLifecycleServicer_to_server(
        _runtime_servicer(provider=provider, kind=ProviderKind.SECRETS),
        server,
    )
    secrets_pb2_grpc.add_SecretsProviderServicer_to_server(
        _secrets_servicer(provider=provider),
        server,
    )


def _provider_servicer(*, plugin: Plugin) -> Any:
    class ProviderServicer(plugin_pb2_grpc.IntegrationProviderServicer):
        def GetMetadata(self, _request: Any, _context: Any) -> Any:
            return plugin_pb2.ProviderMetadata(
                supports_session_catalog=plugin.supports_session_catalog(),
                min_protocol_version=CURRENT_PROTOCOL_VERSION,
                max_protocol_version=CURRENT_PROTOCOL_VERSION,
            )

        def StartProvider(self, request: Any, _context: Any) -> Any:
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
                    Request(
                        token=request.token,
                        connection_params=dict(request.connection_params),
                        subject=_subject_from_proto(getattr(request, "context", None)),
                        credential=_credential_from_proto(getattr(request, "context", None)),
                    ),
                )
            except Exception as error:
                traceback.print_exception(error)
                status = HTTPStatus.INTERNAL_SERVER_ERROR
                body = json_body({"error": INTERNAL_ERROR_MESSAGE})
                return plugin_pb2.OperationResult(status=status, body=body)
            return plugin_pb2.OperationResult(status=result.status, body=result.body)

        def GetSessionCatalog(self, request: Any, context: Any) -> Any:
            if not plugin.supports_session_catalog():
                return context.abort(
                    grpc.StatusCode.UNIMPLEMENTED,
                    "provider does not support session catalogs",
                )

            try:
                catalog = plugin.catalog_for_request(_plugin_request(request))
            except Exception:
                return context.abort(
                    grpc.StatusCode.UNKNOWN,
                    f"session catalog: {INTERNAL_ERROR_MESSAGE}",
                )

            try:
                proto_catalog = catalog_to_proto(catalog)
            except Exception:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    f"encode session catalog: {INTERNAL_ERROR_MESSAGE}",
                )

            return plugin_pb2.GetSessionCatalogResponse(catalog=proto_catalog)

    return ProviderServicer()


def _runtime_servicer(*, provider: PluginProvider, kind: ProviderKind) -> Any:
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
            if request.protocol_version != CURRENT_PROTOCOL_VERSION:
                return context.abort(
                    grpc.StatusCode.FAILED_PRECONDITION,
                    f"host requested protocol version {request.protocol_version}, provider requires {CURRENT_PROTOCOL_VERSION}",
                )
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


def _auth_servicer(*, provider: PluginProvider) -> Any:
    auth_provider = cast(AuthProvider, provider)

    class AuthServicer(auth_pb2_grpc.AuthProviderServicer):
        @_grpc_handler("begin login")
        def BeginLogin(self, request: Any, context: Any) -> Any:
            response = auth_provider.begin_login(request)
            if response is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "auth provider returned nil response",
                )
            return response

        @_grpc_handler("complete login")
        def CompleteLogin(self, request: Any, context: Any) -> Any:
            user = auth_provider.complete_login(request)
            if user is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "auth provider returned nil user",
                )
            return user

        def ValidateExternalToken(self, request: Any, context: Any) -> Any:
            if not isinstance(auth_provider, ExternalTokenValidator):
                return context.abort(
                    grpc.StatusCode.UNIMPLEMENTED,
                    "auth provider does not support external token validation",
                )
            try:
                user = auth_provider.validate_external_token(request.token)
            except Exception as error:
                traceback.print_exception(error)
                return context.abort(
                    grpc.StatusCode.UNKNOWN,
                    f"validate external token: {INTERNAL_ERROR_MESSAGE}",
                )
            if user is None:
                return context.abort(
                    grpc.StatusCode.NOT_FOUND,
                    "token not recognized",
                )
            return user

        def GetSessionSettings(self, _request: Any, context: Any) -> Any:
            if not isinstance(auth_provider, SessionTTLProvider):
                return context.abort(
                    grpc.StatusCode.UNIMPLEMENTED,
                    "auth provider does not expose session settings",
                )
            ttl = auth_provider.session_ttl()
            seconds = int(ttl.total_seconds())
            if seconds < 0:
                seconds = 0
            return auth_pb2.AuthSessionSettings(session_ttl_seconds=seconds)

    return AuthServicer()


def _secrets_servicer(*, provider: PluginProvider) -> Any:
    secrets_provider = cast(SecretsProvider, provider)

    class SecretsServicer(secrets_pb2_grpc.SecretsProviderServicer):
        @_grpc_handler("get secret")
        def GetSecret(self, request: Any, context: Any) -> Any:
            value = secrets_provider.get_secret(request.name)
            return secrets_pb2.GetSecretResponse(value=value)

    return SecretsServicer()


def _plugin_request(request: Any) -> Request:
    return Request(
        token=getattr(request, "token", ""),
        connection_params=dict(getattr(request, "connection_params", {})),
        subject=_subject_from_proto(getattr(request, "context", None)),
        credential=_credential_from_proto(getattr(request, "context", None)),
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


def _provider_metadata(*, provider: PluginProvider, kind: ProviderKind) -> ProviderMetadata:
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
    normalized = _normalized_runtime_kind(kind)
    return {
        ProviderKind.INTEGRATION: runtime_pb2.ProviderKind.PROVIDER_KIND_INTEGRATION,
        ProviderKind.AUTH: runtime_pb2.ProviderKind.PROVIDER_KIND_AUTH,
        ProviderKind.SECRETS: runtime_pb2.ProviderKind.PROVIDER_KIND_SECRETS,
        ProviderKind.TELEMETRY: runtime_pb2.ProviderKind.PROVIDER_KIND_TELEMETRY,
    }.get(normalized, runtime_pb2.ProviderKind.PROVIDER_KIND_UNSPECIFIED)


def _normalized_runtime_kind(kind: ProviderKind | str | None) -> ProviderKind:
    if isinstance(kind, ProviderKind):
        return kind
    if isinstance(kind, str):
        try:
            return ProviderKind(kind.strip().lower())
        except ValueError:
            return ProviderKind.INTEGRATION
    return ProviderKind.INTEGRATION


if __name__ == "__main__":
    raise SystemExit(main())
