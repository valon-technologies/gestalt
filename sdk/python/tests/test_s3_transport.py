"""Transport-backed S3 SDK tests over a real Unix socket."""
from __future__ import annotations

import datetime as dt
import io
import os
import socket
import subprocess
import tempfile
import unittest

from gestalt import (
    S3,
    ByteRange,
    CopyOptions,
    ListOptions,
    ObjectRef,
    PresignMethod,
    PresignOptions,
    ReadOptions,
    S3InvalidRangeError,
    S3NotFoundError,
    S3PreconditionFailedError,
    WriteOptions,
    s3_socket_env,
    s3_socket_token_env,
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
    os.environ["GESTALT_S3_SOCKET"] = _socket_path
    os.environ[s3_socket_env("named")] = _socket_path


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


def _client() -> S3:
    return S3()


class TestNamedSocketEnv(unittest.TestCase):
    def test_named_socket_env_roundtrip(self) -> None:
        client = S3("named")
        obj = client.object("docs", "named.txt")
        obj.write_text("named")
        self.assertEqual(obj.text(), "named")
        client.close()

    def test_named_socket_env_matches_host_ascii_normalization(self) -> None:
        self.assertEqual(s3_socket_env("sø3"), "GESTALT_S3_SOCKET_S_3")


class TestTCPTargetEnv(unittest.TestCase):
    def test_tcp_target_roundtrip(self) -> None:
        proc, target = _start_tcp_harness()
        self.addCleanup(proc.wait)
        self.addCleanup(proc.kill)

        env_name = s3_socket_env()
        previous_target = os.environ.get(env_name)

        def restore_target() -> None:
            if previous_target is None:
                os.environ.pop(env_name, None)
            else:
                os.environ[env_name] = previous_target

        os.environ[env_name] = target
        self.addCleanup(restore_target)

        client = _client()
        obj = client.object("docs", "tcp.txt")
        obj.write_text("tcp")
        self.assertEqual(obj.text(), "tcp")
        client.close()

    def test_tcp_target_token_roundtrip(self) -> None:
        token = "relay-token-python"
        proc, target = _start_tcp_harness(expect_token=token)
        self.addCleanup(proc.wait)
        self.addCleanup(proc.kill)

        target_env = s3_socket_env()
        token_env = s3_socket_token_env()
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

        client = _client()
        obj = client.object("docs", "tcp-token.txt")
        obj.write_text("token")
        self.assertEqual(obj.text(), "token")
        client.close()


class TestWriteReadRoundTrip(unittest.TestCase):
    def test_write_stream_stat_and_read(self) -> None:
        client = _client()
        ref = ObjectRef(bucket="docs", key="streamed.txt")
        meta = client.write_object(
            ref,
            io.BytesIO(b"hello world"),
            WriteOptions(content_type="text/plain", metadata={"lang": "en"}),
        )
        self.assertEqual(meta.ref.bucket, "docs")
        self.assertEqual(meta.ref.key, "streamed.txt")
        self.assertEqual(meta.size, 11)
        self.assertEqual(meta.content_type, "text/plain")
        self.assertEqual(meta.metadata, {"lang": "en"})
        self.assertTrue(bool(meta.etag))

        stat = client.object("docs", "streamed.txt").stat()
        self.assertEqual(stat.size, 11)
        self.assertEqual(stat.content_type, "text/plain")
        self.assertIsNotNone(stat.last_modified)

        read_meta, stream = client.read_object(ref)
        self.assertEqual(read_meta.size, 11)
        with stream:
            self.assertEqual(stream.read(5), b"hello")
            self.assertEqual(stream.read(), b" world")
        client.close()

    def test_large_in_memory_write_bytes_round_trips(self) -> None:
        client = _client()
        payload = b"x" * (5 * 1024 * 1024)
        obj = client.object("docs", "large-bytes.bin")
        meta = obj.write_bytes(payload)

        self.assertEqual(meta.size, len(payload))
        self.assertEqual(obj.stat().size, len(payload))
        self.assertEqual(obj.bytes(), payload)
        client.close()

    def test_close_discards_buffered_bytes(self) -> None:
        client = _client()
        ref = ObjectRef(bucket="docs", key="close-buffered.txt")
        client.write_object(ref, io.BytesIO(b"hello world"))

        _, stream = client.read_object(ref)
        self.assertEqual(stream.read(5), b"hello")
        stream.close()
        self.assertEqual(stream.read(), b"")
        client.close()


class TestJSONRoundTrip(unittest.TestCase):
    def test_write_json_sets_content_type(self) -> None:
        client = _client()
        obj = client.object("docs", "config.json")
        obj.write_json({"role": "admin", "tags": ["python", "go"]})
        stat = obj.stat()
        self.assertEqual(stat.content_type, "application/json")
        self.assertEqual(obj.json(), {"role": "admin", "tags": ["python", "go"]})
        client.close()


class TestZeroByteObject(unittest.TestCase):
    def test_zero_byte_object_reads_cleanly(self) -> None:
        client = _client()
        obj = client.object("docs", "empty.bin")
        meta = obj.write_bytes(b"")
        self.assertEqual(meta.size, 0)

        read_meta, stream = obj.stream()
        self.assertEqual(read_meta.size, 0)
        with stream:
            self.assertEqual(stream.read(), b"")
        client.close()


class TestRangesAndErrors(unittest.TestCase):
    def test_range_reads_and_invalid_range(self) -> None:
        client = _client()
        obj = client.object("docs", "letters.txt")
        obj.write_bytes(b"abcdef")

        chunk = obj.bytes(ReadOptions(range=ByteRange(start=1, end=3)))
        self.assertEqual(chunk, b"bcd")

        with self.assertRaises(S3InvalidRangeError):
            obj.bytes(ReadOptions(range=ByteRange(start=10)))
        client.close()

    def test_not_found_and_precondition_mapping(self) -> None:
        client = _client()
        missing = client.object("docs", "missing.txt")
        self.assertFalse(missing.exists())
        with self.assertRaises(S3NotFoundError):
            missing.stat()

        guarded = client.object("docs", "guarded.txt")
        guarded.write_text("first", WriteOptions(if_none_match="*"))
        with self.assertRaises(S3PreconditionFailedError):
            guarded.write_text("second", WriteOptions(if_none_match="*"))
        client.close()


class TestListCopyDeleteAndPresign(unittest.TestCase):
    def test_list_copy_delete_and_presign(self) -> None:
        client = _client()
        client.object("files", "docs/a.txt").write_text("a")
        client.object("files", "docs/b.txt").write_text("b")
        client.object("files", "docs/nested/c.txt").write_text("c")

        page1 = client.list_objects(ListOptions(bucket="files", prefix="docs/", max_keys=2))
        self.assertEqual([item.ref.key for item in page1.objects], ["docs/a.txt", "docs/b.txt"])
        self.assertTrue(page1.has_more)
        self.assertEqual(page1.next_continuation_token, "docs/b.txt")

        page2 = client.list_objects(
            ListOptions(
                bucket="files",
                prefix="docs/",
                continuation_token=page1.next_continuation_token,
                max_keys=2,
            )
        )
        self.assertEqual([item.ref.key for item in page2.objects], ["docs/nested/c.txt"])
        self.assertFalse(page2.has_more)

        grouped = client.list_objects(
            ListOptions(bucket="files", prefix="docs/", delimiter="/", max_keys=10)
        )
        self.assertEqual([item.ref.key for item in grouped.objects], ["docs/a.txt", "docs/b.txt"])
        self.assertEqual(grouped.common_prefixes, ["docs/nested/"])

        copied = client.copy_object(
            ObjectRef(bucket="files", key="docs/a.txt"),
            ObjectRef(bucket="files", key="docs/copy.txt"),
        )
        self.assertEqual(copied.ref.key, "docs/copy.txt")
        self.assertEqual(client.object("files", "docs/copy.txt").text(), "a")

        signed = client.object("files", "docs/copy.txt").presign(
            PresignOptions(
                method=PresignMethod.PUT,
                expires=dt.timedelta(minutes=5),
                headers={"x-test": "1"},
            )
        )
        self.assertEqual(signed.method, PresignMethod.PUT)
        self.assertTrue(signed.url.startswith("https://example.invalid/files/docs%2Fcopy.txt?"))
        self.assertIn("method=PUT", signed.url)
        self.assertEqual(signed.headers, {"x-test": "1"})
        self.assertIsNotNone(signed.expires_at)

        access_url = client.object("files", "docs/copy.txt").create_access_url(
            PresignOptions(
                method=PresignMethod.PUT,
                expires=dt.timedelta(minutes=5),
                headers={"Content-Length": "5"},
            )
        )
        self.assertEqual(access_url.method, PresignMethod.PUT)
        self.assertTrue(
            access_url.url.startswith("https://gestalt.example.test/api/v1/s3/object-access/")
        )
        self.assertNotIn("docs/copy.txt", access_url.url)
        self.assertEqual(access_url.headers, {"Content-Length": "5"})
        self.assertIsNotNone(access_url.expires_at)

        with self.assertRaises(S3PreconditionFailedError):
            client.copy_object(
                ObjectRef(bucket="files", key="docs/a.txt"),
                ObjectRef(bucket="files", key="docs/copy-precondition.txt"),
                CopyOptions(if_match="wrong-etag"),
            )

        with self.assertRaises(S3NotFoundError):
            client.copy_object(
                ObjectRef(bucket="files", key="docs/missing.txt"),
                ObjectRef(bucket="files", key="docs/missing-copy.txt"),
            )

        copied_obj = client.object("files", "docs/copy.txt")
        copied_obj.delete()
        self.assertFalse(copied_obj.exists())
        client.close()


if __name__ == "__main__":
    unittest.main()
