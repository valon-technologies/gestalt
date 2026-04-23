import importlib
import os
import pathlib
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from typing import Final

if sys.version_info >= (3, 11):
    _toml_loads = importlib.import_module("tomllib").loads
else:
    import tomli

    _toml_loads = tomli.loads

from ._bootstrap import (
    BUNDLED_CONFIG_NAME,
    parse_plugin_target,
    write_bundled_plugin_config,
)

USAGE: Final[str] = (
    "usage: python -m gestalt._build ROOT MODULE[:ATTRIBUTE] OUTPUT PLUGIN_NAME RUNTIME_KIND GOOS GOARCH"
)


@dataclass(frozen=True)
class BuildArgs:
    root: pathlib.Path
    target: str
    output_path: pathlib.Path
    plugin_name: str
    runtime_kind: str
    goos: str
    goarch: str


@dataclass(frozen=True)
class PyInstallerConfig:
    collect_data: tuple[str, ...] = ()
    collect_submodules: tuple[str, ...] = ()
    collect_binaries: tuple[str, ...] = ()
    collect_all: tuple[str, ...] = ()
    copy_metadata: tuple[str, ...] = ()
    recursive_copy_metadata: tuple[str, ...] = ()
    hidden_imports: tuple[str, ...] = ()
    exclude_modules: tuple[str, ...] = ()
    additional_hooks_dir: tuple[pathlib.Path, ...] = ()


def main(argv: list[str] | None = None) -> int:
    build_args = _parse_build_args(sys.argv[1:] if argv is None else argv)
    if build_args is None:
        print(USAGE, file=sys.stderr)
        return 2

    build_plugin_binary(build_args)
    return 0


def _parse_build_args(args: list[str]) -> BuildArgs | None:
    if len(args) != 7:
        return None

    root, target, output_path, plugin_name, runtime_kind, goos, goarch = args
    return BuildArgs(
        root=pathlib.Path(root),
        target=target,
        output_path=pathlib.Path(output_path),
        plugin_name=plugin_name,
        runtime_kind=runtime_kind,
        goos=goos,
        goarch=goarch,
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
            runtime_kind=args.runtime_kind,
        )

        env = os.environ.copy()
        env["PYINSTALLER_CONFIG_DIR"] = str(work_path / "pyinstaller-config")
        env["SOURCE_DATE_EPOCH"] = "0"

        subprocess.run(
            _pyinstaller_command(
                root_path=root_path,
                output_path=output_path,
                module_name=plugin_target.module_name,
                bundle_config_path=bundle_config_path,
                target_goos=args.goos,
                target_goarch=args.goarch,
                pyinstaller_config=_read_pyinstaller_config(root_path),
            ),
            cwd=root_path,
            env=env,
            check=True,
        )


def _pyinstaller_command(
    *,
    root_path: pathlib.Path,
    output_path: pathlib.Path,
    module_name: str,
    bundle_config_path: pathlib.Path,
    target_goos: str,
    target_goarch: str,
    pyinstaller_config: PyInstallerConfig | None = None,
) -> list[str]:
    pyinstaller_name = (
        output_path.name.removesuffix(".exe")
        if target_goos == "windows"
        else output_path.name
    )
    pyinstaller_config = pyinstaller_config or PyInstallerConfig()

    command = [
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
        *_pyinstaller_collection_args(pyinstaller_config),
        "--add-data",
        f"{bundle_config_path}{os.pathsep}.",
        str(pathlib.Path(__file__).with_name("_pyinstaller.py")),
    ]
    if sys.platform == "darwin" and target_goos == "darwin":
        target_arch = _darwin_target_arch(target_goarch)
        if target_arch:
            command.extend(["--target-arch", target_arch])
    return command


def _read_pyinstaller_config(root_path: pathlib.Path) -> PyInstallerConfig:
    project_path = root_path / "pyproject.toml"
    if not project_path.exists():
        return PyInstallerConfig()
    data = _toml_loads(project_path.read_text(encoding="utf-8"))
    raw = data.get("tool", {}).get("gestalt", {}).get("pyinstaller", {})
    if not isinstance(raw, dict):
        raise RuntimeError("tool.gestalt.pyinstaller must be a table")
    return PyInstallerConfig(
        collect_data=_string_tuple(raw, "collect-data"),
        collect_submodules=_string_tuple(raw, "collect-submodules"),
        collect_binaries=_string_tuple(raw, "collect-binaries"),
        collect_all=_string_tuple(raw, "collect-all"),
        copy_metadata=_string_tuple(raw, "copy-metadata"),
        recursive_copy_metadata=_string_tuple(raw, "recursive-copy-metadata"),
        hidden_imports=_string_tuple(raw, "hidden-imports"),
        exclude_modules=_string_tuple(raw, "exclude-modules"),
        additional_hooks_dir=tuple(
            _config_path(root_path, value)
            for value in _string_tuple(raw, "additional-hooks-dir")
        ),
    )


def _string_tuple(raw: dict[str, object], key: str) -> tuple[str, ...]:
    value = raw.get(key, ())
    if value in (None, ()):
        return ()
    if not isinstance(value, list):
        raise RuntimeError(
            f"tool.gestalt.pyinstaller.{key} must be an array of non-empty strings"
        )
    out: list[str] = []
    for item in value:
        if not isinstance(item, str):
            raise RuntimeError(
                f"tool.gestalt.pyinstaller.{key} must be an array of non-empty strings"
            )
        stripped = item.strip()
        if not stripped:
            raise RuntimeError(
                f"tool.gestalt.pyinstaller.{key} must be an array of non-empty strings"
            )
        out.append(stripped)
    return tuple(out)


def _config_path(root_path: pathlib.Path, value: str) -> pathlib.Path:
    path = pathlib.Path(value)
    if not path.is_absolute():
        path = root_path / path
    return path


def _pyinstaller_collection_args(config: PyInstallerConfig) -> list[str]:
    args: list[str] = []
    for option, values in (
        ("--collect-data", config.collect_data),
        ("--collect-submodules", config.collect_submodules),
        ("--collect-binaries", config.collect_binaries),
        ("--collect-all", config.collect_all),
        ("--copy-metadata", config.copy_metadata),
        ("--recursive-copy-metadata", config.recursive_copy_metadata),
        ("--hidden-import", config.hidden_imports),
        ("--exclude-module", config.exclude_modules),
    ):
        for value in values:
            args.extend([option, value])
    for path in config.additional_hooks_dir:
        args.extend(["--additional-hooks-dir", str(path)])
    return args


def _darwin_target_arch(goarch: str) -> str | None:
    return {
        "amd64": "x86_64",
        "arm64": "arm64",
    }.get(goarch)


if __name__ == "__main__":
    raise SystemExit(main())
