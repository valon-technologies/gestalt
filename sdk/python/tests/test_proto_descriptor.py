"""Protobuf descriptor regression tests for the public Python SDK."""

from __future__ import annotations

import unittest

from gestalt._gen.v1 import app_pb2


class ProtoDescriptorTests(unittest.TestCase):
    def test_app_invoke_request_descriptor_contains_runtime_fields(self) -> None:
        descriptor = app_pb2.AppInvokeRequest.DESCRIPTOR
        assert descriptor is not None
        field_names = {field.name for field in descriptor.fields}
        self.assertEqual(
            field_names,
            {
                "app",
                "operation",
                "params",
                "connection",
                "instance",
                "idempotency_key",
                "credential_mode",
                "context",
                "run_as",
            },
        )

    def test_execute_request_descriptor_contains_runtime_fields(self) -> None:
        descriptor = app_pb2.ExecuteRequest.DESCRIPTOR
        assert descriptor is not None
        field_names = {field.name for field in descriptor.fields}
        self.assertEqual(
            field_names,
            {
                "operation",
                "params",
                "token",
                "connection_params",
                "invocation_id",
                "context",
                "idempotency_key",
            },
        )


if __name__ == "__main__":
    unittest.main()
