"""Transport-backed S3 SDK tests over a real Unix socket."""
from __future__ import annotations

import os
import socket
import subprocess
import tempfile
import unittest

from gestalt import (
    ENV_HOST_SERVICE_SOCKET,
    ENV_HOST_SERVICE_TOKEN,
    S3,
    ByteRange,
    S3ObjectAccess,
    S3ObjectMeta,
    S3ObjectRef,
    WriteObjectOpen,
)
from gestalt._grpc_transport import host_service_channel
from gestalt.rpc_support import GestaltError, GestaltErrorCode
from gestalt.s3 import (
    CreateObjectAccessURLRequest,
    PresignMethodValues,
    PresignObjectRequest,
    ReadObjectRequest,
)


def _build_harness() -> str:
    bin_path = os.path.join(tempfile.gettempdir(), "s3transportd")
    src_dir = os.path.join(
        os.path.dirname(__file__),
        "..",
        "..",
        "..",
        "gestaltd",
        "internal",
        "testutil",
        "testdata",
        "cmd",
        "s3transportd",
    )
    subprocess.check_call(
        ["go", "build", "-o", bin_path, "."],
        cwd=os.path.abspath(src_dir),
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    return bin_path


_harness_bin: str | None = None
_harness_proc: subprocess.Popen[bytes] | None = None
_socket_path: str = ""


def setUpModule() -> None:
    global _harness_bin, _harness_proc, _socket_path
    _harness_bin = _build_harness()
    _socket_path = os.path.join(tempfile.gettempdir(), f"py-s3-test-{os.getpid()}.sock")
    _harness_proc = subprocess.Popen(
        [_harness_bin, "--socket", _socket_path],
        stdout=subprocess.PIPE,
    )
    assert _harness_proc.stdout is not None
    line = _harness_proc.stdout.readline().decode().strip()
    if line != "READY":
        _harness_proc.kill()
        raise RuntimeError(f"harness did not print READY, got: {line!r}")
    os.environ[ENV_HOST_SERVICE_SOCKET] = _socket_path


def tearDownModule() -> None:
    if _harness_proc:
        _harness_proc.kill()
        _harness_proc.wait()
    if _socket_path and os.path.exists(_socket_path):
        os.remove(_socket_path)


def _reserve_tcp_address() -> str:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        host, port = sock.getsockname()
    return f"{host}:{port}"


def _start_tcp_harness(expect_token: str = "") -> tuple[subprocess.Popen[bytes], str]:
    harness_bin = _build_harness()
    address = _reserve_tcp_address()
    args = [harness_bin, "--tcp", address]
    if expect_token:
        args.extend(["--expect-token", expect_token])
    proc = subprocess.Popen(
        args,
        stdout=subprocess.PIPE,
    )
    assert proc.stdout is not None
    line = proc.stdout.readline().decode().strip()
    proc.stdout.close()
    if line != "READY":
        proc.kill()
        raise RuntimeError(f"tcp harness did not print READY, got: {line!r}")
    return proc, f"tcp://{address}"


def _write_text(
    client: S3,
    key: str,
    text: str,
    *,
    content_type: str = "",
    metadata: dict[str, str] | None = None,
    if_none_match: str = "",
) -> S3ObjectMeta:
    response = client.write_object(
        WriteObjectOpen(
            ref=S3ObjectRef(key=key),
            content_type=content_type,
            metadata=metadata if metadata is not None else {},
            if_none_match=if_none_match,
        ),
        [text.encode("utf-8")],
    )
    assert response.meta is not None
    return response.meta


def _read_bytes(client: S3, key: str, *, range: ByteRange | None = None) -> bytes:
    _meta, chunks = client.read_object(
        ReadObjectRequest(ref=S3ObjectRef(key=key), range=range)
    )
    return b"".join(chunks)


def _exists(client: S3, key: str) -> bool:
    try:
        client.head_object(ref=S3ObjectRef(key=key))
        return True
    except GestaltError as error:
        if error.code == GestaltErrorCode.NOT_FOUND:
            return False
        raise


class TestNamedSocketEnv(unittest.TestCase):
    def test_named_binding_roundtrip(self) -> None:
        client = S3.connect("named")
        _write_text(client, "named.txt", "named")
        self.assertEqual(_read_bytes(client, "named.txt"), b"named")


class TestTCPTargetEnv(unittest.TestCase):
    def test_tcp_target_roundtrip(self) -> None:
        proc, target = _start_tcp_harness()
        self.addCleanup(proc.wait)
        self.addCleanup(proc.kill)

        previous_target = os.environ.get(ENV_HOST_SERVICE_SOCKET)

        def restore_target() -> None:
            if previous_target is None:
                os.environ.pop(ENV_HOST_SERVICE_SOCKET, None)
            else:
                os.environ[ENV_HOST_SERVICE_SOCKET] = previous_target

        os.environ[ENV_HOST_SERVICE_SOCKET] = target
        self.addCleanup(restore_target)

        client = S3.connect("tcp")
        _write_text(client, "tcp.txt", "tcp")
        self.assertEqual(_read_bytes(client, "tcp.txt"), b"tcp")

    def test_tcp_target_token_roundtrip(self) -> None:
        token = "relay-token-python"
        proc, target = _start_tcp_harness(expect_token=token)
        self.addCleanup(proc.wait)
        self.addCleanup(proc.kill)

        target_env = ENV_HOST_SERVICE_SOCKET
        token_env = ENV_HOST_SERVICE_TOKEN
        previous_target = os.environ.get(target_env)
        previous_token = os.environ.get(token_env)

        def restore_env() -> None:
            if previous_target is None:
                os.environ.pop(target_env, None)
            else:
                os.environ[target_env] = previous_target
            if previous_token is None:
                os.environ.pop(token_env, None)
            else:
                os.environ[token_env] = previous_token

        os.environ[target_env] = target
        os.environ[token_env] = token
        self.addCleanup(restore_env)

        client = S3.connect("tcp-token")
        _write_text(client, "tcp-token.txt", "token")
        self.assertEqual(_read_bytes(client, "tcp-token.txt"), b"token")


class TestWriteReadRoundTrip(unittest.TestCase):
    def test_write_stream_stat_and_read(self) -> None:
        client = S3.connect()
        ref = S3ObjectRef(key="streamed.txt")
        response = client.write_object(
            WriteObjectOpen(
                ref=ref,
                content_type="text/plain",
                metadata={"lang": "en"},
            ),
            [b"hello", b" world"],
        )
        meta = response.meta
        assert meta is not None
        assert meta.ref is not None
        self.assertEqual(meta.ref.key, "streamed.txt")
        self.assertEqual(meta.size, 11)
        self.assertEqual(meta.content_type, "text/plain")
        self.assertEqual(meta.metadata, {"lang": "en"})
        self.assertTrue(bool(meta.etag))

        stat = client.head_object(ref=ref).meta
        assert stat is not None
        self.assertEqual(stat.size, 11)
        self.assertEqual(stat.content_type, "text/plain")
        self.assertIsNotNone(stat.last_modified)

        read_meta, chunks = client.read_object(ReadObjectRequest(ref=ref))
        self.assertEqual(read_meta.size, 11)
        self.assertEqual(b"".join(chunks), b"hello world")

    def test_large_in_memory_write_bytes_round_trips(self) -> None:
        client = S3.connect()
        payload = b"x" * (5 * 1024 * 1024)
        chunks = [
            payload[start : start + 64 * 1024]
            for start in range(0, len(payload), 64 * 1024)
        ]
        ref = S3ObjectRef(key="large-bytes.bin")
        response = client.write_object(WriteObjectOpen(ref=ref), chunks)

        assert response.meta is not None
        self.assertEqual(response.meta.size, len(payload))
        stat = client.head_object(ref=ref).meta
        assert stat is not None
        self.assertEqual(stat.size, len(payload))
        self.assertEqual(_read_bytes(client, "large-bytes.bin"), payload)


class TestZeroByteObject(unittest.TestCase):
    def test_zero_byte_object_reads_cleanly(self) -> None:
        client = S3.connect()
        ref = S3ObjectRef(key="empty.bin")
        response = client.write_object(WriteObjectOpen(ref=ref), [])
        assert response.meta is not None
        self.assertEqual(response.meta.size, 0)

        read_meta, chunks = client.read_object(ReadObjectRequest(ref=ref))
        self.assertEqual(read_meta.size, 0)
        self.assertEqual(b"".join(chunks), b"")


class TestRangesAndErrors(unittest.TestCase):
    def test_range_reads_and_invalid_range(self) -> None:
        client = S3.connect()
        _write_text(client, "letters.txt", "abcdef")

        chunk = _read_bytes(client, "letters.txt", range=ByteRange(start=1, end=3))
        self.assertEqual(chunk, b"bcd")

        with self.assertRaises(GestaltError) as raised:
            _read_bytes(client, "letters.txt", range=ByteRange(start=10))
        self.assertEqual(raised.exception.code, GestaltErrorCode.OUT_OF_RANGE)

    def test_not_found_and_precondition_mapping(self) -> None:
        client = S3.connect()
        self.assertFalse(_exists(client, "missing.txt"))
        with self.assertRaises(GestaltError) as raised:
            client.head_object(ref=S3ObjectRef(key="missing.txt"))
        self.assertEqual(raised.exception.code, GestaltErrorCode.NOT_FOUND)

        _write_text(client, "guarded.txt", "first", if_none_match="*")
        with self.assertRaises(GestaltError) as guarded:
            _write_text(client, "guarded.txt", "second", if_none_match="*")
        self.assertEqual(
            guarded.exception.code, GestaltErrorCode.FAILED_PRECONDITION
        )


class TestListCopyDeleteAndPresign(unittest.TestCase):
    def test_list_copy_delete_and_presign(self) -> None:
        client = S3.connect()
        _write_text(client, "docs/a.txt", "a")
        _write_text(client, "docs/b.txt", "b")
        _write_text(client, "docs/nested/c.txt", "c")

        page1 = client.list_objects(prefix="docs/", max_keys=2)
        self.assertEqual(
            [item.ref.key for item in page1.objects if item.ref is not None],
            ["docs/a.txt", "docs/b.txt"],
        )
        self.assertTrue(page1.has_more)
        self.assertEqual(page1.next_continuation_token, "docs/b.txt")

        page2 = client.list_objects(
            prefix="docs/",
            continuation_token=page1.next_continuation_token,
            max_keys=2,
        )
        self.assertEqual(
            [item.ref.key for item in page2.objects if item.ref is not None],
            ["docs/nested/c.txt"],
        )
        self.assertFalse(page2.has_more)

        grouped = client.list_objects(prefix="docs/", delimiter="/", max_keys=10)
        self.assertEqual(
            [item.ref.key for item in grouped.objects if item.ref is not None],
            ["docs/a.txt", "docs/b.txt"],
        )
        self.assertEqual(grouped.common_prefixes, ["docs/nested/"])

        copied = client.copy_object(
            source=S3ObjectRef(key="docs/a.txt"),
            destination=S3ObjectRef(key="docs/copy.txt"),
        )
        assert copied.meta is not None
        assert copied.meta.ref is not None
        self.assertEqual(copied.meta.ref.key, "docs/copy.txt")
        self.assertEqual(_read_bytes(client, "docs/copy.txt"), b"a")

        signed = client.presign_object(
            PresignObjectRequest(
                ref=S3ObjectRef(key="docs/copy.txt"),
                method=PresignMethodValues.PRESIGN_METHOD_PUT,
                expires_seconds=300,
                headers={"x-test": "1"},
            )
        )
        self.assertEqual(signed.method, PresignMethodValues.PRESIGN_METHOD_PUT)
        self.assertTrue(
            signed.url.startswith("https://example.invalid/docs%2Fcopy.txt?")
        )
        self.assertIn("method=PUT", signed.url)
        self.assertEqual(signed.headers, {"x-test": "1"})
        self.assertIsNotNone(signed.expires_at)

        object_access = S3ObjectAccess(
            host_service_channel("s3", _socket_path)
        )
        access_url = object_access.create_object_access_url(
            CreateObjectAccessURLRequest(
                ref=S3ObjectRef(key="docs/copy.txt"),
                method=PresignMethodValues.PRESIGN_METHOD_PUT,
                expires_seconds=300,
                headers={"Content-Length": "5"},
            )
        )
        self.assertEqual(access_url.method, PresignMethodValues.PRESIGN_METHOD_PUT)
        self.assertEqual(access_url.headers, {"Content-Length": "5"})
        self.assertTrue(
            access_url.url.startswith(
                "https://gestalt.example.test/api/v1/s3/object-access/"
            )
        )
        self.assertNotIn("docs/copy.txt", access_url.url)
        self.assertIsNotNone(access_url.expires_at)

        with self.assertRaises(GestaltError) as precondition:
            client.copy_object(
                source=S3ObjectRef(key="docs/a.txt"),
                destination=S3ObjectRef(key="docs/copy-precondition.txt"),
                if_match="wrong-etag",
            )
        self.assertEqual(
            precondition.exception.code, GestaltErrorCode.FAILED_PRECONDITION
        )

        with self.assertRaises(GestaltError) as missing:
            client.copy_object(
                source=S3ObjectRef(key="docs/missing.txt"),
                destination=S3ObjectRef(key="docs/missing-copy.txt"),
            )
        self.assertEqual(missing.exception.code, GestaltErrorCode.NOT_FOUND)

        client.delete_object(ref=S3ObjectRef(key="docs/copy.txt"))
        self.assertFalse(_exists(client, "docs/copy.txt"))


if __name__ == "__main__":
    unittest.main()
