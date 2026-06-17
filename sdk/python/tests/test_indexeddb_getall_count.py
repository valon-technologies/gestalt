"""Parity guards for IndexedDB getAll count forwarding."""

from __future__ import annotations

import inspect
import unittest

from gestalt._indexeddb import (
    Index,
    ObjectStore,
    TransactionIndex,
    TransactionObjectStore,
)


class TestGetAllCountSignature(unittest.TestCase):
    def test_object_store_exposes_count_keyword(self) -> None:
        for method in (ObjectStore.get_all, ObjectStore.get_all_keys):
            params = inspect.signature(method).parameters
            self.assertIn("count", params)
            self.assertEqual(params["count"].kind, inspect.Parameter.KEYWORD_ONLY)

    def test_index_exposes_count_keyword(self) -> None:
        for method in (Index.get_all, Index.get_all_keys):
            params = inspect.signature(method).parameters
            self.assertIn("count", params)
            self.assertEqual(params["count"].kind, inspect.Parameter.KEYWORD_ONLY)

    def test_transaction_scoped_exposes_count_keyword(self) -> None:
        for cls in (TransactionObjectStore, TransactionIndex):
            for method in (cls.get_all, cls.get_all_keys):
                params = inspect.signature(method).parameters
                self.assertIn("count", params)
                self.assertEqual(params["count"].kind, inspect.Parameter.KEYWORD_ONLY)


if __name__ == "__main__":
    unittest.main()
