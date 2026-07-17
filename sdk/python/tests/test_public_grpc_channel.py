"""gRPC channel option tests for the public Gestalt Python client."""

from __future__ import annotations

import unittest
from unittest import mock

from gestalt.public.grpc_transport import grpc_channel_from_address


class PublicGrpcChannelTests(unittest.TestCase):
    def test_external_channel_disables_http_proxy(self) -> None:
        with (
            mock.patch("gestalt.public.grpc_transport.grpc.secure_channel") as secure,
            mock.patch("gestalt.public.grpc_transport.grpc.insecure_channel") as insecure,
        ):
            grpc_channel_from_address("https://valon.tools")
            secure.assert_called_once()
            options = secure.call_args.kwargs.get("options") or secure.call_args.args[2]
            self.assertIn(("grpc.enable_http_proxy", 0), options)

            secure.reset_mock()
            grpc_channel_from_address("http://localhost:8080")
            insecure.assert_called_once()
            options = insecure.call_args.kwargs.get("options") or insecure.call_args.args[1]
            self.assertIn(("grpc.enable_http_proxy", 0), options)


if __name__ == "__main__":
    unittest.main()
