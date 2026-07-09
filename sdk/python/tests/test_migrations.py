import unittest

from gestalt.migrations import (
    MigrationError,
    MigrationRunOptions,
    Revision,
    SchemaDeclaration,
    SchemaRevision,
    StoreDeclaration,
    run_migrations,
)


class FakeStore:
    def __init__(self) -> None:
        self.rows: dict[str, dict] = {}

    def get_all_keys(self) -> list[str]:
        return list(self.rows)

    def put(self, record: dict) -> None:
        key = record.get("id") or record.get("revision_id")
        if not isinstance(key, str) or not key:
            raise RuntimeError("missing key")
        self.rows[key] = record


class FakeDB:
    def __init__(self) -> None:
        self.stores: dict[str, FakeStore] = {}
        self.calls: list[str] = []
        self.fail_store: str | None = None

    def create_object_store(self, name: str, schema=None) -> FakeStore:
        self.calls.append(f"create:{name}")
        if self.fail_store == name:
            raise RuntimeError("boom")
        return self.object_store(name)

    def delete_object_store(self, name: str) -> None:
        self.stores.pop(name, None)

    def create_index(self, store: str, index) -> None:
        _ = store, index

    def delete_index(self, store: str, name: str) -> None:
        _ = store, name

    def object_store(self, name: str) -> FakeStore:
        if name not in self.stores:
            self.stores[name] = FakeStore()
        return self.stores[name]

    def close(self) -> None:
        return None


def init_revision(revision_id: str, store: str) -> SchemaRevision:
    return SchemaRevision(
        id=revision_id,
        schema=SchemaDeclaration(stores=[StoreDeclaration(name=store)]),
    )


def _data_store_calls(calls: list[str]) -> list[str]:
    return [call for call in calls if call != "create:_gestalt_migrations"]


class MigrationTests(unittest.TestCase):
    def test_fresh_install_and_restart(self) -> None:
        db = FakeDB()
        revisions: list[Revision] = [init_revision("0001_init", "widgets")]
        result = run_migrations(db, MigrationRunOptions(revisions=revisions))
        self.assertEqual(result.applied, ["0001_init"])
        self.assertEqual(result.head, "0001_init")
        self.assertIn("widgets", db.stores)

        calls_before = _data_store_calls(db.calls)
        result = run_migrations(db, MigrationRunOptions(revisions=revisions))
        self.assertEqual(result.applied, [])
        self.assertEqual(_data_store_calls(db.calls), calls_before)

    def test_failing_revision_is_not_recorded(self) -> None:
        db = FakeDB()
        db.fail_store = "widgets"
        with self.assertRaises(MigrationError):
            run_migrations(
                db,
                MigrationRunOptions(revisions=[init_revision("0001_init", "widgets")]),
            )
        self.assertEqual(db.object_store("_gestalt_migrations").get_all_keys(), [])

    def test_ledger_ahead_of_code(self) -> None:
        db = FakeDB()
        revisions: list[Revision] = [init_revision("0001_init", "widgets")]
        run_migrations(db, MigrationRunOptions(revisions=revisions))
        db.object_store("_gestalt_migrations").put(
            {
                "revision_id": "0002_future",
                "id": "0002_future",
                "applied_at": "2026-01-01T00:00:00Z",
            }
        )
        with self.assertRaisesRegex(MigrationError, "ledger is ahead of code"):
            run_migrations(db, MigrationRunOptions(revisions=revisions))

    def test_ignores_other_provider_ledger_rows_on_shared_db(self) -> None:
        db = FakeDB()
        run_migrations(
            db,
            MigrationRunOptions(
                revisions=[init_revision("authorization/indexeddb/0001_init", "relationships")]
            ),
        )
        run_migrations(
            db,
            MigrationRunOptions(
                revisions=[init_revision("auth/oidc/0001_init", "grants")]
            ),
        )

    def test_ignores_deeper_namespace_ledger_rows_on_shared_db(self) -> None:
        db = FakeDB()
        run_migrations(
            db,
            MigrationRunOptions(
                revisions=[init_revision("auth/oidc/nested/0001_init", "nested")]
            ),
        )
        run_migrations(
            db,
            MigrationRunOptions(
                revisions=[init_revision("auth/oidc/0001_init", "grants")]
            ),
        )

    def test_duplicate_revision_ids(self) -> None:
        db = FakeDB()
        revisions: list[Revision] = [
            init_revision("0001_init", "widgets"),
            init_revision("0001_init", "gadgets"),
        ]
        with self.assertRaises(MigrationError):
            run_migrations(db, MigrationRunOptions(revisions=revisions))


if __name__ == "__main__":
    unittest.main()
