"""Transport-backed Cache SDK tests over a real Unix socket."""
from __future__ import annotations

import datetime as dt
import os
import socket
import subprocess
import tempfile
import unittest

from gestalt import (
    ENV_HOST_SERVICE_SOCKET,
    ENV_HOST_SERVICE_TOKEN,
    Cache,
    CacheSetEntry,
)


def _build_harness() -> str:
    bin_path = os.path.join(tempfile.gettempdir(), "cachetransportd")
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
        "cachetransportd",
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
    _socket_path = os.path.join(
        tempfile.gettempdir(), f"py-cache-test-{os.getpid()}.sock"
    )
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


class CacheTransportTests(unittest.TestCase):
    def test_roundtrip_batch_delete_and_touch(self) -> None:
        client = Cache.connect()
        client.set(key="session", value=b"alpha")
        self.assertEqual(client.get(key="session"), b"alpha")

        client.set_many(
            entries=[
                CacheSetEntry(key="a", value=b"one"),
                CacheSetEntry(key="b", value=b"two"),
            ],
            ttl=dt.timedelta(minutes=5),
        )
        self.assertEqual(
            client.get_many(keys=["session", "a", "missing"]),
            {
                "session": b"alpha",
                "a": b"one",
            },
        )
        self.assertTrue(client.touch(key="session", ttl=dt.timedelta(minutes=1)))
        self.assertFalse(client.touch(key="missing", ttl=dt.timedelta(minutes=1)))
        self.assertEqual(client.delete_many(keys=["a", "missing", "a"]), 1)
        self.assertIsNone(client.get(key="a"))
        self.assertTrue(client.delete(key="b"))
        self.assertFalse(client.delete(key="b"))

    def test_named_binding_roundtrip(self) -> None:
        client = Cache.connect("named")
        client.set(key="named-key", value=b"named-value")
        self.assertEqual(client.get(key="named-key"), b"named-value")

    def test_archive_binding_roundtrip(self) -> None:
        client = Cache.connect("archive")
        client.set(key="archive-key", value=b"archive-value")
        self.assertEqual(client.get(key="archive-key"), b"archive-value")


class CacheTransportTCPTests(unittest.TestCase):
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

        client = Cache.connect("tcp")
        client.set(key="tcp-key", value=b"tcp-value")
        self.assertEqual(client.get(key="tcp-key"), b"tcp-value")

    def test_tcp_target_token_roundtrip(self) -> None:
        token = "relay-token-python"
        proc, target = _start_tcp_harness(expect_token=token)
        self.addCleanup(proc.wait)
        self.addCleanup(proc.kill)

        previous_target = os.environ.get(ENV_HOST_SERVICE_SOCKET)
        previous_token = os.environ.get(ENV_HOST_SERVICE_TOKEN)

        def restore_env() -> None:
            if previous_target is None:
                os.environ.pop(ENV_HOST_SERVICE_SOCKET, None)
            else:
                os.environ[ENV_HOST_SERVICE_SOCKET] = previous_target
            if previous_token is None:
                os.environ.pop(ENV_HOST_SERVICE_TOKEN, None)
            else:
                os.environ[ENV_HOST_SERVICE_TOKEN] = previous_token

        os.environ[ENV_HOST_SERVICE_SOCKET] = target
        os.environ[ENV_HOST_SERVICE_TOKEN] = token
        self.addCleanup(restore_env)

        client = Cache.connect("tcp-token")
        client.set(key="tcp-token-key", value=b"tcp-token-value")
        self.assertEqual(client.get(key="tcp-token-key"), b"tcp-token-value")
