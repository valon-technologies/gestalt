from __future__ import annotations

import unittest

from gestalt import KeyRange, only
from gestalt._indexeddb import (
    CURSOR_NEXT_UNIQUE,
    Cursor,
    IndexedDBCursorSnapshotEntry,
    IndexedDBOpenCursorRequest,
    new_indexeddb_cursor_snapshot,
)


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


class TestIndexedDBCursorSnapshot(unittest.TestCase):
    def test_index_cursor_sorts_ranges_and_skips_duplicate_unique_keys(self) -> None:
        snapshot = new_indexeddb_cursor_snapshot(
            IndexedDBOpenCursorRequest(
                direction=CURSOR_NEXT_UNIQUE,
                index="by_status",
            )
        )
        snapshot.load(
            [
                IndexedDBCursorSnapshotEntry(
                    key=["todo"], primary_key="issue-2", primary_key_value="issue-2"
                ),
                IndexedDBCursorSnapshotEntry(
                    key=["done"], primary_key="issue-3", primary_key_value="issue-3"
                ),
                IndexedDBCursorSnapshotEntry(
                    key=["todo"], primary_key="issue-1", primary_key_value="issue-1"
                ),
            ],
            KeyRange(lower=["done"], upper=["todo"]),
        )

        first = snapshot.next()
        second = snapshot.next()
        third = snapshot.next()

        self.assertIsNotNone(first)
        assert first is not None
        self.assertEqual(first.primary_key, "issue-3")
        self.assertIsNotNone(second)
        assert second is not None
        self.assertEqual(second.primary_key, "issue-1")
        self.assertIsNone(third)

    def test_index_range_accepts_scalar_entry_keys(self) -> None:
        snapshot = new_indexeddb_cursor_snapshot(
            IndexedDBOpenCursorRequest(index="by_status")
        )
        snapshot.load(
            [
                IndexedDBCursorSnapshotEntry(
                    key="done", primary_key="issue-2", primary_key_value="issue-2"
                ),
                IndexedDBCursorSnapshotEntry(
                    key="active", primary_key="issue-1", primary_key_value="issue-1"
                ),
            ],
            only("active"),
        )

        first = snapshot.next()
        second = snapshot.next()

        assert first is not None
        self.assertEqual(first.primary_key, "issue-1")
        self.assertEqual(first.key, "active")
        self.assertIsNone(second)

    def test_advance_from_current_position_moves_exactly_count_entries(self) -> None:
        snapshot = new_indexeddb_cursor_snapshot(IndexedDBOpenCursorRequest())
        snapshot.load(
            [
                IndexedDBCursorSnapshotEntry(key="a", primary_key="a"),
                IndexedDBCursorSnapshotEntry(key="b", primary_key="b"),
                IndexedDBCursorSnapshotEntry(key="c", primary_key="c"),
            ]
        )

        first = snapshot.next()
        second = snapshot.advance(1)
        third = snapshot.advance(1)

        assert first is not None
        assert second is not None
        assert third is not None
        self.assertEqual(first.primary_key, "a")
        self.assertEqual(second.primary_key, "b")
        self.assertEqual(third.primary_key, "c")
