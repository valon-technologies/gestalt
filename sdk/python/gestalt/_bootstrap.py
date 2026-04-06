import json
import pathlib
from dataclasses import dataclass

BUNDLED_CONFIG_NAME = "gestalt-runtime.json"


@dataclass(frozen=True)
class PluginTarget:
    module_name: str
    attribute_name: str


@dataclass(frozen=True)
class BundledPluginConfig:
    target: str
    plugin_name: str | None = None


def parse_plugin_target(target: str) -> PluginTarget:
    module_name, _, attribute_name = target.partition(":")
    if not module_name or not attribute_name:
        raise RuntimeError("tool.gestalt.plugin must be in module:attribute form")

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

    plugin_name = data.get("plugin_name")
    if plugin_name is not None:
        plugin_name = str(plugin_name).strip() or None

    return BundledPluginConfig(
        target=target,
        plugin_name=plugin_name,
    )


def write_bundled_plugin_config(
    path: pathlib.Path,
    *,
    target: str,
    plugin_name: str,
) -> None:
    path.write_text(
        json.dumps(
            {
                "target": target,
                "plugin_name": plugin_name,
            }
        ),
        encoding="utf-8",
    )
