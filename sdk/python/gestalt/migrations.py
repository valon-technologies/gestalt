"""Declarative IndexedDB migrations for provider processes."""

from __future__ import annotations

import datetime as dt
import json
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any, Protocol, runtime_checkable

from ._indexeddb import (
    AlreadyExistsError,
    ColumnDef,
    IndexedDB,
    IndexSchema,
    NotFoundError,
    ObjectStoreSchema,
)

DEFAULT_LEDGER_STORE = "_gestalt_migrations"
LEDGER_KEY_COLUMN = "revision_id"
LEDGER_APPLIED_COLUMN = "applied_at"

Record = dict[str, Any]


@dataclass
class ColumnSchema:
    """Column definition for an object store schema."""

    name: str
    primary_key: bool = False
    not_null: bool = False
    unique: bool = False


@dataclass
class StoreDeclaration:
    """Declarative schema for one object store."""

    name: str
    columns: list[ColumnSchema] | None = None
    indexes: list[IndexSchema] | None = None


@dataclass
class AddIndexDeclaration:
    """Index to add to an already-existing store."""

    store: str
    index: IndexSchema


@dataclass
class IndexRef:
    """Reference to an index to remove."""

    store: str
    name: str


@dataclass
class SchemaDeclaration:
    """Declarative schema revision delta."""

    stores: list[StoreDeclaration] | None = None
    add_indexes: list[AddIndexDeclaration] | None = None
    drop_stores: list[str] | None = None
    drop_indexes: list[IndexRef] | None = None


@dataclass
class SchemaRevision:
    """A schema-only migration revision."""

    id: str
    schema: SchemaDeclaration


@dataclass
class BackfillTransform:
    """Copies rows from one store into another."""

    from_store: str
    into: str
    value: Callable[[Record], Record]


@dataclass
class BackfillRevision:
    """A backfill migration revision."""

    id: str
    backfill: BackfillTransform


Revision = SchemaRevision | BackfillRevision


@dataclass
class MigrationRunOptions:
    """Options for one migration run."""

    revisions: list[Revision]
    db_binding: str | None = None
    ledger_store: str | None = None


MigrationsOption = list[Revision] | MigrationRunOptions


@dataclass(frozen=True)
class MigrationResult:
    """Outcome of a migration run."""

    applied: list[str]
    head: str


class MigrationError(Exception):
    """Raised when migrations cannot proceed."""

    def __init__(
        self,
        message: str,
        *,
        current: str | None = None,
        attempted: str | None = None,
        cause: BaseException | None = None,
    ) -> None:
        super().__init__(message)
        self.current = current
        self.attempted = attempted
        if cause is not None:
            self.__cause__ = cause


@runtime_checkable
class MigrationDB(Protocol):
    """Minimal IndexedDB surface used by migrations."""

    def create_object_store(
        self, name: str, schema: ObjectStoreSchema | None = None
    ) -> Any:
        """Create an object store."""

    def create_index(self, store: str, index: IndexSchema) -> None:
        """Add a secondary index to a store."""

    def delete_object_store(self, name: str) -> None:
        """Delete an object store."""

    def delete_index(self, store: str, name: str) -> None:
        """Delete a secondary index."""

    def object_store(self, name: str) -> Any:
        """Return a store-bound client."""

    def close(self) -> None:
        """Close the database client."""


def run_migrations(db: MigrationDB, options: MigrationRunOptions) -> MigrationResult:
    """Bring the database up to the declared revision head."""

    revisions = _validate_revisions(options.revisions)
    if not revisions:
        return MigrationResult(applied=[], head="")

    ledger_store = (options.ledger_store or "").strip() or DEFAULT_LEDGER_STORE
    _ensure_ledger_store(db, ledger_store)

    applied = set(db.object_store(ledger_store).get_all_keys())
    _assert_not_ahead_of_code(revisions, applied)
    _assert_contiguous_prefix(revisions, applied)

    attempted_head = revisions[-1].id if revisions else ""
    current = _latest_applied_id(revisions, applied)
    applied_now: list[str] = []
    for revision in revisions:
        if revision.id in applied:
            continue
        try:
            _apply_revision(db, revision)
            _record_revision(db, ledger_store, revision.id)
            applied_now.append(revision.id)
            current = revision.id
        except MigrationError:
            raise
        except Exception as error:
            raise MigrationError(
                f"migration {json.dumps(revision.id)} failed: {_error_text(error)}",
                current=current,
                attempted=attempted_head,
                cause=error,
            ) from error
    return MigrationResult(applied=applied_now, head=attempted_head)


def normalize_migrations(
    input: MigrationsOption | None,
) -> MigrationRunOptions | None:
    """Normalize app- or provider-declared migration options."""

    if input is None:
        return None
    if isinstance(input, MigrationRunOptions):
        return input if input.revisions else None
    if isinstance(input, list):
        return MigrationRunOptions(revisions=input) if input else None
    return None


def configure_migrations(provider: Any, name: str, config: dict[str, Any]) -> None:
    """Run declared migrations before provider configure."""

    migration_options = getattr(provider, "migration_options", None)
    if not callable(migration_options):
        return

    options = normalize_migrations(migration_options(name, config))
    if options is None:
        return

    binding = (options.db_binding or "").strip()
    if not binding:
        raw_binding = config.get("indexeddb")
        binding = raw_binding.strip() if isinstance(raw_binding, str) else ""

    db = IndexedDB(binding)
    try:
        run_migrations(db, options)
    finally:
        db.close()


def _validate_revisions(revisions: list[Revision]) -> list[Revision]:
    seen: set[str] = set()
    for revision in revisions:
        revision_id = (revision.id or "").strip()
        if not revision_id:
            raise MigrationError("every revision needs a non-empty id")
        if revision_id in seen:
            raise MigrationError(f"duplicate revision id {json.dumps(revision_id)}")
        seen.add(revision_id)

        schema = getattr(revision, "schema", None)
        backfill = getattr(revision, "backfill", None)
        has_schema = schema is not None
        has_backfill = backfill is not None
        if has_schema == has_backfill:
            raise MigrationError(
                f'revision {json.dumps(revision_id)} must declare exactly one of '
                f'"schema" or "backfill"'
            )
        if (
            has_backfill
            and backfill is not None
            and backfill.from_store == backfill.into
        ):
            raise MigrationError(
                f'revision {json.dumps(revision_id)} backfill "from" and "into" must '
                f"differ: a backfill reads an immutable source and writes a distinct "
                f"target, so it cannot read its own output and is idempotent by "
                f"construction"
            )
    return revisions


def _assert_not_ahead_of_code(
    revisions: list[Revision],
    applied: set[str],
) -> None:
    declared = {revision.id for revision in revisions}
    unknown = [revision_id for revision_id in applied if revision_id not in declared]
    if not unknown:
        return
    attempted_head = revisions[-1].id if revisions else None
    raise MigrationError(
        "ledger is ahead of code: applied revision(s) "
        f"{', '.join(json.dumps(revision_id) for revision_id in unknown)} are not "
        f"declared by this binary. Roll forward to a binary that defines them, or "
        f"manually undo them and delete their ledger rows.",
        current=unknown[-1],
        attempted=attempted_head,
    )


def _assert_contiguous_prefix(
    revisions: list[Revision],
    applied: set[str],
) -> None:
    saw_unapplied = False
    out_of_order: list[str] = []
    for revision in revisions:
        if revision.id in applied:
            if saw_unapplied:
                out_of_order.append(revision.id)
        else:
            saw_unapplied = True
    if not out_of_order:
        return
    raise MigrationError(
        "ledger has gaps: revision(s) "
        f"{', '.join(json.dumps(revision_id) for revision_id in out_of_order)} are "
        f"applied but an earlier declared revision is not. Revisions are an "
        f"append-only ledger — a new revision must be added after all applied ones, "
        f"never inserted or reordered before them.",
        attempted=revisions[-1].id if revisions else "",
    )


def _latest_applied_id(
    revisions: list[Revision],
    applied: set[str],
) -> str | None:
    current: str | None = None
    for revision in revisions:
        if revision.id in applied:
            current = revision.id
    return current


def _ensure_ledger_store(db: MigrationDB, ledger_store: str) -> None:
    _create_store_if_absent(
        db,
        StoreDeclaration(
            name=ledger_store,
            columns=[
                ColumnSchema(
                    name=LEDGER_KEY_COLUMN,
                    primary_key=True,
                    not_null=True,
                ),
                ColumnSchema(name=LEDGER_APPLIED_COLUMN, not_null=True),
            ],
        ),
    )


def _apply_revision(db: MigrationDB, revision: Revision) -> None:
<<<<<<< HEAD
    schema = getattr(revision, "schema", None)
    if schema is not None:
        _apply_schema(db, schema)
        return
    backfill = getattr(revision, "backfill", None)
    if backfill is not None:
        _apply_backfill(db, backfill)
        return
    raise MigrationError(
        f'revision {json.dumps(getattr(revision, "id", ""))} must declare exactly one of '
=======
    if isinstance(revision, SchemaRevision):
        _apply_schema(db, revision.schema)
        return
    if isinstance(revision, BackfillRevision):
        _apply_backfill(db, revision.backfill)
        return
    raise MigrationError(
        f'revision {json.dumps(revision.id)} must declare exactly one of '
>>>>>>> b7b7bfe0e (Trim migration SDK surface and fix Python CI regressions.)
        f'"schema" or "backfill"'
    )


def _apply_backfill(db: MigrationDB, transform: BackfillTransform) -> None:
    target = db.object_store(transform.into)
    cursor = db.object_store(transform.from_store).open_cursor()
    try:
        while cursor.continue_():
            row = cursor.value
            if row is None:
                continue
            target.put(transform.value(row))
    finally:
        cursor.close()


def _apply_schema(db: MigrationDB, schema: SchemaDeclaration) -> None:
    for store in schema.stores or []:
        _create_store_if_absent(db, store)
    for entry in schema.add_indexes or []:
        _create_index_if_absent(db, entry.store, entry.index)
    for entry in schema.drop_indexes or []:
        _drop_index_if_present(db, entry.store, entry.name)
    for name in schema.drop_stores or []:
        _drop_store_if_present(db, name)


def _create_store_if_absent(db: MigrationDB, store: StoreDeclaration) -> None:
    schema = ObjectStoreSchema(
        indexes=list(store.indexes or []),
        columns=[
            _indexeddb_column(column) for column in (store.columns or [])
        ],
    )
    try:
        db.create_object_store(store.name, schema)
    except AlreadyExistsError:
        pass


def _create_index_if_absent(
    db: MigrationDB,
    store: str,
    index: IndexSchema,
) -> None:
    try:
        db.create_index(store, index)
    except AlreadyExistsError:
        pass


def _drop_store_if_present(db: MigrationDB, name: str) -> None:
    try:
        db.delete_object_store(name)
    except NotFoundError:
        pass


def _drop_index_if_present(db: MigrationDB, store: str, name: str) -> None:
    try:
        db.delete_index(store, name)
    except NotFoundError:
        pass


def _record_revision(db: MigrationDB, ledger_store: str, revision_id: str) -> None:
    record = {
        LEDGER_KEY_COLUMN: revision_id,
        LEDGER_APPLIED_COLUMN: dt.datetime.now(dt.timezone.utc).isoformat(),
    }
    if LEDGER_KEY_COLUMN != "id":
        record["id"] = revision_id
    db.object_store(ledger_store).put(record)


def _indexeddb_column(column: ColumnSchema) -> ColumnDef:
    return ColumnDef(
        name=column.name,
        primary_key=column.primary_key,
        not_null=column.not_null,
        unique=column.unique,
    )


def _error_text(error: BaseException) -> str:
    return str(error)
