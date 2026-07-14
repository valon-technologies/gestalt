import unittest
from unittest.mock import patch

from gestalt.migrations import (
    MigrationError,
    MigrationRunOptions,
    Revision,
    SchemaDeclaration,
    SchemaRevision,
    StoreDeclaration,
    configure_migrations,
    provider_migration_ledger_store,
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

    def test_provider_migration_ledger_store(self) -> None:
        self.assertEqual(provider_migration_ledger_store("gIssues"), "g_issues_migrations")
        self.assertEqual(provider_migration_ledger_store("dealHub"), "deal_hub_migrations")
        self.assertEqual(provider_migration_ledger_store("deal-hub"), "deal_hub_migrations")
        self.assertEqual(provider_migration_ledger_store("@scope/gIssues"), "g_issues_migrations")
        self.assertEqual(
            provider_migration_ledger_store("github.com/foo/myApp"),
            "my_app_migrations",
        )
        self.assertEqual(provider_migration_ledger_store("my  app"), "my__app_migrations")
        self.assertEqual(provider_migration_ledger_store("   "), "_gestalt_migrations")

    def test_configure_migrations_derives_per_provider_ledger_store(self) -> None:
        class Provider:
            def migration_options(self, _name: str, _config: dict) -> MigrationRunOptions:
                return MigrationRunOptions(
                    revisions=[init_revision("gIssues/0001_init", "widgets")]
                )

        db = FakeDB()

        class FakeIndexedDB:
            def __init__(self, _binding: str = "") -> None:
                self._db = db

            def object_store(self, name: str) -> FakeStore:
                return self._db.object_store(name)

            def create_object_store(self, name: str, schema=None) -> FakeStore:
                return self._db.create_object_store(name, schema)

            def delete_object_store(self, name: str) -> None:
                self._db.delete_object_store(name)

            def create_index(self, store: str, index) -> None:
                self._db.create_index(store, index)

            def delete_index(self, store: str, name: str) -> None:
                self._db.delete_index(store, name)

            def close(self) -> None:
                self._db.close()

        with patch("gestalt.migrations.IndexedDB", FakeIndexedDB):
            configure_migrations(Provider(), "gIssues", {"indexeddb": "main-db"})

        self.assertEqual(
            db.object_store("g_issues_migrations").get_all_keys(),
            ["gIssues/0001_init"],
        )
        self.assertNotIn("_gestalt_migrations", db.stores)


if __name__ == "__main__":
    unittest.main()
