from __future__ import annotations

import os
import pathlib
import subprocess
import sys
import tempfile

from ._runtime import _load_plugin


def main(argv: list[str] | None = None) -> int:
    args = list(sys.argv[1:] if argv is None else argv)
    if len(args) != 4:
        print("usage: python -m gestalt._build ROOT MODULE:ATTRIBUTE OUTPUT PLUGIN_NAME", file=sys.stderr)
        return 2

    root, target, output_path, plugin_name = args
    build_plugin_binary(root, target, output_path, plugin_name)
    return 0


def build_plugin_binary(root: str, target: str, output_path: str, plugin_name: str) -> None:
    root_path = pathlib.Path(root).resolve()
    output = pathlib.Path(output_path).resolve()

    _load_plugin(target, str(root_path))

    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="gestalt-python-release-") as work_dir:
        work_path = pathlib.Path(work_dir)
        launcher_path = work_path / "launcher.py"
        launcher_path.write_text(
            _launcher_source(target, plugin_name),
            encoding="utf-8",
        )

        pyinstaller_name = output.name
        if sys.platform == "win32" and pyinstaller_name.endswith(".exe"):
            pyinstaller_name = pyinstaller_name[:-4]

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
            target.split(":", 1)[0],
            "--paths",
            str(root_path),
            "--paths",
            str(_sdk_import_root()),
            str(launcher_path),
        ]
        subprocess.run(command, cwd=root_path, check=True)


def _launcher_source(target: str, plugin_name: str) -> str:
    module_name, _, attr_name = target.partition(":")
    return f"""from __future__ import annotations

import importlib
import os

from gestalt._runtime import serve

_gestalt_module = importlib.import_module({module_name!r})
_gestalt_plugin = getattr(_gestalt_module, {attr_name!r})

_gestalt_plugin.name = {plugin_name!r}

if __name__ == "__main__":
    catalog_path = os.environ.get("GESTALT_PLUGIN_WRITE_CATALOG")
    if catalog_path:
        _gestalt_plugin.write_catalog(catalog_path)
        raise SystemExit(0)
    serve(_gestalt_plugin)
"""


def _sdk_import_root() -> pathlib.Path:
    return pathlib.Path(__file__).resolve().parents[1]


if __name__ == "__main__":
    raise SystemExit(main())
