"""Transport-backed Cache SDK tests over a real Unix socket."""
from __future__ import annotations

import datetime as dt
import os
import subprocess
import tempfile
import unittest

from gestalt import Cache, CacheEntry, cache_socket_env


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
