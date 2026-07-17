"""REST transport tests for the public Gestalt Python client."""

from __future__ import annotations

import json as json_module
import unittest
from typing import Any
from unittest import mock

import httpx
from google.protobuf import json_format
from google.protobuf.struct_pb2 import Struct

from gestalt._gen.v1 import app_pb2
from gestalt.public.auth import auth_to_provider, bearer, unauthenticated
from gestalt.public.client import create_gestalt_client, rest
from gestalt.public.generated.app import AppInvokeRequest
from gestalt.public.generated.metadata import METHOD_APP_INVOKE
from gestalt.public.rest_transport import RestUnaryTransport
from gestalt.rpc_support import GestaltError, GestaltErrorCode


class _RecordingTransport:
    def __init__(self, handler) -> None:
        self._handler = handler
        self.calls: list[dict[str, Any]] = []

    def request(self, method, url, *, headers=None, params=None, json=None, timeout=None):
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
        return self._handler(method, url, headers=headers, params=params, json=json)


class PublicRestTransportTests(unittest.TestCase):
    def test_rest_transport_maps_requests_and_gateway_errors(self) -> None:
        calls: list[dict[str, Any]] = []

        def handler(method, url, *, headers=None, params=None, json=None):
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
                        headers={
                            "X-Example": app_pb2.StringList(values=["rest-v2"])
                        },
                    )
                ).encode(),
            )

        client = _RecordingTransport(handler)
        transport = RestUnaryTransport(
            "https://gestalt.test/",
            auth_to_provider(bearer(lambda: "token-123")),
            client=client,  # type: ignore[arg-type]
        )
        params = Struct()
        params.update({"ok": True})
        request = app_pb2.AppInvokeRequest(
            app="example",
            operation="sync",
            params=params,
            idempotency_key="key-1",
        )
        response = transport.unary(
            METHOD_APP_INVOKE,
            request,
            app_pb2.OperationResult,
        )

        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0]["method"], "POST")
        self.assertEqual(
            calls[0]["url"],
            "https://gestalt.test/api/v2/app/example/operations/sync",
        )
        self.assertEqual(calls[0]["authorization"], "Bearer token-123")
        self.assertEqual(calls[0]["json"], {"params": {"ok": True}, "idempotencyKey": "key-1"})
        self.assertEqual(response.status, 418)
        self.assertEqual(response.body, b"teapot")

        def gateway_handler(*_args, **_kwargs):
            return httpx.Response(
                401,
                content=json_module.dumps(
                    {"error": "unauthorized", "code": "Unauthenticated"}
                ).encode(),
            )

        gateway_client = _RecordingTransport(gateway_handler)
        gateway_transport = RestUnaryTransport(
            "https://gestalt.test/",
            auth_to_provider(bearer(lambda: "token")),
            client=gateway_client,  # type: ignore[arg-type]
        )
        with self.assertRaises(GestaltError) as caught:
            gateway_transport.unary(
                METHOD_APP_INVOKE,
                request,
                app_pb2.OperationResult,
            )
        self.assertEqual(caught.exception.code, GestaltErrorCode.UNAUTHENTICATED)
        self.assertEqual(caught.exception.message, "unauthorized")

    def test_bearer_rotation_is_evaluated_per_invocation(self) -> None:
        token = {"value": "first"}
        authorizations: list[str | None] = []

        def handler(_method, _url, *, headers=None, **_kwargs):
            authorizations.append((headers or {}).get("Authorization"))
            return httpx.Response(
                200,
                content=json_module.dumps({"status": 200, "body": "", "headers": {}}).encode(),
            )

        transport = RestUnaryTransport(
            "https://gestalt.test/",
            auth_to_provider(bearer(lambda: token["value"])),
            client=_RecordingTransport(handler),  # type: ignore[arg-type]
        )
        request = app_pb2.AppInvokeRequest(app="example", operation="sync")
        transport.unary(METHOD_APP_INVOKE, request, app_pb2.OperationResult)
        token["value"] = "second"
        transport.unary(METHOD_APP_INVOKE, request, app_pb2.OperationResult)
        self.assertEqual(authorizations, ["Bearer first", "Bearer second"])

    def test_unauthenticated_auth_omits_authorization_header(self) -> None:
        seen: list[str | None] = []

        def handler(_method, _url, *, headers=None, **_kwargs):
            seen.append((headers or {}).get("Authorization"))
            return httpx.Response(
                200,
                content=json_module.dumps({"status": 200, "body": "", "headers": {}}).encode(),
            )

        transport = RestUnaryTransport(
            "https://gestalt.test/",
            auth_to_provider(unauthenticated()),
            client=_RecordingTransport(handler),  # type: ignore[arg-type]
        )
        request = app_pb2.AppInvokeRequest(app="example", operation="sync")
        transport.unary(METHOD_APP_INVOKE, request, app_pb2.OperationResult)
        self.assertIsNone(seen[0])

    def test_create_gestalt_client_requires_and_validates_address(self) -> None:
        with self.assertRaises(ValueError):
            with create_gestalt_client(
                address="",
                transport=rest(),
                auth=unauthenticated(),
            ):
                pass
        with self.assertRaises(ValueError):
            with create_gestalt_client(
                address="not-a-url",
                transport=rest(),
                auth=unauthenticated(),
            ):
                pass
        with self.assertRaises(ValueError):
            with create_gestalt_client(
                address="ftp://gestalt.test",
                transport=rest(),
                auth=unauthenticated(),
            ):
                pass

    def test_create_gestalt_client_invokes_through_generated_app_client(self) -> None:
        def handler(_method, _url, *, headers=None, json=None, **_kwargs):
            self.assertEqual(headers.get("Authorization"), "Bearer token")
            self.assertEqual(json, {"params": {"id": "plan-28"}})
            return httpx.Response(
                200,
                content=json_format.MessageToJson(
                    app_pb2.OperationResult(
                        status=200,
                        body=json_module.dumps(
                            {"status": "success", "data": {"id": "plan-28"}}
                        ).encode(),
                    )
                ).encode(),
            )

        with create_gestalt_client(
            address="https://valon.tools",
            transport=rest(),
            auth=bearer(lambda: "token"),
            httpx_client=_RecordingTransport(handler),  # type: ignore[arg-type]
        ) as client:
            result = client.app.invoke(
                AppInvokeRequest(
                    app="gIssues",
                    operation="plans.get",
                    params={"id": "plan-28"},
                )
            )
        self.assertEqual(result, {"id": "plan-28"})

    def test_owned_client_is_closed(self) -> None:
        close_calls = 0
        original_client = httpx.Client

        class TrackingClient(original_client):
            def close(self) -> None:
                nonlocal close_calls
                close_calls += 1
                super().close()

        with mock.patch("gestalt.public.rest_transport.httpx.Client", TrackingClient):
            with create_gestalt_client(
                address="https://gestalt.test",
                transport=rest(),
                auth=unauthenticated(),
            ) as _client:
                pass
        self.assertEqual(close_calls, 1)


if __name__ == "__main__":
    unittest.main()
