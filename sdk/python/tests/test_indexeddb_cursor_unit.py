from __future__ import annotations

import unittest

from gestalt._indexeddb import Cursor


class _DummyRequestIterator:
    def __init__(self) -> None:
        self.closed = False

    def close(self) -> None:
        self.closed = True


class TestCursorCloseClearsState(unittest.TestCase):
    def test_close_clears_last_entry(self) -> None:
        cursor = Cursor.__new__(Cursor)
        cursor._keys_only = False
        cursor._closed = False
        cursor._exhausted = False
        cursor._index_cursor = False
        cursor._key = "key"
        cursor._primary_key = "primary"
        cursor._record = {"id": "primary"}
        cursor._request_iter = _DummyRequestIterator()
        cursor._send_command = lambda **kwargs: None

        cursor.close()

        self.assertTrue(cursor._closed)
        self.assertTrue(cursor._request_iter.closed)
        self.assertIsNone(cursor.key)
        self.assertIsNone(cursor.primary_key)
        with self.assertRaises(TypeError):
            _ = cursor.value
