import dataclasses
from enum import Enum
from typing import Any


class DatastoreCapability(str, Enum):
    KEY_VALUE = "key_value"
    SQL = "sql"
    BLOB_STORE = "blob_store"


# ---------------------------------------------------------------------------
# Data types
# ---------------------------------------------------------------------------


@dataclasses.dataclass(slots=True)
class KVEntry:
    key: str = ""
    value: bytes = b""


@dataclasses.dataclass(slots=True)
class SQLRows:
    columns: list[str] = dataclasses.field(default_factory=list)
    rows: list[list[Any]] = dataclasses.field(default_factory=list)


@dataclasses.dataclass(slots=True)
class SQLExecResult:
    rows_affected: int = 0
    last_insert_id: int = 0


@dataclasses.dataclass(slots=True)
class SQLMigration:
    version: int
    description: str
    up_sql: str


@dataclasses.dataclass(slots=True)
class BlobEntry:
    key: str = ""
    size: int = 0
    content_type: str = ""
    last_modified: float | None = None


# ---------------------------------------------------------------------------
# Consumer interfaces (no namespace -- host injects it)
# ---------------------------------------------------------------------------


class KeyValueStore:
    def get(self, key: str) -> bytes | None:
        raise NotImplementedError

    def put(self, key: str, value: bytes) -> None:
        raise NotImplementedError

    def delete(self, key: str) -> None:
        raise NotImplementedError

    def list(self, _prefix: str = "") -> list[KVEntry]:
        raise NotImplementedError


class SQLStore:
    def query(self, _sql: str, _args: list[Any] | None = None) -> SQLRows:
        raise NotImplementedError

    def exec(self, _sql: str, _args: list[Any] | None = None) -> SQLExecResult:
        raise NotImplementedError

    def migrate(self, _migrations: list[SQLMigration]) -> None:
        raise NotImplementedError


class BlobStore:
    def get(self, _key: str) -> tuple[bytes, str] | None:
        raise NotImplementedError

    def put(self, _key: str, _data: bytes, _content_type: str = "") -> None:
        raise NotImplementedError

    def delete(self, _key: str) -> None:
        raise NotImplementedError

    def list(self, _prefix: str = "") -> list[BlobEntry]:
        raise NotImplementedError


# ---------------------------------------------------------------------------
# Provider interfaces (namespace-aware, implemented by resource providers)
# ---------------------------------------------------------------------------


class DatastoreProvider:
    def configure(self, _name: str, _config: dict[str, Any]) -> None:
        pass


class KeyValueDatastoreProvider(DatastoreProvider):
    def kv_get(self, _namespace: str, _key: str) -> bytes | None:
        raise NotImplementedError

    def kv_put(self, _namespace: str, _key: str, _value: bytes, _ttl_seconds: int = 0) -> None:
        raise NotImplementedError

    def kv_delete(self, _namespace: str, _key: str) -> None:
        raise NotImplementedError

    def kv_list(self, _namespace: str, _prefix: str = "", _cursor: str = "", _limit: int = 100) -> list[KVEntry]:
        raise NotImplementedError

    def kv_migrate(self, _namespace: str) -> None:
        raise NotImplementedError


class SQLDatastoreProvider(DatastoreProvider):
    def sql_query(self, _namespace: str, _sql: str, _args: list[Any] | None = None) -> SQLRows:
        raise NotImplementedError

    def sql_exec(self, _namespace: str, _sql: str, _args: list[Any] | None = None) -> SQLExecResult:
        raise NotImplementedError

    def sql_migrate(self, _namespace: str, _migrations: list[SQLMigration]) -> None:
        raise NotImplementedError


class BlobStoreDatastoreProvider(DatastoreProvider):
    def blob_get(self, _namespace: str, _key: str) -> tuple[bytes, str] | None:
        raise NotImplementedError

    def blob_put(self, _namespace: str, _key: str, _data: bytes, _content_type: str = "") -> None:
        raise NotImplementedError

    def blob_delete(self, _namespace: str, _key: str) -> None:
        raise NotImplementedError

    def blob_list(self, _namespace: str, _prefix: str = "") -> list[BlobEntry]:
        raise NotImplementedError
