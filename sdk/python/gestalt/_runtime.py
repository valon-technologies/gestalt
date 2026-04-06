from __future__ import annotations

import os
import signal
import sys
from concurrent import futures

from ._plugin import ENV_WRITE_CATALOG, Plugin, Request

ENV_PLUGIN_SOCKET = "GESTALT_PLUGIN_SOCKET"
CURRENT_PROTOCOL_VERSION = 2


def serve(plugin: Plugin) -> None:
    grpc, json_format, plugin_pb2, plugin_pb2_grpc = _runtime_imports()

    socket_path = os.environ.get(ENV_PLUGIN_SOCKET)
    if not socket_path:
        raise RuntimeError(f"{ENV_PLUGIN_SOCKET} is required")

    if os.path.exists(socket_path):
        os.unlink(socket_path)

    server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))

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
    args = list(sys.argv[1:] if argv is None else argv)
    if len(args) != 2:
        print("usage: python -m gestalt._runtime ROOT MODULE:ATTRIBUTE", file=sys.stderr)
        return 2

    root, target = args
    if root not in sys.path:
        sys.path.insert(0, root)

    plugin = _load_plugin(target)
    catalog_path = os.environ.get(ENV_WRITE_CATALOG)
    if catalog_path:
        plugin.write_catalog(catalog_path)
        return 0

    serve(plugin)
    return 0


def _load_plugin(target: str) -> Plugin:
    import importlib

    module_name, _, attr_name = target.partition(":")
    if not module_name or not attr_name:
        raise RuntimeError("tool.gestalt.plugin must be in module:attribute form")

    module = importlib.import_module(module_name)
    plugin = getattr(module, attr_name, None)
    if not isinstance(plugin, Plugin):
        raise RuntimeError(f"{target} did not resolve to a gestalt.Plugin")
    return plugin


def _runtime_imports():
    import grpc
    from google.protobuf import json_format

    from .gen.v1 import plugin_pb2, plugin_pb2_grpc

    return grpc, json_format, plugin_pb2, plugin_pb2_grpc


if __name__ == "__main__":
    raise SystemExit(main())
