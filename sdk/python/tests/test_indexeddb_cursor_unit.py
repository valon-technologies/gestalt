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


class TestIndexPaginateHelpers(unittest.TestCase):
    def test_paginate_index_get_all_uses_get_all_count(self) -> None:
        from gestalt import paginate_index_get_all

        rows = [
            {"id": "a", "record_id": "a", "run_id": "run-1", "recorded_at": "t1"},
            {"id": "b", "record_id": "b", "run_id": "run-1", "recorded_at": "t2"},
            {"id": "c", "record_id": "c", "run_id": "run-1", "recorded_at": "t3"},
        ]
        seen_count: list[int | None] = []

        class _Reader:
            def get_all(self, _query, *, count=None):
                seen_count.append(count)
                limit = count if count is not None else len(rows)
                return rows[:limit]

        page = paginate_index_get_all(
            _Reader(),
            None,
            limit=2,
            index_key_path=["run_id", "recorded_at", "record_id"],
        )

        self.assertEqual(seen_count, [3])
        self.assertTrue(page.has_more)
        self.assertEqual([row["id"] for row in page.items], ["a", "b"])
        self.assertIsNotNone(page.next_cursor)
