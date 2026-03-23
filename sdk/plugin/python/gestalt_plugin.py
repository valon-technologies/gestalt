from __future__ import annotations

import dataclasses
import json
import sys
from typing import Any, Callable, Dict, IO, List, Optional

PROTOCOL_VERSION = "gestalt-plugin/1"
JSONRPC_VERSION = "2.0"

ERR_PARSE = -32700
ERR_INVALID_REQUEST = -32600
ERR_METHOD_NOT_FOUND = -32601
ERR_INVALID_PARAMS = -32602
ERR_INTERNAL = -32603

JsonObject = Dict[str, Any]


@dataclasses.dataclass
class HostInfo:
    name: str
    version: str


@dataclasses.dataclass
class IntegrationInfo:
    name: str
    config: JsonObject


@dataclasses.dataclass
class ParameterDef:
    name: str
    type: str
    description: str = ""
    required: bool = False
    default: Any = None


@dataclasses.dataclass
class OperationDef:
    name: str
    description: str = ""
    method: str = ""
    parameters: List[ParameterDef] = dataclasses.field(default_factory=list)


@dataclasses.dataclass
class CatalogOperationDef:
    id: str
    title: str = ""
    description: str = ""
    input_schema: Any = None
    parameters: List[ParameterDef] = dataclasses.field(default_factory=list)
    required_scopes: List[str] = dataclasses.field(default_factory=list)
    tags: List[str] = dataclasses.field(default_factory=list)
    read_only: bool = False
    visible: Optional[bool] = None
    transport: str = ""
    query: str = ""


@dataclasses.dataclass
class CatalogDef:
    name: str
    display_name: str = ""
    description: str = ""
    icon_svg: str = ""
    base_url: str = ""
    auth_style: str = ""
    headers: Dict[str, str] = dataclasses.field(default_factory=dict)
    operations: List[CatalogOperationDef] = dataclasses.field(default_factory=list)


@dataclasses.dataclass
class ProviderManifest:
    display_name: str
    description: str
    connection_mode: str
    operations: List[OperationDef]
    catalog: Any = None
    auth: Optional[JsonObject] = None


@dataclasses.dataclass
class PluginInfo:
    name: str
    version: str


@dataclasses.dataclass
class Capabilities:
    catalog: bool = False
    oauth: bool = False
    manual_auth: bool = False
    cancellation: bool = False


@dataclasses.dataclass
class InitializeRequest:
    protocol_version: str
    host_info: HostInfo
    integration: IntegrationInfo


@dataclasses.dataclass
class InitializeResult:
    protocol_version: str
    plugin_info: PluginInfo
    provider: ProviderManifest
    capabilities: Optional[Capabilities] = None


@dataclasses.dataclass
class ExecuteRequest:
    operation: str
    params: JsonObject
    token: str
    meta: Optional[JsonObject] = None


@dataclasses.dataclass
class ExecuteResult:
    status: int
    body: str


@dataclasses.dataclass
class AuthStartRequest:
    state: str
    scopes: List[str]


@dataclasses.dataclass
class AuthStartResult:
    auth_url: str
    verifier: Optional[str] = None


@dataclasses.dataclass
class AuthExchangeRequest:
    code: str
    verifier: Optional[str] = None


@dataclasses.dataclass
class TokenResult:
    access_token: str
    refresh_token: Optional[str] = None
    expires_in: Optional[int] = None
    token_type: Optional[str] = None


@dataclasses.dataclass
class AuthRefreshRequest:
    refresh_token: str


def _to_camel(name: str) -> str:
    parts = name.split("_")
    return parts[0] + "".join(p.capitalize() for p in parts[1:])


def _asdict(value: Any) -> Any:
    if dataclasses.is_dataclass(value):
        return {
            _to_camel(f.name): _asdict(getattr(value, f.name))
            for f in dataclasses.fields(value)
        }
    if isinstance(value, list):
        return [_asdict(item) for item in value]
    if isinstance(value, dict):
        return {key: _asdict(item) for key, item in value.items()}
    return value


def _read_frame(stream: IO[bytes]) -> Optional[bytes]:
    headers: Dict[str, str] = {}
    while True:
        line = stream.readline()
        if line == b"":
            return None
        stripped = line.strip()
        if stripped == b"":
            break
        key, _, value = line.decode("utf-8").partition(":")
        headers[key.strip().lower()] = value.strip()
    if "content-length" not in headers:
        raise ValueError("missing Content-Length header")
    length = int(headers["content-length"])
    body = stream.read(length)
    if body is None or len(body) < length:
        return None
    return body


def _write_frame(stream: IO[bytes], payload: Any) -> None:
    body = json.dumps(payload, separators=(",", ":"), ensure_ascii=True).encode("utf-8")
    stream.write(f"Content-Length: {len(body)}\r\n\r\n".encode("utf-8"))
    stream.write(body)
    stream.flush()


def _response_ok(id_: Any, result: Any) -> Dict[str, Any]:
    return {"jsonrpc": JSONRPC_VERSION, "id": id_, "result": result}


def _response_err(id_: Any, code: int, message: str, data: Any = None) -> Dict[str, Any]:
    error = {"code": code, "message": message}
    if data is not None:
        error["data"] = data
    return {"jsonrpc": JSONRPC_VERSION, "id": id_, "error": error}


class Plugin:
    def __init__(
        self,
        plugin_info: PluginInfo,
        provider: ProviderManifest,
        *,
        capabilities: Optional[Capabilities] = None,
        initialize: Optional[Callable[[InitializeRequest], InitializeResult]] = None,
        execute: Optional[Callable[[ExecuteRequest], ExecuteResult]] = None,
        auth_start: Optional[Callable[[AuthStartRequest], AuthStartResult]] = None,
        auth_exchange_code: Optional[Callable[[AuthExchangeRequest], TokenResult]] = None,
        auth_refresh_token: Optional[Callable[[AuthRefreshRequest], TokenResult]] = None,
        on_shutdown: Optional[Callable[[], None]] = None,
        stdin: IO[bytes] = sys.stdin.buffer,
        stdout: IO[bytes] = sys.stdout.buffer,
        stderr: IO[bytes] = sys.stderr.buffer,
    ) -> None:
        self.plugin_info = plugin_info
        self.provider = provider
        self.capabilities = capabilities
        self.initialize_handler = initialize
        self.execute_handler = execute
        self.auth_start_handler = auth_start
        self.auth_exchange_code_handler = auth_exchange_code
        self.auth_refresh_token_handler = auth_refresh_token
        self.on_shutdown = on_shutdown
        self.stdin = stdin
        self.stdout = stdout
        self.stderr = stderr

    def execute(self, fn: Callable[[ExecuteRequest], ExecuteResult]) -> Callable[[ExecuteRequest], ExecuteResult]:
        self.execute_handler = fn
        return fn

    def auth_start(self, fn: Callable[[AuthStartRequest], AuthStartResult]) -> Callable[[AuthStartRequest], AuthStartResult]:
        self.auth_start_handler = fn
        return fn

    def auth_exchange_code(self, fn: Callable[[AuthExchangeRequest], TokenResult]) -> Callable[[AuthExchangeRequest], TokenResult]:
        self.auth_exchange_code_handler = fn
        return fn

    def auth_refresh_token(self, fn: Callable[[AuthRefreshRequest], TokenResult]) -> Callable[[AuthRefreshRequest], TokenResult]:
        self.auth_refresh_token_handler = fn
        return fn

    def serve(self) -> None:
        if self.execute_handler is None:
            raise ValueError("execute handler is required")
        while True:
            raw = _read_frame(self.stdin)
            if raw is None:
                return
            try:
                message = json.loads(raw.decode("utf-8"))
            except json.JSONDecodeError as exc:
                _write_frame(self.stdout, _response_err(None, ERR_PARSE, "invalid JSON payload", str(exc)))
                continue

            if not isinstance(message, dict) or "method" not in message:
                _write_frame(self.stdout, _response_err(message.get("id") if isinstance(message, dict) else None, ERR_INVALID_REQUEST, "invalid JSON-RPC request"))
                continue

            if "id" not in message:
                if self._handle_notification(message):
                    return
                continue

            if self._handle_request(message):
                return

    def _handle_notification(self, message: Dict[str, Any]) -> bool:
        if message.get("method") == "exit":
            self._shutdown()
            return True
        return False

    def _handle_request(self, message: Dict[str, Any]) -> bool:
        id_ = message.get("id")
        method = message.get("method")
        try:
            if method == "initialize":
                result = self._handle_initialize(message.get("params"))
                _write_frame(self.stdout, _response_ok(id_, _asdict(result)))
                return False
            if method == "provider.execute":
                result = self._handle_execute(message.get("params"))
                _write_frame(self.stdout, _response_ok(id_, _asdict(result)))
                return False
            if method == "auth.start":
                result = self._handle_auth_start(message.get("params"))
                _write_frame(self.stdout, _response_ok(id_, _asdict(result)))
                return False
            if method == "auth.exchange_code":
                result = self._handle_auth_exchange_code(message.get("params"))
                _write_frame(self.stdout, _response_ok(id_, _asdict(result)))
                return False
            if method == "auth.refresh_token":
                result = self._handle_auth_refresh_token(message.get("params"))
                _write_frame(self.stdout, _response_ok(id_, _asdict(result)))
                return False
            if method == "shutdown":
                _write_frame(self.stdout, _response_ok(id_, None))
                self._shutdown()
                return True
            _write_frame(self.stdout, _response_err(id_, ERR_METHOD_NOT_FOUND, f"method not found: {method}"))
            return False
        except Exception as exc:
            _write_frame(self.stdout, _response_err(id_, ERR_INTERNAL, str(exc)))
        return False

    def _require_object(self, params: Any, method: str) -> Dict[str, Any]:
        if not isinstance(params, dict):
            raise ValueError(f"{method} requires a JSON object")
        return params

    def _handle_initialize(self, params: Any) -> InitializeResult:
        payload = self._require_object(params, "initialize")
        host_info = payload.get("hostInfo", {})
        integration = payload.get("integration", {})
        request = InitializeRequest(
            protocol_version=payload.get("protocolVersion", ""),
            host_info=HostInfo(
                name=host_info.get("name", ""),
                version=host_info.get("version", ""),
            ),
            integration=IntegrationInfo(
                name=integration.get("name", ""),
                config=integration.get("config", {}),
            ),
        )
        if self.initialize_handler is not None:
            return self.initialize_handler(request)
        return InitializeResult(
            protocol_version=PROTOCOL_VERSION,
            plugin_info=self.plugin_info,
            provider=self.provider,
            capabilities=self.capabilities,
        )

    def _handle_execute(self, params: Any) -> ExecuteResult:
        request = self._require_object(params, "provider.execute")
        assert self.execute_handler is not None
        return self.execute_handler(ExecuteRequest(
            operation=request.get("operation", ""),
            params=request.get("params", {}) if isinstance(request.get("params", {}), dict) else {},
            token=request.get("token", ""),
            meta=request.get("meta") if isinstance(request.get("meta"), dict) else None,
        ))

    def _handle_auth_start(self, params: Any) -> AuthStartResult:
        if self.auth_start_handler is None:
            raise ValueError("auth.start is not implemented")
        request = self._require_object(params, "auth.start")
        return self.auth_start_handler(AuthStartRequest(
            state=request.get("state", ""),
            scopes=list(request.get("scopes", [])),
        ))

    def _handle_auth_exchange_code(self, params: Any) -> TokenResult:
        if self.auth_exchange_code_handler is None:
            raise ValueError("auth.exchange_code is not implemented")
        request = self._require_object(params, "auth.exchange_code")
        return self.auth_exchange_code_handler(AuthExchangeRequest(
            code=request.get("code", ""),
            verifier=request.get("verifier"),
        ))

    def _handle_auth_refresh_token(self, params: Any) -> TokenResult:
        if self.auth_refresh_token_handler is None:
            raise ValueError("auth.refresh_token is not implemented")
        request = self._require_object(params, "auth.refresh_token")
        return self.auth_refresh_token_handler(AuthRefreshRequest(
            refresh_token=request.get("refreshToken", ""),
        ))

    def _shutdown(self) -> None:
        if self.on_shutdown is not None:
            self.on_shutdown()


def create_plugin(*, plugin_info: PluginInfo, provider: ProviderManifest, capabilities: Optional[Capabilities] = None) -> Plugin:
    return Plugin(plugin_info=plugin_info, provider=provider, capabilities=capabilities)
