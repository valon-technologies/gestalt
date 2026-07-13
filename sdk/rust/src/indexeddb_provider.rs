use std::cmp::Ordering;
use std::collections::BTreeMap;

use hyper_util::rt::TokioIo;
use tokio::sync::mpsc;
use tokio_stream::wrappers::ReceiverStream;
use tonic::Request;
use tonic::codegen::async_trait;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, ClientTlsConfig, Endpoint, Uri};
use tower::service_fn;

use crate::env::{
    ENV_HOST_SERVICE_SOCKET, ENV_HOST_SERVICE_TOKEN, HOST_SERVICE_BINDING_HEADER,
    host_service_configured,
};
use crate::generated::v1::{self as pb, indexed_db_client::IndexedDbClient};

type IndexedDbTransport = InterceptedService<Channel, RelayTokenInterceptor>;

const INDEXEDDB_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";
const CURSOR_CHANNEL_BUFFER: usize = 1;
const TRANSACTION_CHANNEL_BUFFER: usize = 1;

#[derive(Debug, thiserror::Error)]
/// Errors returned by the IndexedDB transport client.
pub enum IndexedDBError {
    /// The requested record, object store, index, or cursor entry was missing.
    #[error("not found")]
    NotFound,
    /// A create operation conflicted with an existing value.
    #[error("already exists")]
    AlreadyExists,
    /// A cursor was opened in key-only mode and a value was requested.
    #[error("cursor is keys-only; value not available")]
    KeysOnly,
    /// A provider-side helper received invalid input.
    #[error("{0}")]
    InvalidArgument(String),
    /// An explicit transaction failed or was already closed.
    #[error("{0}")]
    Transaction(String),
    /// The host-service transport could not be created.
    #[error("{0}")]
    Transport(#[from] tonic::transport::Error),
    /// The host-service RPC returned a gRPC status.
    #[error("{0}")]
    Status(#[from] tonic::Status),
    /// Required environment or target configuration was invalid.
    #[error("{0}")]
    Env(String),
}
/// JSON-like value stored in an object store row.
pub type Record = BTreeMap<String, serde_json::Value>;

/// One IndexedDB key: number, date, string, binary, or array of keys.
#[derive(Debug, Clone, PartialEq)]
pub enum Key {
    /// Integer number key.
    Int(i64),
    /// Floating-point number key.
    Float(f64),
    /// String key.
    Str(String),
    /// Date key.
    Date(std::time::SystemTime),
    /// Binary key.
    Bytes(Vec<u8>),
    /// Composite array key.
    Array(Vec<Key>),
}

impl From<&str> for Key {
    fn from(value: &str) -> Self {
        Self::Str(value.to_string())
    }
}

impl From<String> for Key {
    fn from(value: String) -> Self {
        Self::Str(value)
    }
}

impl From<i64> for Key {
    fn from(value: i64) -> Self {
        Self::Int(value)
    }
}

impl From<i32> for Key {
    fn from(value: i32) -> Self {
        Self::Int(i64::from(value))
    }
}

impl From<u64> for Key {
    fn from(value: u64) -> Self {
        Self::Int(value as i64)
    }
}

impl From<f64> for Key {
    fn from(value: f64) -> Self {
        Self::Float(value)
    }
}

impl From<bool> for Key {
    fn from(value: bool) -> Self {
        Self::Int(i64::from(value))
    }
}

impl From<std::time::SystemTime> for Key {
    fn from(value: std::time::SystemTime) -> Self {
        Self::Date(value)
    }
}

impl From<Vec<u8>> for Key {
    fn from(value: Vec<u8>) -> Self {
        Self::Bytes(value)
    }
}

impl From<&[u8]> for Key {
    fn from(value: &[u8]) -> Self {
        Self::Bytes(value.to_vec())
    }
}

impl<T: Into<Key>> From<Vec<T>> for Key {
    fn from(values: Vec<T>) -> Self {
        if values.len() == 1 {
            values.into_iter().next().expect("one value").into()
        } else {
            Self::Array(values.into_iter().map(Into::into).collect())
        }
    }
}

/// Constrains a query or cursor by lower and upper bounds.
#[derive(Debug, Clone, PartialEq)]
pub struct KeyRange {
    /// Lower bound, inclusive unless `lower_open` is true.
    pub lower: Option<Key>,
    /// Upper bound, inclusive unless `upper_open` is true.
    pub upper: Option<Key>,
    /// Whether the lower bound is exclusive.
    pub lower_open: bool,
    /// Whether the upper bound is exclusive.
    pub upper_open: bool,
}

impl KeyRange {
    /// Returns a range containing only `key`.
    pub fn only(key: impl Into<Key>) -> Self {
        let key = key.into();
        Self {
            lower: Some(key.clone()),
            upper: Some(key),
            lower_open: false,
            upper_open: false,
        }
    }

    /// Returns a range between optional lower and upper bounds.
    pub fn bound(
        lower: impl Into<Key>,
        upper: impl Into<Key>,
        lower_open: bool,
        upper_open: bool,
    ) -> Self {
        Self {
            lower: Some(lower.into()),
            upper: Some(upper.into()),
            lower_open,
            upper_open,
        }
    }

    /// Returns a range with only a lower bound.
    pub fn lower_bound(key: impl Into<Key>, open: bool) -> Self {
        Self {
            lower: Some(key.into()),
            upper: None,
            lower_open: open,
            upper_open: false,
        }
    }

    /// Returns a range with only an upper bound.
    pub fn upper_bound(key: impl Into<Key>, open: bool) -> Self {
        Self {
            lower: None,
            upper: Some(key.into()),
            lower_open: false,
            upper_open: open,
        }
    }
}

/// Describes one IndexedDB query: all records, one exact key, or a key range.
#[derive(Debug, Clone, PartialEq)]
pub enum Query {
    /// Every record in scope (absent wire query).
    All,
    /// One exact key (scalar or composite array).
    Key(Key),
    /// Records whose keys fall within bounds.
    Range(KeyRange),
}

impl Query {
    /// Query that matches every record in scope.
    pub fn all() -> Self {
        Self::All
    }

    fn to_proto(&self) -> Option<pb::IndexedDbQuery> {
        use crate::indexeddb_query_codec::{QueryKind, query_to_proto};
        query_to_proto(match self {
            Self::All => QueryKind::All,
            Self::Key(key) => QueryKind::Key(key),
            Self::Range(range) => QueryKind::Range {
                lower: range.lower.as_ref(),
                upper: range.upper.as_ref(),
                lower_open: range.lower_open,
                upper_open: range.upper_open,
            },
        })
    }
}

impl From<KeyRange> for Query {
    fn from(range: KeyRange) -> Self {
        Self::Range(range)
    }
}

impl<T: Into<Key>> From<T> for Query {
    fn from(value: T) -> Self {
        Self::Key(value.into())
    }
}

impl From<Option<KeyRange>> for Query {
    fn from(range: Option<KeyRange>) -> Self {
        match range {
            None => Self::All,
            Some(range) => Self::Range(range),
        }
    }
}

/// Describes one secondary index on an object store.
#[derive(Debug, Clone, PartialEq)]
pub struct IndexSchema {
    /// Index name.
    pub name: String,
    /// Record path used as the index key.
    pub key_path: Vec<String>,
    /// Whether the index enforces uniqueness.
    pub unique: bool,
}

/// Describes the indexes attached to an object store.
#[derive(Debug, Clone, PartialEq)]
pub struct ObjectStoreSchema {
    /// Secondary indexes to create with the object store.
    pub indexes: Vec<IndexSchema>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
/// Controls cursor traversal order.
pub enum CursorDirection {
    /// Iterate in ascending key order.
    Next,
    /// Iterate in ascending key order while collapsing duplicate index keys.
    NextUnique,
    /// Iterate in descending key order.
    Prev,
    /// Iterate in descending key order while collapsing duplicate index keys.
    PrevUnique,
}

impl CursorDirection {
    fn to_proto(self) -> i32 {
        match self {
            Self::Next => pb::CursorDirection::CursorNext as i32,
            Self::NextUnique => pb::CursorDirection::CursorNextUnique as i32,
            Self::Prev => pb::CursorDirection::CursorPrev as i32,
            Self::PrevUnique => pb::CursorDirection::CursorPrevUnique as i32,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
/// Controls whether an explicit transaction may mutate scoped stores.
pub enum TransactionMode {
    /// Transaction may only read from scoped object stores.
    Readonly,
    /// Transaction may read and write scoped object stores.
    Readwrite,
}

impl TransactionMode {
    fn to_proto(self) -> i32 {
        match self {
            Self::Readonly => pb::TransactionMode::TransactionReadonly as i32,
            Self::Readwrite => pb::TransactionMode::TransactionReadwrite as i32,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
/// Provider durability hint for explicit transactions.
pub enum TransactionDurabilityHint {
    /// Let the host choose its default durability behavior.
    #[default]
    Default,
    /// Prefer stricter durability.
    Strict,
    /// Prefer relaxed durability.
    Relaxed,
}

impl TransactionDurabilityHint {
    fn to_proto(self) -> i32 {
        match self {
            Self::Default => pb::TransactionDurabilityHint::TransactionDurabilityDefault as i32,
            Self::Strict => pb::TransactionDurabilityHint::TransactionDurabilityStrict as i32,
            Self::Relaxed => pb::TransactionDurabilityHint::TransactionDurabilityRelaxed as i32,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
/// Options for an explicit transaction.
pub struct TransactionOptions {
    /// Durability hint for explicit transactions.
    pub durability_hint: TransactionDurabilityHint,
}

#[async_trait]
/// Fakeable client contract for IndexedDB-compatible storage.
pub trait IndexedDBApi: Send {
    /// The store handle this scope yields.
    type ObjectStore: ObjectStoreApi;
    /// The transaction handle this database yields.
    type Transaction: TransactionApi;

    /// Creates a named object store and returns a typed handle for it.
    async fn create_object_store(
        &mut self,
        name: &str,
        schema: ObjectStoreSchema,
    ) -> Result<Self::ObjectStore, IndexedDBError>;

    /// Deletes a named object store.
    async fn delete_object_store(&mut self, name: &str) -> Result<(), IndexedDBError>;

    /// Returns a typed handle for one object store.
    fn object_store(&self, name: &str) -> Self::ObjectStore;

    /// Opens an explicit transaction over a fixed object-store scope.
    async fn transaction(
        &self,
        stores: &[&str],
        mode: TransactionMode,
        options: TransactionOptions,
    ) -> Result<Self::Transaction, IndexedDBError>;
}

#[async_trait]
/// Fakeable IndexedDB object-store contract.
pub trait ObjectStoreApi: Send {
    /// The index handle this store yields.
    type Index: IndexApi;
    /// The cursor handle this scope yields.
    type Cursor: CursorApi;

    /// Loads one record by primary key.
    async fn get(&mut self, id: &str) -> Result<Record, IndexedDBError>;

    /// Resolves the primary key for id.
    async fn get_key(&mut self, id: &str) -> Result<String, IndexedDBError>;

    /// Inserts a new row and fails if the key already exists.
    async fn add(&mut self, record: Record) -> Result<(), IndexedDBError>;

    /// Upserts a row by primary key.
    async fn put(&mut self, record: Record) -> Result<(), IndexedDBError>;

    /// Deletes one row by primary key.
    async fn delete(&mut self, id: &str) -> Result<(), IndexedDBError>;

    /// Deletes every row in the object store.
    async fn clear(&mut self) -> Result<(), IndexedDBError>;

    /// Loads every row that matches query.
    async fn get_all(&mut self, query: Query) -> Result<Vec<Record>, IndexedDBError>;

    /// Loads every primary key that matches query.
    async fn get_all_keys(&mut self, query: Query) -> Result<Vec<String>, IndexedDBError>;

    /// Counts rows that match query.
    async fn count(&mut self, query: Query) -> Result<i64, IndexedDBError>;

    /// Deletes rows that match query and returns the delete count.
    async fn delete_range(&mut self, query: Query) -> Result<i64, IndexedDBError>;

    /// Returns a typed handle for one secondary index.
    fn index(&self, name: &str) -> Self::Index;

    /// Opens a full-value cursor over the object store.
    async fn open_cursor(
        &mut self,
        query: Query,
        direction: CursorDirection,
    ) -> Result<Self::Cursor, IndexedDBError>;

    /// Opens a key-only cursor over the object store.
    async fn open_key_cursor(
        &mut self,
        query: Query,
        direction: CursorDirection,
    ) -> Result<Self::Cursor, IndexedDBError>;
}

#[async_trait]
/// Fakeable IndexedDB secondary-index contract.
pub trait IndexApi: Send {
    /// The cursor handle this scope yields.
    type Cursor: CursorApi;

    /// Loads the first row that matches query.
    async fn get(&mut self, query: Query) -> Result<Record, IndexedDBError>;

    /// Resolves the primary key for the first row that matches query.
    async fn get_key(&mut self, query: Query) -> Result<String, IndexedDBError>;

    /// Loads every row that matches query.
    async fn get_all(&mut self, query: Query) -> Result<Vec<Record>, IndexedDBError>;

    /// Loads every primary key that matches query.
    async fn get_all_keys(&mut self, query: Query) -> Result<Vec<String>, IndexedDBError>;

    /// Counts rows that match query.
    async fn count(&mut self, query: Query) -> Result<i64, IndexedDBError>;

    /// Deletes rows that match query and returns the delete count.
    async fn delete(&mut self, query: Query) -> Result<i64, IndexedDBError>;

    /// Opens a full-value cursor over the secondary index.
    async fn open_cursor(
        &mut self,
        query: Query,
        direction: CursorDirection,
    ) -> Result<Self::Cursor, IndexedDBError>;

    /// Opens a key-only cursor over the secondary index.
    async fn open_key_cursor(
        &mut self,
        query: Query,
        direction: CursorDirection,
    ) -> Result<Self::Cursor, IndexedDBError>;
}

#[async_trait]
/// Fakeable explicit IndexedDB transaction contract.
pub trait TransactionApi: Send {
    /// The transaction-scoped store handle.
    type ObjectStore<'a>: TransactionObjectStoreApi + 'a
    where
        Self: 'a;

    /// Returns a transaction-scoped object store.
    fn object_store<'a>(&'a mut self, name: &str) -> Self::ObjectStore<'a>;

    /// Commits the transaction.
    async fn commit(&mut self) -> Result<(), IndexedDBError>;

    /// Aborts the transaction.
    async fn abort(&mut self, reason: &str) -> Result<(), IndexedDBError>;
}

#[async_trait]
/// Fakeable transaction-scoped object-store contract.
pub trait TransactionObjectStoreApi: Send {
    /// The transaction-scoped index handle.
    type Index<'a>: TransactionIndexApi + 'a
    where
        Self: 'a;

    /// Loads one record by primary key inside the transaction.
    async fn get(&mut self, id: &str) -> Result<Record, IndexedDBError>;

    /// Resolves the primary key for id inside the transaction.
    async fn get_key(&mut self, id: &str) -> Result<String, IndexedDBError>;

    /// Inserts a new row inside the transaction.
    async fn add(&mut self, record: Record) -> Result<(), IndexedDBError>;

    /// Upserts a row inside the transaction.
    async fn put(&mut self, record: Record) -> Result<(), IndexedDBError>;

    /// Deletes one row inside the transaction.
    async fn delete(&mut self, id: &str) -> Result<(), IndexedDBError>;

    /// Deletes every row in the object store inside the transaction.
    async fn clear(&mut self) -> Result<(), IndexedDBError>;

    /// Loads every row that matches query inside the transaction.
    async fn get_all(&mut self, query: Query) -> Result<Vec<Record>, IndexedDBError>;

    /// Loads every primary key that matches query inside the transaction.
    async fn get_all_keys(&mut self, query: Query) -> Result<Vec<String>, IndexedDBError>;

    /// Counts rows that match query inside the transaction.
    async fn count(&mut self, query: Query) -> Result<i64, IndexedDBError>;

    /// Deletes rows that match query inside the transaction.
    async fn delete_range(&mut self, query: Query) -> Result<i64, IndexedDBError>;

    /// Returns a transaction-scoped secondary index.
    fn index<'a>(&'a mut self, name: &str) -> Self::Index<'a>;
}

#[async_trait]
/// Fakeable transaction-scoped secondary-index contract.
pub trait TransactionIndexApi: Send {
    /// Loads the first row that matches query inside the transaction.
    async fn get(&mut self, query: Query) -> Result<Record, IndexedDBError>;

    /// Resolves the primary key for the first matching row inside the transaction.
    async fn get_key(&mut self, query: Query) -> Result<String, IndexedDBError>;

    /// Loads every row that matches query inside the transaction.
    async fn get_all(&mut self, query: Query) -> Result<Vec<Record>, IndexedDBError>;

    /// Loads every primary key that matches query inside the transaction.
    async fn get_all_keys(&mut self, query: Query) -> Result<Vec<String>, IndexedDBError>;

    /// Counts rows that match query inside the transaction.
    async fn count(&mut self, query: Query) -> Result<i64, IndexedDBError>;

    /// Deletes rows that match query inside the transaction.
    async fn delete(&mut self, query: Query) -> Result<i64, IndexedDBError>;
}

#[async_trait]
/// Fakeable IndexedDB cursor contract.
pub trait CursorApi: Send {
    /// Returns the current cursor key.
    fn key(&self) -> Option<Key>;

    /// Returns the current row's primary key.
    fn primary_key(&self) -> &str;

    /// Returns the current row value.
    fn value(&self) -> Result<Record, IndexedDBError>;

    /// Advances the cursor by one row.
    async fn continue_next(&mut self) -> Result<bool, IndexedDBError>;

    /// Advances the cursor to key, or exhausts it if key does not exist.
    async fn continue_to_key(&mut self, key: impl Into<Key> + Send)
    -> Result<bool, IndexedDBError>;

    /// Skips count rows ahead.
    async fn advance(&mut self, count: i32) -> Result<bool, IndexedDBError>;

    /// Deletes the current row and keeps the cursor open.
    async fn delete(&mut self) -> Result<(), IndexedDBError>;

    /// Replaces the current row and keeps the cursor open.
    async fn update(&mut self, value: Record) -> Result<(), IndexedDBError>;

    /// Closes the cursor stream and releases its transport resources.
    async fn close(self) -> Result<(), IndexedDBError>
    where
        Self: Sized;
}

/// Native open-cursor request used by provider-side cursor helpers.
#[derive(Debug, Clone)]
pub struct IndexedDBOpenCursorRequest {
    /// Object store to open.
    pub store: String,
    /// Optional key range to apply.
    pub range: Option<KeyRange>,
    /// Cursor traversal direction.
    pub direction: CursorDirection,
    /// Whether returned cursor entries omit records.
    pub keys_only: bool,
    /// Secondary index name. Empty means object-store cursor.
    pub index: String,
    /// Index values supplied by an index query.
    pub values: Vec<serde_json::Value>,
}

impl Default for IndexedDBOpenCursorRequest {
    fn default() -> Self {
        Self {
            store: String::new(),
            range: None,
            direction: CursorDirection::Next,
            keys_only: false,
            index: String::new(),
            values: Vec::new(),
        }
    }
}

/// One provider-side cursor row.
#[derive(Debug, Clone, PartialEq)]
pub struct IndexedDBCursorSnapshotEntry {
    /// Object-store key, or secondary-index key for index cursors.
    pub key: Key,
    /// Canonical primary key for the object-store row.
    pub primary_key: String,
    /// Native primary-key value used as a stable tie-breaker for duplicate index keys.
    pub primary_key_value: Key,
    /// Row value returned by full-value cursors.
    pub record: Record,
}

/// Provider-side IndexedDB cursor snapshot.
///
/// The snapshot sorts rows, applies IndexedDB range bounds, and implements
/// movement semantics for native Rust providers without exposing wire message
/// types.
#[derive(Debug, Clone)]
pub struct IndexedDBCursorSnapshot {
    /// Whether entry keys contain secondary-index values.
    pub index_cursor: bool,
    /// Whether returned cursor entries should omit records.
    pub keys_only: bool,
    /// Whether entries are ordered from greatest to least key.
    pub reverse: bool,
    /// Whether duplicate index keys are collapsed while iterating.
    pub unique: bool,
    /// Sorted and range-filtered entries used by cursor movement.
    pub entries: Vec<IndexedDBCursorSnapshotEntry>,
    /// Current cursor position, or -1 when unpositioned.
    pub pos: isize,
}

impl IndexedDBCursorSnapshot {
    /// Creates an empty provider-side cursor snapshot from a native request.
    pub fn new(req: &IndexedDBOpenCursorRequest) -> Self {
        Self {
            index_cursor: !req.index.is_empty(),
            keys_only: req.keys_only,
            reverse: matches!(
                req.direction,
                CursorDirection::Prev | CursorDirection::PrevUnique
            ),
            unique: matches!(
                req.direction,
                CursorDirection::NextUnique | CursorDirection::PrevUnique
            ),
            entries: Vec::new(),
            pos: -1,
        }
    }

    /// Sorts entries, applies the supplied key range, and stores the snapshot.
    pub fn load(
        &mut self,
        mut entries: Vec<IndexedDBCursorSnapshotEntry>,
        range: Option<&KeyRange>,
    ) -> Result<(), IndexedDBError> {
        entries.sort_by(|left, right| {
            let mut cmp = compare_indexeddb_values(&left.key, &right.key);
            if cmp == Ordering::Equal {
                cmp = compare_indexeddb_values(&left.primary_key_value, &right.primary_key_value);
            }
            if self.reverse { cmp.reverse() } else { cmp }
        });
        self.entries = self.apply_range(entries, range)?;
        self.pos = -1;
        Ok(())
    }

    /// Returns entries that satisfy the supplied key range without mutating state.
    pub fn apply_range(
        &self,
        entries: Vec<IndexedDBCursorSnapshotEntry>,
        range: Option<&KeyRange>,
    ) -> Result<Vec<IndexedDBCursorSnapshotEntry>, IndexedDBError> {
        let Some(range) = range else {
            return Ok(entries);
        };
        let (lower, upper) = indexeddb_range_bounds(Some(range), self.index_cursor);
        let mut filtered = Vec::with_capacity(entries.len());
        for entry in entries {
            let key = normalize_indexeddb_bound(&entry.key, self.index_cursor);
            if let Some(lower) = &lower {
                let cmp = compare_indexeddb_values(&key, lower);
                if range.lower_open && cmp != Ordering::Greater {
                    continue;
                }
                if !range.lower_open && cmp == Ordering::Less {
                    continue;
                }
            }
            if let Some(upper) = &upper {
                let cmp = compare_indexeddb_values(&key, upper);
                if range.upper_open && cmp != Ordering::Less {
                    continue;
                }
                if !range.upper_open && cmp == Ordering::Greater {
                    continue;
                }
            }
            filtered.push(entry);
        }
        Ok(filtered)
    }

    /// Advances to the next entry, or returns `None` when exhausted.
    #[allow(clippy::should_implement_trait)]
    pub fn next(&mut self) -> Result<Option<&IndexedDBCursorSnapshotEntry>, IndexedDBError> {
        if self.unique
            && self.index_cursor
            && self.pos >= 0
            && (self.pos as usize) < self.entries.len()
        {
            let previous = self.entries[self.pos as usize].key.clone();
            self.pos += 1;
            while (self.pos as usize) < self.entries.len() {
                if compare_indexeddb_values(&self.entries[self.pos as usize].key, &previous)
                    != Ordering::Equal
                {
                    return Ok(Some(self.current()?));
                }
                self.pos += 1;
            }
            return Ok(None);
        }

        self.pos += 1;
        if (self.pos as usize) >= self.entries.len() {
            return Ok(None);
        }
        Ok(Some(self.current()?))
    }

    /// Advances to `target` or the next entry past it for this direction.
    pub fn continue_to_key(
        &mut self,
        target: &Key,
    ) -> Result<Option<&IndexedDBCursorSnapshotEntry>, IndexedDBError> {
        let previous = if self.unique
            && self.index_cursor
            && self.pos >= 0
            && (self.pos as usize) < self.entries.len()
        {
            Some(self.entries[self.pos as usize].key.clone())
        } else {
            None
        };
        self.pos += 1;
        while (self.pos as usize) < self.entries.len() {
            let current = &self.entries[self.pos as usize].key;
            if let Some(previous) = &previous {
                if self.unique
                    && self.index_cursor
                    && compare_indexeddb_values(current, previous) == Ordering::Equal
                {
                    self.pos += 1;
                    continue;
                }
            }
            let cmp = compare_indexeddb_values(current, target);
            if self.reverse {
                if cmp != Ordering::Greater {
                    return Ok(Some(self.current()?));
                }
            } else if cmp != Ordering::Less {
                return Ok(Some(self.current()?));
            }
            self.pos += 1;
        }
        Ok(None)
    }

    /// Skips `count` entries and returns the new current entry.
    pub fn advance(
        &mut self,
        count: i32,
    ) -> Result<Option<&IndexedDBCursorSnapshotEntry>, IndexedDBError> {
        if count <= 0 {
            return Err(IndexedDBError::InvalidArgument(
                "advance count must be positive".to_string(),
            ));
        }
        for i in 0..count {
            if self.next()?.is_none() {
                return Ok(None);
            }
            if i == count - 1 {
                return Ok(Some(self.current()?));
            }
        }
        Ok(None)
    }

    /// Returns the currently positioned entry.
    pub fn current(&self) -> Result<&IndexedDBCursorSnapshotEntry, IndexedDBError> {
        if self.pos < 0 || (self.pos as usize) >= self.entries.len() {
            return Err(IndexedDBError::NotFound);
        }
        Ok(&self.entries[self.pos as usize])
    }
}

/// Creates an empty provider-side cursor snapshot from a native request.
pub fn new_indexeddb_cursor_snapshot(req: &IndexedDBOpenCursorRequest) -> IndexedDBCursorSnapshot {
    IndexedDBCursorSnapshot::new(req)
}

/// Normalizes object-store or index cursor range bounds.
///
/// Scalar index bounds are compared as one-part composite keys so providers can
/// share the same comparison path for scalar and compound indexes.
pub fn indexeddb_range_bounds(
    range: Option<&KeyRange>,
    index_cursor: bool,
) -> (Option<Key>, Option<Key>) {
    let Some(range) = range else {
        return (None, None);
    };
    let lower = range
        .lower
        .as_ref()
        .map(|value| normalize_indexeddb_bound(value, index_cursor));
    let upper = range
        .upper
        .as_ref()
        .map(|value| normalize_indexeddb_bound(value, index_cursor));
    (lower, upper)
}

/// Compares native IndexedDB key values.
pub fn compare_indexeddb_values(left: &Key, right: &Key) -> Ordering {
    compare_keys(left, right)
}

#[derive(Copy, Clone, Eq, PartialEq, Ord, PartialOrd)]
enum KeyKind {
    Number,
    Date,
    String,
    Binary,
    Array,
}

fn key_kind(key: &Key) -> KeyKind {
    match key {
        Key::Int(_) | Key::Float(_) => KeyKind::Number,
        Key::Date(_) => KeyKind::Date,
        Key::Str(_) => KeyKind::String,
        Key::Bytes(_) => KeyKind::Binary,
        Key::Array(_) => KeyKind::Array,
    }
}

/// Compares native IndexedDB key values using W3C ordering.
pub fn compare_keys(left: &Key, right: &Key) -> Ordering {
    let left_kind = key_kind(left);
    let right_kind = key_kind(right);
    if left_kind != right_kind {
        return left_kind.cmp(&right_kind);
    }
    match (left, right) {
        (Key::Int(left), Key::Int(right)) => left.cmp(right),
        (Key::Float(left), Key::Float(right)) => compare_float_keys(*left, *right),
        (Key::Int(left), Key::Float(right)) => compare_int_float_keys(*left, *right),
        (Key::Float(left), Key::Int(right)) => compare_int_float_keys(*right, *left).reverse(),
        (Key::Date(left), Key::Date(right)) => left.cmp(right),
        (Key::Str(left), Key::Str(right)) => compare_utf16_strings(left, right),
        (Key::Bytes(left), Key::Bytes(right)) => left.cmp(right),
        (Key::Array(left), Key::Array(right)) => {
            for (left_value, right_value) in left.iter().zip(right.iter()) {
                let cmp = compare_keys(left_value, right_value);
                if cmp != Ordering::Equal {
                    return cmp;
                }
            }
            left.len().cmp(&right.len())
        }
        _ => Ordering::Equal,
    }
}

/// Reports whether `key` satisfies `range`. A range with no bounds matches every key.
pub fn key_in_range(key: &Key, range: &KeyRange) -> bool {
    if let Some(lower) = &range.lower {
        let cmp = compare_keys(key, lower);
        if range.lower_open {
            if cmp != Ordering::Greater {
                return false;
            }
        } else if cmp == Ordering::Less {
            return false;
        }
    }
    if let Some(upper) = &range.upper {
        let cmp = compare_keys(key, upper);
        if range.upper_open {
            if cmp != Ordering::Less {
                return false;
            }
        } else if cmp == Ordering::Greater {
            return false;
        }
    }
    true
}

/// Reports whether `key` satisfies `query`.
pub fn match_query(key: &Key, query: &Query) -> bool {
    match query {
        Query::All => true,
        Query::Key(target) => compare_keys(key, target) == Ordering::Equal,
        Query::Range(range) => key_in_range(key, range),
    }
}

fn normalize_indexeddb_bound(value: &Key, index_cursor: bool) -> Key {
    if !index_cursor {
        return value.clone();
    }
    if matches!(value, Key::Array(_)) {
        return value.clone();
    }
    Key::Array(vec![value.clone()])
}

fn compare_utf16_strings(left: &str, right: &str) -> Ordering {
    let left: Vec<u16> = left.encode_utf16().collect();
    let right: Vec<u16> = right.encode_utf16().collect();
    for (left_unit, right_unit) in left.iter().zip(right.iter()) {
        match left_unit.cmp(right_unit) {
            Ordering::Equal => {}
            other => return other,
        }
    }
    left.len().cmp(&right.len())
}

fn compare_int_float_keys(left: i64, right: f64) -> Ordering {
    if right.is_nan() {
        return Ordering::Equal;
    }
    match (left as f64).partial_cmp(&right) {
        Some(ordering) => ordering,
        None => left.cmp(&(right as i64)),
    }
}

fn compare_float_keys(left: f64, right: f64) -> Ordering {
    left.partial_cmp(&right).unwrap_or(Ordering::Equal)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::{Duration, UNIX_EPOCH};

    fn entry(key: Key, primary_key: &str, primary_key_value: Key) -> IndexedDBCursorSnapshotEntry {
        IndexedDBCursorSnapshotEntry {
            key,
            primary_key: primary_key.to_string(),
            primary_key_value,
            record: Record::new(),
        }
    }

    #[test]
    fn cursor_snapshot_sorts_ranges_and_skips_duplicate_unique_index_keys() {
        let mut snapshot = new_indexeddb_cursor_snapshot(&IndexedDBOpenCursorRequest {
            direction: CursorDirection::NextUnique,
            index: "by_status".to_string(),
            ..Default::default()
        });

        snapshot
            .load(
                vec![
                    entry(
                        Key::Array(vec![Key::from("todo")]),
                        "issue-2",
                        Key::from("issue-2"),
                    ),
                    entry(
                        Key::Array(vec![Key::from("done")]),
                        "issue-3",
                        Key::from("issue-3"),
                    ),
                    entry(
                        Key::Array(vec![Key::from("todo")]),
                        "issue-1",
                        Key::from("issue-1"),
                    ),
                ],
                Some(&KeyRange {
                    lower: Some(Key::Array(vec![Key::from("done")])),
                    upper: Some(Key::Array(vec![Key::from("todo")])),
                    lower_open: false,
                    upper_open: false,
                }),
            )
            .expect("load");

        assert_eq!(
            snapshot.next().expect("first").unwrap().primary_key,
            "issue-3"
        );
        assert_eq!(
            snapshot.next().expect("second").unwrap().primary_key,
            "issue-1"
        );
        assert!(snapshot.next().expect("exhausted").is_none());
    }

    #[test]
    fn cursor_snapshot_advance_moves_exactly_count_entries_from_current_position() {
        let mut snapshot = new_indexeddb_cursor_snapshot(&IndexedDBOpenCursorRequest::default());
        snapshot
            .load(
                vec![
                    entry(Key::from("a"), "a", Key::from("a")),
                    entry(Key::from("b"), "b", Key::from("b")),
                    entry(Key::from("c"), "c", Key::from("c")),
                ],
                None,
            )
            .expect("load");

        assert_eq!(snapshot.next().expect("first").unwrap().primary_key, "a");
        assert_eq!(
            snapshot.advance(1).expect("second").unwrap().primary_key,
            "b"
        );
        assert_eq!(
            snapshot.advance(1).expect("third").unwrap().primary_key,
            "c"
        );
    }

    #[test]
    fn cursor_snapshot_index_range_accepts_scalar_entry_keys() {
        let mut snapshot = new_indexeddb_cursor_snapshot(&IndexedDBOpenCursorRequest {
            index: "by_status".to_string(),
            ..Default::default()
        });
        snapshot
            .load(
                vec![
                    entry(Key::from("done"), "issue-2", Key::from("issue-2")),
                    entry(Key::from("active"), "issue-1", Key::from("issue-1")),
                ],
                Some(&KeyRange {
                    lower: Some(Key::from("active")),
                    upper: Some(Key::from("active")),
                    lower_open: false,
                    upper_open: false,
                }),
            )
            .expect("load");

        let first = snapshot.next().expect("first").unwrap();
        assert_eq!(first.primary_key, "issue-1");
        assert_eq!(first.key, Key::from("active"));
        assert!(snapshot.next().expect("exhausted").is_none());
    }

    #[test]
    fn range_bounds_normalize_scalar_index_bounds() {
        let (lower, upper) = indexeddb_range_bounds(
            Some(&KeyRange {
                lower: Some(Key::from("active")),
                upper: Some(Key::Array(vec![Key::from("done")])),
                lower_open: false,
                upper_open: false,
            }),
            true,
        );

        assert_eq!(lower, Some(Key::Array(vec![Key::from("active")])));
        assert_eq!(upper, Some(Key::Array(vec![Key::from("done")])));
    }

    #[test]
    fn compare_values_orders_composite_keys() {
        assert_eq!(
            compare_indexeddb_values(
                &Key::Array(vec![Key::from("active"), Key::Int(1)]),
                &Key::Array(vec![Key::from("active"), Key::Int(2)])
            ),
            Ordering::Less
        );
        assert_eq!(
            compare_indexeddb_values(
                &Key::Array(vec![Key::from("active"), Key::Int(2)]),
                &Key::Array(vec![Key::from("active"), Key::Int(2)])
            ),
            Ordering::Equal
        );
        assert_eq!(
            compare_indexeddb_values(
                &Key::Array(vec![Key::from("active"), Key::Int(3)]),
                &Key::Array(vec![Key::from("active"), Key::Int(2)])
            ),
            Ordering::Greater
        );
    }

    #[test]
    fn compare_values_orders_large_integer_keys_exactly() {
        assert_eq!(
            compare_indexeddb_values(
                &Key::Int(9_007_199_254_740_993),
                &Key::Int(9_007_199_254_740_992)
            ),
            Ordering::Greater
        );
        assert_eq!(
            compare_indexeddb_values(&Key::Int(i64::MAX), &Key::Float(u64::MAX as f64)),
            Ordering::Less
        );
    }

    #[test]
    fn compare_keys_orders_date_before_string_and_bytes_after_string() {
        let date = Key::Date(UNIX_EPOCH + Duration::from_secs(1_700_000_000));
        assert_eq!(compare_keys(&Key::Int(1), &date), Ordering::Less);
        assert_eq!(compare_keys(&date, &Key::from("a")), Ordering::Less);
        assert_eq!(
            compare_keys(&Key::from("a"), &Key::Bytes(vec![0x00])),
            Ordering::Less
        );
    }

    #[test]
    fn date_and_bytes_keys_match_exact_queries() {
        let date = Key::Date(UNIX_EPOCH + Duration::from_secs(1_700_000_000));
        let bytes = Key::Bytes(vec![0x01, 0x02]);
        assert!(match_query(&date, &Query::Key(date.clone())));
        assert!(match_query(&bytes, &Query::Key(bytes.clone())));
        assert!(key_in_range(
            &date,
            &KeyRange {
                lower: Some(date.clone()),
                upper: Some(date.clone()),
                lower_open: false,
                upper_open: false,
            }
        ));
    }
}

/// Streaming cursor over object store or secondary index rows.
pub struct Cursor {
    tx: mpsc::Sender<pb::CursorClientMessage>,
    stream: tonic::Streaming<pb::CursorResponse>,
    keys_only: bool,
    entry: Option<pb::CursorEntry>,
    done: bool,
}

impl Cursor {
    /// Returns the current cursor key.
    pub fn key(&self) -> Option<Key> {
        let entry = self.entry.as_ref()?;
        entry
            .key
            .as_ref()
            .map(crate::indexeddb_query_codec::key_from_wire_key_value)
    }

    /// Returns the current row's primary key.
    pub fn primary_key(&self) -> &str {
        self.entry
            .as_ref()
            .map(|e| e.primary_key.as_str())
            .unwrap_or("")
    }

    /// Returns the current row value.
    pub fn value(&self) -> Result<Record, IndexedDBError> {
        if self.keys_only {
            return Err(IndexedDBError::KeysOnly);
        }
        let entry = self.entry.as_ref().ok_or(IndexedDBError::NotFound)?;
        Ok(entry
            .record
            .as_ref()
            .map(pb_record_to_record)
            .unwrap_or_default())
    }

    /// Advances the cursor by one row.
    pub async fn continue_next(&mut self) -> Result<bool, IndexedDBError> {
        let cmd = pb::cursor_command::Command::Next(true);
        self.send_and_recv(cmd).await
    }

    /// Advances the cursor to key, or exhausts it if key does not exist.
    pub async fn continue_to_key(&mut self, key: impl Into<Key>) -> Result<bool, IndexedDBError> {
        let key = key.into();
        let cmd = pb::cursor_command::Command::ContinueToKey(pb::CursorKeyTarget {
            key: Some(crate::indexeddb_query_codec::cursor_key_to_proto(&key)),
        });
        self.send_and_recv(cmd).await
    }

    /// Skips count rows ahead.
    pub async fn advance(&mut self, count: i32) -> Result<bool, IndexedDBError> {
        let cmd = pb::cursor_command::Command::Advance(count);
        self.send_and_recv(cmd).await
    }

    /// Deletes the current row and keeps the cursor open.
    pub async fn delete(&mut self) -> Result<(), IndexedDBError> {
        if self.done {
            return Err(IndexedDBError::NotFound);
        }
        let cmd = pb::cursor_command::Command::Delete(true);
        self.send_mutation(cmd).await
    }

    /// Replaces the current row and keeps the cursor open.
    pub async fn update(&mut self, value: Record) -> Result<(), IndexedDBError> {
        if self.done {
            return Err(IndexedDBError::NotFound);
        }
        let cmd = pb::cursor_command::Command::Update(record_to_pb_record(value));
        self.send_mutation(cmd).await
    }

    /// Closes the cursor stream and releases its transport resources.
    pub async fn close(self) -> Result<(), IndexedDBError> {
        let msg = pb::CursorClientMessage {
            msg: Some(pb::cursor_client_message::Msg::Command(pb::CursorCommand {
                command: Some(pb::cursor_command::Command::Close(true)),
            })),
        };
        self.tx
            .send(msg)
            .await
            .map_err(|e| IndexedDBError::Status(tonic::Status::internal(e.to_string())))?;
        Ok(())
    }

    async fn send_mutation(
        &mut self,
        cmd: pb::cursor_command::Command,
    ) -> Result<(), IndexedDBError> {
        let msg = pb::CursorClientMessage {
            msg: Some(pb::cursor_client_message::Msg::Command(pb::CursorCommand {
                command: Some(cmd),
            })),
        };
        self.tx
            .send(msg)
            .await
            .map_err(|e| IndexedDBError::Status(tonic::Status::internal(e.to_string())))?;
        // Read ack -- if it contains an entry, update cursor state.
        let resp = self
            .stream
            .message()
            .await
            .map_err(map_status)?
            .ok_or_else(|| {
                IndexedDBError::Status(tonic::Status::internal(
                    "cursor stream ended during mutation",
                ))
            })?;
        match resp.result {
            Some(pb::cursor_response::Result::Entry(entry)) => {
                self.entry = Some(entry);
            }
            Some(pb::cursor_response::Result::Done(_)) => {}
            None => {
                return Err(IndexedDBError::Status(tonic::Status::internal(
                    "unexpected cursor mutation ack",
                )));
            }
        }
        Ok(())
    }

    async fn send_and_recv(
        &mut self,
        cmd: pb::cursor_command::Command,
    ) -> Result<bool, IndexedDBError> {
        if self.done {
            return Ok(false);
        }
        let msg = pb::CursorClientMessage {
            msg: Some(pb::cursor_client_message::Msg::Command(pb::CursorCommand {
                command: Some(cmd),
            })),
        };
        self.tx
            .send(msg)
            .await
            .map_err(|e| IndexedDBError::Status(tonic::Status::internal(e.to_string())))?;

        let resp = self
            .stream
            .message()
            .await
            .map_err(map_status)?
            .ok_or_else(|| {
                IndexedDBError::Status(tonic::Status::internal("cursor stream ended"))
            })?;

        match resp.result {
            Some(pb::cursor_response::Result::Entry(entry)) => {
                self.entry = Some(entry);
                self.done = false;
                Ok(true)
            }
            Some(pb::cursor_response::Result::Done(exhausted)) => {
                if exhausted {
                    self.done = true;
                }
                self.entry = None;
                Ok(false)
            }
            None => {
                self.entry = None;
                self.done = true;
                Ok(false)
            }
        }
    }
}

async fn open_cursor_inner(
    client: &mut IndexedDbClient<IndexedDbTransport>,
    req: pb::OpenCursorRequest,
) -> Result<Cursor, IndexedDBError> {
    let keys_only = req.keys_only;
    let (tx, rx) = mpsc::channel::<pb::CursorClientMessage>(CURSOR_CHANNEL_BUFFER);

    let open_msg = pb::CursorClientMessage {
        msg: Some(pb::cursor_client_message::Msg::Open(req)),
    };
    tx.send(open_msg)
        .await
        .map_err(|e| IndexedDBError::Status(tonic::Status::internal(e.to_string())))?;

    let receiver_stream = ReceiverStream::new(rx);
    let mut stream = client
        .open_cursor(receiver_stream)
        .await
        .map_err(map_status)?
        .into_inner();

    // Read the open ack to surface creation errors synchronously.
    let ack = stream.message().await.map_err(map_status)?.ok_or_else(|| {
        IndexedDBError::Status(tonic::Status::internal("cursor stream ended during open"))
    })?;
    match ack.result {
        Some(pb::cursor_response::Result::Done(false)) => {}
        Some(pb::cursor_response::Result::Done(true)) => {
            return Err(IndexedDBError::Status(tonic::Status::internal(
                "unexpected exhausted cursor open ack",
            )));
        }
        _ => {
            return Err(IndexedDBError::Status(tonic::Status::internal(
                "unexpected cursor open ack",
            )));
        }
    }

    Ok(Cursor {
        tx,
        stream,
        keys_only,
        entry: None,
        done: false,
    })
}

/// Client for a running IndexedDB provider.
pub struct IndexedDB {
    client: IndexedDbClient<IndexedDbTransport>,
}

impl IndexedDB {
    /// Connects to the default IndexedDB transport socket.
    pub async fn connect() -> Result<Self, IndexedDBError> {
        Self::connect_named("").await
    }

    /// Connects to a named IndexedDB transport socket.
    pub async fn connect_named(name: &str) -> Result<Self, IndexedDBError> {
        host_service_configured("indexeddb").map_err(IndexedDBError::Env)?;
        let target = std::env::var(ENV_HOST_SERVICE_SOCKET)
            .map_err(|_| IndexedDBError::Env(format!("{ENV_HOST_SERVICE_SOCKET} is not set")))?;
        let token = std::env::var(ENV_HOST_SERVICE_TOKEN).unwrap_or_default();
        let channel = match parse_indexeddb_target(&target)? {
            IndexedDBTarget::Unix(path) => {
                Endpoint::try_from("http://[::]:50051")?
                    .connect_with_connector(service_fn(move |_: Uri| {
                        let path = path.clone();
                        async move {
                            tokio::net::UnixStream::connect(path)
                                .await
                                .map(TokioIo::new)
                        }
                    }))
                    .await?
            }
            IndexedDBTarget::Tcp(address) => {
                Endpoint::from_shared(format!("http://{address}"))?
                    .connect()
                    .await?
            }
            IndexedDBTarget::Tls(address) => {
                Endpoint::from_shared(format!("https://{address}"))?
                    .tls_config(ClientTlsConfig::new().with_native_roots())?
                    .connect()
                    .await?
            }
        };

        let client = IndexedDbClient::with_interceptor(
            channel,
            relay_token_interceptor(token.trim(), name)?,
        );

        Ok(Self { client })
    }

    /// Creates a named object store.
    pub async fn create_object_store(
        &mut self,
        name: &str,
        schema: ObjectStoreSchema,
    ) -> Result<ObjectStore, IndexedDBError> {
        let indexes = schema
            .indexes
            .into_iter()
            .map(|idx| pb::IndexSchema {
                name: idx.name,
                key_path: idx.key_path,
                unique: idx.unique,
            })
            .collect();
        self.client
            .create_object_store(pb::CreateObjectStoreRequest {
                name: name.to_string(),
                schema: Some(pb::ObjectStoreSchema {
                    indexes,
                    columns: vec![],
                }),
            })
            .await
            .map_err(map_status)?;
        Ok(self.object_store(name))
    }

    /// Deletes a named object store.
    pub async fn delete_object_store(&mut self, name: &str) -> Result<(), IndexedDBError> {
        self.client
            .delete_object_store(pb::DeleteObjectStoreRequest {
                name: name.to_string(),
            })
            .await
            .map_err(map_status)?;
        Ok(())
    }

    /// Returns a typed handle for one object store.
    pub fn object_store(&self, name: &str) -> ObjectStore {
        ObjectStore {
            client: self.client.clone(),
            store: name.to_string(),
        }
    }

    /// Opens an explicit transaction over a fixed object-store scope.
    pub async fn transaction(
        &self,
        stores: &[&str],
        mode: TransactionMode,
        options: TransactionOptions,
    ) -> Result<Transaction, IndexedDBError> {
        let (tx, rx) = mpsc::channel::<pb::TransactionClientMessage>(TRANSACTION_CHANNEL_BUFFER);
        tx.send(pb::TransactionClientMessage {
            msg: Some(pb::transaction_client_message::Msg::Begin(
                pb::BeginTransactionRequest {
                    stores: stores.iter().map(|store| store.to_string()).collect(),
                    mode: mode.to_proto(),
                    durability_hint: options.durability_hint.to_proto(),
                },
            )),
        })
        .await
        .map_err(|e| IndexedDBError::Status(tonic::Status::internal(e.to_string())))?;

        let receiver_stream = ReceiverStream::new(rx);
        let mut client = self.client.clone();
        let mut stream = client
            .transaction(receiver_stream)
            .await
            .map_err(map_status)?
            .into_inner();

        let ack = stream.message().await.map_err(map_status)?.ok_or_else(|| {
            IndexedDBError::Transaction("transaction stream ended during begin".to_string())
        })?;
        match ack.msg {
            Some(pb::transaction_server_message::Msg::Begin(_)) => {}
            _ => {
                return Err(IndexedDBError::Transaction(
                    "expected transaction begin response".to_string(),
                ));
            }
        }

        Ok(Transaction {
            tx: Some(tx),
            stream,
            request_id: 0,
            closed: false,
        })
    }
}

/// Explicit transaction over one or more object stores.
pub struct Transaction {
    tx: Option<mpsc::Sender<pb::TransactionClientMessage>>,
    stream: tonic::Streaming<pb::TransactionServerMessage>,
    request_id: u64,
    closed: bool,
}

impl Transaction {
    /// Returns a transaction-scoped object store.
    pub fn object_store<'a>(&'a mut self, name: &str) -> TransactionObjectStore<'a> {
        TransactionObjectStore {
            tx: self,
            store: name.to_string(),
        }
    }

    /// Commits the transaction.
    pub async fn commit(&mut self) -> Result<(), IndexedDBError> {
        self.ensure_open()?;
        let tx = self.tx.as_ref().ok_or_else(|| {
            IndexedDBError::Transaction("transaction is already finished".to_string())
        })?;
        tx.send(pb::TransactionClientMessage {
            msg: Some(pb::transaction_client_message::Msg::Commit(
                pb::TransactionCommitRequest {},
            )),
        })
        .await
        .map_err(|e| IndexedDBError::Status(tonic::Status::internal(e.to_string())))?;
        self.closed = true;
        self.tx.take();

        let resp = self
            .stream
            .message()
            .await
            .map_err(map_status)?
            .ok_or_else(|| {
                IndexedDBError::Transaction("transaction stream ended during commit".to_string())
            })?;
        match resp.msg {
            Some(pb::transaction_server_message::Msg::Commit(commit)) => {
                map_rpc_status(commit.error)
            }
            _ => Err(IndexedDBError::Transaction(
                "expected transaction commit response".to_string(),
            )),
        }
    }

    /// Aborts the transaction. Aborting an already finished transaction is a no-op.
    pub async fn abort(&mut self, reason: &str) -> Result<(), IndexedDBError> {
        if self.closed {
            return Ok(());
        }
        let tx = self.tx.as_ref().ok_or_else(|| {
            IndexedDBError::Transaction("transaction is already finished".to_string())
        })?;
        tx.send(pb::TransactionClientMessage {
            msg: Some(pb::transaction_client_message::Msg::Abort(
                pb::TransactionAbortRequest {
                    reason: reason.to_string(),
                },
            )),
        })
        .await
        .map_err(|e| IndexedDBError::Status(tonic::Status::internal(e.to_string())))?;
        self.closed = true;
        self.tx.take();

        let resp = self
            .stream
            .message()
            .await
            .map_err(map_status)?
            .ok_or_else(|| {
                IndexedDBError::Transaction("transaction stream ended during abort".to_string())
            })?;
        match resp.msg {
            Some(pb::transaction_server_message::Msg::Abort(abort)) => map_rpc_status(abort.error),
            _ => Err(IndexedDBError::Transaction(
                "expected transaction abort response".to_string(),
            )),
        }
    }

    async fn send_operation(
        &mut self,
        operation: pb::transaction_operation::Operation,
    ) -> Result<pb::TransactionOperationResponse, IndexedDBError> {
        self.ensure_open()?;
        self.request_id += 1;
        let request_id = self.request_id;
        let tx = self.tx.as_ref().ok_or_else(|| {
            IndexedDBError::Transaction("transaction is already finished".to_string())
        })?;
        tx.send(pb::TransactionClientMessage {
            msg: Some(pb::transaction_client_message::Msg::Operation(
                pb::TransactionOperation {
                    request_id,
                    operation: Some(operation),
                },
            )),
        })
        .await
        .map_err(|e| IndexedDBError::Status(tonic::Status::internal(e.to_string())))?;

        let resp = self
            .stream
            .message()
            .await
            .map_err(map_status)?
            .ok_or_else(|| {
                IndexedDBError::Transaction("transaction stream ended during operation".to_string())
            })?;
        let op = match resp.msg {
            Some(pb::transaction_server_message::Msg::Operation(op)) => op,
            _ => {
                self.close_locally();
                return Err(IndexedDBError::Transaction(
                    "expected transaction operation response".to_string(),
                ));
            }
        };
        if op.request_id != request_id {
            self.close_locally();
            return Err(IndexedDBError::Transaction(
                "transaction response request id mismatch".to_string(),
            ));
        }
        if let Err(err) = map_rpc_status(op.error.clone()) {
            self.close_locally();
            return Err(err);
        }
        Ok(op)
    }

    fn ensure_open(&self) -> Result<(), IndexedDBError> {
        if self.closed {
            return Err(IndexedDBError::Transaction(
                "transaction is already finished".to_string(),
            ));
        }
        Ok(())
    }

    fn close_locally(&mut self) {
        self.closed = true;
        self.tx.take();
    }
}

/// Object-store operations scoped to an explicit transaction.
pub struct TransactionObjectStore<'a> {
    tx: &'a mut Transaction,
    store: String,
}

impl TransactionObjectStore<'_> {
    /// Loads one record by primary key inside the transaction.
    pub async fn get(&mut self, id: &str) -> Result<Record, IndexedDBError> {
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::Get(
                pb::ObjectStoreRequest {
                    store: self.store.clone(),
                    id: id.to_string(),
                },
            ))
            .await?;
        match resp.result {
            Some(pb::transaction_operation_response::Result::Record(record)) => Ok(record
                .record
                .as_ref()
                .map(pb_record_to_record)
                .unwrap_or_default()),
            _ => Err(unexpected_transaction_result()),
        }
    }

    /// Resolves the primary key for id inside the transaction.
    pub async fn get_key(&mut self, id: &str) -> Result<String, IndexedDBError> {
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::GetKey(
                pb::ObjectStoreRequest {
                    store: self.store.clone(),
                    id: id.to_string(),
                },
            ))
            .await?;
        match resp.result {
            Some(pb::transaction_operation_response::Result::Key(key)) => Ok(key.key),
            _ => Err(unexpected_transaction_result()),
        }
    }

    /// Inserts a new row inside the transaction.
    pub async fn add(&mut self, record: Record) -> Result<(), IndexedDBError> {
        self.tx
            .send_operation(pb::transaction_operation::Operation::Add(
                pb::RecordRequest {
                    store: self.store.clone(),
                    record: Some(record_to_pb_record(record)),
                },
            ))
            .await?;
        Ok(())
    }

    /// Upserts a row inside the transaction.
    pub async fn put(&mut self, record: Record) -> Result<(), IndexedDBError> {
        self.tx
            .send_operation(pb::transaction_operation::Operation::Put(
                pb::RecordRequest {
                    store: self.store.clone(),
                    record: Some(record_to_pb_record(record)),
                },
            ))
            .await?;
        Ok(())
    }

    /// Deletes one row inside the transaction.
    pub async fn delete(&mut self, id: &str) -> Result<(), IndexedDBError> {
        self.tx
            .send_operation(pb::transaction_operation::Operation::Delete(
                pb::ObjectStoreRequest {
                    store: self.store.clone(),
                    id: id.to_string(),
                },
            ))
            .await?;
        Ok(())
    }

    /// Deletes every row in the object store inside the transaction.
    pub async fn clear(&mut self) -> Result<(), IndexedDBError> {
        self.tx
            .send_operation(pb::transaction_operation::Operation::Clear(
                pb::ObjectStoreNameRequest {
                    store: self.store.clone(),
                },
            ))
            .await?;
        Ok(())
    }

    /// Loads every row that matches query inside the transaction.
    pub async fn get_all(
        &mut self,
        query: impl Into<Query>,
        count: Option<u32>,
    ) -> Result<Vec<Record>, IndexedDBError> {
        let query = query.into();
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::GetAll(
                pb::ObjectStoreRangeRequest {
                    store: self.store.clone(),
                    query: query.to_proto(),
                    count,
                },
            ))
            .await?;
        match resp.result {
            Some(pb::transaction_operation_response::Result::Records(records)) => {
                Ok(records.records.iter().map(pb_record_to_record).collect())
            }
            _ => Err(unexpected_transaction_result()),
        }
    }

    /// Loads every primary key that matches query inside the transaction.
    pub async fn get_all_keys(
        &mut self,
        query: impl Into<Query>,
        count: Option<u32>,
    ) -> Result<Vec<String>, IndexedDBError> {
        let query = query.into();
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::GetAllKeys(
                pb::ObjectStoreRangeRequest {
                    store: self.store.clone(),
                    query: query.to_proto(),
                    count,
                },
            ))
            .await?;
        match resp.result {
            Some(pb::transaction_operation_response::Result::Keys(keys)) => Ok(keys.keys),
            _ => Err(unexpected_transaction_result()),
        }
    }

    /// Counts rows that match query inside the transaction.
    pub async fn count(&mut self, query: impl Into<Query>) -> Result<i64, IndexedDBError> {
        let query = query.into();
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::Count(
                pb::ObjectStoreRangeRequest {
                    store: self.store.clone(),
                    query: query.to_proto(),
                    count: None,
                },
            ))
            .await?;
        match resp.result {
            Some(pb::transaction_operation_response::Result::Count(count)) => Ok(count.count),
            _ => Err(unexpected_transaction_result()),
        }
    }

    /// Deletes rows that match query inside the transaction.
    pub async fn delete_range(&mut self, query: impl Into<Query>) -> Result<i64, IndexedDBError> {
        let query = query.into();
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::DeleteRange(
                pb::ObjectStoreRangeRequest {
                    store: self.store.clone(),
                    query: query.to_proto(),
                    count: None,
                },
            ))
            .await?;
        match resp.result {
            Some(pb::transaction_operation_response::Result::Delete(deleted)) => {
                Ok(deleted.deleted)
            }
            _ => Err(unexpected_transaction_result()),
        }
    }

    /// Returns a transaction-scoped secondary index.
    pub fn index<'a>(&'a mut self, name: &str) -> TransactionIndex<'a> {
        TransactionIndex {
            tx: &mut *self.tx,
            store: self.store.clone(),
            index: name.to_string(),
        }
    }
}

/// Secondary-index operations scoped to an explicit transaction.
pub struct TransactionIndex<'a> {
    tx: &'a mut Transaction,
    store: String,
    index: String,
}

impl TransactionIndex<'_> {
    /// Loads the first row that matches query inside the transaction.
    pub async fn get(&mut self, query: impl Into<Query>) -> Result<Record, IndexedDBError> {
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::IndexGet(
                self.index_request(query.into(), None),
            ))
            .await?;
        match resp.result {
            Some(pb::transaction_operation_response::Result::Record(record)) => Ok(record
                .record
                .as_ref()
                .map(pb_record_to_record)
                .unwrap_or_default()),
            _ => Err(unexpected_transaction_result()),
        }
    }

    /// Resolves the primary key for the first matching row inside the transaction.
    pub async fn get_key(&mut self, query: impl Into<Query>) -> Result<String, IndexedDBError> {
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::IndexGetKey(
                self.index_request(query.into(), None),
            ))
            .await?;
        match resp.result {
            Some(pb::transaction_operation_response::Result::Key(key)) => Ok(key.key),
            _ => Err(unexpected_transaction_result()),
        }
    }

    /// Loads every row that matches query inside the transaction.
    pub async fn get_all(
        &mut self,
        query: impl Into<Query>,
        count: Option<u32>,
    ) -> Result<Vec<Record>, IndexedDBError> {
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::IndexGetAll(
                self.index_request(query.into(), count),
            ))
            .await?;
        match resp.result {
            Some(pb::transaction_operation_response::Result::Records(records)) => {
                Ok(records.records.iter().map(pb_record_to_record).collect())
            }
            _ => Err(unexpected_transaction_result()),
        }
    }

    /// Loads every primary key that matches query inside the transaction.
    pub async fn get_all_keys(
        &mut self,
        query: impl Into<Query>,
        count: Option<u32>,
    ) -> Result<Vec<String>, IndexedDBError> {
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::IndexGetAllKeys(
                self.index_request(query.into(), count),
            ))
            .await?;
        match resp.result {
            Some(pb::transaction_operation_response::Result::Keys(keys)) => Ok(keys.keys),
            _ => Err(unexpected_transaction_result()),
        }
    }

    /// Counts rows that match query inside the transaction.
    pub async fn count(&mut self, query: impl Into<Query>) -> Result<i64, IndexedDBError> {
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::IndexCount(
                self.index_request(query.into(), None),
            ))
            .await?;
        match resp.result {
            Some(pb::transaction_operation_response::Result::Count(count)) => Ok(count.count),
            _ => Err(unexpected_transaction_result()),
        }
    }

    /// Deletes rows that match query inside the transaction.
    pub async fn delete(&mut self, query: impl Into<Query>) -> Result<i64, IndexedDBError> {
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::IndexDelete(
                self.index_request(query.into(), None),
            ))
            .await?;
        match resp.result {
            Some(pb::transaction_operation_response::Result::Delete(deleted)) => {
                Ok(deleted.deleted)
            }
            _ => Err(unexpected_transaction_result()),
        }
    }

    fn index_request(&self, query: Query, count: Option<u32>) -> pb::IndexQueryRequest {
        pb::IndexQueryRequest {
            store: self.store.clone(),
            index: self.index.clone(),
            query: query.to_proto(),
            count,
        }
    }
}

enum IndexedDBTarget {
    Unix(String),
    Tcp(String),
    Tls(String),
}

fn parse_indexeddb_target(raw_target: &str) -> Result<IndexedDBTarget, IndexedDBError> {
    let target = raw_target.trim();
    if target.is_empty() {
        return Err(IndexedDBError::Env(
            "IndexedDB transport target is required".to_string(),
        ));
    }
    if let Some(address) = target.strip_prefix("tcp://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(IndexedDBError::Env(format!(
                "IndexedDB tcp target {raw_target:?} is missing host:port"
            )));
        }
        return Ok(IndexedDBTarget::Tcp(address.to_string()));
    }
    if let Some(address) = target.strip_prefix("tls://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(IndexedDBError::Env(format!(
                "IndexedDB tls target {raw_target:?} is missing host:port"
            )));
        }
        return Ok(IndexedDBTarget::Tls(address.to_string()));
    }
    if let Some(path) = target.strip_prefix("unix://") {
        let path = path.trim();
        if path.is_empty() {
            return Err(IndexedDBError::Env(format!(
                "IndexedDB unix target {raw_target:?} is missing a socket path"
            )));
        }
        return Ok(IndexedDBTarget::Unix(path.to_string()));
    }
    if target.contains("://") {
        let scheme = target.split("://").next().unwrap_or_default();
        return Err(IndexedDBError::Env(format!(
            "unsupported IndexedDB target scheme {scheme:?}"
        )));
    }
    Ok(IndexedDBTarget::Unix(target.to_string()))
}

/// CRUD, range-query, and cursor access for one object store.
pub struct ObjectStore {
    client: IndexedDbClient<IndexedDbTransport>,
    store: String,
}

impl ObjectStore {
    /// Loads one record by primary key.
    pub async fn get(&mut self, id: &str) -> Result<Record, IndexedDBError> {
        let resp = self
            .client
            .get(pb::ObjectStoreRequest {
                store: self.store.clone(),
                id: id.to_string(),
            })
            .await
            .map_err(map_status)?;
        Ok(resp
            .into_inner()
            .record
            .as_ref()
            .map(pb_record_to_record)
            .unwrap_or_default())
    }

    /// Resolves the primary key for id.
    pub async fn get_key(&mut self, id: &str) -> Result<String, IndexedDBError> {
        let resp = self
            .client
            .get_key(pb::ObjectStoreRequest {
                store: self.store.clone(),
                id: id.to_string(),
            })
            .await
            .map_err(map_status)?;
        Ok(resp.into_inner().key)
    }

    /// Inserts a new row and fails if the key already exists.
    pub async fn add(&mut self, record: Record) -> Result<(), IndexedDBError> {
        self.client
            .add(pb::RecordRequest {
                store: self.store.clone(),
                record: Some(record_to_pb_record(record)),
            })
            .await
            .map_err(map_status)?;
        Ok(())
    }

    /// Upserts a row by primary key.
    pub async fn put(&mut self, record: Record) -> Result<(), IndexedDBError> {
        self.client
            .put(pb::RecordRequest {
                store: self.store.clone(),
                record: Some(record_to_pb_record(record)),
            })
            .await
            .map_err(map_status)?;
        Ok(())
    }

    /// Deletes one row by primary key.
    pub async fn delete(&mut self, id: &str) -> Result<(), IndexedDBError> {
        self.client
            .delete(pb::ObjectStoreRequest {
                store: self.store.clone(),
                id: id.to_string(),
            })
            .await
            .map_err(map_status)?;
        Ok(())
    }

    /// Deletes every row in the object store.
    pub async fn clear(&mut self) -> Result<(), IndexedDBError> {
        self.client
            .clear(pb::ObjectStoreNameRequest {
                store: self.store.clone(),
            })
            .await
            .map_err(map_status)?;
        Ok(())
    }

    /// Loads every row that matches query.
    pub async fn get_all(
        &mut self,
        query: impl Into<Query>,
        count: Option<u32>,
    ) -> Result<Vec<Record>, IndexedDBError> {
        let query = query.into();
        let resp = self
            .client
            .get_all(pb::ObjectStoreRangeRequest {
                store: self.store.clone(),
                query: query.to_proto(),
                count,
            })
            .await
            .map_err(map_status)?;
        Ok(resp
            .into_inner()
            .records
            .iter()
            .map(pb_record_to_record)
            .collect())
    }

    /// Loads every primary key that matches query.
    pub async fn get_all_keys(
        &mut self,
        query: impl Into<Query>,
        count: Option<u32>,
    ) -> Result<Vec<String>, IndexedDBError> {
        let query = query.into();
        let resp = self
            .client
            .get_all_keys(pb::ObjectStoreRangeRequest {
                store: self.store.clone(),
                query: query.to_proto(),
                count,
            })
            .await
            .map_err(map_status)?;
        Ok(resp.into_inner().keys)
    }

    /// Counts rows that match query.
    pub async fn count(&mut self, query: impl Into<Query>) -> Result<i64, IndexedDBError> {
        let query = query.into();
        let resp = self
            .client
            .count(pb::ObjectStoreRangeRequest {
                store: self.store.clone(),
                query: query.to_proto(),
                count: None,
            })
            .await
            .map_err(map_status)?;
        Ok(resp.into_inner().count)
    }

    /// Deletes rows that match query and returns the delete count.
    pub async fn delete_range(&mut self, query: impl Into<Query>) -> Result<i64, IndexedDBError> {
        let query = query.into();
        let resp = self
            .client
            .delete_range(pb::ObjectStoreRangeRequest {
                store: self.store.clone(),
                query: query.to_proto(),
                count: None,
            })
            .await
            .map_err(map_status)?;
        Ok(resp.into_inner().deleted)
    }

    /// Returns a typed handle for one secondary index.
    pub fn index(&self, name: &str) -> Index {
        Index {
            client: self.client.clone(),
            store: self.store.clone(),
            index: name.to_string(),
        }
    }

    /// Opens a full-value cursor over the object store.
    pub async fn open_cursor(
        &mut self,
        query: impl Into<Query>,
        direction: CursorDirection,
    ) -> Result<Cursor, IndexedDBError> {
        let query = query.into();
        let req = pb::OpenCursorRequest {
            store: self.store.clone(),
            index: String::new(),
            query: query.to_proto(),
            direction: direction.to_proto(),
            keys_only: false,
        };
        open_cursor_inner(&mut self.client, req).await
    }

    /// Opens a key-only cursor over the object store.
    pub async fn open_key_cursor(
        &mut self,
        query: impl Into<Query>,
        direction: CursorDirection,
    ) -> Result<Cursor, IndexedDBError> {
        let query = query.into();
        let req = pb::OpenCursorRequest {
            store: self.store.clone(),
            index: String::new(),
            query: query.to_proto(),
            direction: direction.to_proto(),
            keys_only: true,
        };
        open_cursor_inner(&mut self.client, req).await
    }
}

/// Lookup and cursor access through one secondary index.
pub struct Index {
    client: IndexedDbClient<IndexedDbTransport>,
    store: String,
    index: String,
}

impl Index {
    fn index_request(&self, query: Query, count: Option<u32>) -> pb::IndexQueryRequest {
        pb::IndexQueryRequest {
            store: self.store.clone(),
            index: self.index.clone(),
            query: query.to_proto(),
            count,
        }
    }

    /// Loads the first row that matches query.
    pub async fn get(&mut self, query: impl Into<Query>) -> Result<Record, IndexedDBError> {
        let resp = self
            .client
            .index_get(self.index_request(query.into(), None))
            .await
            .map_err(map_status)?;
        Ok(resp
            .into_inner()
            .record
            .as_ref()
            .map(pb_record_to_record)
            .unwrap_or_default())
    }

    /// Resolves the primary key for the first row that matches query.
    pub async fn get_key(&mut self, query: impl Into<Query>) -> Result<String, IndexedDBError> {
        let resp = self
            .client
            .index_get_key(self.index_request(query.into(), None))
            .await
            .map_err(map_status)?;
        Ok(resp.into_inner().key)
    }

    /// Loads every row that matches query.
    pub async fn get_all(
        &mut self,
        query: impl Into<Query>,
        count: Option<u32>,
    ) -> Result<Vec<Record>, IndexedDBError> {
        let resp = self
            .client
            .index_get_all(self.index_request(query.into(), count))
            .await
            .map_err(map_status)?;
        Ok(resp
            .into_inner()
            .records
            .iter()
            .map(pb_record_to_record)
            .collect())
    }

    /// Loads every primary key that matches query.
    pub async fn get_all_keys(
        &mut self,
        query: impl Into<Query>,
        count: Option<u32>,
    ) -> Result<Vec<String>, IndexedDBError> {
        let resp = self
            .client
            .index_get_all_keys(self.index_request(query.into(), count))
            .await
            .map_err(map_status)?;
        Ok(resp.into_inner().keys)
    }

    /// Counts rows that match query.
    pub async fn count(&mut self, query: impl Into<Query>) -> Result<i64, IndexedDBError> {
        let resp = self
            .client
            .index_count(self.index_request(query.into(), None))
            .await
            .map_err(map_status)?;
        Ok(resp.into_inner().count)
    }

    /// Deletes rows that match query and returns the delete count.
    pub async fn delete(&mut self, query: impl Into<Query>) -> Result<i64, IndexedDBError> {
        let resp = self
            .client
            .index_delete(self.index_request(query.into(), None))
            .await
            .map_err(map_status)?;
        Ok(resp.into_inner().deleted)
    }

    /// Opens a full-value cursor over the secondary index.
    pub async fn open_cursor(
        &mut self,
        query: impl Into<Query>,
        direction: CursorDirection,
    ) -> Result<Cursor, IndexedDBError> {
        let query = query.into();
        let req = pb::OpenCursorRequest {
            store: self.store.clone(),
            index: self.index.clone(),
            query: query.to_proto(),
            direction: direction.to_proto(),
            keys_only: false,
        };
        open_cursor_inner(&mut self.client, req).await
    }

    /// Opens a key-only cursor over the secondary index.
    pub async fn open_key_cursor(
        &mut self,
        query: impl Into<Query>,
        direction: CursorDirection,
    ) -> Result<Cursor, IndexedDBError> {
        let query = query.into();
        let req = pb::OpenCursorRequest {
            store: self.store.clone(),
            index: self.index.clone(),
            query: query.to_proto(),
            direction: direction.to_proto(),
            keys_only: true,
        };
        open_cursor_inner(&mut self.client, req).await
    }
}

#[async_trait]
impl IndexedDBApi for IndexedDB {
    type ObjectStore = ObjectStore;
    type Transaction = Transaction;

    async fn create_object_store(
        &mut self,
        name: &str,
        schema: ObjectStoreSchema,
    ) -> Result<ObjectStore, IndexedDBError> {
        IndexedDB::create_object_store(self, name, schema).await
    }

    async fn delete_object_store(&mut self, name: &str) -> Result<(), IndexedDBError> {
        IndexedDB::delete_object_store(self, name).await
    }

    fn object_store(&self, name: &str) -> ObjectStore {
        IndexedDB::object_store(self, name)
    }

    async fn transaction(
        &self,
        stores: &[&str],
        mode: TransactionMode,
        options: TransactionOptions,
    ) -> Result<Transaction, IndexedDBError> {
        IndexedDB::transaction(self, stores, mode, options).await
    }
}

#[async_trait]
impl ObjectStoreApi for ObjectStore {
    type Index = Index;
    type Cursor = Cursor;

    async fn get(&mut self, id: &str) -> Result<Record, IndexedDBError> {
        ObjectStore::get(self, id).await
    }

    async fn get_key(&mut self, id: &str) -> Result<String, IndexedDBError> {
        ObjectStore::get_key(self, id).await
    }

    async fn add(&mut self, record: Record) -> Result<(), IndexedDBError> {
        ObjectStore::add(self, record).await
    }

    async fn put(&mut self, record: Record) -> Result<(), IndexedDBError> {
        ObjectStore::put(self, record).await
    }

    async fn delete(&mut self, id: &str) -> Result<(), IndexedDBError> {
        ObjectStore::delete(self, id).await
    }

    async fn clear(&mut self) -> Result<(), IndexedDBError> {
        ObjectStore::clear(self).await
    }

    async fn get_all(&mut self, query: Query) -> Result<Vec<Record>, IndexedDBError> {
        ObjectStore::get_all(self, query, None).await
    }

    async fn get_all_keys(&mut self, query: Query) -> Result<Vec<String>, IndexedDBError> {
        ObjectStore::get_all_keys(self, query, None).await
    }

    async fn count(&mut self, query: Query) -> Result<i64, IndexedDBError> {
        ObjectStore::count(self, query).await
    }

    async fn delete_range(&mut self, query: Query) -> Result<i64, IndexedDBError> {
        ObjectStore::delete_range(self, query).await
    }

    fn index(&self, name: &str) -> Index {
        ObjectStore::index(self, name)
    }

    async fn open_cursor(
        &mut self,
        query: Query,
        direction: CursorDirection,
    ) -> Result<Cursor, IndexedDBError> {
        ObjectStore::open_cursor(self, query, direction).await
    }

    async fn open_key_cursor(
        &mut self,
        query: Query,
        direction: CursorDirection,
    ) -> Result<Cursor, IndexedDBError> {
        ObjectStore::open_key_cursor(self, query, direction).await
    }
}

#[async_trait]
impl IndexApi for Index {
    type Cursor = Cursor;

    async fn get(&mut self, query: Query) -> Result<Record, IndexedDBError> {
        Index::get(self, query).await
    }

    async fn get_key(&mut self, query: Query) -> Result<String, IndexedDBError> {
        Index::get_key(self, query).await
    }

    async fn get_all(&mut self, query: Query) -> Result<Vec<Record>, IndexedDBError> {
        Index::get_all(self, query, None).await
    }

    async fn get_all_keys(&mut self, query: Query) -> Result<Vec<String>, IndexedDBError> {
        Index::get_all_keys(self, query, None).await
    }

    async fn count(&mut self, query: Query) -> Result<i64, IndexedDBError> {
        Index::count(self, query).await
    }

    async fn delete(&mut self, query: Query) -> Result<i64, IndexedDBError> {
        Index::delete(self, query).await
    }

    async fn open_cursor(
        &mut self,
        query: Query,
        direction: CursorDirection,
    ) -> Result<Cursor, IndexedDBError> {
        Index::open_cursor(self, query, direction).await
    }

    async fn open_key_cursor(
        &mut self,
        query: Query,
        direction: CursorDirection,
    ) -> Result<Cursor, IndexedDBError> {
        Index::open_key_cursor(self, query, direction).await
    }
}

#[async_trait]
impl TransactionApi for Transaction {
    type ObjectStore<'a> = TransactionObjectStore<'a>;

    fn object_store<'a>(&'a mut self, name: &str) -> TransactionObjectStore<'a> {
        Transaction::object_store(self, name)
    }

    async fn commit(&mut self) -> Result<(), IndexedDBError> {
        Transaction::commit(self).await
    }

    async fn abort(&mut self, reason: &str) -> Result<(), IndexedDBError> {
        Transaction::abort(self, reason).await
    }
}

#[async_trait]
impl<'tx> TransactionObjectStoreApi for TransactionObjectStore<'tx> {
    type Index<'a>
        = TransactionIndex<'a>
    where
        Self: 'a;

    async fn get(&mut self, id: &str) -> Result<Record, IndexedDBError> {
        TransactionObjectStore::get(self, id).await
    }

    async fn get_key(&mut self, id: &str) -> Result<String, IndexedDBError> {
        TransactionObjectStore::get_key(self, id).await
    }

    async fn add(&mut self, record: Record) -> Result<(), IndexedDBError> {
        TransactionObjectStore::add(self, record).await
    }

    async fn put(&mut self, record: Record) -> Result<(), IndexedDBError> {
        TransactionObjectStore::put(self, record).await
    }

    async fn delete(&mut self, id: &str) -> Result<(), IndexedDBError> {
        TransactionObjectStore::delete(self, id).await
    }

    async fn clear(&mut self) -> Result<(), IndexedDBError> {
        TransactionObjectStore::clear(self).await
    }

    async fn get_all(&mut self, query: Query) -> Result<Vec<Record>, IndexedDBError> {
        TransactionObjectStore::get_all(self, query, None).await
    }

    async fn get_all_keys(&mut self, query: Query) -> Result<Vec<String>, IndexedDBError> {
        TransactionObjectStore::get_all_keys(self, query, None).await
    }

    async fn count(&mut self, query: Query) -> Result<i64, IndexedDBError> {
        TransactionObjectStore::count(self, query).await
    }

    async fn delete_range(&mut self, query: Query) -> Result<i64, IndexedDBError> {
        TransactionObjectStore::delete_range(self, query).await
    }

    fn index<'a>(&'a mut self, name: &str) -> TransactionIndex<'a> {
        TransactionObjectStore::index(self, name)
    }
}

#[async_trait]
impl TransactionIndexApi for TransactionIndex<'_> {
    async fn get(&mut self, query: Query) -> Result<Record, IndexedDBError> {
        TransactionIndex::get(self, query).await
    }

    async fn get_key(&mut self, query: Query) -> Result<String, IndexedDBError> {
        TransactionIndex::get_key(self, query).await
    }

    async fn get_all(&mut self, query: Query) -> Result<Vec<Record>, IndexedDBError> {
        TransactionIndex::get_all(self, query, None).await
    }

    async fn get_all_keys(&mut self, query: Query) -> Result<Vec<String>, IndexedDBError> {
        TransactionIndex::get_all_keys(self, query, None).await
    }

    async fn count(&mut self, query: Query) -> Result<i64, IndexedDBError> {
        TransactionIndex::count(self, query).await
    }

    async fn delete(&mut self, query: Query) -> Result<i64, IndexedDBError> {
        TransactionIndex::delete(self, query).await
    }
}

#[async_trait]
impl CursorApi for Cursor {
    fn key(&self) -> Option<Key> {
        Cursor::key(self)
    }

    fn primary_key(&self) -> &str {
        Cursor::primary_key(self)
    }

    fn value(&self) -> Result<Record, IndexedDBError> {
        Cursor::value(self)
    }

    async fn continue_next(&mut self) -> Result<bool, IndexedDBError> {
        Cursor::continue_next(self).await
    }

    async fn continue_to_key(
        &mut self,
        key: impl Into<Key> + Send,
    ) -> Result<bool, IndexedDBError> {
        Cursor::continue_to_key(self, key).await
    }

    async fn advance(&mut self, count: i32) -> Result<bool, IndexedDBError> {
        Cursor::advance(self, count).await
    }

    async fn delete(&mut self) -> Result<(), IndexedDBError> {
        Cursor::delete(self).await
    }

    async fn update(&mut self, value: Record) -> Result<(), IndexedDBError> {
        Cursor::update(self, value).await
    }

    async fn close(self) -> Result<(), IndexedDBError> {
        Cursor::close(self).await
    }
}

fn map_status(err: tonic::Status) -> IndexedDBError {
    match err.code() {
        tonic::Code::NotFound => IndexedDBError::NotFound,
        tonic::Code::AlreadyExists => IndexedDBError::AlreadyExists,
        tonic::Code::InvalidArgument => IndexedDBError::InvalidArgument(err.message().to_string()),
        tonic::Code::FailedPrecondition => IndexedDBError::Transaction(err.message().to_string()),
        _ => IndexedDBError::Status(err),
    }
}

fn map_rpc_status(
    status: Option<crate::generated::google::rpc::Status>,
) -> Result<(), IndexedDBError> {
    let Some(status) = status else {
        return Ok(());
    };
    match status.code {
        0 => Ok(()),
        5 => Err(IndexedDBError::NotFound),
        6 => Err(IndexedDBError::AlreadyExists),
        3 => Err(IndexedDBError::InvalidArgument(status.message)),
        9 => Err(IndexedDBError::Transaction(status.message)),
        _ => Err(IndexedDBError::Transaction(status.message)),
    }
}

fn unexpected_transaction_result() -> IndexedDBError {
    IndexedDBError::Transaction("unexpected transaction operation result".to_string())
}

fn record_to_pb_record(record: Record) -> pb::Record {
    pb::Record {
        fields: record
            .into_iter()
            .map(|(k, v)| (k, json_to_typed_value(&v)))
            .collect(),
    }
}

fn pb_record_to_record(r: &pb::Record) -> Record {
    r.fields
        .iter()
        .map(|(k, v)| (k.clone(), typed_value_to_json(v)))
        .collect()
}

fn json_to_typed_value(v: &serde_json::Value) -> pb::TypedValue {
    use pb::typed_value::Kind;
    let kind = match v {
        serde_json::Value::Null => Kind::NullValue(0),
        serde_json::Value::Bool(b) => Kind::BoolValue(*b),
        serde_json::Value::Number(n) => {
            if let Some(i) = n.as_i64() {
                Kind::IntValue(i)
            } else {
                Kind::FloatValue(n.as_f64().unwrap_or(0.0))
            }
        }
        serde_json::Value::String(s) => Kind::StringValue(s.clone()),
        serde_json::Value::Array(arr) => {
            let values = arr.iter().map(json_to_prost_value).collect();
            Kind::JsonValue(prost_types::Value {
                kind: Some(prost_types::value::Kind::ListValue(
                    prost_types::ListValue { values },
                )),
            })
        }
        serde_json::Value::Object(obj) => {
            let fields = obj
                .iter()
                .map(|(k, v)| (k.clone(), json_to_prost_value(v)))
                .collect();
            Kind::JsonValue(prost_types::Value {
                kind: Some(prost_types::value::Kind::StructValue(prost_types::Struct {
                    fields,
                })),
            })
        }
    };
    pb::TypedValue { kind: Some(kind) }
}

fn prost_value_to_json(v: &prost_types::Value) -> serde_json::Value {
    use prost_types::value::Kind;
    match &v.kind {
        Some(Kind::NullValue(_)) => serde_json::Value::Null,
        Some(Kind::BoolValue(b)) => serde_json::Value::Bool(*b),
        Some(Kind::NumberValue(n)) => serde_json::json!(*n),
        Some(Kind::StringValue(s)) => serde_json::Value::String(s.clone()),
        Some(Kind::ListValue(list)) => {
            serde_json::Value::Array(list.values.iter().map(prost_value_to_json).collect())
        }
        Some(Kind::StructValue(st)) => {
            let obj: serde_json::Map<String, serde_json::Value> = st
                .fields
                .iter()
                .map(|(k, v)| (k.clone(), prost_value_to_json(v)))
                .collect();
            serde_json::Value::Object(obj)
        }
        None => serde_json::Value::Null,
    }
}

fn json_to_prost_value(v: &serde_json::Value) -> prost_types::Value {
    use prost_types::value::Kind;
    let kind = match v {
        serde_json::Value::Null => Kind::NullValue(0),
        serde_json::Value::Bool(b) => Kind::BoolValue(*b),
        serde_json::Value::Number(n) => Kind::NumberValue(n.as_f64().unwrap_or(0.0)),
        serde_json::Value::String(s) => Kind::StringValue(s.clone()),
        serde_json::Value::Array(arr) => {
            let values = arr.iter().map(json_to_prost_value).collect();
            Kind::ListValue(prost_types::ListValue { values })
        }
        serde_json::Value::Object(obj) => {
            let fields = obj
                .iter()
                .map(|(k, v)| (k.clone(), json_to_prost_value(v)))
                .collect();
            Kind::StructValue(prost_types::Struct { fields })
        }
    };
    prost_types::Value { kind: Some(kind) }
}

fn typed_value_to_json(v: &pb::TypedValue) -> serde_json::Value {
    use pb::typed_value::Kind;
    match &v.kind {
        Some(Kind::NullValue(_)) => serde_json::Value::Null,
        Some(Kind::BoolValue(b)) => serde_json::Value::Bool(*b),
        Some(Kind::IntValue(i)) => serde_json::json!(*i),
        Some(Kind::FloatValue(f)) => serde_json::json!(*f),
        Some(Kind::StringValue(s)) => serde_json::Value::String(s.clone()),
        Some(Kind::BytesValue(b)) => serde_json::json!(b),
        Some(Kind::JsonValue(pv)) => prost_value_to_json(pv),
        Some(Kind::TimeValue(ts)) => {
            serde_json::Value::String(format!("{}.{}", ts.seconds, ts.nanos))
        }
        None => serde_json::Value::Null,
    }
}

fn relay_token_interceptor(
    token: &str,
    binding: &str,
) -> Result<RelayTokenInterceptor, IndexedDBError> {
    let relay_token = if token.trim().is_empty() {
        None
    } else {
        Some(MetadataValue::try_from(token.to_string()).map_err(|err| {
            IndexedDBError::Env(format!("invalid IndexedDB relay token metadata: {err}"))
        })?)
    };
    let binding = if binding.trim().is_empty() {
        None
    } else {
        Some(
            MetadataValue::try_from(binding.trim().to_string()).map_err(|err| {
                IndexedDBError::Env(format!("invalid IndexedDB binding metadata: {err}"))
            })?,
        )
    };
    Ok(RelayTokenInterceptor {
        relay_token,
        binding,
    })
}

#[derive(Clone)]
struct RelayTokenInterceptor {
    relay_token: Option<MetadataValue<tonic::metadata::Ascii>>,
    binding: Option<MetadataValue<tonic::metadata::Ascii>>,
}

impl Interceptor for RelayTokenInterceptor {
    fn call(&mut self, mut request: Request<()>) -> Result<Request<()>, tonic::Status> {
        if let Some(header) = self.relay_token.clone() {
            request
                .metadata_mut()
                .insert(INDEXEDDB_RELAY_TOKEN_HEADER, header);
        }
        if let Some(header) = self.binding.clone() {
            request
                .metadata_mut()
                .insert(HOST_SERVICE_BINDING_HEADER, header);
        }
        Ok(request)
    }
}
