"""Transport-backed IndexedDB SDK tests over a real Unix socket."""
from __future__ import annotations

import os
import subprocess
import tempfile
import time
import unittest

from gestalt import (
    CURSOR_NEXT,
    AlreadyExistsError,
    IndexedDB,
    IndexSchema,
    NotFoundError,
    ObjectStoreSchema,
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
    if line != "READY":
        _harness_proc.kill()
        raise RuntimeError(f"harness did not print READY, got: {line!r}")
    os.environ["GESTALT_INDEXEDDB_SOCKET"] = _socket_path


def tearDownModule() -> None:
    if _harness_proc:
        _harness_proc.kill()
        _harness_proc.wait()
    if _socket_path and os.path.exists(_socket_path):
        os.remove(_socket_path)


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
