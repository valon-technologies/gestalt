import importlib
import json
import pathlib
from dataclasses import dataclass
from typing import Any, Final

import yaml

BUNDLED_CONFIG_NAME: Final[str] = "gestalt-runtime.json"

_MANIFEST_BASENAMES: Final[tuple[str, ...]] = (
    "manifest.yaml",
    "manifest.yml",
    "manifest.json",
)


@dataclass(frozen=True)
class SourceManifest:
    kind: str | None
    source: str | None
    entrypoint_artifact_path: str | None


def read_source_manifest(root: pathlib.Path) -> SourceManifest:
    """Read the build-relevant fields from a provider source manifest.

    Dispatches on the manifest file extension. Raises ``FileNotFoundError`` if
    no manifest is present in ``root``.
    """
    for name in _MANIFEST_BASENAMES:
        path = root / name
        if not path.exists():
            continue
        return _parse_source_manifest(path)
    raise FileNotFoundError(
        f"no manifest file found in {root} (tried {', '.join(_MANIFEST_BASENAMES)})"
    )


def _parse_source_manifest(path: pathlib.Path) -> SourceManifest:
    text = path.read_text(encoding="utf-8")
    data: Any
    if path.suffix.lower() == ".json":
        try:
            data = json.loads(text)
        except json.JSONDecodeError as err:
            raise RuntimeError(f"{path.name}: {err}") from err
    else:
        try:
            data = yaml.safe_load(text)
        except yaml.YAMLError as err:
            raise RuntimeError(f"{path.name}: {err}") from err
    if not isinstance(data, dict):
        raise RuntimeError(f"{path.name} must be a mapping")

    kind = data.get("kind")
    kind = kind.strip() if isinstance(kind, str) else None

    source = data.get("source")
    source = source.strip() if isinstance(source, str) else None

    entrypoint = data.get("entrypoint")
    artifact_path = None
    if isinstance(entrypoint, dict):
        raw_path = entrypoint.get("artifactPath")
        if isinstance(raw_path, str):
            artifact_path = raw_path.strip() or None

    return SourceManifest(
        kind=kind,
        source=source,
        entrypoint_artifact_path=artifact_path,
    )


def read_pyproject_provider(root: pathlib.Path) -> str:
    """Read ``[tool.gestalt].provider`` from a project's pyproject.toml.

    Returns the provider target string (``module`` or ``module:attribute``).
    Raises ``RuntimeError`` if the value is missing or not a string.
    """
    tomllib = _load_tomllib()

    pyproject_path = root / "pyproject.toml"
    with pyproject_path.open("rb") as handle:
        data = tomllib.load(handle)

    provider = data.get("tool", {}).get("gestalt", {}).get("provider")
    if not isinstance(provider, str) or not provider.strip():
        raise RuntimeError("pyproject.toml [tool.gestalt].provider is required")
    return provider.strip()


def _load_tomllib() -> Any:
    # tomllib is stdlib on 3.11+; tomli is the 3.10 backport (declared dep).
    # Resolved via importlib so static analysis does not pin to one version.
    try:
        return importlib.import_module("tomllib")
    except ModuleNotFoundError:
        return importlib.import_module("tomli")


@dataclass(frozen=True)
class PluginTarget:
    module_name: str
    attribute_name: str | None = None


@dataclass(frozen=True)
class BundledPluginConfig:
    target: str
    app_name: str | None = None
    runtime_kind: str | None = None


def parse_plugin_target(target: str) -> PluginTarget:
    module_name, sep, attribute_name = target.partition(":")
    module_name = module_name.strip()
    attribute_name = attribute_name.strip() or None
    if not module_name:
        raise RuntimeError("tool.gestalt.provider must be in module or module:attribute form")
    if sep and attribute_name is None:
        raise RuntimeError("tool.gestalt.provider attribute is required when ':' is present")

    return PluginTarget(
        module_name=module_name,
        attribute_name=attribute_name,
    )


def read_bundled_plugin_config(*, bundle_root: pathlib.Path) -> BundledPluginConfig | None:
    config_path = bundle_root / BUNDLED_CONFIG_NAME
    if not config_path.exists():
        return None

    data = json.loads(config_path.read_text(encoding="utf-8"))
    target = str(data.get("target", "")).strip()
    if not target:
        raise RuntimeError(f"{config_path} is missing target")

    app_name = data.get("app_name")
    if app_name is not None:
        app_name = str(app_name).strip() or None

    runtime_kind = data.get("runtime_kind")
    if runtime_kind is not None:
        runtime_kind = str(runtime_kind).strip() or None

    return BundledPluginConfig(
        target=target,
        app_name=app_name,
        runtime_kind=runtime_kind,
    )


def write_bundled_plugin_config(
    path: pathlib.Path,
    *,
    target: str,
    app_name: str,
    runtime_kind: str,
) -> None:
    path.write_text(
        json.dumps(
            {
                "target": target,
                "app_name": app_name,
                "runtime_kind": runtime_kind,
            }
        ),
        encoding="utf-8",
    )

