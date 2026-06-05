from __future__ import annotations

import os
import unittest
from unittest import mock

from gestalt import ENV_HOST_SERVICE_SOCKET, ENV_HOST_SERVICE_TOKEN, Request
from gestalt._grpc_transport import (
    INVOCATION_TOKEN_HEADER,
    HostServiceMetadataInterceptor,
)


class _CallDetails:
    method = "/gestalt.authorization.v1.AuthorizationProvider/SetAuthorizationState"
    timeout = None
    metadata = None
    credentials = None
    wait_for_ready = None
    compression = None


class AuthorizationTransportTests(unittest.TestCase):
    def test_request_authorization_passes_invocation_token_to_host_channel(self) -> None:
        with (
            mock.patch.dict(
                os.environ,
                {ENV_HOST_SERVICE_SOCKET: "/tmp/auth.sock", ENV_HOST_SERVICE_TOKEN: ""},
            ),
            mock.patch("gestalt._authorization.host_service_channel") as channel,
            mock.patch("gestalt._authorization.pb_grpc.AuthorizationProviderStub"),
        ):
            Request(invocation_token=" invoke-authz ").authorization().close()

        channel.assert_called_once_with(
            "authorization",
            "/tmp/auth.sock",
            token="",
            invocation_token="invoke-authz",
        )

    def test_host_metadata_interceptor_adds_invocation_token(self) -> None:
        details = HostServiceMetadataInterceptor(
            invocation_token="invoke-authz",
        )._with_metadata(_CallDetails())

        self.assertIn((INVOCATION_TOKEN_HEADER, "invoke-authz"), details.metadata)


if __name__ == "__main__":
    unittest.main()
