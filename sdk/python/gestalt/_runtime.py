from __future__ import annotations

import json
import os
import pathlib
import signal
import subprocess
import sys
import tempfile
from concurrent import futures

from ._plugin import ENV_WRITE_CATALOG, Plugin, Request

ENV_PLUGIN_SOCKET = "GESTALT_PLUGIN_SOCKET"
CURRENT_PROTOCOL_VERSION = 2
BUNDLED_CONFIG_NAME = "gestalt-runtime.json"


def serve(plugin: Plugin) -> None:
    import grpc
    from google.protobuf import json_format

    from .gen.v1 import plugin_pb2, plugin_pb2_grpc

    socket_path = os.environ.get(ENV_PLUGIN_SOCKET)
    if not socket_path:
        raise RuntimeError(f"{ENV_PLUGIN_SOCKET} is required")

    if os.path.exists(socket_path):
        os.unlink(socket_path)

    server = grpc.server(futures.ThreadPoolExecutor(max_workers=1))

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
    if not args:
        config_path = pathlib.Path(getattr(sys, "_MEIPASS", pathlib.Path(__file__).resolve().parent))
        config_path = config_path / BUNDLED_CONFIG_NAME
        if not config_path.exists():
            _print_usage()
            return 2

        data = json.loads(config_path.read_text(encoding="utf-8"))
        target = data.get("target", "").strip()
        if not target:
            raise RuntimeError(f"{config_path} is missing target")

        plugin_name = data.get("plugin_name")
        if plugin_name is not None:
            plugin_name = str(plugin_name).strip() or None
        root = None
    elif args[0] == "build":
        if len(args) != 5:
            _print_usage()
            return 2
        _, root, target, output_path, plugin_name = args
        build_plugin_binary(root, target, output_path, plugin_name)
        return 0
    else:
        if len(args) != 2:
            _print_usage()
            return 2
        root, target = args
        plugin_name = None

    plugin = _load_plugin(target, root)
    if plugin_name:
        plugin.name = plugin_name
    catalog_path = os.environ.get(ENV_WRITE_CATALOG)
    if catalog_path:
        plugin.write_catalog(catalog_path)
        return 0

    serve(plugin)
    return 0


def build_plugin_binary(root: str, target: str, output_path: str, plugin_name: str) -> None:
    root_path = pathlib.Path(root).resolve()
    output = pathlib.Path(output_path).resolve()
    module_name, _attr_name = _split_target(target)

    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="gestalt-python-release-") as work_dir:
        work_path = pathlib.Path(work_dir)
        bundle_config_path = work_path / BUNDLED_CONFIG_NAME
        bundle_config_path.write_text(
            json.dumps(
                {
                    "target": target,
                    "plugin_name": plugin_name,
                }
            ),
            encoding="utf-8",
        )

        pyinstaller_name = output.name
        if sys.platform == "win32" and pyinstaller_name.endswith(".exe"):
            pyinstaller_name = pyinstaller_name[:-4]
        separator = ";" if sys.platform == "win32" else ":"

        command = [
            sys.executable,
            "-m",
            "PyInstaller",
            "--noconfirm",
            "--clean",
            "--onefile",
            "--distpath",
            str(output.parent),
            "--workpath",
            str(work_path / "build"),
            "--specpath",
            str(work_path / "spec"),
            "--name",
            pyinstaller_name,
            "--hidden-import",
            module_name,
            "--paths",
            str(root_path),
            "--add-data",
            f"{bundle_config_path}{separator}{BUNDLED_CONFIG_NAME}",
            str(pathlib.Path(__file__).with_name("_pyinstaller.py")),
        ]
        subprocess.run(command, cwd=root_path, check=True)


def _load_plugin(target: str, root: str | None = None) -> Plugin:
    import importlib

    if root and root not in sys.path:
        sys.path.insert(0, root)

    module_name, attr_name = _split_target(target)
    module = importlib.import_module(module_name)
    plugin = getattr(module, attr_name, None)
    if not isinstance(plugin, Plugin):
        raise RuntimeError(f"{target} did not resolve to a gestalt.Plugin")
    return plugin


def _split_target(target: str) -> tuple[str, str]:
    module_name, _, attr_name = target.partition(":")
    if not module_name or not attr_name:
        raise RuntimeError("tool.gestalt.plugin must be in module:attribute form")
    return module_name, attr_name


def _print_usage() -> None:
    print(
        "usage: python -m gestalt._runtime ROOT MODULE:ATTRIBUTE\n"
        "   or: python -m gestalt._runtime build ROOT MODULE:ATTRIBUTE OUTPUT PLUGIN_NAME",
        file=sys.stderr,
    )


if __name__ == "__main__":
    raise SystemExit(main())
