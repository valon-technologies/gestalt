"""Transport-backed Cache SDK tests over a real Unix socket."""
from __future__ import annotations

import datetime as dt
import os
import socket
import subprocess
import tempfile
import unittest

from gestalt import Cache, CacheEntry, cache_socket_env, cache_socket_token_env


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
    os.environ["GESTALT_CACHE_SOCKET"] = _socket_path
    os.environ[cache_socket_env("named")] = _socket_path
    os.environ[cache_socket_env("café")] = _socket_path


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
        client = Cache()
        client.set("session", b"alpha")
        self.assertEqual(client.get("session"), b"alpha")

        client.set_many(
            [
                CacheEntry(key="a", value=b"one"),
                CacheEntry(key="b", value=b"two"),
            ],
            ttl=dt.timedelta(minutes=5),
        )
        self.assertEqual(
            client.get_many(["session", "a", "missing"]),
            {
                "session": b"alpha",
                "a": b"one",
            },
        )
        self.assertTrue(client.touch("session", dt.timedelta(minutes=1)))
        self.assertFalse(client.touch("missing", dt.timedelta(minutes=1)))
        self.assertEqual(client.delete_many(["a", "missing", "a"]), 1)
        self.assertIsNone(client.get("a"))
        self.assertTrue(client.delete("b"))
        self.assertFalse(client.delete("b"))
        client.close()

    def test_named_socket_env_roundtrip(self) -> None:
        client = Cache("named")
        client.set("named-key", b"named-value")
        self.assertEqual(client.get("named-key"), b"named-value")
        client.close()

    def test_unicode_binding_name_uses_host_normalization(self) -> None:
        self.assertEqual(cache_socket_env("café"), "GESTALT_CACHE_SOCKET_CAF_")
        client = Cache("café")
        client.set("unicode-key", b"unicode-value")
        self.assertEqual(client.get("unicode-key"), b"unicode-value")
        client.close()


class CacheTransportTCPTests(unittest.TestCase):
    def test_tcp_target_roundtrip(self) -> None:
        proc, target = _start_tcp_harness()
        self.addCleanup(proc.wait)
        self.addCleanup(proc.kill)

        env_name = cache_socket_env()
        previous_target = os.environ.get(env_name)

        def restore_target() -> None:
            if previous_target is None:
                os.environ.pop(env_name, None)
            else:
                os.environ[env_name] = previous_target

        os.environ[env_name] = target
        self.addCleanup(restore_target)

        client = Cache()
        client.set("tcp-key", b"tcp-value")
        self.assertEqual(client.get("tcp-key"), b"tcp-value")
        client.close()

    def test_tcp_target_token_roundtrip(self) -> None:
        token = "relay-token-python"
        proc, target = _start_tcp_harness(expect_token=token)
        self.addCleanup(proc.wait)
        self.addCleanup(proc.kill)

        target_env = cache_socket_env()
        token_env = cache_socket_token_env()
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

        client = Cache()
        client.set("tcp-token-key", b"tcp-token-value")
        self.assertEqual(client.get("tcp-token-key"), b"tcp-token-value")
        client.close()
