"""Authored Python S3 provider tests over a real gRPC server."""
from __future__ import annotations

import datetime as dt
import os
import pathlib
import sys
import tempfile
import unittest
from concurrent import futures
from typing import Any, Iterable, cast

import grpc

from gestalt import (
    ENV_HOST_SERVICE_SOCKET,
    S3,
    AppProviderAdapter,
    ByteRange,
    CopyObjectRequest,
    CopyObjectResponse,
    DeleteObjectRequest,
    HeadObjectRequest,
    HeadObjectResponse,
    ListObjectsRequest,
    ListObjectsResponse,
    PresignObjectRequest,
    PresignObjectResponse,
    ProviderKind,
    ProviderReadResult,
    ReadObjectRequest,
    S3InvalidRangeError,
    S3NotFoundError,
    S3ObjectMeta,
    S3ObjectRef,
    S3PreconditionFailedError,
    S3Provider,
    WriteObjectOpen,
    WriteObjectResponse,
    _grpc_transport,
    _runtime,
)
from gestalt._gen.v1 import s3_pb2, s3_pb2_grpc
from gestalt.rpc_support import GestaltError, GestaltErrorCode
from gestalt.s3 import PresignMethodValues

UTC = dt.timezone.utc


class _ClosableBody:
    def __init__(self, chunks: Iterable[bytes]) -> None:
        self._chunks = list(chunks)
        self.closed = False

    def __iter__(self):
        return iter(self._chunks)

    def close(self) -> None:
        self.closed = True


class _BrokenBody:
    def __iter__(self):
        yield b"before-error"
        raise S3InvalidRangeError("range exploded")


class _AuthoredS3Provider(S3Provider):
    def __init__(self) -> None:
        self.objects: dict[str, tuple[bytes, str, dict[str, str]]] = {}
        self.last_read_request: ReadObjectRequest | None = None
        self.last_write_open: WriteObjectOpen | None = None
        self.last_copy_request: CopyObjectRequest | None = None
        self.last_presign_request: PresignObjectRequest | None = None
        self.closable_body: _ClosableBody | None = None

    def _meta(self, ref: S3ObjectRef) -> S3ObjectMeta:
        data, content_type, metadata = self._get(ref)
        return S3ObjectMeta(
            ref=ref,
            etag=f"etag-{len(data)}",
            size=len(data),
            content_type=content_type,
            last_modified=dt.datetime(2026, 1, 2, 3, 4, 5, tzinfo=UTC),
            metadata=dict(metadata),
            storage_class="STANDARD",
        )

    def _get(self, ref: S3ObjectRef) -> tuple[bytes, str, dict[str, str]]:
        try:
            return self.objects[ref.key]
        except KeyError as error:
            raise S3NotFoundError("missing object") from error

    def head_object(self, request: HeadObjectRequest) -> HeadObjectResponse:
        assert request.ref is not None
        return HeadObjectResponse(meta=self._meta(request.ref))

    def read_object(self, request: ReadObjectRequest) -> ProviderReadResult:
        self.last_read_request = request
        ref = request.ref
        assert ref is not None
        if ref.key == "broken-body.txt":
            return ProviderReadResult(
                meta=S3ObjectMeta(ref=ref, size=12),
                body=_BrokenBody(),
            )

        data, _content_type, _metadata = self._get(ref)
        if request.range is not None:
            start = request.range.start if request.range.start is not None else 0
            end = (
                request.range.end
                if request.range.end is not None
                else len(data) - 1
            )
            if start >= len(data):
                raise S3InvalidRangeError("range starts after object end")
            data = data[start : end + 1]

        body: Iterable[bytes]
        if ref.key == "closable.txt":
            self.closable_body = _ClosableBody([data])
            body = self.closable_body
        else:
            body = [data]
        return ProviderReadResult(meta=self._meta(ref), body=body)

    def write_object(
        self,
        open: WriteObjectOpen,
        body: Iterable[bytes],
    ) -> WriteObjectResponse:
        self.last_write_open = open
        ref = open.ref
        assert ref is not None
        if open.if_none_match == "fail":
            raise S3PreconditionFailedError("write precondition failed")
        if ref.key == "ignore-body.txt":
            self.objects[ref.key] = (b"", open.content_type, {})
            return WriteObjectResponse(meta=self._meta(ref))
        data = b"".join(bytes(chunk) for chunk in body)
        self.objects[ref.key] = (
            data,
            open.content_type,
            dict(open.metadata),
        )
        return WriteObjectResponse(meta=self._meta(ref))

    def delete_object(self, request: DeleteObjectRequest) -> None:
        assert request.ref is not None
        self.objects.pop(request.ref.key, None)

    def list_objects(self, request: ListObjectsRequest) -> ListObjectsResponse:
        objects = [
            self._meta(S3ObjectRef(key=key))
            for key in sorted(self.objects)
            if key.startswith(request.prefix)
        ]
        if request.max_keys:
            objects = objects[: request.max_keys]
        return ListObjectsResponse(objects=objects)

    def copy_object(self, request: CopyObjectRequest) -> CopyObjectResponse:
        self.last_copy_request = request
        assert request.source is not None
        assert request.destination is not None
        if request.if_match == "fail":
            raise S3PreconditionFailedError("copy precondition failed")
        data, content_type, metadata = self._get(request.source)
        self.objects[request.destination.key] = (
            data,
            content_type,
            dict(metadata),
        )
        return CopyObjectResponse(meta=self._meta(request.destination))

    def presign_object(self, request: PresignObjectRequest) -> PresignObjectResponse:
        self.last_presign_request = request
        assert request.ref is not None
        return PresignObjectResponse(
            url=f"https://example.invalid/{request.ref.key}",
            method=request.method
            if request.method
            else PresignMethodValues.GET,
            expires_at=dt.datetime(2026, 1, 2, 3, 9, 5, tzinfo=UTC),
            headers=dict(request.headers),
        )


class _RunningProvider:
    def __init__(self, provider: S3Provider) -> None:
        _runtime._ensure_grpc_runtime()
        self.temp_dir = tempfile.TemporaryDirectory()
        self.socket_path = os.path.join(self.temp_dir.name, "s3-provider.sock")
        self.server = grpc.server(
            futures.ThreadPoolExecutor(max_workers=4),
            options=_grpc_transport.INTERNAL_GRPC_MESSAGE_OPTIONS,
        )
        _runtime._register_s3_services(self.server, provider)
        self.server.add_insecure_port(f"unix:{self.socket_path}")
        self.server.start()
        self.channel = grpc.insecure_channel(
            f"unix:{self.socket_path}",
            options=_grpc_transport.INTERNAL_GRPC_MESSAGE_OPTIONS,
        )
        grpc.channel_ready_future(self.channel).result(timeout=5)

    def close(self) -> None:
        self.channel.close()
        self.server.stop(0).wait(timeout=5)
        self.temp_dir.cleanup()


class S3AuthoredProviderRuntimeTests(unittest.TestCase):
    def setUp(self) -> None:
        self.provider = _AuthoredS3Provider()
        self.running = _RunningProvider(self.provider)
        self.previous_socket = os.environ.get(ENV_HOST_SERVICE_SOCKET)
        os.environ[ENV_HOST_SERVICE_SOCKET] = self.running.socket_path
        self.stub = s3_pb2_grpc.S3Stub(self.running.channel)

    def tearDown(self) -> None:
        if self.previous_socket is None:
            os.environ.pop(ENV_HOST_SERVICE_SOCKET, None)
        else:
            os.environ[ENV_HOST_SERVICE_SOCKET] = self.previous_socket
        self.running.close()

    def test_client_round_trips_against_authored_provider(self) -> None:
        client = S3.connect()
        written = client.write_object(
            WriteObjectOpen(
                ref=S3ObjectRef(key="hello.txt"),
                content_type="text/plain",
                metadata={"lang": "en"},
            ),
            [b"hello"],
        )
        assert written.meta is not None
        self.assertEqual(written.meta.size, 5)
        self.assertIsInstance(self.provider.last_write_open, WriteObjectOpen)
        assert self.provider.last_write_open is not None
        self.assertEqual(self.provider.last_write_open.content_type, "text/plain")
        self.assertEqual(self.provider.last_write_open.metadata, {"lang": "en"})

        stat = client.head_object(ref=S3ObjectRef(key="hello.txt")).meta
        assert stat is not None
        self.assertEqual(stat.content_type, "text/plain")
        _read_meta, chunks = client.read_object(
            ReadObjectRequest(ref=S3ObjectRef(key="hello.txt"))
        )
        self.assertEqual(b"".join(chunks), b"hello")

        page = client.list_objects(prefix="he")
        self.assertEqual(
            [item.ref.key for item in page.objects if item.ref is not None],
            ["hello.txt"],
        )

        copied = client.copy_object(
            source=S3ObjectRef(key="hello.txt"),
            destination=S3ObjectRef(key="copy.txt"),
            if_match="etag-5",
        )
        assert copied.meta is not None
        assert copied.meta.ref is not None
        self.assertEqual(copied.meta.ref.key, "copy.txt")
        self.assertIsInstance(self.provider.last_copy_request, CopyObjectRequest)

        signed = client.presign_object(
            PresignObjectRequest(
                ref=S3ObjectRef(key="copy.txt"),
                method=PresignMethodValues.PUT,
                expires_seconds=30,
                headers={"x-test": "1"},
            )
        )
        self.assertEqual(signed.method, PresignMethodValues.PUT)
        self.assertEqual(signed.headers, {"x-test": "1"})
        self.assertIsInstance(self.provider.last_presign_request, PresignObjectRequest)

        default_signed = client.presign_object(ref=S3ObjectRef(key="copy.txt"))
        self.assertEqual(
            default_signed.method, PresignMethodValues.GET
        )
        self.assertIsInstance(self.provider.last_presign_request, PresignObjectRequest)
        assert self.provider.last_presign_request is not None
        self.assertEqual(self.provider.last_presign_request.expires_seconds, 0)

        client.delete_object(ref=S3ObjectRef(key="copy.txt"))
        with self.assertRaises(GestaltError) as missing:
            client.head_object(ref=S3ObjectRef(key="copy.txt"))
        self.assertEqual(missing.exception.code, GestaltErrorCode.NOT_FOUND)

    def test_read_options_preserve_zero_range_values(self) -> None:
        self.provider.objects["letters.txt"] = (
            b"abcdef",
            "text/plain",
            {},
        )

        frames = list(
            self.stub.ReadObject(
                s3_pb2.ReadObjectRequest(
                    ref=s3_pb2.S3ObjectRef(key="letters.txt"),
                    range=s3_pb2.ByteRange(start=0, end=0),
                )
            )
        )

        self.assertEqual(frames[1].data, b"a")
        self.assertIsInstance(self.provider.last_read_request, ReadObjectRequest)
        assert self.provider.last_read_request is not None
        assert self.provider.last_read_request.range is not None
        self.assertEqual(self.provider.last_read_request.range.start, 0)
        self.assertEqual(self.provider.last_read_request.range.end, 0)

    def test_read_object_closes_returned_body(self) -> None:
        self.provider.objects["closable.txt"] = (b"close me", "", {})

        frames = list(
            self.stub.ReadObject(
                s3_pb2.ReadObjectRequest(
                    ref=s3_pb2.S3ObjectRef(key="closable.txt")
                )
            )
        )

        self.assertEqual(frames[1].data, b"close me")
        self.assertIsNotNone(self.provider.closable_body)
        assert self.provider.closable_body is not None
        self.assertTrue(self.provider.closable_body.closed)

    def test_s3_errors_map_through_grpc_statuses(self) -> None:
        client = S3.connect()
        with self.assertRaises(GestaltError) as missing:
            client.head_object(ref=S3ObjectRef(key="missing.txt"))
        self.assertEqual(missing.exception.code, GestaltErrorCode.NOT_FOUND)

        with self.assertRaises(GestaltError) as guarded:
            client.write_object(
                WriteObjectOpen(
                    ref=S3ObjectRef(key="guarded.txt"),
                    if_none_match="fail",
                ),
                [b"body"],
            )
        self.assertEqual(
            guarded.exception.code, GestaltErrorCode.FAILED_PRECONDITION
        )

        self.provider.objects["small.txt"] = (b"abc", "", {})
        with self.assertRaises(GestaltError) as out_of_range:
            _meta, chunks = client.read_object(
                ReadObjectRequest(
                    ref=S3ObjectRef(key="small.txt"),
                    range=ByteRange(start=10),
                )
            )
            b"".join(chunks)
        self.assertEqual(
            out_of_range.exception.code, GestaltErrorCode.OUT_OF_RANGE
        )

        self.provider.objects["broken-body.txt"] = (b"ignored", "", {})
        with self.assertRaises(GestaltError) as broken:
            _meta, chunks = client.read_object(
                ReadObjectRequest(ref=S3ObjectRef(key="broken-body.txt"))
            )
            b"".join(chunks)
        self.assertEqual(broken.exception.code, GestaltErrorCode.OUT_OF_RANGE)

    def test_write_object_rejects_malformed_streams(self) -> None:
        cases = [
            iter(()),
            iter([s3_pb2.WriteObjectRequest(data=b"no-open")]),
            iter(
                [
                    s3_pb2.WriteObjectRequest(
                        open=s3_pb2.WriteObjectOpen(
                            ref=s3_pb2.S3ObjectRef(key="bad.txt")
                        )
                    ),
                    s3_pb2.WriteObjectRequest(
                        open=s3_pb2.WriteObjectOpen(
                            ref=s3_pb2.S3ObjectRef(key="again.txt")
                        )
                    ),
                ]
            ),
            iter(
                [
                    s3_pb2.WriteObjectRequest(
                        open=s3_pb2.WriteObjectOpen(
                            ref=s3_pb2.S3ObjectRef(key="ignore-body.txt")
                        )
                    ),
                    s3_pb2.WriteObjectRequest(
                        open=s3_pb2.WriteObjectOpen(
                            ref=s3_pb2.S3ObjectRef(key="again.txt")
                        )
                    ),
                ]
            ),
        ]
        for request_iter in cases:
            with self.subTest(request_iter=request_iter):
                with self.assertRaises(grpc.RpcError) as raised:
                    self.stub.WriteObject(request_iter)
                error = cast(Any, raised.exception)
                self.assertEqual(error.code(), grpc.StatusCode.INVALID_ARGUMENT)

    def test_module_level_s3_provider_resolves_to_s3_adapter(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            root = pathlib.Path(tmpdir)
            (root / "provider.py").write_text(
                "from gestalt import S3Provider\n"
                "class Provider(S3Provider):\n"
                "    pass\n"
                "s3 = Provider()\n",
                encoding="utf-8",
            )
            sys.modules.pop("provider", None)
            try:
                target = _runtime._load_target(
                    _runtime.RuntimeArgs(
                        target="provider",
                        root=root,
                        runtime_kind=ProviderKind.S3.value,
                    )
                )
            finally:
                sys.modules.pop("provider", None)
                if str(root) in sys.path:
                    sys.path.remove(str(root))

        self.assertIsInstance(target, AppProviderAdapter)
        adapter = cast(AppProviderAdapter, target)
        self.assertEqual(adapter.kind, ProviderKind.S3)


if __name__ == "__main__":
    unittest.main()
