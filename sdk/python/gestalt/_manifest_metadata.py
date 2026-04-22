"""Generated manifest metadata helpers for integration plugins."""

import copy
import pathlib
from collections.abc import Mapping
from typing import Any

import yaml
from typing_extensions import NotRequired, TypedDict


class HTTPSecretRef(TypedDict, total=False):
    env: str
    secret: str


HTTPSecurityScheme = TypedDict(
    "HTTPSecurityScheme",
    {
        "type": str,
        "description": str,
        "name": str,
        "in": str,
        "scheme": str,
        "secret": HTTPSecretRef,
    },
    total=False,
)


class HTTPMediaType(TypedDict, total=False):
    pass


class HTTPRequestBody(TypedDict, total=False):
    required: bool
    content: dict[str, HTTPMediaType]


class HTTPAck(TypedDict, total=False):
    status: int
    headers: dict[str, str]
    body: Any


class HTTPBinding(TypedDict):
    path: str
    method: str
    security: str
    target: str
    requestBody: NotRequired[HTTPRequestBody]
    ack: NotRequired[HTTPAck]


class PluginManifestMetadata(TypedDict, total=False):
    securitySchemes: dict[str, HTTPSecurityScheme]
    http: dict[str, HTTPBinding]


def has_plugin_manifest_metadata(
    metadata: PluginManifestMetadata | Mapping[str, Any] | None,
) -> bool:
    """Report whether manifest metadata contains HTTP bindings or security schemes."""

    return bool(
        metadata
        and (
            bool(metadata.get("securitySchemes"))
            or bool(metadata.get("http"))
        )
    )


def manifest_metadata_to_dict(
    metadata: PluginManifestMetadata | Mapping[str, Any],
) -> dict[str, Any]:
    """Normalize manifest metadata to plain Python data."""

    if "securitySchemes" not in metadata and "http" not in metadata:
        return copy.deepcopy({key: value for key, value in metadata.items()})

    output: dict[str, Any] = {}
    security_schemes = metadata.get("securitySchemes")
    if security_schemes:
        output["securitySchemes"] = copy.deepcopy(dict(security_schemes))

    http_bindings = metadata.get("http")
    if http_bindings:
        output["http"] = copy.deepcopy(dict(http_bindings))

    return output


def write_manifest_metadata(
    path: str | pathlib.Path,
    *,
    metadata: PluginManifestMetadata | Mapping[str, Any],
) -> None:
    """Write manifest metadata to YAML on disk."""

    manifest_path = pathlib.Path(path)
    manifest_path.parent.mkdir(parents=True, exist_ok=True)
    data = yaml.dump(
        manifest_metadata_to_dict(metadata),
        default_flow_style=False,
        sort_keys=False,
    )
    manifest_path.write_text(data, encoding="utf-8")
