from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any


@dataclass
class ParameterDef:
    name: str
    type: str = "string"
    description: str = ""
    required: bool = False
    default: Any = None


@dataclass
class OperationDef:
    name: str
    description: str = ""
    method: str = ""
    parameters: list[ParameterDef] = field(default_factory=list)


@dataclass
class ExecuteRequest:
    operation: str
    params: dict[str, Any]
    token: str = ""
    connection_params: dict[str, str] = field(default_factory=dict)


@dataclass
class OperationResult:
    status: int
    body: str


@dataclass
class TokenResponse:
    access_token: str
    refresh_token: str = ""
    expires_in: int = 0
    token_type: str = ""
    extra: dict[str, Any] = field(default_factory=dict)


@dataclass
class Capability:
    provider: str
    operation: str
    description: str = ""
    parameters: list[ParameterDef] = field(default_factory=list)
