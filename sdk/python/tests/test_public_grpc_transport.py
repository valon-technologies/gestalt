"""gRPC transport tests for the public Gestalt Python client."""

from __future__ import annotations

import json
import unittest
from concurrent import futures
from unittest import mock

import grpc

from gestalt._gen.v1 import app_pb2, app_pb2_grpc
from gestalt.public.auth import bearer
from gestalt.public.generated.metadata import METHOD_APP_INVOKE
from gestalt.public.grpc_transport import GrpcUnaryTransport, grpc_channel_from_address
from gestalt.rpc_support import GestaltError, GestaltErrorCode


class _AppServicer(app_pb2_grpc.AppServicer):
    def Invoke(self, request, context):
        metadata = dict(context.invocation_metadata())
        if metadata.get("authorization") != "Bearer secret":
            context.set_code(grpc.StatusCode.UNAUTHENTICATED)
            context.set_details("missing bearer")
            return app_pb2.OperationResult()
        return app_pb2.OperationResult(
            status=200,
            body=json.dumps({"status": "success", "data": {"ok": True}}).encode(),
        )

    def InvokeGraphQL(self, request, context):
        context.set_code(grpc.StatusCode.UNIMPLEMENTED)
        context.set_details("not implemented")
        return app_pb2.OperationResult()


class PublicGrpcTransportTests(unittest.TestCase):
    def setUp(self) -> None:
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=1))
        app_pb2_grpc.add_AppServicer_to_server(_AppServicer(), self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.addCleanup(self.server.stop, 0)
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        self.addCleanup(self.channel.close)

    def test_grpc_transport_sends_bearer_metadata(self) -> None:
        transport = GrpcUnaryTransport(
            self.channel,
            bearer(lambda: "secret"),
        )
        request = app_pb2.AppInvokeRequest(app="example", operation="sync")
        response = transport.unary(
            METHOD_APP_INVOKE,
            request,
            app_pb2.OperationResult,
        )
        self.assertEqual(response.status, 200)

    def test_grpc_transport_maps_rpc_errors(self) -> None:
        transport = GrpcUnaryTransport(
            self.channel,
            bearer(lambda: ""),
        )
        request = app_pb2.AppInvokeRequest(app="example", operation="sync")
        with self.assertRaises(GestaltError) as caught:
            transport.unary(METHOD_APP_INVOKE, request, app_pb2.OperationResult)
        self.assertEqual(caught.exception.code, GestaltErrorCode.UNAUTHENTICATED)

    def test_external_channel_disables_http_proxy(self) -> None:
        with (
            mock.patch(
                "gestalt.public.grpc_transport.secure_internal_channel"
            ) as secure,
            mock.patch(
                "gestalt.public.grpc_transport.insecure_internal_channel"
            ) as insecure,
        ):
            grpc_channel_from_address("https://valon.tools")
            secure.assert_called_once_with("valon.tools")

            grpc_channel_from_address("http://localhost:8080")
            insecure.assert_called_once_with("localhost:8080")


if __name__ == "__main__":
    unittest.main()
