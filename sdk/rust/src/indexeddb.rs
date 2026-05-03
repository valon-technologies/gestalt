use std::collections::BTreeMap;

use hyper_util::rt::TokioIo;
use tokio::sync::mpsc;
use tokio_stream::wrappers::ReceiverStream;
use tonic::Request;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, ClientTlsConfig, Endpoint, Uri};
use tower::service_fn;

use crate::generated::v1::{self as pb, indexed_db_client::IndexedDbClient};

type IndexedDbTransport = InterceptedService<Channel, RelayTokenInterceptor>;

/// Default Unix-socket environment variable used by [`IndexedDB::connect`].
pub const ENV_INDEXEDDB_SOCKET: &str = "GESTALT_INDEXEDDB_SOCKET";
/// Suffix added to named IndexedDB socket variables for relay-token variables.
pub const ENV_INDEXEDDB_SOCKET_TOKEN_SUFFIX: &str = "_TOKEN";
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

/// Constrains a query or cursor by lower and upper bounds.
pub struct KeyRange {
    /// Lower bound, inclusive unless `lower_open` is true.
    pub lower: Option<serde_json::Value>,
    /// Upper bound, inclusive unless `upper_open` is true.
    pub upper: Option<serde_json::Value>,
    /// Whether the lower bound is exclusive.
    pub lower_open: bool,
    /// Whether the upper bound is exclusive.
    pub upper_open: bool,
}

/// Describes one secondary index on an object store.
pub struct IndexSchema {
    /// Index name.
    pub name: String,
    /// Record path used as the index key.
    pub key_path: Vec<String>,
    /// Whether the index enforces uniqueness.
    pub unique: bool,
}

/// Describes the indexes attached to an object store.
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

/// Streaming cursor over object store or secondary index rows.
pub struct Cursor {
    tx: mpsc::Sender<pb::CursorClientMessage>,
    stream: tonic::Streaming<pb::CursorResponse>,
    keys_only: bool,
    index_cursor: bool,
    entry: Option<pb::CursorEntry>,
    done: bool,
}

impl Cursor {
    /// Returns the current cursor key.
    pub fn key(&self) -> Option<serde_json::Value> {
        let entry = self.entry.as_ref()?;
        match entry.key.len() {
            0 => None,
            1 if !self.index_cursor => Some(key_value_to_json(&entry.key[0])),
            _ => Some(serde_json::Value::Array(
                entry.key.iter().map(key_value_to_json).collect(),
            )),
        }
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
    pub async fn continue_to_key(
        &mut self,
        key: serde_json::Value,
    ) -> Result<bool, IndexedDBError> {
        let cmd = pb::cursor_command::Command::ContinueToKey(pb::CursorKeyTarget {
            key: cursor_key_to_proto(&key, self.index_cursor),
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
    let is_index = !req.index.is_empty();
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
        index_cursor: is_index,
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
        let env_name = indexeddb_socket_env(name);
        let target = std::env::var(&env_name)
            .map_err(|_| IndexedDBError::Env(format!("{env_name} is not set")))?;
        let token = std::env::var(indexeddb_socket_token_env(name)).unwrap_or_default();
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

        let client =
            IndexedDbClient::with_interceptor(channel, relay_token_interceptor(token.trim())?);

        Ok(Self { client })
    }

    /// Creates a named object store.
    pub async fn create_object_store(
        &mut self,
        name: &str,
        schema: ObjectStoreSchema,
    ) -> Result<(), IndexedDBError> {
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
        Ok(())
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

    /// Loads every row that matches range inside the transaction.
    pub async fn get_all(
        &mut self,
        range: Option<KeyRange>,
    ) -> Result<Vec<Record>, IndexedDBError> {
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::GetAll(
                pb::ObjectStoreRangeRequest {
                    store: self.store.clone(),
                    range: range.map(key_range_to_pb),
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

    /// Loads every primary key that matches range inside the transaction.
    pub async fn get_all_keys(
        &mut self,
        range: Option<KeyRange>,
    ) -> Result<Vec<String>, IndexedDBError> {
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::GetAllKeys(
                pb::ObjectStoreRangeRequest {
                    store: self.store.clone(),
                    range: range.map(key_range_to_pb),
                },
            ))
            .await?;
        match resp.result {
            Some(pb::transaction_operation_response::Result::Keys(keys)) => Ok(keys.keys),
            _ => Err(unexpected_transaction_result()),
        }
    }

    /// Counts rows that match range inside the transaction.
    pub async fn count(&mut self, range: Option<KeyRange>) -> Result<i64, IndexedDBError> {
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::Count(
                pb::ObjectStoreRangeRequest {
                    store: self.store.clone(),
                    range: range.map(key_range_to_pb),
                },
            ))
            .await?;
        match resp.result {
            Some(pb::transaction_operation_response::Result::Count(count)) => Ok(count.count),
            _ => Err(unexpected_transaction_result()),
        }
    }

    /// Deletes rows that match range inside the transaction.
    pub async fn delete_range(&mut self, range: KeyRange) -> Result<i64, IndexedDBError> {
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::DeleteRange(
                pb::ObjectStoreRangeRequest {
                    store: self.store.clone(),
                    range: Some(key_range_to_pb(range)),
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
    pub fn index<'a>(&'a mut self, name: &str) -> TransactionIndexClient<'a> {
        TransactionIndexClient {
            tx: &mut *self.tx,
            store: self.store.clone(),
            index: name.to_string(),
        }
    }
}

/// Secondary-index operations scoped to an explicit transaction.
pub struct TransactionIndexClient<'a> {
    tx: &'a mut Transaction,
    store: String,
    index: String,
}

impl TransactionIndexClient<'_> {
    /// Loads the first row that matches values inside the transaction.
    pub async fn get(&mut self, values: &[serde_json::Value]) -> Result<Record, IndexedDBError> {
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::IndexGet(
                self.index_request(values, None),
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
    pub async fn get_key(
        &mut self,
        values: &[serde_json::Value],
    ) -> Result<String, IndexedDBError> {
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::IndexGetKey(
                self.index_request(values, None),
            ))
            .await?;
        match resp.result {
            Some(pb::transaction_operation_response::Result::Key(key)) => Ok(key.key),
            _ => Err(unexpected_transaction_result()),
        }
    }

    /// Loads every row that matches values and range inside the transaction.
    pub async fn get_all(
        &mut self,
        values: &[serde_json::Value],
        range: Option<KeyRange>,
    ) -> Result<Vec<Record>, IndexedDBError> {
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::IndexGetAll(
                self.index_request(values, range),
            ))
            .await?;
        match resp.result {
            Some(pb::transaction_operation_response::Result::Records(records)) => {
                Ok(records.records.iter().map(pb_record_to_record).collect())
            }
            _ => Err(unexpected_transaction_result()),
        }
    }

    /// Loads every primary key that matches values and range inside the transaction.
    pub async fn get_all_keys(
        &mut self,
        values: &[serde_json::Value],
        range: Option<KeyRange>,
    ) -> Result<Vec<String>, IndexedDBError> {
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::IndexGetAllKeys(
                self.index_request(values, range),
            ))
            .await?;
        match resp.result {
            Some(pb::transaction_operation_response::Result::Keys(keys)) => Ok(keys.keys),
            _ => Err(unexpected_transaction_result()),
        }
    }

    /// Counts rows that match values and range inside the transaction.
    pub async fn count(
        &mut self,
        values: &[serde_json::Value],
        range: Option<KeyRange>,
    ) -> Result<i64, IndexedDBError> {
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::IndexCount(
                self.index_request(values, range),
            ))
            .await?;
        match resp.result {
            Some(pb::transaction_operation_response::Result::Count(count)) => Ok(count.count),
            _ => Err(unexpected_transaction_result()),
        }
    }

    /// Deletes rows that match values inside the transaction.
    pub async fn delete(&mut self, values: &[serde_json::Value]) -> Result<i64, IndexedDBError> {
        let resp = self
            .tx
            .send_operation(pb::transaction_operation::Operation::IndexDelete(
                self.index_request(values, None),
            ))
            .await?;
        match resp.result {
            Some(pb::transaction_operation_response::Result::Delete(deleted)) => {
                Ok(deleted.deleted)
            }
            _ => Err(unexpected_transaction_result()),
        }
    }

    fn index_request(
        &self,
        values: &[serde_json::Value],
        range: Option<KeyRange>,
    ) -> pb::IndexQueryRequest {
        pb::IndexQueryRequest {
            store: self.store.clone(),
            index: self.index.clone(),
            values: values.iter().map(json_to_typed_value).collect(),
            range: range.map(key_range_to_pb),
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

    /// Loads every row that matches range.
    pub async fn get_all(
        &mut self,
        range: Option<KeyRange>,
    ) -> Result<Vec<Record>, IndexedDBError> {
        let resp = self
            .client
            .get_all(pb::ObjectStoreRangeRequest {
                store: self.store.clone(),
                range: range.map(key_range_to_pb),
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

    /// Loads every primary key that matches range.
    pub async fn get_all_keys(
        &mut self,
        range: Option<KeyRange>,
    ) -> Result<Vec<String>, IndexedDBError> {
        let resp = self
            .client
            .get_all_keys(pb::ObjectStoreRangeRequest {
                store: self.store.clone(),
                range: range.map(key_range_to_pb),
            })
            .await
            .map_err(map_status)?;
        Ok(resp.into_inner().keys)
    }

    /// Counts rows that match range.
    pub async fn count(&mut self, range: Option<KeyRange>) -> Result<i64, IndexedDBError> {
        let resp = self
            .client
            .count(pb::ObjectStoreRangeRequest {
                store: self.store.clone(),
                range: range.map(key_range_to_pb),
            })
            .await
            .map_err(map_status)?;
        Ok(resp.into_inner().count)
    }

    /// Deletes rows that match range and returns the delete count.
    pub async fn delete_range(&mut self, range: KeyRange) -> Result<i64, IndexedDBError> {
        let resp = self
            .client
            .delete_range(pb::ObjectStoreRangeRequest {
                store: self.store.clone(),
                range: Some(key_range_to_pb(range)),
            })
            .await
            .map_err(map_status)?;
        Ok(resp.into_inner().deleted)
    }

    /// Returns a typed handle for one secondary index.
    pub fn index(&self, name: &str) -> IndexClient {
        IndexClient {
            client: self.client.clone(),
            store: self.store.clone(),
            index: name.to_string(),
        }
    }

    /// Opens a full-value cursor over the object store.
    pub async fn open_cursor(
        &mut self,
        range: Option<KeyRange>,
        direction: CursorDirection,
    ) -> Result<Cursor, IndexedDBError> {
        let req = pb::OpenCursorRequest {
            store: self.store.clone(),
            range: range.map(key_range_to_pb),
            direction: direction.to_proto(),
            keys_only: false,
            index: String::new(),
            values: vec![],
        };
        open_cursor_inner(&mut self.client, req).await
    }

    /// Opens a key-only cursor over the object store.
    pub async fn open_key_cursor(
        &mut self,
        range: Option<KeyRange>,
        direction: CursorDirection,
    ) -> Result<Cursor, IndexedDBError> {
        let req = pb::OpenCursorRequest {
            store: self.store.clone(),
            range: range.map(key_range_to_pb),
            direction: direction.to_proto(),
            keys_only: true,
            index: String::new(),
            values: vec![],
        };
        open_cursor_inner(&mut self.client, req).await
    }
}

/// Lookup and cursor access through one secondary index.
pub struct IndexClient {
    client: IndexedDbClient<IndexedDbTransport>,
    store: String,
    index: String,
}

impl IndexClient {
    /// Loads the first row that matches values.
    pub async fn get(&mut self, values: &[serde_json::Value]) -> Result<Record, IndexedDBError> {
        let resp = self
            .client
            .index_get(pb::IndexQueryRequest {
                store: self.store.clone(),
                index: self.index.clone(),
                values: values.iter().map(json_to_typed_value).collect(),
                range: None,
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

    /// Resolves the primary key for the first row that matches values.
    pub async fn get_key(
        &mut self,
        values: &[serde_json::Value],
    ) -> Result<String, IndexedDBError> {
        let resp = self
            .client
            .index_get_key(pb::IndexQueryRequest {
                store: self.store.clone(),
                index: self.index.clone(),
                values: values.iter().map(json_to_typed_value).collect(),
                range: None,
            })
            .await
            .map_err(map_status)?;
        Ok(resp.into_inner().key)
    }

    /// Loads every row that matches values and range.
    pub async fn get_all(
        &mut self,
        values: &[serde_json::Value],
        range: Option<KeyRange>,
    ) -> Result<Vec<Record>, IndexedDBError> {
        let resp = self
            .client
            .index_get_all(pb::IndexQueryRequest {
                store: self.store.clone(),
                index: self.index.clone(),
                values: values.iter().map(json_to_typed_value).collect(),
                range: range.map(key_range_to_pb),
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

    /// Loads every primary key that matches values and range.
    pub async fn get_all_keys(
        &mut self,
        values: &[serde_json::Value],
        range: Option<KeyRange>,
    ) -> Result<Vec<String>, IndexedDBError> {
        let resp = self
            .client
            .index_get_all_keys(pb::IndexQueryRequest {
                store: self.store.clone(),
                index: self.index.clone(),
                values: values.iter().map(json_to_typed_value).collect(),
                range: range.map(key_range_to_pb),
            })
            .await
            .map_err(map_status)?;
        Ok(resp.into_inner().keys)
    }

    /// Counts rows that match values and range.
    pub async fn count(
        &mut self,
        values: &[serde_json::Value],
        range: Option<KeyRange>,
    ) -> Result<i64, IndexedDBError> {
        let resp = self
            .client
            .index_count(pb::IndexQueryRequest {
                store: self.store.clone(),
                index: self.index.clone(),
                values: values.iter().map(json_to_typed_value).collect(),
                range: range.map(key_range_to_pb),
            })
            .await
            .map_err(map_status)?;
        Ok(resp.into_inner().count)
    }

    /// Deletes rows that match values and returns the delete count.
    pub async fn delete(&mut self, values: &[serde_json::Value]) -> Result<i64, IndexedDBError> {
        let resp = self
            .client
            .index_delete(pb::IndexQueryRequest {
                store: self.store.clone(),
                index: self.index.clone(),
                values: values.iter().map(json_to_typed_value).collect(),
                range: None,
            })
            .await
            .map_err(map_status)?;
        Ok(resp.into_inner().deleted)
    }

    /// Opens a full-value cursor over the secondary index.
    pub async fn open_cursor(
        &mut self,
        values: &[serde_json::Value],
        range: Option<KeyRange>,
        direction: CursorDirection,
    ) -> Result<Cursor, IndexedDBError> {
        let req = pb::OpenCursorRequest {
            store: self.store.clone(),
            range: range.map(key_range_to_pb),
            direction: direction.to_proto(),
            keys_only: false,
            index: self.index.clone(),
            values: values.iter().map(json_to_typed_value).collect(),
        };
        open_cursor_inner(&mut self.client, req).await
    }

    /// Opens a key-only cursor over the secondary index.
    pub async fn open_key_cursor(
        &mut self,
        values: &[serde_json::Value],
        range: Option<KeyRange>,
        direction: CursorDirection,
    ) -> Result<Cursor, IndexedDBError> {
        let req = pb::OpenCursorRequest {
            store: self.store.clone(),
            range: range.map(key_range_to_pb),
            direction: direction.to_proto(),
            keys_only: true,
            index: self.index.clone(),
            values: values.iter().map(json_to_typed_value).collect(),
        };
        open_cursor_inner(&mut self.client, req).await
    }
}

fn map_status(err: tonic::Status) -> IndexedDBError {
    match err.code() {
        tonic::Code::NotFound => IndexedDBError::NotFound,
        tonic::Code::AlreadyExists => IndexedDBError::AlreadyExists,
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
        3 | 9 => Err(IndexedDBError::Transaction(status.message)),
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

fn key_value_to_json(kv: &pb::KeyValue) -> serde_json::Value {
    match &kv.kind {
        Some(pb::key_value::Kind::Scalar(tv)) => typed_value_to_json(tv),
        Some(pb::key_value::Kind::Array(arr)) => {
            serde_json::Value::Array(arr.elements.iter().map(key_value_to_json).collect())
        }
        None => serde_json::Value::Null,
    }
}

fn json_to_key_value(v: &serde_json::Value) -> pb::KeyValue {
    if let serde_json::Value::Array(arr) = v {
        pb::KeyValue {
            kind: Some(pb::key_value::Kind::Array(pb::KeyValueArray {
                elements: arr.iter().map(json_to_key_value).collect(),
            })),
        }
    } else {
        pb::KeyValue {
            kind: Some(pb::key_value::Kind::Scalar(json_to_typed_value(v))),
        }
    }
}

fn cursor_key_to_proto(key: &serde_json::Value, index_cursor: bool) -> Vec<pb::KeyValue> {
    if index_cursor {
        if let serde_json::Value::Array(parts) = key {
            return parts.iter().map(json_to_key_value).collect();
        }
    }
    vec![json_to_key_value(key)]
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

fn key_range_to_pb(kr: KeyRange) -> pb::KeyRange {
    pb::KeyRange {
        lower: kr.lower.map(|v| json_to_typed_value(&v)),
        upper: kr.upper.map(|v| json_to_typed_value(&v)),
        lower_open: kr.lower_open,
        upper_open: kr.upper_open,
    }
}
/// Returns the environment variable used for a named IndexedDB socket.
pub fn indexeddb_socket_env(name: &str) -> String {
    let trimmed = name.trim();
    if trimmed.is_empty() {
        return ENV_INDEXEDDB_SOCKET.to_string();
    }
    let mut env = String::from(ENV_INDEXEDDB_SOCKET);
    env.push('_');
    for ch in trimmed.chars() {
        if ch.is_ascii_alphanumeric() {
            env.push(ch.to_ascii_uppercase());
        } else {
            env.push('_');
        }
    }
    env
}

/// Returns the environment variable used for a named IndexedDB relay token.
pub fn indexeddb_socket_token_env(name: &str) -> String {
    format!(
        "{}{}",
        indexeddb_socket_env(name),
        ENV_INDEXEDDB_SOCKET_TOKEN_SUFFIX
    )
}

fn relay_token_interceptor(token: &str) -> Result<RelayTokenInterceptor, IndexedDBError> {
    let header = if token.trim().is_empty() {
        None
    } else {
        Some(MetadataValue::try_from(token.to_string()).map_err(|err| {
            IndexedDBError::Env(format!("invalid IndexedDB relay token metadata: {err}"))
        })?)
    };
    Ok(RelayTokenInterceptor { header })
}

#[derive(Clone)]
struct RelayTokenInterceptor {
    header: Option<MetadataValue<tonic::metadata::Ascii>>,
}

impl Interceptor for RelayTokenInterceptor {
    fn call(&mut self, mut request: Request<()>) -> Result<Request<()>, tonic::Status> {
        if let Some(header) = self.header.clone() {
            request
                .metadata_mut()
                .insert(INDEXEDDB_RELAY_TOKEN_HEADER, header);
        }
        Ok(request)
    }
}
