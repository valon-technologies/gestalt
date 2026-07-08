from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass, field
from typing import Any

import pytest

from gestalt._indexeddb import (
    AlreadyExistsError,
    IndexSchema,
    NotFoundError,
    ObjectStoreSchema,
)
from gestalt.migrations import (
    AddIndexDeclaration,
    BackfillRevision,
    BackfillTransform,
    ColumnSchema,
    MigrationError,
    MigrationRunOptions,
    Revision,
    SchemaDeclaration,
    SchemaRevision,
    StoreDeclaration,
    run_migrations,
)


@dataclass
class FakeStoreState:
    pk: str
    rows: dict[str, dict[str, Any]] = field(default_factory=dict)


class FakeIndexedDB:
    def __init__(self) -> None:
        self.stores: dict[str, FakeStoreState] = {}
        self.indexes: set[str] = set()
        self.calls: list[str] = []
        self.create_object_store_error: Exception | None = None

    def create_object_store(
        self,
        name: str,
        schema: ObjectStoreSchema | None = None,
    ) -> Any:
        self.calls.append(f"createObjectStore:{name}")
        if self.create_object_store_error is not None:
            raise self.create_object_store_error
        if name in self.stores:
            raise AlreadyExistsError()
        pk = "id"
        if schema and schema.columns:
            for column in schema.columns:
                if column.primary_key:
                    pk = column.name
                    break
        self.stores[name] = FakeStoreState(pk=pk)
        for index in (schema.indexes if schema else []):
            self.indexes.add(f"{name}/{index.name}")
        return self.object_store(name)

    def delete_object_store(self, name: str) -> None:
        self.calls.append(f"deleteObjectStore:{name}")
        if name not in self.stores:
            raise NotFoundError()
        del self.stores[name]

    def create_index(self, store: str, index: IndexSchema) -> None:
        self.calls.append(f"createIndex:{store}/{index.name}")
        key = f"{store}/{index.name}"
        if key in self.indexes:
            raise AlreadyExistsError()
        self.indexes.add(key)

    def delete_index(self, store: str, name: str) -> None:
        self.calls.append(f"deleteIndex:{store}/{name}")
        key = f"{store}/{name}"
        if key not in self.indexes:
            raise NotFoundError()
        self.indexes.remove(key)

    def object_store(self, name: str) -> Any:
        db = self

        def state() -> FakeStoreState:
            found = db.stores.get(name)
            if found is None:
                raise NotFoundError(f"store {name} missing")
            return found

        class FakeObjectStore:
            def get_all_keys(
                self, query: Any = None, *, count: int | None = None
            ) -> list[str]:
                _ = query, count
                return list(state().rows.keys())

            def get_all(
                self, query: Any = None, *, count: int | None = None
            ) -> list[dict[str, Any]]:
                _ = query, count
                return list(state().rows.values())

            def get(self, id: str) -> dict[str, Any]:
                row = state().rows.get(id)
                if row is None:
                    raise NotFoundError()
                return row

            def put(self, record: dict[str, Any]) -> None:
                store_state = state()
                store_state.rows[str(record[store_state.pk])] = record

            def delete(self, id: str) -> None:
                state().rows.pop(id, None)

            def open_cursor(self, query: Any = None, direction: int = 0) -> Any:
                _ = query, direction
                store_state = state()
                entries = list(store_state.rows.items())
                index = -1

                class FakeCursor:
                    key: Any = None
                    primary_key: str = ""
                    value: dict[str, Any] | None = None
                    done = False

                    def continue_(self) -> bool:
                        nonlocal index
                        index += 1
                        entry = entries[index] if index < len(entries) else None
                        if entry is None:
                            self.done = True
                            self.value = None
                            self.primary_key = ""
                            return False
                        self.primary_key = entry[0]
                        self.value = entry[1]
                        return True

                    def close(self) -> None:
                        return None

                return FakeCursor()

        return FakeObjectStore()

    def close(self) -> None:
        return None


def fake_db() -> tuple[Any, FakeIndexedDB]:
    fake = FakeIndexedDB()
    return fake, fake


ISSUES_REVISION = SchemaRevision(
    id="0001_issues",
    schema=SchemaDeclaration(
        stores=[
            StoreDeclaration(
                name="issues",
                columns=[
                    ColumnSchema(name="id", primary_key=True, not_null=True),
                    ColumnSchema(name="payload", not_null=True),
                ],
            )
        ]
    ),
)


def ledger_ids(fake: FakeIndexedDB) -> list[str]:
    return list(fake.stores.get("_gestalt_migrations", FakeStoreState(pk="id")).rows.keys())


def test_fresh_install_applies_revision_and_records_ledger() -> None:
    db, fake = fake_db()
    run_migrations(db, MigrationRunOptions(revisions=[ISSUES_REVISION]))

    assert "issues" in fake.stores
    assert ledger_ids(fake) == ["0001_issues"]


def test_returns_applied_ids_and_declared_head() -> None:
    db, _fake = fake_db()
    second = SchemaRevision(
        id="0002_more",
        schema=SchemaDeclaration(
            stores=[
                StoreDeclaration(
                    name="more",
                    columns=[ColumnSchema(name="id", primary_key=True)],
                )
            ]
        ),
    )

    first = run_migrations(db, MigrationRunOptions(revisions=[ISSUES_REVISION, second]))
    assert first.applied == ["0001_issues", "0002_more"]
    assert first.head == "0002_more"

    again = run_migrations(db, MigrationRunOptions(revisions=[ISSUES_REVISION, second]))
    assert again.applied == []
    assert again.head == "0002_more"


def test_restart_is_noop_and_does_not_recreate_stores() -> None:
    db, fake = fake_db()
    run_migrations(db, MigrationRunOptions(revisions=[ISSUES_REVISION]))
    creates_before = sum(
        1 for call in fake.calls if call.startswith("createObjectStore:issues")
    )

    run_migrations(db, MigrationRunOptions(revisions=[ISSUES_REVISION]))
    creates_after = sum(
        1 for call in fake.calls if call.startswith("createObjectStore:issues")
    )

    assert creates_after == creates_before
    assert ledger_ids(fake) == ["0001_issues"]


def test_adds_second_revision_on_existing_ledger() -> None:
    db, fake = fake_db()
    run_migrations(db, MigrationRunOptions(revisions=[ISSUES_REVISION]))

    add_index = SchemaRevision(
        id="0002_index",
        schema=SchemaDeclaration(
            add_indexes=[
                AddIndexDeclaration(
                    store="issues",
                    index=IndexSchema(name="by_status", key_path=["status"]),
                )
            ]
        ),
    )
    run_migrations(db, MigrationRunOptions(revisions=[ISSUES_REVISION, add_index]))

    assert "issues/by_status" in fake.indexes
    assert ledger_ids(fake) == ["0001_issues", "0002_index"]


def test_rejects_backfill_whose_from_equals_into() -> None:
    db, _fake = fake_db()
    in_place = BackfillRevision(
        id="0001_inplace",
        backfill=BackfillTransform(
            from_store="issues",
            into="issues",
            value=lambda row: {**row, "status": row.get("status", "open")},
        ),
    )

    with pytest.raises(MigrationError, match='"from" and "into" must differ'):
        run_migrations(db, MigrationRunOptions(revisions=[in_place]))


def test_backfill_revision_copies_rows_into_another_store() -> None:
    db, fake = fake_db()
    seed = SchemaRevision(
        id="0001_seed",
        schema=SchemaDeclaration(
            stores=[
                StoreDeclaration(
                    name="issues",
                    columns=[ColumnSchema(name="id", primary_key=True)],
                ),
                StoreDeclaration(
                    name="issue_index",
                    columns=[ColumnSchema(name="id", primary_key=True)],
                ),
            ]
        ),
    )
    backfill = BackfillRevision(
        id="0002_index",
        backfill=BackfillTransform(
            from_store="issues",
            into="issue_index",
            value=lambda row: {"id": row["id"], "text": f"issue-{row['id']}"},
        ),
    )

    run_migrations(db, MigrationRunOptions(revisions=[seed]))
    db.object_store("issues").put({"id": "a"})
    run_migrations(db, MigrationRunOptions(revisions=[seed, backfill]))

    assert db.object_store("issue_index").get("a")["text"] == "issue-a"
    assert db.object_store("issues").get("a")["id"] == "a"
    assert ledger_ids(fake) == ["0001_seed", "0002_index"]


def test_failing_revision_aborts_and_is_not_recorded() -> None:
    db, fake = fake_db()
    boom = BackfillRevision(
        id="0002_boom",
        backfill=BackfillTransform(
            from_store="missing_src",
            into="missing_dst",
            value=lambda row: row,
        ),
    )

    with pytest.raises(MigrationError):
        run_migrations(db, MigrationRunOptions(revisions=[ISSUES_REVISION, boom]))

    assert ledger_ids(fake) == ["0001_issues"]


def test_failure_after_earlier_revision_reports_current() -> None:
    db, _fake = fake_db()
    boom = BackfillRevision(
        id="0002_boom",
        backfill=BackfillTransform(
            from_store="missing_src",
            into="missing_dst",
            value=lambda row: row,
        ),
    )

    with pytest.raises(MigrationError) as exc_info:
        run_migrations(db, MigrationRunOptions(revisions=[ISSUES_REVISION, boom]))

    error = exc_info.value
    assert error.current == "0001_issues"
    assert error.attempted == "0002_boom"


def test_concurrent_runners_converge_without_lock() -> None:
    db, fake = fake_db()
    seed = SchemaRevision(
        id="0001_seed",
        schema=SchemaDeclaration(
            stores=[
                StoreDeclaration(
                    name="issues",
                    columns=[ColumnSchema(name="id", primary_key=True)],
                )
            ]
        ),
    )
    run_migrations(db, MigrationRunOptions(revisions=[seed]))
    db.object_store("issues").put({"id": "a"})
    db.object_store("issues").put({"id": "b", "status": "closed"})

    backfill = BackfillRevision(
        id="0002_index",
        backfill=BackfillTransform(
            from_store="issues",
            into="issue_index",
            value=lambda row: {"id": row["id"], "text": f"issue-{row['id']}"},
        ),
    )
    full: list[Revision] = [
        seed,
        SchemaRevision(
            id="0001_5_index",
            schema=SchemaDeclaration(
                stores=[
                    StoreDeclaration(
                        name="issue_index",
                        columns=[ColumnSchema(name="id", primary_key=True)],
                    )
                ]
            ),
        ),
        backfill,
    ]

    with ThreadPoolExecutor(max_workers=2) as executor:
        futures = [
            executor.submit(run_migrations, db, MigrationRunOptions(revisions=full))
            for _ in range(2)
        ]
        for future in futures:
            future.result()

    assert ledger_ids(fake) == ["0001_seed", "0001_5_index", "0002_index"]
    assert db.object_store("issue_index").get("a")["text"] == "issue-a"
    assert db.object_store("issue_index").get("b")["text"] == "issue-b"


def test_fails_closed_when_ledger_is_ahead_of_code() -> None:
    db, _fake = fake_db()
    run_migrations(db, MigrationRunOptions(revisions=[ISSUES_REVISION]))

    db.object_store("_gestalt_migrations").put(
        {
            "revision_id": "0002_future",
            "applied_at": "2026-01-01T00:00:00+00:00",
        }
    )

    with pytest.raises(MigrationError, match="ledger is ahead"):
        run_migrations(db, MigrationRunOptions(revisions=[ISSUES_REVISION]))


def test_fails_closed_when_revision_inserted_before_applied_one() -> None:
    db, _fake = fake_db()
    first = ISSUES_REVISION
    later = SchemaRevision(
        id="0003_more",
        schema=SchemaDeclaration(
            stores=[
                StoreDeclaration(
                    name="more",
                    columns=[ColumnSchema(name="id", primary_key=True)],
                )
            ]
        ),
    )
    run_migrations(db, MigrationRunOptions(revisions=[first, later]))

    inserted = SchemaRevision(
        id="0002_between",
        schema=SchemaDeclaration(
            stores=[
                StoreDeclaration(
                    name="between",
                    columns=[ColumnSchema(name="id", primary_key=True)],
                )
            ]
        ),
    )
    with pytest.raises(MigrationError, match="ledger has gaps"):
        run_migrations(db, MigrationRunOptions(revisions=[first, inserted, later]))


def test_rejects_duplicate_revision_ids() -> None:
    db, _fake = fake_db()
    with pytest.raises(MigrationError, match="duplicate revision id"):
        run_migrations(
            db,
            MigrationRunOptions(revisions=[ISSUES_REVISION, ISSUES_REVISION]),
        )


def test_rejects_revision_that_is_neither_schema_nor_backfill() -> None:
    db, _fake = fake_db()

    class BadRevision:
        id = "0001_bad"

    with pytest.raises(MigrationError, match="exactly one of"):
        run_migrations(db, MigrationRunOptions(revisions=[BadRevision()]))  # type: ignore[list-item]


def test_null_schema_with_backfill_runs_backfill_not_apply_schema() -> None:
    db, fake = fake_db()
    seed = SchemaRevision(
        id="0001_seed",
        schema=SchemaDeclaration(
            stores=[
                StoreDeclaration(
                    name="src",
                    columns=[ColumnSchema(name="id", primary_key=True)],
                ),
                StoreDeclaration(
                    name="dst",
                    columns=[ColumnSchema(name="id", primary_key=True)],
                ),
            ]
        ),
    )
    run_migrations(db, MigrationRunOptions(revisions=[seed]))
    db.object_store("src").put({"id": "a"})

    class WeirdRevision:
        id = "0002_weird"
        schema = None
        backfill = BackfillTransform(
            from_store="src",
            into="dst",
            value=lambda row: row,
        )

    run_migrations(db, MigrationRunOptions(revisions=[seed, WeirdRevision()]))  # type: ignore[list-item]

    assert db.object_store("dst").get("a")["id"] == "a"
    assert ledger_ids(fake) == ["0001_seed", "0002_weird"]
