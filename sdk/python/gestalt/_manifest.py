"""Manifest metadata helpers for hosted webhooks and security schemes."""
from __future__ import annotations

import dataclasses
import pathlib
from collections.abc import Mapping
from typing import Any

import yaml

SecurityRequirement = dict[str, list[str]]


@dataclasses.dataclass(slots=True)
class WebhookSecretRef:
    env: str = ""
    secret: str = ""


@dataclasses.dataclass(slots=True)
class WebhookSignatureConfig:
    algorithm: str = ""
    signatureHeader: str = ""
    timestampHeader: str = ""
    deliveryIdHeader: str = ""
    payloadTemplate: str = ""
    digestPrefix: str = ""


@dataclasses.dataclass(slots=True)
class WebhookReplayConfig:
    maxAge: str = ""


@dataclasses.dataclass(slots=True)
class WebhookMTLSConfig:
    subjectAltName: str = ""


@dataclasses.dataclass(slots=True)
class WebhookSecurityScheme:
    type: str = ""
    description: str = ""
    name: str = ""
    in_: str = ""
    scheme: str = ""
    secret: WebhookSecretRef | Mapping[str, Any] | None = None
    signature: WebhookSignatureConfig | Mapping[str, Any] | None = None
    replay: WebhookReplayConfig | Mapping[str, Any] | None = None
    mtls: WebhookMTLSConfig | Mapping[str, Any] | None = None


@dataclasses.dataclass(slots=True)
class WebhookMediaType:
    schema: Any = None


@dataclasses.dataclass(slots=True)
class WebhookRequestBody:
    required: bool = False
    content: dict[str, WebhookMediaType | Mapping[str, Any]] = dataclasses.field(
        default_factory=dict
    )


@dataclasses.dataclass(slots=True)
class WebhookResponse:
    description: str = ""
    headers: dict[str, str] = dataclasses.field(default_factory=dict)
    content: dict[str, WebhookMediaType | Mapping[str, Any]] = dataclasses.field(
        default_factory=dict
    )
    body: Any = None


@dataclasses.dataclass(slots=True)
class WebhookOperation:
    operationId: str = ""
    summary: str = ""
    description: str = ""
    requestBody: WebhookRequestBody | Mapping[str, Any] | None = None
    responses: dict[str, WebhookResponse | Mapping[str, Any]] = dataclasses.field(
        default_factory=dict
    )
    security: list[SecurityRequirement] = dataclasses.field(default_factory=list)


@dataclasses.dataclass(slots=True)
class WebhookWorkflowTarget:
    provider: str = ""
    plugin: str = ""
    operation: str = ""
    connection: str = ""
    instance: str = ""
    input: dict[str, Any] = dataclasses.field(default_factory=dict)


@dataclasses.dataclass(slots=True)
class WebhookTarget:
    operation: str = ""
    workflow: WebhookWorkflowTarget | Mapping[str, Any] | None = None


@dataclasses.dataclass(slots=True)
class WebhookExecution:
    mode: str = ""
    acceptedResponse: str = ""


@dataclasses.dataclass(slots=True)
class Webhook:
    summary: str = ""
    description: str = ""
    path: str = ""
    get: WebhookOperation | Mapping[str, Any] | None = None
    post: WebhookOperation | Mapping[str, Any] | None = None
    put: WebhookOperation | Mapping[str, Any] | None = None
    delete: WebhookOperation | Mapping[str, Any] | None = None
    target: WebhookTarget | Mapping[str, Any] | None = None
    execution: WebhookExecution | Mapping[str, Any] | None = None


def manifest_metadata_dict(
    *,
    security_schemes: Mapping[str, Any] | None = None,
    webhooks: Mapping[str, Any] | None = None,
) -> dict[str, Any]:
    """Build a manifest metadata fragment for hosted webhooks."""

    document: dict[str, Any] = {}
    normalized_schemes = _normalized_mapping(security_schemes)
    if normalized_schemes:
        document["securitySchemes"] = normalized_schemes

    normalized_webhooks = _normalized_mapping(webhooks)
    if normalized_webhooks:
        document["webhooks"] = normalized_webhooks

    return document


def write_manifest_metadata(
    path: str | pathlib.Path,
    *,
    security_schemes: Mapping[str, Any] | None = None,
    webhooks: Mapping[str, Any] | None = None,
) -> None:
    """Write a manifest metadata fragment to YAML on disk."""

    metadata_path = pathlib.Path(path)
    metadata_path.parent.mkdir(parents=True, exist_ok=True)
    document = manifest_metadata_dict(
        security_schemes=security_schemes,
        webhooks=webhooks,
    )
    metadata_path.write_text(
        yaml.dump(document, default_flow_style=False, sort_keys=False),
        encoding="utf-8",
    )


def _normalized_mapping(values: Mapping[str, Any] | None) -> dict[str, Any]:
    if not values:
        return {}
    normalized: dict[str, Any] = {}
    for key, value in values.items():
        rendered = _manifest_value(value)
        if _is_empty(rendered):
            continue
        normalized[str(key)] = rendered
    return normalized


def _manifest_value(value: Any) -> Any:
    if dataclasses.is_dataclass(value):
        rendered: dict[str, Any] = {}
        for field_definition in dataclasses.fields(value):
            field_name = (
                "in" if field_definition.name == "in_" else field_definition.name
            )
            normalized = _manifest_value(getattr(value, field_definition.name))
            if _is_empty(normalized):
                continue
            rendered[field_name] = normalized
        return rendered
    if isinstance(value, pathlib.Path):
        return str(value)
    if isinstance(value, Mapping):
        rendered: dict[str, Any] = {}
        for key, item in value.items():
            normalized = _manifest_value(item)
            if _is_empty(normalized):
                continue
            rendered[str(key)] = normalized
        return rendered
    if isinstance(value, (list, tuple, set)):
        rendered_items = [
            normalized
            for normalized in (_manifest_value(item) for item in value)
            if not _is_empty(normalized)
        ]
        return rendered_items
    return value


def _is_empty(value: Any) -> bool:
    if value is None:
        return True
    if value == "":
        return True
    if isinstance(value, (dict, list)) and not value:
        return True
    return False
