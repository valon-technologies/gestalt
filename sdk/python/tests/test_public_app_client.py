"""Recording-transport tests for the generated public App client."""

from __future__ import annotations

import base64
import json
import pathlib
import unittest
from typing import Any, TypeVar, cast

from google.protobuf import json_format
from google.protobuf.message import Message

from gestalt._gen.v1 import app_pb2
from gestalt.public.generated.app import AppInvokeRequest
from gestalt.public.generated.app_client import AppClient
from gestalt.public.generated.unary_transport import UnaryTransport
from gestalt.rpc_support import GestaltError

FIXTURE_ROOT = pathlib.Path(__file__).resolve().parents[2] / "testdata" / "public_conformance"
ResponseT = TypeVar("ResponseT", bound=Message)


class RecordingTransport(UnaryTransport):
    def __init__(self) -> None:
        self.calls = 0
        self.err: GestaltError | None = None
        self.body: bytes = b""
        self.last_request: Message | None = None

    def unary(
        self,
        method,
        request: Message,
        response_type: type[ResponseT],
    ) -> ResponseT:
        self.calls += 1
        self.last_request = request
        if method.name != "Invoke":
            raise AssertionError(f"unexpected method {method.name!r}")
        if self.err is not None:
            raise self.err
        return cast(
            ResponseT,
            app_pb2.OperationResult(status=200, body=self.body),
        )


class PublicAppClientTests(unittest.TestCase):
    def load_cases(self) -> list[dict[str, Any]]:
        return json.loads((FIXTURE_ROOT / "client_cases.json").read_text())

    def test_shared_client_cases(self) -> None:
        for case in self.load_cases():
            with self.subTest(case["id"]):
                transport = RecordingTransport()
                if case["id"] == "invoke_success":
                    transport.body = base64.b64decode(
                        case["response"]["operationResult"]["bodyBase64"]
                    )
                elif case["id"] == "platform_error":
                    err = case["response"]["gestaltError"]
                    transport.err = GestaltError(
                        err["code"],
                        err["message"],
                    )
                else:
                    self.fail(f"unknown case {case['id']!r}")

                client = AppClient(transport)
                request = AppInvokeRequest(
                    app=case["publicRequest"]["app"],
                    operation=case["publicRequest"]["operation"],
                    params=case["publicRequest"].get("params"),
                )

                if case["id"] == "invoke_success":
                    self.assertEqual(client.invoke(request), case["expect"]["result"])
                else:
                    with self.assertRaises(GestaltError) as err:
                        client.invoke(request)
                    self.assertEqual(
                        int(err.exception.code),
                        case["expect"]["gestaltError"]["code"],
                    )
                    self.assertEqual(
                        err.exception.message,
                        case["expect"]["gestaltError"]["message"],
                    )

                self.assertIsNotNone(transport.last_request)
                got_wire = json_format.MessageToDict(
                    transport.last_request,
                    preserving_proto_field_name=False,
                )
                self.assertEqual(got_wire, case["wireRequest"])
                self.assertEqual(transport.calls, case["expect"]["calls"])


if __name__ == "__main__":
    unittest.main()
