from __future__ import annotations

import os
import signal
import sys
from concurrent import futures
from typing import Any, Callable, List, Optional

import grpc
from google.protobuf import empty_pb2, struct_pb2
from google.protobuf.json_format import MessageToDict, ParseDict

from gestalt_plugin._proto import pb2, pb2_grpc
from gestalt_plugin.types import (
    ExecuteRequest,
    OperationDef,
    OperationResult,
    TokenResponse,
)

ENV_PLUGIN_SOCKET = "GESTALT_PLUGIN_SOCKET"


class _ProviderServicer(pb2_grpc.ProviderPluginServicer):
    def __init__(
        self,
        name: str,
        display_name: str,
        operations: List[OperationDef],
        execute: Callable[[ExecuteRequest], OperationResult],
        description: str = "",
        auth_types: Optional[List[str]] = None,
        authorization_url: Optional[Callable[[str, List[str]], str]] = None,
        exchange_code: Optional[Callable[[str], TokenResponse]] = None,
        refresh_token: Optional[Callable[[str], TokenResponse]] = None,
    ):
        self._name = name
        self._display_name = display_name
        self._description = description
        self._operations = operations
        self._execute = execute
        self._auth_types = auth_types or []
        self._authorization_url = authorization_url
        self._exchange_code = exchange_code
        self._refresh_token = refresh_token

    def GetMetadata(self, request, context):
        return pb2.ProviderMetadata(
            name=self._name,
            display_name=self._display_name,
            description=self._description,
            auth_types=self._auth_types,
        )

    def ListOperations(self, request, context):
        proto_ops = []
        for op in self._operations:
            params = []
            for p in op.parameters:
                param = pb2.Parameter(
                    name=p.name,
                    type=p.type,
                    description=p.description,
                    required=p.required,
                )
                if p.default is not None:
                    param.default_value.CopyFrom(
                        ParseDict(p.default, struct_pb2.Value())
                    )
                params.append(param)
            proto_ops.append(
                pb2.Operation(
                    name=op.name,
                    description=op.description,
                    method=op.method,
                    parameters=params,
                )
            )
        return pb2.ListOperationsResponse(operations=proto_ops)

    def Execute(self, request, context):
        params: dict[str, Any] = {}
        if request.HasField("params"):
            params = MessageToDict(request.params, preserving_proto_field_name=True)

        exec_req = ExecuteRequest(
            operation=request.operation,
            params=params,
            token=request.token,
            connection_params=dict(request.connection_params),
        )
        result = self._execute(exec_req)
        return pb2.OperationResult(status=result.status, body=result.body)

    def AuthorizationURL(self, request, context):
        if self._authorization_url is None:
            context.set_code(grpc.StatusCode.UNIMPLEMENTED)
            context.set_details("provider does not support OAuth")
            return pb2.AuthorizationURLResponse()
        url = self._authorization_url(request.state, list(request.scopes))
        return pb2.AuthorizationURLResponse(url=url)

    def ExchangeCode(self, request, context):
        if self._exchange_code is None:
            context.set_code(grpc.StatusCode.UNIMPLEMENTED)
            context.set_details("provider does not support OAuth")
            return pb2.TokenResponse()
        resp = self._exchange_code(request.code)
        return _token_response_to_proto(resp)

    def RefreshToken(self, request, context):
        if self._refresh_token is None:
            context.set_code(grpc.StatusCode.UNIMPLEMENTED)
            context.set_details("provider does not support OAuth")
            return pb2.TokenResponse()
        resp = self._refresh_token(request.refresh_token)
        return _token_response_to_proto(resp)


def _token_response_to_proto(resp: TokenResponse) -> pb2.TokenResponse:
    msg = pb2.TokenResponse(
        access_token=resp.access_token,
        refresh_token=resp.refresh_token,
        expires_in=resp.expires_in,
        token_type=resp.token_type,
    )
    if resp.extra:
        msg.extra.CopyFrom(ParseDict(resp.extra, struct_pb2.Struct()))
    return msg


def serve_provider(
    name: str,
    display_name: str,
    operations: List[OperationDef],
    execute: Callable[[ExecuteRequest], OperationResult],
    description: str = "",
    auth_types: Optional[List[str]] = None,
    authorization_url: Optional[Callable[[str, List[str]], str]] = None,
    exchange_code: Optional[Callable[[str], TokenResponse]] = None,
    refresh_token: Optional[Callable[[str], TokenResponse]] = None,
) -> None:
    socket_path = os.environ.get(ENV_PLUGIN_SOCKET)
    if not socket_path:
        print(f"error: {ENV_PLUGIN_SOCKET} is required", file=sys.stderr)
        sys.exit(1)

    if os.path.exists(socket_path):
        os.unlink(socket_path)

    server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
    servicer = _ProviderServicer(
        name=name,
        display_name=display_name,
        operations=operations,
        execute=execute,
        description=description,
        auth_types=auth_types,
        authorization_url=authorization_url,
        exchange_code=exchange_code,
        refresh_token=refresh_token,
    )
    pb2_grpc.add_ProviderPluginServicer_to_server(servicer, server)
    server.add_insecure_port(f"unix:{socket_path}")
    server.start()

    def _shutdown(signum, frame):
        server.stop(grace=2)

    signal.signal(signal.SIGTERM, _shutdown)
    signal.signal(signal.SIGINT, _shutdown)

    server.wait_for_termination()
