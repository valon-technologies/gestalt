from __future__ import annotations

import datetime as dt
import functools
import importlib
import inspect
import os
import pathlib
import signal
import sys
import traceback
import urllib.parse as _urlparse
from concurrent import futures
from dataclasses import dataclass
from http import HTTPStatus
from typing import Any, Final, cast

from . import _agent as _agent_native
from . import _runtime_provider as _runtime_provider_native
from . import _telemetry
from . import _workflow as _workflow_native
from ._api import Access, Credential, Error, Host, Request, Subject, SubjectPermission
from ._app import App, _module_app
from ._bootstrap import parse_plugin_target, read_bundled_plugin_config
from ._catalog import catalog_to_proto
from ._codec import identity as _identity_codec
from ._codec import authorization as _authorization_codec
from ._codec import s3 as _s3_codec
from ._grpc_transport import INTERNAL_GRPC_MESSAGE_OPTIONS
from ._http_subject import HTTPSubjectRequest, HTTPSubjectResolutionError
from ._operations import INTERNAL_ERROR_MESSAGE, JSON_CONTENT_TYPE
from ._protocol import string_lists_from_proto_map
from ._providers import (
    CALLER_BEARER_TOKEN_METADATA_KEY,
    AgentProvider,
    AppProvider,
    AppProviderAdapter,
    IdentityCallContext,
    IdentityProvider,
    AuthorizationProvider,
    CacheProvider,
    Closer,
    HealthChecker,
    MetadataProvider,
    ProviderKind,
    ProviderMetadata,
    RuntimeProvider,
    S3InvalidRangeError,
    S3NotFoundError,
    S3PreconditionFailedError,
    S3Provider,
    SecretsProvider,
    Starter,
    WarningsProvider,
    WorkflowProvider,
)
from ._serialization import json_body
from .s3 import ReadObjectChunk, ReadObjectChunkData, ReadObjectChunkMeta

json_format: Any = cast(Any, None)

grpc: Any = cast(Any, None)
empty_pb2: Any = cast(Any, None)
duration_pb2: Any = cast(Any, None)
app_pb2: Any = cast(Any, None)
app_pb2_grpc: Any = cast(Any, None)
runtime_provider_pb2: Any = cast(Any, None)
runtime_provider_pb2_grpc: Any = cast(Any, None)
runtime_pb2: Any = cast(Any, None)
runtime_pb2_grpc: Any = cast(Any, None)
authentication_pb2: Any = cast(Any, None)
authentication_pb2_grpc: Any = cast(Any, None)
authorization_pb2_grpc: Any = cast(Any, None)
cache_pb2: Any = cast(Any, None)
cache_pb2_grpc: Any = cast(Any, None)
s3_pb2_grpc: Any = cast(Any, None)
secrets_pb2: Any = cast(Any, None)
secrets_pb2_grpc: Any = cast(Any, None)
agent_pb2_grpc: Any = cast(Any, None)
workflow_pb2: Any = cast(Any, None)
workflow_pb2_grpc: Any = cast(Any, None)

ENV_PROVIDER_SOCKET: Final[str] = "GESTALT_PROVIDER_SOCKET"
ENV_PROVIDER_NAME: Final[str] = "GESTALT_APP_NAME"
ENV_WRITE_CATALOG: Final[str] = "GESTALT_APP_WRITE_CATALOG"
CURRENT_PROTOCOL_VERSION: Final[int] = 4
GRPC_SERVER_MAX_WORKERS: Final[int] = 4
GRPC_SHUTDOWN_GRACE_SECONDS: Final[int] = 2
USAGE: Final[str] = (
    "usage: python -m gestalt._runtime ROOT MODULE[:ATTRIBUTE] [RUNTIME_KIND]"
)


def _ensure_grpc_runtime() -> None:
    global json_format
    global authentication_pb2
    global authentication_pb2_grpc
    global authorization_pb2_grpc
    global cache_pb2
    global cache_pb2_grpc
    global duration_pb2
    global empty_pb2
    global grpc
    global app_pb2
    global app_pb2_grpc
    global runtime_provider_pb2
    global runtime_provider_pb2_grpc
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

    from ._gen.v1 import agent_pb2_grpc as _agent_pb2_grpc
    from ._gen.v1 import app_pb2 as _app_pb2
    from ._gen.v1 import app_pb2_grpc as _app_pb2_grpc
    from ._gen.v1 import authentication_pb2 as _authentication_pb2
    from ._gen.v1 import authentication_pb2_grpc as _authentication_pb2_grpc
    from ._gen.v1 import authorization_pb2_grpc as _authorization_pb2_grpc
    from ._gen.v1 import cache_pb2 as _cache_pb2
    from ._gen.v1 import cache_pb2_grpc as _cache_pb2_grpc
    from ._gen.v1 import runtime_pb2 as _runtime_pb2
    from ._gen.v1 import runtime_pb2_grpc as _runtime_pb2_grpc
    from ._gen.v1 import runtime_provider_pb2 as _runtime_provider_pb2
    from ._gen.v1 import runtime_provider_pb2_grpc as _runtime_provider_pb2_grpc
    from ._gen.v1 import s3_pb2_grpc as _s3_pb2_grpc
    from ._gen.v1 import secrets_pb2 as _secrets_pb2
    from ._gen.v1 import secrets_pb2_grpc as _secrets_pb2_grpc
    from ._gen.v1 import workflow_pb2 as _workflow_pb2
    from ._gen.v1 import workflow_pb2_grpc as _workflow_pb2_grpc

    grpc = _grpc
    json_format = _json_format
    duration_pb2 = _duration_pb2
    empty_pb2 = _empty_pb2
    app_pb2 = _app_pb2
    app_pb2_grpc = _app_pb2_grpc
    runtime_provider_pb2 = _runtime_provider_pb2
    runtime_provider_pb2_grpc = _runtime_provider_pb2_grpc
    runtime_pb2 = _runtime_pb2
    runtime_pb2_grpc = _runtime_pb2_grpc
    authentication_pb2 = _authentication_pb2
    authentication_pb2_grpc = _authentication_pb2_grpc
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
    app_name: str | None = None
    runtime_kind: str | None = None


def _grpc_handler(label: str):
    def decorator(fn):
        @functools.wraps(fn)
        def wrapper(self, request, context):
            _ensure_grpc_runtime()
            try:
                return fn(self, request, context)
            except Error as error:
                context.abort(
                    _grpc_status_from_http_status(error.status), error.message
                )
            except Exception as error:
                if context.code() is not None:
                    raise
                traceback.print_exception(error)
                context.abort(grpc.StatusCode.UNKNOWN, f"{label}: {error}")

        return wrapper

    return decorator


def _grpc_status_from_http_status(status: int) -> Any:
    if status == HTTPStatus.BAD_REQUEST:
        return grpc.StatusCode.INVALID_ARGUMENT
    if status == HTTPStatus.UNAUTHORIZED:
        return grpc.StatusCode.UNAUTHENTICATED
    if status == HTTPStatus.FORBIDDEN:
        return grpc.StatusCode.PERMISSION_DENIED
    if status == HTTPStatus.NOT_FOUND:
        return grpc.StatusCode.NOT_FOUND
    if status == HTTPStatus.CONFLICT:
        return grpc.StatusCode.ALREADY_EXISTS
    if status == HTTPStatus.PRECONDITION_FAILED:
        return grpc.StatusCode.FAILED_PRECONDITION
    if status == HTTPStatus.NOT_IMPLEMENTED:
        return grpc.StatusCode.UNIMPLEMENTED
    if status == HTTPStatus.SERVICE_UNAVAILABLE:
        return grpc.StatusCode.UNAVAILABLE
    if status == HTTPStatus.INTERNAL_SERVER_ERROR:
        return grpc.StatusCode.INTERNAL
    return grpc.StatusCode.UNKNOWN


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
    target: App | AppProviderAdapter | AppProvider,
    *,
    runtime_kind: ProviderKind | str | None = None,
) -> None:
    _ensure_grpc_runtime()
    _telemetry.configure_from_environment(service_name=_provider_service_name())
    scheme, address = _provider_socket_target_from_env()
    bind_target = address
    if scheme == "unix":
        _remove_stale_socket(pathlib.Path(address))
        bind_target = f"unix:{address}"

    server = grpc.server(
        futures.ThreadPoolExecutor(max_workers=GRPC_SERVER_MAX_WORKERS),
        options=INTERNAL_GRPC_MESSAGE_OPTIONS,
    )
    servable = _servable_target(target, runtime_kind=runtime_kind)
    _register_services(server=server, servable=servable)
    server.add_insecure_port(bind_target)
    close_provider = _close_once_callable(servable)
    server.start()
    _register_shutdown_handlers(server, close_provider)
    try:
        server.wait_for_termination()
    finally:
        close_provider()
        _telemetry.shutdown()


def _provider_service_name() -> str:
    provider_name = os.environ.get(ENV_PROVIDER_NAME, "").strip()
    if provider_name:
        return f"gestalt-provider-{provider_name}"
    return "gestalt-provider"


def main(argv: list[str] | None = None) -> int:
    runtime_args = _parse_runtime_args(sys.argv[1:] if argv is None else argv)
    if runtime_args is None:
        print(USAGE, file=sys.stderr)
        return 2

    target = _load_target(runtime_args)
    if runtime_args.app_name and isinstance(target, App):
        target.name = runtime_args.app_name

    catalog_path = os.environ.get(ENV_WRITE_CATALOG)
    if catalog_path:
        if not isinstance(target, App):
            raise RuntimeError("catalog export is only supported for integration apps")
        target.write_catalog(catalog_path)
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
        app_name=bundled_config.app_name,
        runtime_kind=_normalized_runtime_kind(
            bundled_config.runtime_kind or ProviderKind.INTEGRATION.value
        ).value,
    )


def _load_target(args: RuntimeArgs) -> App | AppProviderAdapter | AppProvider:
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

    if isinstance(target, (App, AppProviderAdapter)):
        return target

    if resolved_kind == ProviderKind.AUTHENTICATION and isinstance(
        target, IdentityProvider
    ):
        return _authentication_runtime_plugin(target)
    if resolved_kind == ProviderKind.AUTHORIZATION and isinstance(
        target, AuthorizationProvider
    ):
        return _authorization_runtime_plugin(target)
    if resolved_kind == ProviderKind.CACHE and isinstance(target, CacheProvider):
        return _cache_runtime_plugin(target)
    if resolved_kind == ProviderKind.S3 and isinstance(target, S3Provider):
        return _s3_runtime_plugin(target)
    if resolved_kind == ProviderKind.AGENT and isinstance(target, AgentProvider):
        return _agent_runtime_app(target)
    if resolved_kind == ProviderKind.RUNTIME and isinstance(target, RuntimeProvider):
        return _runtime_provider_runtime_app(target)
    if resolved_kind == ProviderKind.WORKFLOW and isinstance(target, WorkflowProvider):
        return _workflow_runtime_adapter(target)
    if resolved_kind == ProviderKind.SECRETS and isinstance(target, SecretsProvider):
        return _secrets_runtime_plugin(target)
    if isinstance(target, AppProvider):
        raise RuntimeError(
            "providers must be wrapped in gestalt.AppProviderAdapter unless runtime_kind is authorization, authentication, cache, s3, agent, runtime, workflow, or secrets"
        )
    raise RuntimeError(f"{args.target} did not resolve to a supported gestalt target")


def _module_target(
    module: Any,
    runtime_kind: ProviderKind,
) -> App | AppProviderAdapter | AppProvider | Any:
    if runtime_kind == ProviderKind.INTEGRATION:
        return _module_app(module)

    for attribute_name in (runtime_kind.value, "provider", "app"):
        value = getattr(module, attribute_name, None)
        if value is not None:
            return value
    return None


def _provider_socket_target_from_env() -> tuple[str, str]:
    raw_target = os.environ.get(ENV_PROVIDER_SOCKET)
    if not raw_target:
        raise RuntimeError(f"{ENV_PROVIDER_SOCKET} is required")
    return _parse_provider_socket_target(raw_target)


def _parse_provider_socket_target(raw: str) -> tuple[str, str]:
    target = raw.strip()
    if not target:
        raise RuntimeError("provider socket target is required")
    if target.startswith("tcp://"):
        address = target.removeprefix("tcp://").strip()
        if not address:
            raise RuntimeError(f"provider tcp target {raw!r} is missing host:port")
        return "tcp", address
    if target.startswith("unix://"):
        address = target.removeprefix("unix://").strip()
        if not address:
            raise RuntimeError(f"provider unix target {raw!r} is missing a socket path")
        return "unix", address
    if "://" in target:
        parsed = _urlparse.urlparse(target)
        raise RuntimeError(
            f"unsupported provider socket target scheme {parsed.scheme!r}"
        )
    return "unix", target


def _remove_stale_socket(socket_path: pathlib.Path) -> None:
    if socket_path.exists():
        socket_path.unlink()


def _register_shutdown_handlers(server: Any, close_provider: Any) -> None:
    def _shutdown(_signum: int, _frame: Any) -> None:
        server.stop(grace=GRPC_SHUTDOWN_GRACE_SECONDS)
        close_provider()

    signal.signal(signal.SIGTERM, _shutdown)
    signal.signal(signal.SIGINT, _shutdown)


def _register_services(*, server: Any, servable: App | AppProviderAdapter) -> None:
    _ensure_grpc_runtime()
    if isinstance(servable, App):
        app_pb2_grpc.add_AppProviderServicer_to_server(
            _provider_servicer(app=servable),
            server,
        )
        return

    servable.register_services(server, servable.provider)


def _close_once_callable(target: App | AppProviderAdapter) -> Any:
    provider = target.provider if isinstance(target, AppProviderAdapter) else target
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
    target: App | AppProviderAdapter | AppProvider,
    *,
    runtime_kind: ProviderKind | str | None,
) -> App | AppProviderAdapter:
    if isinstance(target, (App, AppProviderAdapter)):
        return target

    kind = _normalized_runtime_kind(runtime_kind)
    if kind == ProviderKind.AUTHENTICATION and isinstance(
        target, IdentityProvider
    ):
        return _authentication_runtime_plugin(target)
    if kind == ProviderKind.AUTHORIZATION and isinstance(target, AuthorizationProvider):
        return _authorization_runtime_plugin(target)
    if kind == ProviderKind.CACHE and isinstance(target, CacheProvider):
        return _cache_runtime_plugin(target)
    if kind == ProviderKind.S3 and isinstance(target, S3Provider):
        return _s3_runtime_plugin(target)
    if kind == ProviderKind.AGENT and isinstance(target, AgentProvider):
        return _agent_runtime_app(target)
    if kind == ProviderKind.RUNTIME and isinstance(target, RuntimeProvider):
        return _runtime_provider_runtime_app(target)
    if kind == ProviderKind.WORKFLOW and isinstance(target, WorkflowProvider):
        return _workflow_runtime_adapter(target)
    if kind == ProviderKind.SECRETS and isinstance(target, SecretsProvider):
        return _secrets_runtime_plugin(target)
    raise RuntimeError("unsupported runtime target")


def _authentication_runtime_plugin(
    provider: IdentityProvider,
) -> AppProviderAdapter:
    return AppProviderAdapter(
        kind=ProviderKind.AUTHENTICATION,
        provider=provider,
        register_services=_register_authentication_services,
    )


def _register_authentication_services(server: Any, provider: AppProvider) -> None:
    _ensure_grpc_runtime()
    runtime_pb2_grpc.add_ProviderLifecycleServicer_to_server(
        _runtime_servicer(provider=provider, kind=ProviderKind.AUTHENTICATION),
        server,
    )
    authentication_pb2_grpc.add_AuthenticationServicer_to_server(
        _authentication_servicer(provider=provider),
        server,
    )


def _authorization_runtime_plugin(
    provider: AuthorizationProvider,
) -> AppProviderAdapter:
    return AppProviderAdapter(
        kind=ProviderKind.AUTHORIZATION,
        provider=provider,
        register_services=_register_authorization_services,
    )


def _register_authorization_services(server: Any, provider: AppProvider) -> None:
    _ensure_grpc_runtime()
    runtime_pb2_grpc.add_ProviderLifecycleServicer_to_server(
        _runtime_servicer(provider=provider, kind=ProviderKind.AUTHORIZATION),
        server,
    )
    authorization_pb2_grpc.add_AuthorizationServicer_to_server(
        _authorization_servicer(provider=provider),
        server,
    )


def _s3_runtime_plugin(provider: S3Provider) -> AppProviderAdapter:
    return AppProviderAdapter(
        kind=ProviderKind.S3,
        provider=provider,
        register_services=_register_s3_services,
    )


def _register_s3_services(server: Any, provider: AppProvider) -> None:
    _ensure_grpc_runtime()
    runtime_pb2_grpc.add_ProviderLifecycleServicer_to_server(
        _runtime_servicer(provider=provider, kind=ProviderKind.S3),
        server,
    )
    s3_pb2_grpc.add_S3Servicer_to_server(
        _s3_servicer(provider=provider),
        server,
    )


def _s3_servicer(*, provider: AppProvider) -> Any:
    _ensure_grpc_runtime()
    s3_provider = cast(S3Provider, provider)

    class S3Servicer(s3_pb2_grpc.S3Servicer):
        def __init__(self) -> None:
            self._provider = s3_provider

        @_s3_grpc_handler("s3 head object")
        def HeadObject(self, request: Any, context: Any) -> Any:
            result = s3_provider.head_object(
                _s3_codec.from_wire_head_object_request(request)
            )
            return _s3_codec.to_wire_head_object_response(result)

        def ReadObject(self, request: Any, context: Any) -> Any:
            body: Any = None
            try:
                result = s3_provider.read_object(
                    _s3_codec.from_wire_read_object_request(request)
                )
                body = result.body
                yield _s3_codec.to_wire_read_object_chunk(
                    ReadObjectChunk(result=ReadObjectChunkMeta(value=result.meta))
                )
                for chunk in _s3_body_chunks(body):
                    if chunk:
                        yield _s3_codec.to_wire_read_object_chunk(
                            ReadObjectChunk(result=ReadObjectChunkData(value=chunk))
                        )
            except Exception as error:
                _abort_s3_error(context, "s3 read object", error)
            finally:
                _close_s3_body(body)

        @_s3_grpc_handler("s3 write object")
        def WriteObject(self, request_iterator: Any, context: Any) -> Any:
            try:
                first = next(request_iterator)
            except StopIteration:
                return context.abort(
                    grpc.StatusCode.INVALID_ARGUMENT,
                    "first message must be WriteObjectOpen",
                )

            if first.WhichOneof("msg") != "open":
                return context.abort(
                    grpc.StatusCode.INVALID_ARGUMENT,
                    "first message must be WriteObjectOpen",
                )

            open_request = _s3_codec.from_wire_write_object_open(first.open)
            body = _S3WriteBodyChunks(request_iterator)
            result = s3_provider.write_object(open_request, body)
            body.drain()
            return _s3_codec.to_wire_write_object_response(result)

        @_s3_grpc_handler("s3 delete object")
        def DeleteObject(self, request: Any, context: Any) -> Any:
            s3_provider.delete_object(
                _s3_codec.from_wire_delete_object_request(request)
            )
            return empty_pb2.Empty()

        @_s3_grpc_handler("s3 list objects")
        def ListObjects(self, request: Any, context: Any) -> Any:
            page = s3_provider.list_objects(
                _s3_codec.from_wire_list_objects_request(request)
            )
            return _s3_codec.to_wire_list_objects_response(page)

        @_s3_grpc_handler("s3 copy object")
        def CopyObject(self, request: Any, context: Any) -> Any:
            result = s3_provider.copy_object(
                _s3_codec.from_wire_copy_object_request(request)
            )
            return _s3_codec.to_wire_copy_object_response(result)

        @_s3_grpc_handler("s3 presign object")
        def PresignObject(self, request: Any, context: Any) -> Any:
            result = s3_provider.presign_object(
                _s3_codec.from_wire_presign_object_request(request)
            )
            return _s3_codec.to_wire_presign_object_response(result)

    return S3Servicer()


def _s3_grpc_handler(label: str):
    def decorator(fn):
        @functools.wraps(fn)
        def wrapper(self, request, context):
            try:
                return fn(self, request, context)
            except Exception as error:
                _abort_s3_error(context, label, error)

        return wrapper

    return decorator


def _abort_s3_error(context: Any, label: str, error: Exception) -> None:
    if context.code() is not None:
        raise error
    if isinstance(error, Error):
        return context.abort(_grpc_status_from_http_status(error.status), error.message)
    if isinstance(error, S3NotFoundError):
        return context.abort(grpc.StatusCode.NOT_FOUND, str(error) or "s3: not found")
    if isinstance(error, S3PreconditionFailedError):
        return context.abort(
            grpc.StatusCode.FAILED_PRECONDITION,
            str(error) or "s3: precondition failed",
        )
    if isinstance(error, S3InvalidRangeError):
        return context.abort(
            grpc.StatusCode.OUT_OF_RANGE,
            str(error) or "s3: invalid range",
        )
    traceback.print_exception(error)
    return context.abort(grpc.StatusCode.UNKNOWN, f"{label}: {error}")


class _S3WriteBodyChunks:
    def __init__(self, request_iterator: Any) -> None:
        self._request_iterator = iter(request_iterator)

    def __iter__(self) -> "_S3WriteBodyChunks":
        return self

    def __next__(self) -> bytes:
        while True:
            message = next(self._request_iterator)
            if message.WhichOneof("msg") != "data":
                raise Error(
                    HTTPStatus.BAD_REQUEST,
                    "write object stream frames after open must be data",
                )
            data = bytes(message.data)
            if data:
                return data

    def drain(self) -> None:
        for _chunk in self:
            pass


def _close_s3_body(body: Any) -> None:
    close = getattr(body, "close", None)
    if callable(close):
        try:
            close()
        except Exception:
            pass


_S3_READ_CHUNK_SIZE = 64 * 1024


def _s3_body_chunks(body: Any) -> Any:
    if body is None:
        return
    if isinstance(body, (bytes, bytearray, memoryview)):
        data = bytes(body)
        for start in range(0, len(data), _S3_READ_CHUNK_SIZE):
            yield data[start : start + _S3_READ_CHUNK_SIZE]
        return
    reader = getattr(body, "read", None)
    if callable(reader):
        while True:
            chunk = reader(_S3_READ_CHUNK_SIZE)
            if chunk in (b"", None):
                return
            yield _s3_ensure_bytes(chunk)
    for chunk in body:
        piece = _s3_ensure_bytes(chunk)
        if piece:
            yield piece


def _s3_ensure_bytes(value: Any) -> bytes:
    if isinstance(value, bytes):
        return value
    if isinstance(value, bytearray):
        return bytes(value)
    if isinstance(value, memoryview):
        return value.tobytes()
    raise TypeError("s3: body chunks must be bytes")


def _agent_runtime_app(provider: AgentProvider) -> AppProviderAdapter:
    return AppProviderAdapter(
        kind=ProviderKind.AGENT,
        provider=provider,
        register_services=_register_agent_services,
    )


def _register_agent_services(server: Any, provider: AppProvider) -> None:
    _ensure_grpc_runtime()
    runtime_pb2_grpc.add_ProviderLifecycleServicer_to_server(
        _runtime_servicer(provider=provider, kind=ProviderKind.AGENT),
        server,
    )
    agent_pb2_grpc.add_AgentServicer_to_server(
        _agent_provider_servicer(provider),
        server,
    )


def _runtime_provider_runtime_app(
    provider: RuntimeProvider,
) -> AppProviderAdapter:
    return AppProviderAdapter(
        kind=ProviderKind.RUNTIME,
        provider=provider,
        register_services=_register_runtime_provider_services,
    )


def _register_runtime_provider_services(server: Any, provider: AppProvider) -> None:
    _ensure_grpc_runtime()
    runtime_pb2_grpc.add_ProviderLifecycleServicer_to_server(
        _runtime_servicer(provider=provider, kind=ProviderKind.RUNTIME),
        server,
    )
    runtime_provider_pb2_grpc.add_RuntimeServicer_to_server(
        _runtime_provider_servicer(provider),
        server,
    )


def _workflow_runtime_adapter(provider: WorkflowProvider) -> AppProviderAdapter:
    return AppProviderAdapter(
        kind=ProviderKind.WORKFLOW,
        provider=provider,
        register_services=_register_workflow_services,
    )


def _register_workflow_services(server: Any, provider: AppProvider) -> None:
    _ensure_grpc_runtime()
    runtime_pb2_grpc.add_ProviderLifecycleServicer_to_server(
        _runtime_servicer(provider=provider, kind=ProviderKind.WORKFLOW),
        server,
    )
    workflow_pb2_grpc.add_WorkflowServicer_to_server(
        _workflow_provider_servicer(provider),
        server,
    )


def _agent_provider_servicer(provider: AppProvider) -> Any:
    _ensure_grpc_runtime()

    class AgentServicer(agent_pb2_grpc.AgentServicer):  # type: ignore[misc]
        def __init__(self, inner: AppProvider) -> None:
            self._provider = inner

        @_grpc_handler("CreateSession")
        def CreateSession(self, request: Any, context: Any) -> Any:
            result = self._provider.create_session(
                _agent_native.create_agent_provider_session_request_from_proto(request)
            )
            return _agent_native.agent_session_to_proto(result)

        @_grpc_handler("GetSession")
        def GetSession(self, request: Any, context: Any) -> Any:
            result = self._provider.get_session(
                _agent_native.get_agent_provider_session_request_from_proto(request)
            )
            return _agent_native.agent_session_to_proto(result)

        @_grpc_handler("ListSessions")
        def ListSessions(self, request: Any, context: Any) -> Any:
            result = self._provider.list_sessions(
                _agent_native.list_agent_provider_sessions_request_from_proto(request)
            )
            return _agent_native.list_agent_provider_sessions_response_to_proto(result)

        @_grpc_handler("UpdateSession")
        def UpdateSession(self, request: Any, context: Any) -> Any:
            result = self._provider.update_session(
                _agent_native.update_agent_provider_session_request_from_proto(request)
            )
            return _agent_native.agent_session_to_proto(result)

        @_grpc_handler("CreateTurn")
        def CreateTurn(self, request: Any, context: Any) -> Any:
            result = self._provider.create_turn(
                _agent_native.create_agent_provider_turn_request_from_proto(request)
            )
            return _agent_native.agent_turn_to_proto(result)

        @_grpc_handler("GetTurn")
        def GetTurn(self, request: Any, context: Any) -> Any:
            result = self._provider.get_turn(
                _agent_native.get_agent_provider_turn_request_from_proto(request)
            )
            return _agent_native.agent_turn_to_proto(result)

        @_grpc_handler("ListTurns")
        def ListTurns(self, request: Any, context: Any) -> Any:
            result = self._provider.list_turns(
                _agent_native.list_agent_provider_turns_request_from_proto(request)
            )
            return _agent_native.list_agent_provider_turns_response_to_proto(result)

        @_grpc_handler("CancelTurn")
        def CancelTurn(self, request: Any, context: Any) -> Any:
            result = self._provider.cancel_turn(
                _agent_native.cancel_agent_provider_turn_request_from_proto(request)
            )
            return _agent_native.agent_turn_to_proto(result)

        @_grpc_handler("ListTurnEvents")
        def ListTurnEvents(self, request: Any, context: Any) -> Any:
            result = self._provider.list_turn_events(
                _agent_native.list_agent_provider_turn_events_request_from_proto(
                    request
                )
            )
            return _agent_native.list_agent_provider_turn_events_response_to_proto(
                result
            )

        @_grpc_handler("GetInteraction")
        def GetInteraction(self, request: Any, context: Any) -> Any:
            result = self._provider.get_interaction(
                _agent_native.get_agent_provider_interaction_request_from_proto(request)
            )
            return _agent_native.agent_interaction_to_proto(result)

        @_grpc_handler("ListInteractions")
        def ListInteractions(self, request: Any, context: Any) -> Any:
            result = self._provider.list_interactions(
                _agent_native.list_agent_provider_interactions_request_from_proto(
                    request
                )
            )
            return _agent_native.list_agent_provider_interactions_response_to_proto(
                result
            )

        @_grpc_handler("ResolveInteraction")
        def ResolveInteraction(self, request: Any, context: Any) -> Any:
            result = self._provider.resolve_interaction(
                _agent_native.resolve_agent_provider_interaction_request_from_proto(
                    request
                )
            )
            return _agent_native.agent_interaction_to_proto(result)

        @_grpc_handler("GetCapabilities")
        def GetCapabilities(self, request: Any, context: Any) -> Any:
            result = self._provider.get_capabilities(
                _agent_native.GetAgentProviderCapabilitiesRequest()
            )
            return _agent_native.agent_provider_capabilities_to_proto(result)

    return AgentServicer(provider)


def _runtime_provider_servicer(provider: AppProvider) -> Any:
    _ensure_grpc_runtime()

    class RuntimeServicer(runtime_provider_pb2_grpc.RuntimeServicer):
        def __init__(self, inner: AppProvider) -> None:
            self._provider = inner

        @_grpc_handler("runtime get support")
        def GetSupport(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                RuntimeProvider,
                "get_support",
                "GetSupport",
                _runtime_provider_native.get_runtime_provider_support_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _runtime_provider_native.runtime_provider_support_to_proto(result)

        @_grpc_handler("runtime start session")
        def StartSession(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                RuntimeProvider,
                "start_session",
                "StartSession",
                _runtime_provider_native.start_runtime_provider_session_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _runtime_provider_native.runtime_provider_session_to_proto(result)

        @_grpc_handler("runtime get session")
        def GetSession(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                RuntimeProvider,
                "get_session",
                "GetSession",
                _runtime_provider_native.get_runtime_provider_session_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _runtime_provider_native.runtime_provider_session_to_proto(result)

        @_grpc_handler("runtime list sessions")
        def ListSessions(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                RuntimeProvider,
                "list_sessions",
                "ListSessions",
                _runtime_provider_native.list_runtime_provider_sessions_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _runtime_provider_native.list_runtime_provider_sessions_response_to_proto(
                result
            )

        @_grpc_handler("runtime stop session")
        def StopSession(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                RuntimeProvider,
                "stop_session",
                "StopSession",
                _runtime_provider_native.stop_runtime_provider_session_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _empty_response(result)

        @_grpc_handler("runtime prepare workspace")
        def PrepareWorkspace(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                RuntimeProvider,
                "prepare_workspace",
                "PrepareWorkspace",
                _runtime_provider_native.prepare_runtime_provider_workspace_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _runtime_provider_native.prepare_runtime_provider_workspace_response_to_proto(
                result
            )

        @_grpc_handler("runtime remove workspace")
        def RemoveWorkspace(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                RuntimeProvider,
                "remove_workspace",
                "RemoveWorkspace",
                _runtime_provider_native.remove_runtime_provider_workspace_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _empty_response(result)

        @_grpc_handler("runtime start app")
        def StartApp(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                RuntimeProvider,
                "start_app",
                "StartApp",
                _runtime_provider_native.start_hosted_app_request_from_proto(request),
                request,
                context,
            )
            return _runtime_provider_native.hosted_app_to_proto(result)

    return RuntimeServicer(provider)


def _workflow_provider_servicer(provider: AppProvider) -> Any:
    _ensure_grpc_runtime()

    class WorkflowServicer(workflow_pb2_grpc.WorkflowServicer):
        def __init__(self, inner: AppProvider) -> None:
            self._provider = inner

        @_grpc_handler("workflow apply definition")
        def ApplyDefinition(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                WorkflowProvider,
                "apply_definition",
                "ApplyDefinition",
                _workflow_native.apply_workflow_provider_definition_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _workflow_native.workflow_definition(result)

        @_grpc_handler("workflow get definition")
        def GetDefinition(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                WorkflowProvider,
                "get_definition",
                "GetDefinition",
                _workflow_native.get_workflow_provider_definition_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _workflow_native.workflow_definition(result)

        @_grpc_handler("workflow list definitions")
        def ListDefinitions(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                WorkflowProvider,
                "list_definitions",
                "ListDefinitions",
                _workflow_native.list_workflow_provider_definitions_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return (
                _workflow_native.list_workflow_provider_definitions_response_to_proto(
                    result
                )
            )

        @_grpc_handler("workflow set definition paused")
        def SetDefinitionPaused(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                WorkflowProvider,
                "set_definition_paused",
                "SetDefinitionPaused",
                _workflow_native.set_workflow_provider_definition_paused_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _workflow_native.workflow_definition(result)

        @_grpc_handler("workflow set activation paused")
        def SetActivationPaused(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                WorkflowProvider,
                "set_activation_paused",
                "SetActivationPaused",
                _workflow_native.set_workflow_provider_activation_paused_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _workflow_native.workflow_definition(result)

        @_grpc_handler("workflow delete definition")
        def DeleteDefinition(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                WorkflowProvider,
                "delete_definition",
                "DeleteDefinition",
                _workflow_native.delete_workflow_provider_definition_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _empty_response(result)

        @_grpc_handler("workflow start run")
        def StartRun(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                WorkflowProvider,
                "start_run",
                "StartRun",
                _workflow_native.start_workflow_provider_run_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _workflow_native.workflow_run(result)

        @_grpc_handler("workflow get run")
        def GetRun(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                WorkflowProvider,
                "get_run",
                "GetRun",
                _workflow_native.get_workflow_provider_run_request_from_proto(request),
                request,
                context,
            )
            return _workflow_native.workflow_run(result)

        @_grpc_handler("workflow list runs")
        def ListRuns(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                WorkflowProvider,
                "list_runs",
                "ListRuns",
                _workflow_native.list_workflow_provider_runs_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _workflow_native.list_workflow_provider_runs_response_to_proto(
                result
            )

        @_grpc_handler("workflow get run events")
        def GetRunEvents(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                WorkflowProvider,
                "get_run_events",
                "GetRunEvents",
                _workflow_native.get_workflow_provider_run_events_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _workflow_native.get_workflow_provider_run_events_response_to_proto(
                result
            )

        @_grpc_handler("workflow get run output")
        def GetRunOutput(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                WorkflowProvider,
                "get_run_output",
                "GetRunOutput",
                _workflow_native.get_workflow_provider_run_output_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _workflow_native.get_workflow_provider_run_output_response_to_proto(
                result
            )

        @_grpc_handler("workflow cancel run")
        def CancelRun(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                WorkflowProvider,
                "cancel_run",
                "CancelRun",
                _workflow_native.cancel_workflow_provider_run_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _workflow_native.workflow_run(result)

        @_grpc_handler("workflow signal run")
        def SignalRun(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                WorkflowProvider,
                "signal_run",
                "SignalRun",
                _workflow_native.signal_workflow_provider_run_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _workflow_native.signal_workflow_run_response_to_proto(result)

        @_grpc_handler("workflow signal or start run")
        def SignalOrStartRun(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                WorkflowProvider,
                "signal_or_start_run",
                "SignalOrStartRun",
                _workflow_native.signal_or_start_workflow_provider_run_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _workflow_native.signal_workflow_run_response_to_proto(result)

        @_grpc_handler("workflow deliver event")
        def DeliverEvent(self, request: Any, context: Any) -> Any:
            result = _call_native_provider_handler(
                self._provider,
                WorkflowProvider,
                "deliver_event",
                "DeliverEvent",
                _workflow_native.deliver_workflow_provider_event_request_from_proto(
                    request
                ),
                request,
                context,
            )
            return _workflow_native.workflow_event(result)

    return WorkflowServicer(provider)


def _secrets_runtime_plugin(provider: SecretsProvider) -> AppProviderAdapter:
    return AppProviderAdapter(
        kind=ProviderKind.SECRETS,
        provider=provider,
        register_services=_register_secrets_services,
    )


def _register_secrets_services(server: Any, provider: AppProvider) -> None:
    _ensure_grpc_runtime()
    runtime_pb2_grpc.add_ProviderLifecycleServicer_to_server(
        _runtime_servicer(provider=provider, kind=ProviderKind.SECRETS),
        server,
    )
    secrets_pb2_grpc.add_SecretsServicer_to_server(
        _secrets_servicer(provider=provider),
        server,
    )


def _cache_runtime_plugin(provider: CacheProvider) -> AppProviderAdapter:
    return AppProviderAdapter(
        kind=ProviderKind.CACHE,
        provider=provider,
        register_services=_register_cache_services,
    )


def _register_cache_services(server: Any, provider: AppProvider) -> None:
    _ensure_grpc_runtime()
    runtime_pb2_grpc.add_ProviderLifecycleServicer_to_server(
        _runtime_servicer(provider=provider, kind=ProviderKind.CACHE),
        server,
    )
    cache_pb2_grpc.add_CacheServicer_to_server(
        _cache_servicer(provider=provider),
        server,
    )


def _call_provider_handler(handler: Any, *args: Any) -> Any:
    try:
        signature = inspect.signature(handler)
    except (TypeError, ValueError):
        return handler(*args)

    positional = [
        parameter
        for parameter in signature.parameters.values()
        if parameter.kind
        in (inspect.Parameter.POSITIONAL_ONLY, inspect.Parameter.POSITIONAL_OR_KEYWORD)
    ]
    if any(
        parameter.kind is inspect.Parameter.VAR_POSITIONAL
        for parameter in signature.parameters.values()
    ):
        return handler(*args)
    if len(positional) == 0:
        return handler()
    if len(positional) == 1 and args:
        return handler(args[0])
    return handler(*args)


def _call_native_provider_handler(
    provider: AppProvider,
    base_cls: type[Any],
    sdk_method_name: str,
    protocol_method_name: str,
    native_request: Any,
    raw_request: Any,
    context: Any,
) -> Any:
    if _provider_overrides(provider, sdk_method_name, base_cls):
        return _call_provider_handler(
            getattr(provider, sdk_method_name),
            native_request,
        )
    protocol_handler = getattr(provider, protocol_method_name, None)
    if protocol_handler is not None:
        return _call_provider_handler(protocol_handler, raw_request, context)
    return _call_provider_handler(getattr(provider, sdk_method_name), native_request)


def _empty_response(value: Any) -> Any:
    _ensure_grpc_runtime()
    if isinstance(value, empty_pb2.Empty):
        return value
    return empty_pb2.Empty()


def _provider_servicer(*, app: App) -> Any:
    _ensure_grpc_runtime()

    class ProviderServicer(app_pb2_grpc.AppProviderServicer):
        @_grpc_handler("provider metadata")
        def GetMetadata(self, _request: Any, _context: Any) -> Any:
            return app_pb2.ProviderMetadata(
                supports_session_catalog=app.supports_session_catalog(),
                min_protocol_version=CURRENT_PROTOCOL_VERSION,
                max_protocol_version=CURRENT_PROTOCOL_VERSION,
            )

        @_grpc_handler("configure provider")
        def StartProvider(self, request: Any, context: Any) -> Any:
            if _abort_if_protocol_version_mismatch(request.protocol_version, context):
                return None
            app.configure_provider(
                request.name,
                _message_to_dict(
                    field_name="config",
                    message=request.config,
                    request=request,
                ),
            )
            return app_pb2.StartProviderResponse(
                protocol_version=CURRENT_PROTOCOL_VERSION
            )

        def Execute(self, request: Any, _context: Any) -> Any:
            try:
                result = app.execute(
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
                body = json_body({"error": INTERNAL_ERROR_MESSAGE}).encode("utf-8")
                return app_pb2.OperationResult(
                    status=status,
                    body=body,
                    headers=_proto_string_lists({"Content-Type": [JSON_CONTENT_TYPE]}),
                )
            return app_pb2.OperationResult(
                status=result.status,
                body=result.body,
                headers=_proto_string_lists(result.headers),
            )

        def ResolveHTTPSubject(self, request: Any, context: Any) -> Any:
            if not app.supports_http_subject():
                return app_pb2.ResolveHTTPSubjectResponse()

            try:
                subject = app.resolve_http_subject(
                    _http_subject_request(getattr(request, "request", None)),
                    _plugin_request(request),
                )
            except HTTPSubjectResolutionError as error:
                return app_pb2.ResolveHTTPSubjectResponse(
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
                return app_pb2.ResolveHTTPSubjectResponse()
            return app_pb2.ResolveHTTPSubjectResponse(
                subject=app_pb2.SubjectContext(
                    id=subject.id,
                    email=subject.email,
                    display_name=subject.display_name,
                )
            )

        def GetSessionCatalog(self, request: Any, context: Any) -> Any:
            if not app.supports_session_catalog():
                return context.abort(
                    grpc.StatusCode.UNIMPLEMENTED,
                    "provider does not support session catalogs",
                )

            try:
                catalog = app.catalog_for_request(_plugin_request(request))
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

            return app_pb2.GetSessionCatalogResponse(catalog=proto_catalog)

    return ProviderServicer()


def _runtime_servicer(*, provider: AppProvider, kind: ProviderKind) -> Any:
    _ensure_grpc_runtime()

    class RuntimeServicer(runtime_pb2_grpc.ProviderLifecycleServicer):
        @_grpc_handler("provider identity")
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

        @_grpc_handler("start provider")
        def StartProvider(self, _request: Any, _context: Any) -> Any:
            if isinstance(provider, Starter):
                provider.start()
            return runtime_pb2.StartRuntimeProviderResponse(
                protocol_version=CURRENT_PROTOCOL_VERSION
            )

    return RuntimeServicer()


def _auth_call_context_from_handler(context: Any) -> IdentityCallContext:
    invocation_metadata = getattr(context, "invocation_metadata", None)
    if invocation_metadata is None:
        return IdentityCallContext()
    for key, value in invocation_metadata():
        if key != CALLER_BEARER_TOKEN_METADATA_KEY:
            continue
        if isinstance(value, bytes):
            token = value.decode("utf-8")
        else:
            token = str(value)
        return IdentityCallContext(caller_bearer_token=token.strip())
    return IdentityCallContext()


def _authentication_servicer(*, provider: AppProvider) -> Any:
    _ensure_grpc_runtime()
    auth_provider = cast(IdentityProvider, provider)

    class AuthenticationServicer(
        authentication_pb2_grpc.AuthenticationServicer
    ):
        @_grpc_handler("authorize")
        def Authorize(self, request: Any, context: Any) -> Any:
            response = auth_provider.authorize(
                _identity_codec.from_wire_authorize_request(request)
            )
            if response is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "authentication provider returned nil response",
                )
            return _identity_codec.to_wire_authorize_response(response)

        @_grpc_handler("token")
        def Token(self, request: Any, context: Any) -> Any:
            response = auth_provider.token(
                _identity_codec.from_wire_token_request(request)
            )
            if response is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "authentication provider returned nil response",
                )
            return _identity_codec.to_wire_token_response(response)

        @_grpc_handler("introspect")
        def Introspect(self, request: Any, context: Any) -> Any:
            response = auth_provider.introspect(
                _identity_codec.from_wire_introspect_request(request)
            )
            if response is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "authentication provider returned nil response",
                )
            return _identity_codec.to_wire_introspect_response(response)

        @_grpc_handler("userinfo")
        def UserInfo(self, request: Any, context: Any) -> Any:
            _ = request
            response = auth_provider.user_info(
                _identity_codec.from_wire_user_info_request(request),
                _auth_call_context_from_handler(context),
            )
            if response is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "authentication provider returned nil response",
                )
            return _identity_codec.to_wire_user_info_response(response)

        @_grpc_handler("list grants")
        def ListGrants(self, request: Any, context: Any) -> Any:
            _ = request
            response = auth_provider.list_grants(
                _identity_codec.from_wire_list_grants_request(request),
                _auth_call_context_from_handler(context),
            )
            if response is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "authentication provider returned nil response",
                )
            return _identity_codec.to_wire_list_grants_response(response)

        @_grpc_handler("get grant")
        def GetGrant(self, request: Any, context: Any) -> Any:
            response = auth_provider.get_grant(
                _identity_codec.from_wire_get_grant_request(request),
                _auth_call_context_from_handler(context),
            )
            if response is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "authentication provider returned nil response",
                )
            return _identity_codec.to_wire_get_grant_response(response)

        @_grpc_handler("revoke grant")
        def RevokeGrant(self, request: Any, context: Any) -> Any:
            response = auth_provider.revoke_grant(
                _identity_codec.from_wire_revoke_grant_request(request),
                _auth_call_context_from_handler(context),
            )
            if response is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "authentication provider returned nil response",
                )
            return _identity_codec.to_wire_revoke_grant_response(response)

    return AuthenticationServicer()


def _authorization_servicer(*, provider: AppProvider) -> Any:
    _ensure_grpc_runtime()
    authorization_provider = cast(AuthorizationProvider, provider)

    class AuthorizationServicer(authorization_pb2_grpc.AuthorizationServicer):
        @_grpc_handler("authorization check access")
        def CheckAccess(self, request: Any, context: Any) -> Any:
            response = authorization_provider.check_access(
                _authorization_codec.from_wire_check_access_request(request)
            )
            if response is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "authorization provider returned nil response",
                )
            return _authorization_codec.to_wire_check_access_response(response)

        @_grpc_handler("authorization check access many")
        def CheckAccessMany(self, request: Any, context: Any) -> Any:
            response = authorization_provider.check_access_many(
                _authorization_codec.from_wire_check_access_many_request(request)
            )
            if response is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "authorization provider returned nil response",
                )
            return _authorization_codec.to_wire_check_access_many_response(response)

        @_grpc_handler("authorization list relationships")
        def ListRelationships(self, request: Any, context: Any) -> Any:
            response = authorization_provider.list_relationships(
                _authorization_codec.from_wire_list_relationships_request(request)
            )
            if response is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "authorization provider returned nil response",
                )
            return _authorization_codec.to_wire_list_relationships_response(response)

        @_grpc_handler("authorization add relationship")
        def AddRelationship(self, request: Any, context: Any) -> Any:
            response = authorization_provider.add_relationship(
                _authorization_codec.from_wire_add_relationship_request(request)
            )
            if response is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "authorization provider returned nil response",
                )
            return _authorization_codec.to_wire_add_relationship_response(response)

        @_grpc_handler("authorization delete relationship")
        def DeleteRelationship(self, request: Any, _context: Any) -> Any:
            response = authorization_provider.delete_relationship(
                _authorization_codec.from_wire_delete_relationship_request(request)
            )
            if response is None:
                from .authorization import DeleteRelationshipResponse

                response = DeleteRelationshipResponse()
            return _authorization_codec.to_wire_delete_relationship_response(response)

        @_grpc_handler("authorization set authorization state")
        def SetAuthorizationState(self, request: Any, context: Any) -> Any:
            response = authorization_provider.set_authorization_state(
                _authorization_codec.from_wire_set_authorization_state_request(
                    request
                )
            )
            if response is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "authorization provider returned nil response",
                )
            return _authorization_codec.to_wire_set_authorization_state_response(
                response
            )

        @_grpc_handler("authorization get active model ref")
        def GetActiveModelRef(self, _request: Any, context: Any) -> Any:
            response = authorization_provider.get_active_model_ref()
            if response is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "authorization provider returned nil response",
                )
            return _authorization_codec.to_wire_get_active_model_ref_response(
                response
            )

        @_grpc_handler("authorization set active model")
        def SetActiveModel(self, request: Any, context: Any) -> Any:
            response = authorization_provider.set_active_model(
                _authorization_codec.from_wire_set_active_model_request(request)
            )
            if response is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "authorization provider returned nil response",
                )
            return _authorization_codec.to_wire_set_active_model_response(response)

        @_grpc_handler("authorization list active model resource types")
        def ListActiveModelResourceTypes(self, request: Any, context: Any) -> Any:
            response = authorization_provider.list_active_model_resource_types(
                _authorization_codec.from_wire_list_active_model_resource_types_request(
                    request
                )
            )
            if response is None:
                return context.abort(
                    grpc.StatusCode.INTERNAL,
                    "authorization provider returned nil response",
                )
            return _authorization_codec.to_wire_list_active_model_resource_types_response(
                response
            )

    return AuthorizationServicer()


def _provider_overrides(
    provider: AppProvider,
    method_name: str,
    base_cls: type[Any],
) -> bool:
    provider_method = getattr(type(provider), method_name, None)
    base_method = getattr(base_cls, method_name, None)
    return provider_method is not None and provider_method is not base_method


def _secrets_servicer(*, provider: AppProvider) -> Any:
    _ensure_grpc_runtime()
    secrets_provider = cast(SecretsProvider, provider)

    class SecretsServicer(secrets_pb2_grpc.SecretsServicer):
        @_grpc_handler("get secret")
        def GetSecret(self, request: Any, context: Any) -> Any:
            value = secrets_provider.get_secret(request.name)
            return secrets_pb2.GetSecretResponse(value=value)

    return SecretsServicer()


def _cache_servicer(*, provider: AppProvider) -> Any:
    _ensure_grpc_runtime()
    from .cache import CacheSetEntry

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
                    entries.append(
                        cache_pb2.CacheResult(key=key, found=False, value=b"")
                    )
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
                [
                    CacheSetEntry(key=entry.key, value=bytes(entry.value))
                    for entry in request.entries
                ],
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
    request_context = getattr(request, "context", None)
    tool_refs, tool_refs_set = _tool_refs_from_proto(request_context)
    return Request(
        token=getattr(request, "token", ""),
        connection_params=dict(getattr(request, "connection_params", {})),
        subject=_subject_from_proto(request_context, "subject"),
        agent_subject=_subject_from_proto(request_context, "agent_subject"),
        credential=_credential_from_proto(request_context),
        access=_access_from_proto(request_context),
        host=_host_from_proto(request_context),
        workflow=_workflow_from_proto(request_context),
        tool_refs=tool_refs,
        tool_refs_set=tool_refs_set,
        idempotency_key=getattr(request, "idempotency_key", "").strip(),
        context=request_context,
    )


def _http_subject_request(request: Any) -> HTTPSubjectRequest:
    if request is None:
        return HTTPSubjectRequest()
    return HTTPSubjectRequest(
        binding=getattr(request, "binding", ""),
        method=getattr(request, "method", ""),
        path=getattr(request, "path", ""),
        content_type=getattr(request, "content_type", ""),
        headers=string_lists_from_proto_map(getattr(request, "headers", {})),
        query=string_lists_from_proto_map(getattr(request, "query", {})),
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


def _proto_string_lists(values: dict[str, list[str]]) -> dict[str, Any]:
    return {
        key: app_pb2.StringList(values=list(items)) for key, items in values.items()
    }


def _subject_from_proto(request_context: Any, field_name: str) -> Subject:
    if request_context is None:
        return Subject()
    subject = getattr(request_context, field_name, None)
    if subject is None:
        return Subject()
    return Subject(
        id=getattr(subject, "id", ""),
        email=getattr(subject, "email", ""),
        display_name=getattr(subject, "display_name", ""),
        scopes=list(getattr(subject, "scopes", ())),
        permissions=[
            SubjectPermission(
                app=getattr(permission, "app", ""),
                operations=list(getattr(permission, "operations", ())),
                all_operations=bool(getattr(permission, "all_operations", False)),
            )
            for permission in getattr(subject, "permissions", ())
        ],
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


def _host_from_proto(request_context: Any) -> Host:
    if request_context is None:
        return Host()
    host = getattr(request_context, "host", None)
    if host is None:
        return Host()
    return Host(public_base_url=getattr(host, "public_base_url", ""))


def _workflow_from_proto(request_context: Any) -> dict[str, Any]:
    if request_context is None:
        return {}
    if hasattr(request_context, "HasField") and not request_context.HasField(
        "workflow"
    ):
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


def _tool_refs_from_proto(request_context: Any) -> tuple[list[Any], bool]:
    if request_context is None:
        return [], False
    if not bool(getattr(request_context, "tool_refs_set", False)):
        return [], False
    return [
        _agent_native.agent_tool_ref_from_proto(ref)
        for ref in getattr(request_context, "tool_refs", ())
    ], True


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
    *, provider: AppProvider, kind: ProviderKind
) -> ProviderMetadata:
    if isinstance(provider, MetadataProvider):
        metadata = provider.metadata()
        if isinstance(metadata, ProviderMetadata):
            return metadata
    return ProviderMetadata(kind=kind)


def _provider_warnings(provider: AppProvider) -> list[str]:
    if isinstance(provider, WarningsProvider):
        return list(provider.warnings())
    return []


def _provider_kind_to_proto(kind: ProviderKind | str) -> Any:
    _ensure_grpc_runtime()
    normalized = _normalized_runtime_kind(kind)
    return {
        ProviderKind.INTEGRATION: runtime_pb2.ProviderKind.PROVIDER_KIND_APP,
        ProviderKind.AUTHORIZATION: runtime_pb2.ProviderKind.PROVIDER_KIND_AUTHORIZATION,
        ProviderKind.AUTHENTICATION: runtime_pb2.ProviderKind.PROVIDER_KIND_AUTHENTICATION,
        ProviderKind.CACHE: runtime_pb2.ProviderKind.PROVIDER_KIND_CACHE,
        ProviderKind.S3: runtime_pb2.ProviderKind.PROVIDER_KIND_S3,
        ProviderKind.AGENT: runtime_pb2.ProviderKind.PROVIDER_KIND_AGENT,
        ProviderKind.RUNTIME: runtime_pb2.ProviderKind.PROVIDER_KIND_RUNTIME,
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


if __name__ == "__main__":
    raise SystemExit(main())
