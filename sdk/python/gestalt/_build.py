import contextlib
import hashlib
import importlib.metadata
import json
import os
import pathlib
import platform
import subprocess
import sys
import sysconfig
import tempfile
import time
from dataclasses import dataclass
from typing import Final

from ._bootstrap import (
    BUNDLED_CONFIG_NAME,
    parse_plugin_target,
    read_pyproject_provider,
    read_source_manifest,
    write_bundled_plugin_config,
)

USAGE: Final[str] = (
    "usage: python -m gestalt._build [ROOT MODULE[:ATTRIBUTE] OUTPUT PLUGIN_NAME RUNTIME_KIND GOOS GOARCH]\n"
    "  (zero args: derive from manifest.yaml + pyproject.toml + env)"
)
BUILD_CACHE_DIR_ENV_VAR: Final[str] = "GESTALT_BUILD_CACHE_DIR"
BUILD_CACHE_SCHEMA_VERSION: Final[str] = "python-provider-package-v1"
TARGET_OS_ENV: Final[str] = "GESTALT_TARGET_OS"
TARGET_ARCH_ENV: Final[str] = "GESTALT_TARGET_ARCH"

_RUNTIME_KIND_BY_MANIFEST_KIND: Final[dict[str, str]] = {
    "app": "integration",
    "identity": "identity",
    "authorization": "authorization",
    "externalcredentials": "externalcredentials",
    "indexeddb": "indexeddb",
    "cache": "cache",
    "s3": "s3",
    "workflow": "workflow",
    "agent": "agent",
    "secrets": "secrets",
    "runtime": "runtime",
    "telemetry": "telemetry",
}
_DEFAULT_RUNTIME_KIND: Final[str] = "integration"
_ARCH_ALIASES: Final[dict[str, str]] = {
    "x86_64": "amd64",
    "aarch64": "arm64",
}


@dataclass(frozen=True)
class BuildArgs:
    root: pathlib.Path
    target: str
    output_path: pathlib.Path
    app_name: str
    runtime_kind: str
    goos: str
    goarch: str


def main(argv: list[str] | None = None) -> int:
    args = sys.argv[1:] if argv is None else argv
    build_args = derive_build_args(pathlib.Path.cwd()) if len(args) == 0 else _parse_build_args(args)
    if build_args is None:
        print(USAGE, file=sys.stderr)
        return 2

    build_plugin_binary(build_args)
    return 0


def derive_build_args(root: pathlib.Path) -> BuildArgs | None:
    """Derive build args from manifest.yaml + pyproject.toml + env.

    Used when the build entrypoint is invoked with zero positional args.
    Returns ``None`` (after printing the error) so ``main`` surfaces USAGE.
    """
    try:
        manifest = read_source_manifest(root)
        if manifest.entrypoint_artifact_path is None:
            raise RuntimeError("manifest entrypoint.artifactPath is required")
        if manifest.source is None:
            raise RuntimeError("manifest source is required")
        return BuildArgs(
            root=root,
            target=read_pyproject_provider(root),
            output_path=pathlib.Path(manifest.entrypoint_artifact_path),
            app_name=manifest.source.rsplit("/", 1)[-1],
            runtime_kind=_runtime_kind_for_manifest_kind(manifest.kind),
            goos=_target_goos(),
            goarch=_target_goarch(),
        )
    except (OSError, RuntimeError, ValueError) as err:
        print(str(err) if str(err) else repr(err), file=sys.stderr)
        return None


def _runtime_kind_for_manifest_kind(kind: str | None) -> str:
    if kind is None:
        return _DEFAULT_RUNTIME_KIND
    normalized = kind.strip().lower()
    return _RUNTIME_KIND_BY_MANIFEST_KIND.get(normalized, _DEFAULT_RUNTIME_KIND)


def _target_goos() -> str:
    raw = os.environ.get(TARGET_OS_ENV, "").strip()
    if raw:
        return raw
    return platform.system().lower()


def _target_goarch() -> str:
    raw = os.environ.get(TARGET_ARCH_ENV, "").strip()
    if raw:
        return raw
    machine = platform.machine().lower()
    return _ARCH_ALIASES.get(machine, machine)


def _parse_build_args(args: list[str]) -> BuildArgs | None:
    if len(args) != 7:
        return None

    root, target, output_path, app_name, runtime_kind, goos, goarch = args
    return BuildArgs(
        root=pathlib.Path(root),
        target=target,
        output_path=pathlib.Path(output_path),
        app_name=app_name,
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
            app_name=args.app_name,
            runtime_kind=args.runtime_kind,
        )

        cache_dir = _build_cache_dir(
            args=args,
            root_path=root_path,
            module_name=plugin_target.module_name,
        )
        clean_cache = cache_dir is None
        pyinstaller_config_dir = (
            work_path / "pyinstaller-config"
            if cache_dir is None
            else cache_dir / "pyinstaller-config"
        )
        if cache_dir is not None:
            pyinstaller_config_dir.mkdir(parents=True, exist_ok=True)

        env = os.environ.copy()
        env["PYINSTALLER_CONFIG_DIR"] = str(pyinstaller_config_dir)
        env["SOURCE_DATE_EPOCH"] = "0"

        with _build_cache_lock(None if cache_dir is None else cache_dir / "build.lock"):
            subprocess.run(
                _pyinstaller_command(
                    root_path=root_path,
                    output_path=output_path,
                    module_name=plugin_target.module_name,
                    bundle_config_path=bundle_config_path,
                    target_goos=args.goos,
                    target_goarch=args.goarch,
                    clean_cache=clean_cache,
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
    clean_cache: bool,
) -> list[str]:
    pyinstaller_name = (
        output_path.name.removesuffix(".exe")
        if target_goos == "windows"
        else output_path.name
    )

    command = [
        sys.executable,
        "-m",
        "PyInstaller",
        "--noconfirm",
        "--noupx",
        "--onefile",
        "--distpath",
        str(output_path.parent),
        "--workpath",
        str(bundle_config_path.parent / "build"),
        "--specpath",
        str(bundle_config_path.parent / "spec"),
        "--name",
        pyinstaller_name,
        # Bundle the entire SDK package so lazy `gestalt.__getattr__` exports
        # (for example `gestalt._indexeddb`) remain available in frozen builds.
        "--collect-submodules",
        "gestalt",
        "--hidden-import",
        module_name,
        "--paths",
        str(root_path),
        "--add-data",
        f"{bundle_config_path}{os.pathsep}.",
        str(pathlib.Path(__file__).with_name("_pyinstaller.py")),
    ]
    if clean_cache:
        command.insert(command.index("--onefile"), "--clean")
    if sys.platform == "darwin" and target_goos == "darwin":
        target_arch = _darwin_target_arch(target_goarch)
        if target_arch:
            command.extend(["--target-arch", target_arch])
    return command


def _build_cache_dir(
    *,
    args: BuildArgs,
    root_path: pathlib.Path,
    module_name: str,
) -> pathlib.Path | None:
    cache_root = _build_cache_root()
    if cache_root is None:
        return None
    namespace = _build_cache_namespace(
        args=args,
        root_path=root_path,
        module_name=module_name,
    )
    return cache_root / "python-provider-packages" / namespace


def _build_cache_root() -> pathlib.Path | None:
    raw = os.environ.get(BUILD_CACHE_DIR_ENV_VAR, "").strip()
    if raw == "":
        return None

    path = pathlib.Path(raw).expanduser()
    try:
        path.mkdir(parents=True, exist_ok=True)
    except OSError as err:
        raise RuntimeError(
            f"{BUILD_CACHE_DIR_ENV_VAR} {path}: create cache directory: {err}"
        ) from err
    if not path.is_dir():
        raise RuntimeError(f"{BUILD_CACHE_DIR_ENV_VAR} {path} is not a directory")
    return path.resolve()


def _build_cache_namespace(
    *,
    args: BuildArgs,
    root_path: pathlib.Path,
    module_name: str,
) -> str:
    payload = {
        "schema": BUILD_CACHE_SCHEMA_VERSION,
        "root": str(root_path),
        "target": args.target,
        "module": module_name,
        "app_name": args.app_name,
        "runtime_kind": args.runtime_kind,
        "goos": args.goos,
        "goarch": args.goarch,
        "python": {
            "implementation": platform.python_implementation(),
            "cache_tag": sys.implementation.cache_tag,
            "version": list(sys.version_info[:3]),
            "platform": sys.platform,
            "machine": platform.machine(),
            "sysconfig_platform": _sysconfig_platform(),
            "soabi": _sysconfig_config_var("SOABI"),
            "executable": str(pathlib.Path(sys.executable).resolve()),
            "prefix": sys.prefix,
            "base_prefix": sys.base_prefix,
        },
        "packager": {
            "pyinstaller": _package_version("pyinstaller"),
            "pyinstaller_hooks_contrib": _package_version(
                "pyinstaller-hooks-contrib"
            ),
        },
    }
    encoded = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode(
        "utf-8"
    )
    return "v1-" + hashlib.sha256(encoded).hexdigest()[:48]


def _package_version(name: str) -> str:
    try:
        return importlib.metadata.version(name)
    except importlib.metadata.PackageNotFoundError:
        return ""


def _sysconfig_platform() -> str:
    try:
        return sysconfig.get_platform()
    except Exception:
        return ""


def _sysconfig_config_var(name: str) -> str:
    try:
        value = sysconfig.get_config_var(name)
    except Exception:
        return ""
    return "" if value is None else str(value)


@contextlib.contextmanager
def _build_cache_lock(lock_path: pathlib.Path | None):
    if lock_path is None:
        yield
        return

    lock_path.parent.mkdir(parents=True, exist_ok=True)
    with lock_path.open("a+b") as lock_file:
        if os.name == "nt":
            _lock_file_windows(lock_file)
            try:
                yield
            finally:
                _unlock_file_windows(lock_file)
        else:
            import fcntl

            fcntl.flock(lock_file.fileno(), fcntl.LOCK_EX)
            try:
                yield
            finally:
                fcntl.flock(lock_file.fileno(), fcntl.LOCK_UN)


def _lock_file_windows(lock_file) -> None:
    import importlib

    msvcrt = importlib.import_module("msvcrt")
    locking = getattr(msvcrt, "locking")
    nonblocking_lock = getattr(msvcrt, "LK_NBLCK")

    _ensure_lock_byte(lock_file)
    while True:
        lock_file.seek(0)
        try:
            locking(lock_file.fileno(), nonblocking_lock, 1)
            return
        except OSError:
            time.sleep(0.1)


def _unlock_file_windows(lock_file) -> None:
    import importlib

    msvcrt = importlib.import_module("msvcrt")
    locking = getattr(msvcrt, "locking")
    unlock = getattr(msvcrt, "LK_UNLCK")

    lock_file.seek(0)
    locking(lock_file.fileno(), unlock, 1)


def _ensure_lock_byte(lock_file) -> None:
    lock_file.seek(0, os.SEEK_END)
    if lock_file.tell() == 0:
        lock_file.write(b"\0")
        lock_file.flush()


def _darwin_target_arch(goarch: str) -> str | None:
    return {
        "amd64": "x86_64",
        "arm64": "arm64",
    }.get(goarch)


if __name__ == "__main__":
    raise SystemExit(main())
