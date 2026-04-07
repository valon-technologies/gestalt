from __future__ import annotations

import os
import pathlib
import subprocess
import sys
import tempfile
from dataclasses import dataclass

from ._bootstrap import (
    BUNDLED_CONFIG_NAME,
    parse_plugin_target,
    write_bundled_plugin_config,
)

USAGE = "usage: python -m gestalt._build ROOT MODULE[:ATTRIBUTE] OUTPUT PLUGIN_NAME"


@dataclass(frozen=True)
class BuildArgs:
    root: pathlib.Path
    target: str
    output_path: pathlib.Path
    plugin_name: str


def main(argv: list[str] | None = None) -> int:
    build_args = _parse_build_args(sys.argv[1:] if argv is None else argv)
    if build_args is None:
        print(USAGE, file=sys.stderr)
        return 2

    build_plugin_binary(build_args)
    return 0


def _parse_build_args(args: list[str]) -> BuildArgs | None:
    if len(args) != 4:
        return None

    root, target, output_path, plugin_name = args
    return BuildArgs(
        root=pathlib.Path(root),
        target=target,
        output_path=pathlib.Path(output_path),
        plugin_name=plugin_name,
    )


def build_plugin_binary(args: BuildArgs) -> None:
    root_path = args.root.resolve()
    output_path = args.output_path.resolve()
    plugin_target = parse_plugin_target(args.target)

    output_path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="gestalt-python-release-") as work_dir:
        work_path = pathlib.Path(work_dir)
        bundle_config_path = work_path / BUNDLED_CONFIG_NAME
        write_bundled_plugin_config(
            bundle_config_path,
            target=args.target,
            plugin_name=args.plugin_name,
        )

        subprocess.run(
            _pyinstaller_command(
                root_path=root_path,
                output_path=output_path,
                module_name=plugin_target.module_name,
                bundle_config_path=bundle_config_path,
            ),
            cwd=root_path,
            check=True,
        )


def _pyinstaller_command(
    *,
    root_path: pathlib.Path,
    output_path: pathlib.Path,
    module_name: str,
    bundle_config_path: pathlib.Path,
) -> list[str]:
    pyinstaller_name = output_path.stem if output_path.suffix == ".exe" else output_path.name

    return [
        sys.executable,
        "-m",
        "PyInstaller",
        "--noconfirm",
        "--clean",
        "--onefile",
        "--distpath",
        str(output_path.parent),
        "--workpath",
        str(bundle_config_path.parent / "build"),
        "--specpath",
        str(bundle_config_path.parent / "spec"),
        "--name",
        pyinstaller_name,
        "--hidden-import",
        module_name,
        "--paths",
        str(root_path),
        "--add-data",
        f"{bundle_config_path}{os.pathsep}.",
        str(pathlib.Path(__file__).with_name("_pyinstaller.py")),
    ]


if __name__ == "__main__":
    raise SystemExit(main())
