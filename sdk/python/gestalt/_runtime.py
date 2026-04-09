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
from http import HTTPStatus
from typing import Any, Final, cast

import grpc
from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import json_format
from google.protobuf import timestamp_pb2 as _timestamp_pb2

from ._api import Request
from ._catalog import catalog_to_json
from ._plugin import Plugin, _module_plugin
from ._providers import (
    UNIX_EPOCH,
    AuthenticatedUser,
    AuthProvider,
    BeginLoginRequest,
    Closer,
    CompleteLoginRequest,
    DatastoreProvider,
    ExternalTokenValidator,
    HealthChecker,
    MetadataProvider,
    OAuthRegistration,
    OAuthRegistrationStore,
    ProviderKind,
    ProviderMetadata,
    RuntimePlugin,
    RuntimeProvider,
    RuntimeProviderAdapter,
    SessionTTLProvider,
    StoredAPIToken,
    StoredIntegrationToken,
    StoredUser,
    WarningsProvider,
)
from ._serialization import json_body
from .gen.v1 import auth_pb2 as _auth_pb2
from .gen.v1 import auth_pb2_grpc as _auth_pb2_grpc
from .gen.v1 import datastore_pb2 as _datastore_pb2
from .gen.v1 import datastore_pb2_grpc as _datastore_pb2_grpc
from .gen.v1 import plugin_pb2 as _plugin_pb2
from .gen.v1 import plugin_pb2_grpc as _plugin_pb2_grpc
from .gen.v1 import runtime_pb2 as _runtime_pb2
from .gen.v1 import runtime_pb2_grpc as _runtime_pb2_grpc

UTC = dt.timezone.utc

BUNDLED_CONFIG_NAME: Final[str] = "gestalt-runtime.json"
ENV_PLUGIN_SOCKET: Final[str] = "GESTALT_PLUGIN_SOCKET"
ENV_WRITE_CATALOG: Final[str] = "GESTALT_PLUGIN_WRITE_CATALOG"
CURRENT_PROTOCOL_VERSION: Final[int] = 2
GRPC_SERVER_MAX_WORKERS: Final[int] = 4
GRPC_SHUTDOWN_GRACE_SECONDS: Final[int] = 2
USAGE: Final[str] = "usage: python -m gestalt._runtime ROOT MODULE[:ATTRIBUTE] [RUNTIME_KIND]"


@dataclass(frozen=True)
class PluginTarget:
    module_name: str
    attribute_name: str | None = None


@dataclass(frozen=True)
class BundledPluginConfig:
    target: str
    plugin_name: str | None = None
    runtime_kind: str | None = None


@dataclass(frozen=True)
class RuntimeArgs:
    target: str
    root: pathlib.Path | None = None
    plugin_name: str | None = None
    runtime_kind: str | None = None


def parse_plugin_target(target: str) -> PluginTarget:
    module_name, sep, attribute_name = target.partition(":")
    module_name = module_name.strip()
    attribute_name = attribute_name.strip() or None
    if not module_name:
        raise RuntimeError("tool.gestalt.plugin must be in module or module:attribute form")
    if sep and attribute_name is None:
        raise RuntimeError("tool.gestalt.plugin attribute is required when ':' is present")

    return PluginTarget(
        module_name=module_name,
        attribute_name=attribute_name,
    )


def read_bundled_plugin_config(*, bundle_root: pathlib.Path) -> BundledPluginConfig | None:
    config_path = bundle_root / BUNDLED_CONFIG_NAME
    if not config_path.exists():
        return None

    data = json.loads(config_path.read_text(encoding="utf-8"))
    target = str(data.get("target", "")).strip()
    if not target:
        raise RuntimeError(f"{config_path} is missing target")

    plugin_name = data.get("plugin_name")
    if plugin_name is not None:
        plugin_name = str(plugin_name).strip() or None

    runtime_kind = data.get("runtime_kind")
    if runtime_kind is not None:
        runtime_kind = str(runtime_kind).strip() or None

    return BundledPluginConfig(
        target=target,
        plugin_name=plugin_name,
        runtime_kind=runtime_kind,
    )


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
                context.abort(grpc.StatusCode.UNKNOWN, f"{label}: {error}")
        return wrapper
    return decorator


def serve(
    target: Plugin | RuntimePlugin | RuntimeProvider,
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


def _load_target(args: RuntimeArgs) -> Plugin | RuntimePlugin | RuntimeProvider:
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

    if isinstance(target, (Plugin, RuntimePlugin)):
        return target

    if resolved_kind == ProviderKind.AUTH and isinstance(target, AuthProvider):
        return _auth_runtime_plugin(target)
    if resolved_kind == ProviderKind.DATASTORE and isinstance(target, DatastoreProvider):
        return _datastore_runtime_plugin(target)
    if isinstance(target, RuntimeProvider):
        raise RuntimeError(
            "runtime providers must be wrapped in gestalt.RuntimeProviderAdapter (or the legacy gestalt.RuntimePlugin alias) unless runtime_kind is auth or datastore"
        )
    raise RuntimeError(f"{args.target} did not resolve to a supported gestalt target")


def _module_target(
    module: Any,
    runtime_kind: ProviderKind,
) -> Plugin | RuntimePlugin | RuntimeProvider | Any:
    if runtime_kind == ProviderKind.INTEGRATION:
        return _module_plugin(module)

    for attribute_name in (runtime_kind.value, "provider", "plugin"):
        value = getattr(module, attribute_name, None)
        if value is not None:
            return value
    return None


def _socket_path_from_env() -> pathlib.Path:
    socket_path = os.environ.get(ENV_PLUGIN_SOCKET)
    if not socket_path:
        raise RuntimeError(f"{ENV_PLUGIN_SOCKET} is required")
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


def _register_services(*, server: Any, servable: Plugin | RuntimePlugin) -> None:
    if isinstance(servable, Plugin):
        _plugin_pb2_grpc.add_ProviderPluginServicer_to_server(
            _provider_servicer(plugin=servable),
            server,
        )
        return

    servable.register_services(server, servable.provider)


def _close_once_callable(target: Plugin | RuntimePlugin) -> Any:
    provider = target.provider if isinstance(target, RuntimePlugin) else target
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
    target: Plugin | RuntimePlugin | RuntimeProvider,
    *,
    runtime_kind: ProviderKind | str | None,
) -> Plugin | RuntimePlugin:
    if isinstance(target, (Plugin, RuntimePlugin)):
        return target

    kind = _normalized_runtime_kind(runtime_kind)
    if kind == ProviderKind.AUTH and isinstance(target, AuthProvider):
        return _auth_runtime_plugin(target)
    if kind == ProviderKind.DATASTORE and isinstance(target, DatastoreProvider):
        return _datastore_runtime_plugin(target)
    raise RuntimeError("unsupported runtime target")


def _auth_runtime_plugin(provider: AuthProvider) -> RuntimeProviderAdapter:
    return RuntimeProviderAdapter(
        kind=ProviderKind.AUTH,
        provider=provider,
        register_services=_register_auth_services,
    )


def _datastore_runtime_plugin(provider: DatastoreProvider) -> RuntimeProviderAdapter:
    return RuntimeProviderAdapter(
        kind=ProviderKind.DATASTORE,
        provider=provider,
        register_services=_register_datastore_services,
    )


def _register_auth_services(server: Any, provider: RuntimeProvider) -> None:
    _runtime_pb2_grpc.add_PluginRuntimeServicer_to_server(
        _runtime_servicer(provider=provider, kind=ProviderKind.AUTH),
        server,
    )
    _auth_pb2_grpc.add_AuthPluginServicer_to_server(
        _auth_servicer(provider=provider),
        server,
    )


def _register_datastore_services(server: Any, provider: RuntimeProvider) -> None:
    _runtime_pb2_grpc.add_PluginRuntimeServicer_to_server(
        _runtime_servicer(provider=provider, kind=ProviderKind.DATASTORE),
        server,
    )
    _datastore_pb2_grpc.add_DatastorePluginServicer_to_server(
        _datastore_servicer(provider=provider),
        server,
    )


def _provider_servicer(*, plugin: Plugin) -> Any:
    class ProviderServicer(_plugin_pb2_grpc.ProviderPluginServicer):
        def GetMetadata(self, _request: Any, _context: Any) -> Any:
            return _plugin_pb2.ProviderMetadata(
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
            return _plugin_pb2.StartProviderResponse(
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
                    ),
                )
            except Exception as error:
                traceback.print_exception(error)
                status = HTTPStatus.INTERNAL_SERVER_ERROR
                body = json_body({"error": str(error)})
                return _plugin_pb2.OperationResult(status=status, body=body)
            return _plugin_pb2.OperationResult(status=result.status, body=result.body)

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
                raw_catalog = catalog_to_json(catalog)
            except Exception as error:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    f"encode session catalog: {error}",
                )

            return _plugin_pb2.GetSessionCatalogResponse(catalog_json=raw_catalog)

    return ProviderServicer()


def _runtime_servicer(*, provider: RuntimeProvider, kind: ProviderKind) -> Any:
    class RuntimeServicer(_runtime_pb2_grpc.PluginRuntimeServicer):
        def GetPluginMetadata(self, _request: Any, _context: Any) -> Any:
            metadata = _provider_metadata(provider=provider, kind=kind)
            return _runtime_pb2.PluginMetadata(
                kind=_provider_kind_to_proto(metadata.kind),
                name=metadata.name,
                display_name=metadata.display_name,
                description=metadata.description,
                version=metadata.version,
                warnings=_provider_warnings(provider),
                min_protocol_version=CURRENT_PROTOCOL_VERSION,
                max_protocol_version=CURRENT_PROTOCOL_VERSION,
            )

        @_grpc_handler("configure plugin")
        def ConfigurePlugin(self, request: Any, context: Any) -> Any:
            if request.protocol_version != CURRENT_PROTOCOL_VERSION:
                return context.abort(
                    grpc.StatusCode.FAILED_PRECONDITION,
                    f"host requested protocol version {request.protocol_version}, plugin requires {CURRENT_PROTOCOL_VERSION}",
                )
            config = _message_to_dict(
                field_name="config",
                message=request.config,
                request=request,
            )
            provider.configure(request.name, config)
            return _runtime_pb2.ConfigurePluginResponse(
                protocol_version=CURRENT_PROTOCOL_VERSION
            )

        def HealthCheck(self, _request: Any, _context: Any) -> Any:
            if isinstance(provider, HealthChecker):
                try:
                    provider.health_check()
                except Exception as error:
                    return _runtime_pb2.HealthCheckResponse(
                        ready=False,
                        message=str(error),
                    )
                return _runtime_pb2.HealthCheckResponse(ready=True)
            if kind == ProviderKind.DATASTORE:
                return _runtime_pb2.HealthCheckResponse(
                    ready=False,
                    message="datastore provider must implement HealthChecker",
                )
            return _runtime_pb2.HealthCheckResponse(ready=True)

    return RuntimeServicer()


def _auth_servicer(*, provider: RuntimeProvider) -> Any:
    auth_provider = cast(AuthProvider, provider)

    class AuthServicer(_auth_pb2_grpc.AuthPluginServicer):
        @_grpc_handler("begin login")
        def BeginLogin(self, request: Any, context: Any) -> Any:
            response = auth_provider.begin_login(
                BeginLoginRequest(
                    callback_url=request.callback_url,
                    host_state=request.host_state,
                    scopes=list(request.scopes),
                    options=dict(request.options),
                )
            )
            if response is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "auth provider returned nil response",
                )
            return _auth_pb2.BeginLoginResponse(
                authorization_url=response.authorization_url,
                plugin_state=response.provider_state,
            )

        @_grpc_handler("complete login")
        def CompleteLogin(self, request: Any, context: Any) -> Any:
            user = auth_provider.complete_login(
                CompleteLoginRequest(
                    query=dict(request.query),
                    provider_state=request.plugin_state,
                    callback_url=request.callback_url,
                )
            )
            if user is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "auth provider returned nil user",
                )
            return _authenticated_user_to_proto(user)

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
                    f"validate external token: {error}",
                )
            if user is None:
                return context.abort(
                    grpc.StatusCode.NOT_FOUND,
                    "token not recognized",
                )
            return _authenticated_user_to_proto(user)

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
            return _auth_pb2.AuthSessionSettings(session_ttl_seconds=seconds)

    return AuthServicer()


def _datastore_servicer(*, provider: RuntimeProvider) -> Any:
    datastore_provider = cast(DatastoreProvider, provider)

    class DatastoreServicer(_datastore_pb2_grpc.DatastorePluginServicer):
        @_grpc_handler("migrate")
        def Migrate(self, request: Any, context: Any) -> Any:
            datastore_provider.migrate()
            return _empty_pb2.Empty()

        @_grpc_handler("get user")
        def GetUser(self, request: Any, context: Any) -> Any:
            user = datastore_provider.get_user(request.id)
            if user is None:
                return context.abort(grpc.StatusCode.NOT_FOUND, "user not found")
            return _stored_user_to_proto(user)

        @_grpc_handler("find or create user")
        def FindOrCreateUser(self, request: Any, context: Any) -> Any:
            user = datastore_provider.find_or_create_user(request.email)
            if user is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "datastore plugin returned nil user",
                )
            return _stored_user_to_proto(user)

        @_grpc_handler("put integration token")
        def PutStoredIntegrationToken(self, request: Any, context: Any) -> Any:
            datastore_provider.put_integration_token(
                _stored_integration_token_from_proto(request)
            )
            return _empty_pb2.Empty()

        @_grpc_handler("get integration token")
        def GetStoredIntegrationToken(self, request: Any, context: Any) -> Any:
            token = datastore_provider.get_integration_token(
                request.user_id,
                request.integration,
                request.connection,
                request.instance,
            )
            if token is None:
                return context.abort(
                    grpc.StatusCode.NOT_FOUND,
                    "integration token not found",
                )
            return _stored_integration_token_to_proto(token)

        @_grpc_handler("list integration tokens")
        def ListStoredIntegrationTokens(self, request: Any, context: Any) -> Any:
            tokens = datastore_provider.list_integration_tokens(
                request.user_id,
                request.integration,
                request.connection,
            )
            return _datastore_pb2.ListStoredIntegrationTokensResponse(
                tokens=[
                    _stored_integration_token_to_proto(token)
                    for token in tokens
                ]
            )

        @_grpc_handler("delete integration token")
        def DeleteStoredIntegrationToken(self, request: Any, context: Any) -> Any:
            datastore_provider.delete_integration_token(request.id)
            return _empty_pb2.Empty()

        @_grpc_handler("put api token")
        def PutAPIToken(self, request: Any, context: Any) -> Any:
            datastore_provider.put_api_token(_stored_api_token_from_proto(request))
            return _empty_pb2.Empty()

        @_grpc_handler("get api token by hash")
        def GetAPITokenByHash(self, request: Any, context: Any) -> Any:
            token = datastore_provider.get_api_token_by_hash(request.hashed_token)
            if token is None:
                return context.abort(grpc.StatusCode.NOT_FOUND, "api token not found")
            return _stored_api_token_to_proto(token)

        @_grpc_handler("list api tokens")
        def ListAPITokens(self, request: Any, context: Any) -> Any:
            tokens = datastore_provider.list_api_tokens(request.user_id)
            return _datastore_pb2.ListAPITokensResponse(
                tokens=[_stored_api_token_to_proto(token) for token in tokens]
            )

        @_grpc_handler("revoke api token")
        def RevokeAPIToken(self, request: Any, context: Any) -> Any:
            datastore_provider.revoke_api_token(request.user_id, request.id)
            return _empty_pb2.Empty()

        @_grpc_handler("revoke all api tokens")
        def RevokeAllAPITokens(self, request: Any, context: Any) -> Any:
            revoked = datastore_provider.revoke_all_api_tokens(request.user_id)
            return _datastore_pb2.RevokeAllAPITokensResponse(revoked=revoked)

        @_grpc_handler("get oauth registration")
        def GetOAuthRegistration(self, request: Any, context: Any) -> Any:
            if not isinstance(datastore_provider, OAuthRegistrationStore):
                return context.abort(grpc.StatusCode.UNIMPLEMENTED, "datastore provider does not support oauth registrations")
            registration = datastore_provider.get_oauth_registration(request.auth_server_url, request.redirect_uri)
            if registration is None:
                return context.abort(grpc.StatusCode.NOT_FOUND, "oauth registration not found")
            return _oauth_registration_to_proto(registration)

        @_grpc_handler("put oauth registration")
        def PutOAuthRegistration(self, request: Any, context: Any) -> Any:
            if not isinstance(datastore_provider, OAuthRegistrationStore):
                return context.abort(grpc.StatusCode.UNIMPLEMENTED, "datastore provider does not support oauth registrations")
            datastore_provider.put_oauth_registration(_oauth_registration_from_proto(request))
            return _empty_pb2.Empty()

        @_grpc_handler("delete oauth registration")
        def DeleteOAuthRegistration(self, request: Any, context: Any) -> Any:
            if not isinstance(datastore_provider, OAuthRegistrationStore):
                return context.abort(grpc.StatusCode.UNIMPLEMENTED, "datastore provider does not support oauth registrations")
            datastore_provider.delete_oauth_registration(request.auth_server_url, request.redirect_uri)
            return _empty_pb2.Empty()

    return DatastoreServicer()


def _plugin_request(request: Any) -> Request:
    return Request(
        token=getattr(request, "token", ""),
        connection_params=dict(getattr(request, "connection_params", {})),
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


def _provider_metadata(*, provider: RuntimeProvider, kind: ProviderKind) -> ProviderMetadata:
    if isinstance(provider, MetadataProvider):
        metadata = provider.metadata()
        if isinstance(metadata, ProviderMetadata):
            return metadata
    return ProviderMetadata(kind=kind)


def _provider_warnings(provider: RuntimeProvider) -> list[str]:
    if isinstance(provider, WarningsProvider):
        return list(provider.warnings())
    return []


def _provider_kind_to_proto(kind: ProviderKind | str) -> Any:
    normalized = _normalized_runtime_kind(kind)
    return {
        ProviderKind.INTEGRATION: _runtime_pb2.PluginKind.PLUGIN_KIND_INTEGRATION,
        ProviderKind.AUTH: _runtime_pb2.PluginKind.PLUGIN_KIND_AUTH,
        ProviderKind.DATASTORE: _runtime_pb2.PluginKind.PLUGIN_KIND_DATASTORE,
        ProviderKind.SECRETS: _runtime_pb2.PluginKind.PLUGIN_KIND_SECRETS,
        ProviderKind.TELEMETRY: _runtime_pb2.PluginKind.PLUGIN_KIND_TELEMETRY,
    }.get(normalized, _runtime_pb2.PluginKind.PLUGIN_KIND_UNSPECIFIED)


def _normalized_runtime_kind(kind: ProviderKind | str | None) -> ProviderKind:
    if isinstance(kind, ProviderKind):
        return kind
    if isinstance(kind, str):
        try:
            return ProviderKind(kind.strip().lower())
        except ValueError:
            return ProviderKind.INTEGRATION
    return ProviderKind.INTEGRATION


def _authenticated_user_to_proto(user: AuthenticatedUser) -> Any:
    return _auth_pb2.AuthenticatedUser(
        subject=user.subject,
        email=user.email,
        email_verified=user.email_verified,
        display_name=user.display_name,
        avatar_url=user.avatar_url,
        claims=dict(user.claims),
    )


def _stored_user_to_proto(user: StoredUser) -> Any:
    message = _datastore_pb2.StoredUser(
        id=user.id,
        email=user.email,
        display_name=user.display_name,
    )
    message.created_at.CopyFrom(_datetime_to_proto(user.created_at))
    message.updated_at.CopyFrom(_datetime_to_proto(user.updated_at))
    return message


def _stored_integration_token_to_proto(token: StoredIntegrationToken) -> Any:
    message = _datastore_pb2.StoredIntegrationToken(
        id=token.id,
        user_id=token.user_id,
        integration=token.integration,
        connection=token.connection,
        instance=token.instance,
        access_token_sealed=token.access_token_sealed,
        refresh_token_sealed=token.refresh_token_sealed,
        scopes=token.scopes,
        refresh_error_count=token.refresh_error_count,
        connection_params=dict(token.connection_params),
    )
    _copy_optional_timestamp(message, "expires_at", _datetime_to_proto(token.expires_at))
    _copy_optional_timestamp(message, "last_refreshed_at", _datetime_to_proto(token.last_refreshed_at))
    message.created_at.CopyFrom(_datetime_to_proto(token.created_at))
    message.updated_at.CopyFrom(_datetime_to_proto(token.updated_at))
    return message


def _stored_integration_token_from_proto(token: Any) -> StoredIntegrationToken:
    return StoredIntegrationToken(
        id=token.id,
        user_id=token.user_id,
        integration=token.integration,
        connection=token.connection,
        instance=token.instance,
        access_token_sealed=bytes(token.access_token_sealed),
        refresh_token_sealed=bytes(token.refresh_token_sealed),
        scopes=token.scopes,
        expires_at=_proto_to_datetime(token.expires_at),
        last_refreshed_at=_proto_to_datetime(token.last_refreshed_at),
        refresh_error_count=token.refresh_error_count,
        connection_params=dict(token.connection_params),
        created_at=_proto_to_datetime(token.created_at) or UNIX_EPOCH,
        updated_at=_proto_to_datetime(token.updated_at) or UNIX_EPOCH,
    )


def _stored_api_token_to_proto(token: StoredAPIToken) -> Any:
    message = _datastore_pb2.StoredAPIToken(
        id=token.id,
        user_id=token.user_id,
        name=token.name,
        hashed_token=token.hashed_token,
        scopes=token.scopes,
    )
    _copy_optional_timestamp(message, "expires_at", _datetime_to_proto(token.expires_at))
    message.created_at.CopyFrom(_datetime_to_proto(token.created_at))
    message.updated_at.CopyFrom(_datetime_to_proto(token.updated_at))
    return message


def _stored_api_token_from_proto(token: Any) -> StoredAPIToken:
    return StoredAPIToken(
        id=token.id,
        user_id=token.user_id,
        name=token.name,
        hashed_token=token.hashed_token,
        scopes=token.scopes,
        expires_at=_proto_to_datetime(token.expires_at),
        created_at=_proto_to_datetime(token.created_at) or UNIX_EPOCH,
        updated_at=_proto_to_datetime(token.updated_at) or UNIX_EPOCH,
    )


def _oauth_registration_to_proto(registration: OAuthRegistration) -> Any:
    message = _datastore_pb2.OAuthRegistration(
        auth_server_url=registration.auth_server_url,
        redirect_uri=registration.redirect_uri,
        client_id=registration.client_id,
        client_secret_sealed=registration.client_secret_sealed,
        authorization_endpoint=registration.authorization_endpoint,
        token_endpoint=registration.token_endpoint,
        scopes_supported=registration.scopes_supported,
    )
    _copy_optional_timestamp(message, "expires_at", _datetime_to_proto(registration.expires_at))
    message.discovered_at.CopyFrom(_datetime_to_proto(registration.discovered_at))
    return message


def _oauth_registration_from_proto(registration: Any) -> OAuthRegistration:
    return OAuthRegistration(
        auth_server_url=registration.auth_server_url,
        redirect_uri=registration.redirect_uri,
        client_id=registration.client_id,
        client_secret_sealed=bytes(registration.client_secret_sealed),
        expires_at=_proto_to_datetime(registration.expires_at),
        authorization_endpoint=registration.authorization_endpoint,
        token_endpoint=registration.token_endpoint,
        scopes_supported=registration.scopes_supported,
        discovered_at=_proto_to_datetime(registration.discovered_at) or UNIX_EPOCH,
    )


def _datetime_to_proto(value: Any) -> Any:
    if value is None:
        return None
    if value.tzinfo is None:
        value = value.replace(tzinfo=UTC)
    timestamp = _timestamp_pb2.Timestamp()
    timestamp.FromDatetime(value.astimezone(UTC))
    return timestamp


def _proto_to_datetime(value: Any) -> Any:
    if value is None:
        return None
    if hasattr(value, "seconds") and hasattr(value, "nanos") and value.seconds == 0 and value.nanos == 0:
        return None
    return value.ToDatetime(tzinfo=UTC)


def _copy_optional_timestamp(message: Any, field_name: str, value: Any) -> None:
    if value is not None:
        getattr(message, field_name).CopyFrom(value)


if __name__ == "__main__":
    raise SystemExit(main())
