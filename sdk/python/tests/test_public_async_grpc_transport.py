"""Async gRPC transport tests for the public Gestalt Python client."""

from __future__ import annotations

import json
from concurrent import futures

import grpc
import grpc.aio
from grpc.aio import server as aio_server

from gestalt._gen.v1 import app_pb2, app_pb2_grpc
from gestalt.public.auth import bearer
from gestalt.public.generated.metadata import METHOD_APP_INVOKE
from gestalt.public.grpc_transport import AsyncGrpcUnaryTransport
from gestalt.rpc_support import GestaltError, GestaltErrorCode


class _AsyncAppServicer(app_pb2_grpc.AppServicer):
    async def Invoke(self, request, context):
        metadata = dict(context.invocation_metadata())
        if metadata.get("authorization") != "Bearer secret":
            await context.abort(grpc.StatusCode.UNAUTHENTICATED, "missing bearer")
        return app_pb2.OperationResult(
            status=200,
            body=json.dumps({"status": "success", "data": {"ok": True}}).encode(),
        )

    async def InvokeGraphQL(self, request, context):
        await context.abort(grpc.StatusCode.UNIMPLEMENTED, "not implemented")


async def test_async_grpc_transport_sends_bearer_metadata() -> None:
    server = aio_server(futures.ThreadPoolExecutor(max_workers=1))
    app_pb2_grpc.add_AppServicer_to_server(_AsyncAppServicer(), server)
    port = server.add_insecure_port("127.0.0.1:0")
    await server.start()
    try:
        channel = grpc.aio.insecure_channel(f"127.0.0.1:{port}")
        try:
            transport = AsyncGrpcUnaryTransport(channel, bearer(lambda: "secret"))
            request = app_pb2.AppInvokeRequest(app="example", operation="sync")
            response = await transport.unary(
                METHOD_APP_INVOKE,
                request,
                app_pb2.OperationResult,
            )
            assert response.status == 200
        finally:
            await channel.close()
    finally:
        await server.stop(grace=None)


async def test_async_grpc_transport_maps_rpc_errors() -> None:
    server = aio_server(futures.ThreadPoolExecutor(max_workers=1))
    app_pb2_grpc.add_AppServicer_to_server(_AsyncAppServicer(), server)
    port = server.add_insecure_port("127.0.0.1:0")
    await server.start()
    try:
        channel = grpc.aio.insecure_channel(f"127.0.0.1:{port}")
        try:
            transport = AsyncGrpcUnaryTransport(channel, bearer(lambda: ""))
            request = app_pb2.AppInvokeRequest(app="example", operation="sync")
            try:
                await transport.unary(
                    METHOD_APP_INVOKE, request, app_pb2.OperationResult
                )
                assert False, "expected GestaltError"
            except GestaltError as err:
                assert err.code == GestaltErrorCode.UNAUTHENTICATED
        finally:
            await channel.close()
    finally:
        await server.stop(grace=None)


async def test_async_generated_client_invoke() -> None:
    """The generated AsyncAppClient drives the async gRPC transport end-to-end."""
    from gestalt.public.generated.app import AppInvokeRequest
    from gestalt.public.generated.app_client import AsyncAppClient

    server = aio_server(futures.ThreadPoolExecutor(max_workers=1))
    app_pb2_grpc.add_AppServicer_to_server(_AsyncAppServicer(), server)
    port = server.add_insecure_port("127.0.0.1:0")
    await server.start()
    try:
        channel = grpc.aio.insecure_channel(f"127.0.0.1:{port}")
        try:
            transport = AsyncGrpcUnaryTransport(channel, bearer(lambda: "secret"))
            client = AsyncAppClient(transport)
            response = await client.invoke_raw(
                AppInvokeRequest(app="example", operation="sync")
            )
            assert response.status == 200
        finally:
            await channel.close()
    finally:
        await server.stop(grace=None)


async def test_async_channel_applies_internal_options() -> None:
    """async_grpc_channel_from_address must pass the same internal channel
    options (proxy disable + 64MB message limits) as the sync path."""
    from unittest import mock

    from gestalt._grpc_transport import _INTERNAL_CHANNEL_OPTIONS
    from gestalt.public.grpc_transport import async_grpc_channel_from_address

    with (
        mock.patch("gestalt.public.grpc_transport.grpc.aio.secure_channel") as secure,
        mock.patch(
            "gestalt.public.grpc_transport.grpc.aio.insecure_channel"
        ) as insecure,
    ):
        async_grpc_channel_from_address("https://valon.tools")
        secure.assert_called_once_with(
            "valon.tools",
            mock.ANY,
            options=_INTERNAL_CHANNEL_OPTIONS,
        )

        async_grpc_channel_from_address("http://localhost:8080")
        insecure.assert_called_once_with(
            "localhost:8080",
            options=_INTERNAL_CHANNEL_OPTIONS,
        )


async def test_async_grpc_transport_close_releases_owned_channel() -> None:
    """AsyncGrpcUnaryTransport.close() must close the channel when owns_channel=True."""
    channel = grpc.aio.insecure_channel("127.0.0.1:1")
    transport = AsyncGrpcUnaryTransport(
        channel, bearer(lambda: "token"), owns_channel=True
    )
    await transport.close()
    # Channel state after close is SHUTDOWN; accessing it should not raise.
