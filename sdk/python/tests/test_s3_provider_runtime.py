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
    ByteRange,
    CopyOptions,
    ListOptions,
    ListPage,
    ObjectMeta,
    ObjectRef,
    AppProviderAdapter,
    PresignMethod,
    PresignOptions,
    PresignResult,
    ProviderKind,
    ProviderReadResult,
    ReadOptions,
    S3InvalidRangeError,
    S3NotFoundError,
    S3PreconditionFailedError,
    S3Provider,
    WriteOptions,
    _grpc_transport,
    _runtime,
)
from gestalt._gen.v1 import s3_pb2, s3_pb2_grpc

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
        self.objects: dict[tuple[str, str], tuple[bytes, str, dict[str, str]]] = {}
        self.last_read_options: ReadOptions | None = None
        self.last_write_options: WriteOptions | None = None
        self.last_copy_options: CopyOptions | None = None
        self.last_presign_options: PresignOptions | None = None
        self.closable_body: _ClosableBody | None = None

    def _meta(self, ref: ObjectRef) -> ObjectMeta:
        data, content_type, metadata = self._get(ref)
        return ObjectMeta(
            ref=ref,
            etag=f"etag-{len(data)}",
            size=len(data),
            content_type=content_type,
            last_modified=dt.datetime(2026, 1, 2, 3, 4, 5, tzinfo=UTC),
            metadata=dict(metadata),
            storage_class="STANDARD",
        )

    def _get(self, ref: ObjectRef) -> tuple[bytes, str, dict[str, str]]:
        try:
            return self.objects[(ref.bucket, ref.key)]
        except KeyError as error:
            raise S3NotFoundError("missing object") from error

    def head_object(self, ref: ObjectRef) -> ObjectMeta:
        return self._meta(ref)

    def read_object(
        self,
        ref: ObjectRef,
        opts: ReadOptions | None = None,
    ) -> ProviderReadResult:
        self.last_read_options = opts
        if ref.key == "broken-body.txt":
            return ProviderReadResult(
                meta=ObjectMeta(ref=ref, size=12),
                body=_BrokenBody(),
            )

        data, _content_type, _metadata = self._get(ref)
        if opts and opts.range:
            start = opts.range.start if opts.range.start is not None else 0
            end = opts.range.end if opts.range.end is not None else len(data) - 1
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
        ref: ObjectRef,
        body: Iterable[bytes],
        opts: WriteOptions | None = None,
    ) -> ObjectMeta:
        self.last_write_options = opts
        if opts and opts.if_none_match == "fail":
            raise S3PreconditionFailedError("write precondition failed")
        if ref.key == "ignore-body.txt":
            self.objects[(ref.bucket, ref.key)] = (
                b"",
                opts.content_type if opts else "",
                {},
            )
            return self._meta(ref)
        data = b"".join(bytes(chunk) for chunk in body)
        self.objects[(ref.bucket, ref.key)] = (
            data,
            opts.content_type if opts else "",
            dict(opts.metadata) if opts else {},
        )
        return self._meta(ref)

    def delete_object(self, ref: ObjectRef) -> None:
        self.objects.pop((ref.bucket, ref.key), None)

    def list_objects(self, opts: ListOptions) -> ListPage:
        objects = [
            self._meta(ObjectRef(bucket=bucket, key=key))
            for bucket, key in sorted(self.objects)
            if bucket == opts.bucket and key.startswith(opts.prefix)
        ]
        if opts.max_keys:
            objects = objects[: opts.max_keys]
        return ListPage(objects=objects)

    def copy_object(
        self,
        source: ObjectRef,
        destination: ObjectRef,
        opts: CopyOptions | None = None,
    ) -> ObjectMeta:
        self.last_copy_options = opts
        if opts and opts.if_match == "fail":
            raise S3PreconditionFailedError("copy precondition failed")
        data, content_type, metadata = self._get(source)
        self.objects[(destination.bucket, destination.key)] = (
            data,
            content_type,
            dict(metadata),
        )
        return self._meta(destination)

    def presign_object(
        self,
        ref: ObjectRef,
        opts: PresignOptions | None = None,
    ) -> PresignResult:
        self.last_presign_options = opts
        return PresignResult(
            url=f"https://example.invalid/{ref.bucket}/{ref.key}",
            method=opts.method if opts and opts.method else PresignMethod.GET,
            expires_at=dt.datetime(2026, 1, 2, 3, 9, 5, tzinfo=UTC),
            headers=dict(opts.headers) if opts else {},
        )


class _LegacyS3Provider(S3Provider):
    def HeadObject(self, request, _context):
        return s3_pb2.HeadObjectResponse(
            meta=s3_pb2.S3ObjectMeta(
                ref=request.ref,
                etag="legacy",
                size=6,
                content_type="text/plain",
            )
        )

    def ReadObject(self, request, _context):
        yield s3_pb2.ReadObjectChunk(
            meta=s3_pb2.S3ObjectMeta(
                ref=request.ref,
                etag="legacy-read",
                size=6,
                content_type="text/plain",
            )
        )
        yield s3_pb2.ReadObjectChunk(data=b"legacy")

    def WriteObject(self, request_iterator, _context):
        first = next(request_iterator)
        body = b"".join(
            bytes(message.data)
            for message in request_iterator
            if message.WhichOneof("msg") == "data"
        )
        return s3_pb2.WriteObjectResponse(
            meta=s3_pb2.S3ObjectMeta(
                ref=first.open.ref,
                etag="legacy-write",
                size=len(body),
                content_type=first.open.content_type,
            )
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
        client = S3()
        obj = client.object("docs", "hello.txt")
        written = obj.write_text(
            "hello",
            WriteOptions(content_type="text/plain", metadata={"lang": "en"}),
        )
        self.assertEqual(written.size, 5)
        self.assertIsInstance(self.provider.last_write_options, WriteOptions)
        assert self.provider.last_write_options is not None
        self.assertEqual(self.provider.last_write_options.content_type, "text/plain")
        self.assertEqual(self.provider.last_write_options.metadata, {"lang": "en"})

        self.assertEqual(obj.stat().content_type, "text/plain")
        self.assertEqual(obj.text(), "hello")

        page = client.list_objects(ListOptions(bucket="docs", prefix="he"))
        self.assertEqual([item.ref.key for item in page.objects], ["hello.txt"])

        copied = client.copy_object(
            ObjectRef(bucket="docs", key="hello.txt"),
            ObjectRef(bucket="docs", key="copy.txt"),
            CopyOptions(if_match="etag-5"),
        )
        self.assertEqual(copied.ref.key, "copy.txt")
        self.assertIsInstance(self.provider.last_copy_options, CopyOptions)

        signed = client.object("docs", "copy.txt").presign(
            PresignOptions(
                method=PresignMethod.PUT,
                expires=dt.timedelta(seconds=30),
                headers={"x-test": "1"},
            )
        )
        self.assertEqual(signed.method, PresignMethod.PUT)
        self.assertEqual(signed.headers, {"x-test": "1"})
        self.assertIsInstance(self.provider.last_presign_options, PresignOptions)

        default_signed = client.object("docs", "copy.txt").presign()
        self.assertEqual(default_signed.method, PresignMethod.GET)
        self.assertIsInstance(self.provider.last_presign_options, PresignOptions)
        assert self.provider.last_presign_options is not None
        self.assertIsNone(self.provider.last_presign_options.expires)

        client.object("docs", "copy.txt").delete()
        self.assertFalse(client.object("docs", "copy.txt").exists())
        client.close()

    def test_read_options_preserve_zero_range_values(self) -> None:
        self.provider.objects[("docs", "letters.txt")] = (
            b"abcdef",
            "text/plain",
            {},
        )

        frames = list(
            self.stub.ReadObject(
                s3_pb2.ReadObjectRequest(
                    ref=s3_pb2.S3ObjectRef(bucket="docs", key="letters.txt"),
                    range=s3_pb2.ByteRange(start=0, end=0),
                )
            )
        )

        self.assertEqual(frames[1].data, b"a")
        self.assertIsInstance(self.provider.last_read_options, ReadOptions)
        assert self.provider.last_read_options is not None
        assert self.provider.last_read_options.range is not None
        self.assertEqual(self.provider.last_read_options.range.start, 0)
        self.assertEqual(self.provider.last_read_options.range.end, 0)

    def test_read_object_closes_returned_body(self) -> None:
        self.provider.objects[("docs", "closable.txt")] = (b"close me", "", {})

        frames = list(
            self.stub.ReadObject(
                s3_pb2.ReadObjectRequest(
                    ref=s3_pb2.S3ObjectRef(bucket="docs", key="closable.txt")
                )
            )
        )

        self.assertEqual(frames[1].data, b"close me")
        self.assertIsNotNone(self.provider.closable_body)
        assert self.provider.closable_body is not None
        self.assertTrue(self.provider.closable_body.closed)

    def test_s3_errors_map_through_grpc_statuses(self) -> None:
        client = S3()
        with self.assertRaises(S3NotFoundError):
            client.object("docs", "missing.txt").stat()

        with self.assertRaises(S3PreconditionFailedError):
            client.object("docs", "guarded.txt").write_text(
                "body",
                WriteOptions(if_none_match="fail"),
            )

        self.provider.objects[("docs", "small.txt")] = (b"abc", "", {})
        with self.assertRaises(S3InvalidRangeError):
            client.object("docs", "small.txt").bytes(
                ReadOptions(range=ByteRange(start=10))
            )

        self.provider.objects[("docs", "broken-body.txt")] = (b"ignored", "", {})
        with self.assertRaises(S3InvalidRangeError):
            client.object("docs", "broken-body.txt").bytes()
        client.close()

    def test_write_object_rejects_malformed_streams(self) -> None:
        cases = [
            iter(()),
            iter([s3_pb2.WriteObjectRequest(data=b"no-open")]),
            iter(
                [
                    s3_pb2.WriteObjectRequest(
                        open=s3_pb2.WriteObjectOpen(
                            ref=s3_pb2.S3ObjectRef(bucket="docs", key="bad.txt")
                        )
                    ),
                    s3_pb2.WriteObjectRequest(
                        open=s3_pb2.WriteObjectOpen(
                            ref=s3_pb2.S3ObjectRef(bucket="docs", key="again.txt")
                        )
                    ),
                ]
            ),
            iter(
                [
                    s3_pb2.WriteObjectRequest(
                        open=s3_pb2.WriteObjectOpen(
                            ref=s3_pb2.S3ObjectRef(
                                bucket="docs", key="ignore-body.txt"
                            )
                        )
                    ),
                    s3_pb2.WriteObjectRequest(
                        open=s3_pb2.WriteObjectOpen(
                            ref=s3_pb2.S3ObjectRef(bucket="docs", key="again.txt")
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


class S3LegacyProviderRuntimeTests(unittest.TestCase):
    def test_raw_generated_unary_method_fallback_still_serves_legacy_provider(
        self,
    ) -> None:
        running = _RunningProvider(_LegacyS3Provider())
        self.addCleanup(running.close)
        stub = s3_pb2_grpc.S3Stub(running.channel)

        response = stub.HeadObject(
            s3_pb2.HeadObjectRequest(
                ref=s3_pb2.S3ObjectRef(bucket="legacy", key="object.txt")
            )
        )

        self.assertEqual(response.meta.etag, "legacy")
        self.assertEqual(response.meta.ref.bucket, "legacy")

    def test_raw_generated_streaming_method_fallback_still_serves_legacy_provider(
        self,
    ) -> None:
        running = _RunningProvider(_LegacyS3Provider())
        self.addCleanup(running.close)
        stub = s3_pb2_grpc.S3Stub(running.channel)

        read_frames = list(
            stub.ReadObject(
                s3_pb2.ReadObjectRequest(
                    ref=s3_pb2.S3ObjectRef(bucket="legacy", key="object.txt")
                )
            )
        )
        self.assertEqual(read_frames[0].meta.etag, "legacy-read")
        self.assertEqual(read_frames[1].data, b"legacy")

        written = stub.WriteObject(
            iter(
                [
                    s3_pb2.WriteObjectRequest(
                        open=s3_pb2.WriteObjectOpen(
                            ref=s3_pb2.S3ObjectRef(
                                bucket="legacy",
                                key="written.txt",
                            ),
                            content_type="text/plain",
                        )
                    ),
                    s3_pb2.WriteObjectRequest(data=b"leg"),
                    s3_pb2.WriteObjectRequest(data=b"acy"),
                ]
            )
        )
        self.assertEqual(written.meta.etag, "legacy-write")
        self.assertEqual(written.meta.size, 6)


if __name__ == "__main__":
    unittest.main()
