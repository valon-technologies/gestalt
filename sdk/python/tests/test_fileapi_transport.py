"""Transport-backed FileAPI SDK tests over a real Unix socket."""
from __future__ import annotations

import datetime as dt
import os
import subprocess
import tempfile
import unittest

from gestalt import FileAPI, _fileapi, fileapi_socket_env


def _harness_source_dir() -> str:
    return os.path.abspath(
        os.path.join(
            os.path.dirname(__file__),
            "..",
            "..",
            "..",
            "gestaltd",
            "internal",
            "testutil",
            "cmd",
            "fileapitransportd",
        )
    )


def _build_harness() -> str:
    bin_path = os.path.join(tempfile.gettempdir(), "fileapitransportd")
    subprocess.check_call(
        ["go", "build", "-o", bin_path, "."],
        cwd=_harness_source_dir(),
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    return bin_path


_harness_bin: str | None = None
_harness_proc: subprocess.Popen[bytes] | None = None
_socket_path: str = ""


def setUpModule() -> None:
    global _harness_bin, _harness_proc, _socket_path
    try:
        _fileapi._require_generated_fileapi_modules()
    except RuntimeError as error:
        raise unittest.SkipTest(str(error)) from error

    if not os.path.isdir(_harness_source_dir()):
        raise unittest.SkipTest("FileAPI transport harness is not present in this worktree")

    _harness_bin = _build_harness()
    _socket_path = os.path.join(tempfile.gettempdir(), f"py-fileapi-test-{os.getpid()}.sock")
    _harness_proc = subprocess.Popen(
        [_harness_bin, "--socket", _socket_path],
        stdout=subprocess.PIPE,
    )
    assert _harness_proc.stdout is not None
    line = _harness_proc.stdout.readline().decode().strip()
    if line != "READY":
        _harness_proc.kill()
        raise RuntimeError(f"harness did not print READY, got: {line!r}")
    os.environ["GESTALT_FILEAPI_SOCKET"] = _socket_path
    os.environ[fileapi_socket_env("named")] = _socket_path


def tearDownModule() -> None:
    if _harness_proc:
        _harness_proc.kill()
        _harness_proc.wait()
    if _socket_path and os.path.exists(_socket_path):
        os.remove(_socket_path)


def _client(name: str | None = None) -> FileAPI:
    return FileAPI(name)


class TestNamedSocketEnv(unittest.TestCase):
    def test_named_socket_env_roundtrip(self) -> None:
        client = _client("named")
        blob = client.create_blob(["named"], type="text/plain")
        self.assertEqual(blob.text(), "named")
        client.close()


class TestCreateBlob(unittest.TestCase):
    def test_blob_roundtrip(self) -> None:
        client = _client()
        blob = client.create_blob(["hello", b" world"], type="text/plain")

        self.assertEqual(blob.size, 11)
        self.assertEqual(blob.type, "text/plain")
        self.assertEqual(blob.text(), "hello world")
        self.assertEqual(blob.bytes(), b"hello world")
        self.assertEqual(blob.array_buffer(), b"hello world")
        statted = client.stat(blob.id)
        self.assertEqual(statted.bytes(), b"hello world")
        client.close()


class TestSlice(unittest.TestCase):
    def test_slice_uses_file_api_byte_ranges(self) -> None:
        client = _client()
        blob = client.create_blob([b"abcdef"], type="application/octet-stream")

        self.assertEqual(blob.slice(1, 4).bytes(), b"bcd")
        self.assertEqual(blob.slice(-2).bytes(), b"ef")
        self.assertEqual(blob.slice(0, -1).bytes(), b"abcde")
        self.assertEqual(b"".join(blob.stream()), b"abcdef")
        client.close()


class TestCreateFile(unittest.TestCase):
    def test_file_roundtrip(self) -> None:
        client = _client()
        when = dt.datetime(2024, 5, 1, 12, 30, tzinfo=dt.timezone.utc)
        file = client.create_file(
            [b"payload"],
            "report.txt",
            type="text/plain",
            last_modified=when,
        )

        self.assertEqual(file.name, "report.txt")
        self.assertEqual(file.type, "text/plain")
        self.assertEqual(file.text(), "payload")
        self.assertEqual(file.last_modified, int(when.timestamp() * 1000))
        client.close()


class TestObjectURLLifecycle(unittest.TestCase):
    def test_object_url_roundtrip(self) -> None:
        client = _client()
        blob = client.create_blob([b"url-data"], type="application/octet-stream")

        url = blob.create_object_url()
        resolved = client.resolve_object_url(url)

        self.assertEqual(resolved.bytes(), b"url-data")
        client.revoke_object_url(url)
        with self.assertRaises(_fileapi.NotFoundError):
            client.resolve_object_url(url)
        client.close()


if __name__ == "__main__":
    unittest.main()
