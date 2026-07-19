"""Async REST transport tests for the public Gestalt Python client."""

from __future__ import annotations

import json as json_module
from collections.abc import Mapping
from typing import Any

import httpx
from google.protobuf import json_format

from gestalt._gen.v1 import app_pb2
from gestalt.public.auth import bearer, unauthenticated
from gestalt.public.generated.metadata import METHOD_APP_INVOKE
from gestalt.public.rest_transport import AsyncRestUnaryTransport
from gestalt.rpc_support import GestaltError, GestaltErrorCode


class _AsyncRecordingTransport:
    """Async httpx-like client that records each call and delegates to a handler."""

    def __init__(self, handler) -> None:
        self._handler = handler
        self.calls: list[dict[str, Any]] = []

    async def request(
        self,
        method: str,
        url: str,
        *,
        headers: Mapping[str, str] | None = None,
        params: str | None = None,
        json: dict[str, Any] | None = None,
        timeout: float | None = None,
    ) -> httpx.Response:
        self.calls.append(
            {
                "method": method,
                "url": url,
                "headers": dict(headers or {}),
                "params": params,
                "json": json,
                "timeout": timeout,
            }
        )
        return self._handler(
            method, url, headers=headers, params=params, json=json, timeout=timeout
        )


async def test_async_rest_transport_maps_requests_and_gateway_errors() -> None:
    calls: list[dict[str, Any]] = []

    def handler(method, url, *, headers=None, params=None, json=None, **kwargs):
        calls.append(
            {
                "method": method,
                "url": url,
                "authorization": (headers or {}).get("Authorization"),
                "json": json,
            }
        )
        return httpx.Response(
            200,
            headers={"Content-Type": "application/json"},
            content=json_format.MessageToJson(
                app_pb2.OperationResult(
                    status=418,
                    body=b"teapot",
                    headers={"X-Example": app_pb2.StringList(values=["rest-v2"])},
                )
            ).encode(),
        )

    client = _AsyncRecordingTransport(handler)
    transport = AsyncRestUnaryTransport(
        "https://gestalt.test/",
        bearer(lambda: "token-123"),
        client=client,
    )
    request = app_pb2.AppInvokeRequest(
        app="example",
        operation="sync",
        idempotency_key="key-1",
    )
    request.params["ok"] = True
    response = await transport.unary(
        METHOD_APP_INVOKE,
        request,
        app_pb2.OperationResult,
    )

    assert len(calls) == 1
    assert calls[0]["method"] == "POST"
    assert calls[0]["url"] == "https://gestalt.test/api/v2/app/example/operations/sync"
    assert calls[0]["authorization"] == "Bearer token-123"
    assert calls[0]["json"] == {"params": {"ok": True}, "idempotencyKey": "key-1"}
    assert response.status == 418
    assert response.body == b"teapot"

    # Gateway error path: 401 maps to UNAUTHENTICATED.
    def gateway_handler(*_args, **_kwargs):
        return httpx.Response(
            401,
            content=json_module.dumps(
                {"error": "unauthorized", "code": "Unauthenticated"}
            ).encode(),
        )

    gateway_client = _AsyncRecordingTransport(gateway_handler)
    gateway_transport = AsyncRestUnaryTransport(
        "https://gestalt.test/",
        bearer(lambda: "token"),
        client=gateway_client,
    )
    try:
        await gateway_transport.unary(
            METHOD_APP_INVOKE,
            request,
            app_pb2.OperationResult,
        )
        assert False, "expected GestaltError"
    except GestaltError as err:
        assert err.code == GestaltErrorCode.UNAUTHENTICATED
        assert err.message == "unauthorized"


async def test_async_bearer_rotation_is_evaluated_per_invocation() -> None:
    token = {"value": "first"}
    authorizations: list[str | None] = []

    def handler(_method, _url, *, headers=None, **_kwargs):
        authorizations.append((headers or {}).get("Authorization"))
        return httpx.Response(
            200,
            content=json_module.dumps(
                {"status": 200, "body": "", "headers": {}}
            ).encode(),
        )

    transport = AsyncRestUnaryTransport(
        "https://gestalt.test/",
        bearer(lambda: token["value"]),
        client=_AsyncRecordingTransport(handler),
    )
    request = app_pb2.AppInvokeRequest(app="example", operation="sync")
    await transport.unary(METHOD_APP_INVOKE, request, app_pb2.OperationResult)
    token["value"] = "second"
    await transport.unary(METHOD_APP_INVOKE, request, app_pb2.OperationResult)
    assert authorizations == ["Bearer first", "Bearer second"]


async def test_async_unauthenticated_auth_omits_authorization_header() -> None:
    seen: list[str | None] = []

    def handler(_method, _url, *, headers=None, **_kwargs):
        seen.append((headers or {}).get("Authorization"))
        return httpx.Response(
            200,
            content=json_module.dumps(
                {"status": 200, "body": "", "headers": {}}
            ).encode(),
        )

    transport = AsyncRestUnaryTransport(
        "https://gestalt.test/",
        unauthenticated(),
        client=_AsyncRecordingTransport(handler),
    )
    request = app_pb2.AppInvokeRequest(app="example", operation="sync")
    await transport.unary(METHOD_APP_INVOKE, request, app_pb2.OperationResult)
    assert seen[0] is None


async def test_async_generated_client_check_access() -> None:
    """The generated AsyncAuthorizationClient drives the async transport end-to-end."""
    from gestalt.public.generated.authorization import (
        Action,
        CheckAccessRequest,
        Resource,
        Subject,
    )
    from gestalt.public.generated.authorization_client import AsyncAuthorizationClient

    def handler(method, url, *, headers=None, json=None, **kwargs):
        return httpx.Response(
            200,
            headers={"Content-Type": "application/json"},
            content=json_module.dumps({"allowed": True, "modelId": "model-1"}).encode(),
        )

    transport = AsyncRestUnaryTransport(
        "https://gestalt.test/",
        bearer(lambda: "token"),
        client=_AsyncRecordingTransport(handler),
    )
    client = AsyncAuthorizationClient(transport)
    resp = await client.check_access(
        CheckAccessRequest(
            subject=Subject(type="user", id="u1"),
            action=Action(name="read"),
            resource=Resource(type="doc", id="d1"),
        )
    )
    assert resp.allowed is True
    assert resp.model_id == "model-1"


async def test_async_rest_transport_close_releases_owned_client() -> None:
    """AsyncRestUnaryTransport.close() must release the owned httpx.AsyncClient."""

    transport = AsyncRestUnaryTransport(
        "https://gestalt.test/",
        bearer(lambda: "token"),
    )
    assert transport._owned_client is not None
    await transport.close()
    assert transport._owned_client is None
