"""Transport-backed IndexedDB SDK tests over a real Unix socket."""
from __future__ import annotations

import os
import socket
import subprocess
import tempfile
import unittest

import grpc

from gestalt import (
    CURSOR_NEXT,
    AlreadyExistsError,
    IndexedDB,
    IndexSchema,
    NotFoundError,
    ObjectStoreSchema,
    indexeddb_socket_env,
    indexeddb_socket_token_env,
)


def _build_harness() -> str:
    """Build the Go harness binary and return its path."""
    bin_path = os.path.join(tempfile.gettempdir(), "indexeddbtransportd")
    src_dir = os.path.join(
        os.path.dirname(__file__),
        "..",
        "..",
        "..",
        "gestaltd",
        "internal",
        "testutil",
        "cmd",
        "indexeddbtransportd",
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
    _socket_path = os.path.join(tempfile.gettempdir(), f"py-idb-test-{os.getpid()}.sock")
    _harness_proc = subprocess.Popen(
        [_harness_bin, "--socket", _socket_path],
        stdout=subprocess.PIPE,
    )
    assert _harness_proc.stdout is not None
    line = _harness_proc.stdout.readline().decode().strip()
    _harness_proc.stdout.close()
    if line != "READY":
        _harness_proc.kill()
        raise RuntimeError(f"harness did not print READY, got: {line!r}")
    os.environ["GESTALT_INDEXEDDB_SOCKET"] = _socket_path
    os.environ[indexeddb_socket_env("named")] = f"unix://{_socket_path}"


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


def _client() -> IndexedDB:
    return IndexedDB()


def _seed_store(client: IndexedDB, name: str) -> None:
    schema = ObjectStoreSchema(
        indexes=[
            IndexSchema(name="by_status", key_path=["status"], unique=False),
            IndexSchema(name="by_email", key_path=["email"], unique=True),
        ],
    )
    client.create_object_store(name, schema)
    store = client.object_store(name)
    store.add({"id": "a", "name": "Alice", "status": "active", "email": "alice@test.com"})
    store.add({"id": "b", "name": "Bob", "status": "active", "email": "bob@test.com"})
    store.add({"id": "c", "name": "Carol", "status": "inactive", "email": "carol@test.com"})
    store.add({"id": "d", "name": "Dave", "status": "active", "email": "dave@test.com"})


class TestNestedJSON(unittest.TestCase):
    def test_nested_json_roundtrip(self) -> None:
        c = _client()
        c.create_object_store("nested_json")
        s = c.object_store("nested_json")
        s.put({"id": "r1", "meta": {"role": "admin", "level": 5}, "tags": ["rust", "go"]})
        got = s.get("r1")
        self.assertIsInstance(got["meta"], dict)
        self.assertEqual(got["meta"]["role"], "admin")
        self.assertIsInstance(got["tags"], list)
        self.assertEqual(got["tags"][0], "rust")
        c.close()


class TestNamedSocketEnv(unittest.TestCase):
    def test_named_socket_env_roundtrip(self) -> None:
        c = IndexedDB("named")
        c.create_object_store("named_socket_env")
        s = c.object_store("named_socket_env")
        s.put({"id": "row-1", "value": "named"})
        got = s.get("row-1")
        self.assertEqual(got["value"], "named")
        c.close()


class TestTCPTarget(unittest.TestCase):
    def test_tcp_target_roundtrip(self) -> None:
        proc, target = _start_tcp_harness()
        self.addCleanup(proc.wait)
        self.addCleanup(proc.kill)
        os.environ["GESTALT_INDEXEDDB_SOCKET"] = target
        self.addCleanup(os.environ.pop, "GESTALT_INDEXEDDB_SOCKET", None)

        c = _client()
        c.create_object_store("tcp_target_env")
        s = c.object_store("tcp_target_env")
        s.put({"id": "row-1", "value": "tcp"})
        got = s.get("row-1")
        self.assertEqual(got["value"], "tcp")
        c.close()

    def test_tcp_target_token_roundtrip(self) -> None:
        token = "relay-token-python"
        proc, target = _start_tcp_harness(expect_token=token)
        self.addCleanup(proc.wait)
        self.addCleanup(proc.kill)
        os.environ["GESTALT_INDEXEDDB_SOCKET"] = target
        os.environ[indexeddb_socket_token_env()] = token
        self.addCleanup(os.environ.pop, "GESTALT_INDEXEDDB_SOCKET", None)
        self.addCleanup(os.environ.pop, indexeddb_socket_token_env(), None)

        c = _client()
        c.create_object_store("tcp_target_token_env")
        s = c.object_store("tcp_target_token_env")
        s.put({"id": "row-1", "value": "token"})
        got = s.get("row-1")
        self.assertEqual(got["value"], "token")
        c.close()


class TestCursorHappyPath(unittest.TestCase):
    def test_cursor_iterates_all_records(self) -> None:
        c = _client()
        _seed_store(c, "cursor_happy")
        s = c.object_store("cursor_happy")
        with s.open_cursor(direction=CURSOR_NEXT) as cursor:
            keys: list[str] = []
            while cursor.continue_():
                keys.append(cursor.primary_key or "")
        self.assertEqual(len(keys), 4)
        self.assertEqual(keys, sorted(keys))
        c.close()


class TestEmptyCursor(unittest.TestCase):
    def test_empty_store_cursor(self) -> None:
        c = _client()
        c.create_object_store("empty_cursor")
        s = c.object_store("empty_cursor")
        with s.open_cursor(direction=CURSOR_NEXT) as cursor:
            self.assertFalse(cursor.continue_())
        c.close()


class TestKeysOnlyCursor(unittest.TestCase):
    def test_keys_only_value_raises(self) -> None:
        c = _client()
        _seed_store(c, "keys_only")
        s = c.object_store("keys_only")
        with s.open_key_cursor(direction=CURSOR_NEXT) as cursor:
            self.assertTrue(cursor.continue_())
            self.assertIsNotNone(cursor.primary_key)
            with self.assertRaises(TypeError):
                _ = cursor.value
        c.close()


class TestCursorExhaustion(unittest.TestCase):
    def test_no_error_after_exhaustion(self) -> None:
        c = _client()
        _seed_store(c, "exhaustion")
        s = c.object_store("exhaustion")
        with s.open_cursor(direction=CURSOR_NEXT) as cursor:
            count = 0
            while cursor.continue_():
                count += 1
            self.assertEqual(count, 4)
        c.close()


class TestContinueToKeyBeyondEnd(unittest.TestCase):
    def test_returns_false(self) -> None:
        c = _client()
        _seed_store(c, "ctk_beyond")
        s = c.object_store("ctk_beyond")
        with s.open_cursor(direction=CURSOR_NEXT) as cursor:
            self.assertFalse(cursor.continue_to_key("zzz"))
        c.close()


class TestAdvancePastEnd(unittest.TestCase):
    def test_returns_false(self) -> None:
        c = _client()
        _seed_store(c, "advance_past")
        s = c.object_store("advance_past")
        with s.open_cursor(direction=CURSOR_NEXT) as cursor:
            self.assertFalse(cursor.advance(100))
        c.close()


class TestCursorCloseClearsState(unittest.TestCase):
    def test_close_clears_last_entry(self) -> None:
        c = _client()
        _seed_store(c, "close_clears_state")
        s = c.object_store("close_clears_state")
        with s.open_cursor(direction=CURSOR_NEXT) as cursor:
            self.assertTrue(cursor.continue_())
            self.assertEqual(cursor.primary_key, "a")

        self.assertIsNone(cursor.key)
        self.assertIsNone(cursor.primary_key)
        with self.assertRaises(TypeError):
            _ = cursor.value
        c.close()


class TestAdvanceRejectsNonPositiveCounts(unittest.TestCase):
    def test_zero_raises_invalid_argument(self) -> None:
        c = _client()
        _seed_store(c, "advance_invalid")
        s = c.object_store("advance_invalid")
        with s.open_cursor(direction=CURSOR_NEXT) as cursor:
            with self.assertRaises(grpc.RpcError):
                cursor.advance(0)
        c.close()


class TestPostExhaustion(unittest.TestCase):
    def test_post_exhaustion_behavior(self) -> None:
        c = _client()
        _seed_store(c, "post_exhaust")
        s = c.object_store("post_exhaust")
        with s.open_cursor(direction=CURSOR_NEXT) as cursor:
            while cursor.continue_():
                pass
            self.assertFalse(cursor.continue_())
            with self.assertRaises(NotFoundError):
                cursor.delete()
        c.close()


class TestIndexCursor(unittest.TestCase):
    def test_index_filter(self) -> None:
        c = _client()
        _seed_store(c, "index_cursor")
        s = c.object_store("index_cursor")
        with s.index("by_status").open_cursor("active", direction=CURSOR_NEXT) as cursor:
            count = 0
            while cursor.continue_():
                self.assertEqual(cursor.value["status"], "active")
                count += 1
            self.assertEqual(count, 3)
        c.close()


class TestIndexContinueToKey(unittest.TestCase):
    def test_round_trips_cursor_key(self) -> None:
        c = _client()
        c.create_object_store(
            "index_seek",
            ObjectStoreSchema(indexes=[IndexSchema(name="by_num", key_path=["n"], unique=False)]),
        )
        s = c.object_store("index_seek")
        s.add({"id": "a", "n": 1})
        s.add({"id": "b", "n": 2})
        s.add({"id": "c", "n": 3})

        with s.index("by_num").open_cursor(direction=CURSOR_NEXT) as cursor:
            self.assertTrue(cursor.continue_())
            self.assertEqual(cursor.key, [1])
            self.assertTrue(cursor.continue_to_key(cursor.key))
            self.assertEqual(cursor.primary_key, "b")
        c.close()


class TestErrorMapping(unittest.TestCase):
    def test_not_found(self) -> None:
        c = _client()
        c.create_object_store("err_map")
        s = c.object_store("err_map")
        with self.assertRaises(NotFoundError):
            s.get("nonexistent")
        c.close()

    def test_already_exists(self) -> None:
        c = _client()
        c.create_object_store("err_map_dup")
        s = c.object_store("err_map_dup")
        s.add({"id": "x"})
        with self.assertRaises(AlreadyExistsError):
            s.add({"id": "x"})
        c.close()
