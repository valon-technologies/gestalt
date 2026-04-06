from __future__ import annotations

import json
import pathlib
import subprocess
import sys
import tempfile

from ._runtime import BUNDLED_CONFIG_NAME, _split_target


def main(argv: list[str] | None = None) -> int:
    args = list(sys.argv[1:] if argv is None else argv)
    if len(args) != 4:
        print(
            "usage: python -m gestalt._build ROOT MODULE:ATTRIBUTE OUTPUT PLUGIN_NAME",
            file=sys.stderr,
        )
        return 2

    root, target, output_path, plugin_name = args
    build_plugin_binary(root, target, output_path, plugin_name)
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


if __name__ == "__main__":
    raise SystemExit(main())
