import importlib
import os
import pathlib
import signal
import sys
import traceback
from concurrent import futures
from dataclasses import dataclass
from http import HTTPStatus
from typing import Any, Final

import grpc
from google.protobuf import json_format

from ._api import Request
from ._bootstrap import parse_plugin_target, read_bundled_plugin_config
from ._plugin import Plugin, _module_plugin
from ._serialization import json_body
from .gen.v1 import plugin_pb2 as _plugin_pb2
from .gen.v1 import plugin_pb2_grpc as _plugin_pb2_grpc

# Protobuf-generated modules use dynamic descriptors that type checkers cannot
# resolve. Explicit Any-typed aliases let the rest of the file access message
# classes without per-line suppressions.
plugin_pb2: Any = _plugin_pb2
plugin_pb2_grpc: Any = _plugin_pb2_grpc

ENV_PLUGIN_SOCKET: Final[str] = "GESTALT_PLUGIN_SOCKET"
ENV_WRITE_CATALOG: Final[str] = "GESTALT_PLUGIN_WRITE_CATALOG"
CURRENT_PROTOCOL_VERSION: Final[int] = 2
GRPC_SERVER_MAX_WORKERS: Final[int] = 4
GRPC_SHUTDOWN_GRACE_SECONDS: Final[int] = 2
USAGE: Final[str] = "usage: python -m gestalt._runtime ROOT MODULE[:ATTRIBUTE]"


@dataclass(frozen=True)
class RuntimeArgs:
    target: str
    root: pathlib.Path | None = None
    plugin_name: str | None = None


class _ProviderServicer(plugin_pb2_grpc.ProviderPluginServicer):
    def __init__(self, plugin: Plugin) -> None:
        self._plugin = plugin

    def GetMetadata(self, request: Any, context: Any) -> Any:
        del request, context
        return plugin_pb2.ProviderMetadata(
            min_protocol_version=CURRENT_PROTOCOL_VERSION,
            max_protocol_version=CURRENT_PROTOCOL_VERSION,
        )

    def StartProvider(self, request: Any, context: Any) -> Any:
        del context
        self._plugin.configure_provider(
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

    def Execute(self, request: Any, context: Any) -> Any:
        del context
        try:
            result = self._plugin.execute(
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
            return plugin_pb2.OperationResult(status=status, body=body)
        return plugin_pb2.OperationResult(status=result.status, body=result.body)


def serve(plugin: Plugin) -> None:
    socket_path = _socket_path_from_env()
    _remove_stale_socket(socket_path)

    server = grpc.server(
        futures.ThreadPoolExecutor(max_workers=GRPC_SERVER_MAX_WORKERS)
    )
    plugin_pb2_grpc.add_ProviderPluginServicer_to_server(
        _ProviderServicer(plugin),
        server,
    )
    server.add_insecure_port(f"unix:{socket_path}")
    server.start()
    _register_shutdown_handlers(server)
    server.wait_for_termination()


def main(argv: list[str] | None = None) -> int:
    runtime_args = _parse_runtime_args(sys.argv[1:] if argv is None else argv)
    if runtime_args is None:
        print(USAGE, file=sys.stderr)
        return 2

    plugin = _load_plugin(runtime_args)
    if runtime_args.plugin_name:
        plugin.name = runtime_args.plugin_name

    catalog_path = os.environ.get(ENV_WRITE_CATALOG)
    if catalog_path:
        plugin.write_catalog(catalog_path)
        return 0

    serve(plugin)
    return 0


def _parse_runtime_args(args: list[str]) -> RuntimeArgs | None:
    if args:
        if len(args) != 2:
            return None

        root, target = args
        return RuntimeArgs(target=target, root=pathlib.Path(root))

    bundled_config = read_bundled_plugin_config(
        bundle_root=pathlib.Path(getattr(sys, "_MEIPASS", pathlib.Path(__file__).resolve().parent))
    )
    if bundled_config is None:
        return None

    return RuntimeArgs(
        target=bundled_config.target,
        plugin_name=bundled_config.plugin_name,
    )


def _load_plugin(args: RuntimeArgs) -> Plugin:
    if args.root is not None:
        root = str(args.root)
        if root not in sys.path:
            sys.path.insert(0, root)

    plugin_target = parse_plugin_target(args.target)
    module = importlib.import_module(plugin_target.module_name)
    if plugin_target.attribute_name is None:
        plugin = _module_plugin(module)
    else:
        plugin = getattr(module, plugin_target.attribute_name, None)
    if not isinstance(plugin, Plugin):
        raise RuntimeError(f"{args.target} did not resolve to a gestalt.Plugin")
    return plugin


def _socket_path_from_env() -> pathlib.Path:
    socket_path = os.environ.get(ENV_PLUGIN_SOCKET)
    if not socket_path:
        raise RuntimeError(f"{ENV_PLUGIN_SOCKET} is required")
    return pathlib.Path(socket_path)


def _remove_stale_socket(socket_path: pathlib.Path) -> None:
    if socket_path.exists():
        socket_path.unlink()


def _register_shutdown_handlers(server: Any) -> None:
    def _shutdown(_signum: int, _frame: Any) -> None:
        server.stop(grace=GRPC_SHUTDOWN_GRACE_SECONDS)

    signal.signal(signal.SIGTERM, _shutdown)
    signal.signal(signal.SIGINT, _shutdown)


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


if __name__ == "__main__":
    raise SystemExit(main())
