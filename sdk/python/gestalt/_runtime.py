import importlib
import os
import pathlib
import signal
import sys
from concurrent import futures
from dataclasses import dataclass

from ._bootstrap import parse_plugin_target, read_bundled_plugin_config
from ._plugin import ENV_WRITE_CATALOG, Plugin, Request

ENV_PLUGIN_SOCKET = "GESTALT_PLUGIN_SOCKET"
CURRENT_PROTOCOL_VERSION = 2
GRPC_SERVER_MAX_WORKERS = 4


@dataclass(frozen=True)
class RuntimeArgs:
    target: str
    root: str | None = None
    plugin_name: str | None = None


def serve(plugin: Plugin) -> None:
    import grpc
    from google.protobuf import json_format

    from .gen.v1 import plugin_pb2, plugin_pb2_grpc

    socket_path = os.environ.get(ENV_PLUGIN_SOCKET)
    if not socket_path:
        raise RuntimeError(f"{ENV_PLUGIN_SOCKET} is required")

    if os.path.exists(socket_path):
        os.unlink(socket_path)

    server = grpc.server(futures.ThreadPoolExecutor(max_workers=GRPC_SERVER_MAX_WORKERS))

    class ProviderServicer(plugin_pb2_grpc.ProviderPluginServicer):
        def GetMetadata(self, _request, _context):
            return plugin_pb2.ProviderMetadata(
                min_protocol_version=CURRENT_PROTOCOL_VERSION,
                max_protocol_version=CURRENT_PROTOCOL_VERSION,
            )

        def StartProvider(self, request, _context):
            config = {}
            if request.HasField("config"):
                config = json_format.MessageToDict(
                    request.config,
                    preserving_proto_field_name=True,
                )
            plugin.configure_provider(request.name, config)
            return plugin_pb2.StartProviderResponse(protocol_version=CURRENT_PROTOCOL_VERSION)

        def Execute(self, request, _context):
            params = {}
            if request.HasField("params"):
                params = json_format.MessageToDict(
                    request.params,
                    preserving_proto_field_name=True,
                )
            status, body = plugin.execute(
                request.operation,
                params,
                Request(
                    token=request.token,
                    connection_params=dict(request.connection_params),
                ),
            )
            return plugin_pb2.OperationResult(status=status, body=body)

    plugin_pb2_grpc.add_ProviderPluginServicer_to_server(ProviderServicer(), server)
    server.add_insecure_port(f"unix:{socket_path}")
    server.start()

    def _shutdown(_signum, _frame):
        server.stop(grace=2)

    signal.signal(signal.SIGTERM, _shutdown)
    signal.signal(signal.SIGINT, _shutdown)
    server.wait_for_termination()


def main(argv: list[str] | None = None) -> int:
    runtime_args = _parse_runtime_args(sys.argv[1:] if argv is None else argv)
    if runtime_args is None:
        _print_usage()
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
        return RuntimeArgs(target=target, root=root)

    bundled_config = read_bundled_plugin_config(bundle_root=_bundle_root())
    if bundled_config is None:
        return None

    return RuntimeArgs(
        target=bundled_config.target,
        plugin_name=bundled_config.plugin_name,
    )


def _bundle_root() -> pathlib.Path:
    return pathlib.Path(getattr(sys, "_MEIPASS", pathlib.Path(__file__).resolve().parent))


def _load_plugin(args: RuntimeArgs) -> Plugin:
    if args.root and args.root not in sys.path:
        sys.path.insert(0, args.root)

    plugin_target = parse_plugin_target(args.target)
    module = importlib.import_module(plugin_target.module_name)
    plugin = getattr(module, plugin_target.attribute_name, None)
    if not isinstance(plugin, Plugin):
        raise RuntimeError(f"{args.target} did not resolve to a gestalt.Plugin")
    return plugin


def _print_usage() -> None:
    print("usage: python -m gestalt._runtime ROOT MODULE:ATTRIBUTE", file=sys.stderr)


if __name__ == "__main__":
    raise SystemExit(main())
