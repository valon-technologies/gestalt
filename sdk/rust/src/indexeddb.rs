use std::collections::BTreeMap;

use hyper_util::rt::TokioIo;
use tokio::sync::mpsc;
use tokio_stream::wrappers::ReceiverStream;
use tonic::transport::{Channel, Endpoint, Uri};
use tower::service_fn;

use crate::generated::v1::{self as pb, indexed_db_client::IndexedDbClient};

pub const ENV_INDEXEDDB_SOCKET: &str = "GESTALT_INDEXEDDB_SOCKET";

const CURSOR_CHANNEL_BUFFER: usize = 1;

#[derive(Debug, thiserror::Error)]
pub enum IndexedDBError {
    #[error("not found")]
    NotFound,
    #[error("already exists")]
    AlreadyExists,
    #[error("cursor is keys-only; value not available")]
    KeysOnly,
    #[error("{0}")]
    Transport(#[from] tonic::transport::Error),
    #[error("{0}")]
    Status(#[from] tonic::Status),
    #[error("{0}")]
    Env(String),
}

pub type Record = BTreeMap<String, serde_json::Value>;

pub struct KeyRange {
    pub lower: Option<serde_json::Value>,
    pub upper: Option<serde_json::Value>,
    pub lower_open: bool,
    pub upper_open: bool,
}

pub struct IndexSchema {
    pub name: String,
    pub key_path: Vec<String>,
    pub unique: bool,
}

pub struct ObjectStoreSchema {
    pub indexes: Vec<IndexSchema>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CursorDirection {
    Next,
    NextUnique,
    Prev,
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

pub struct Cursor {
    tx: mpsc::Sender<pb::CursorClientMessage>,
    stream: tonic::Streaming<pb::CursorResponse>,
    keys_only: bool,
    index_cursor: bool,
    entry: Option<pb::CursorEntry>,
    done: bool,
}

impl Cursor {
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

    pub fn primary_key(&self) -> &str {
        self.entry
            .as_ref()
            .map(|e| e.primary_key.as_str())
            .unwrap_or("")
    }

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

    pub async fn continue_next(&mut self) -> Result<bool, IndexedDBError> {
        let cmd = pb::cursor_command::Command::Next(true);
        self.send_and_recv(cmd).await
    }

    pub async fn continue_to_key(
        &mut self,
        key: serde_json::Value,
    ) -> Result<bool, IndexedDBError> {
        let cmd = pb::cursor_command::Command::ContinueToKey(pb::CursorKeyTarget {
            key: cursor_key_to_proto(&key, self.index_cursor),
        });
        self.send_and_recv(cmd).await
    }

    pub async fn advance(&mut self, count: i32) -> Result<bool, IndexedDBError> {
        let cmd = pb::cursor_command::Command::Advance(count);
        self.send_and_recv(cmd).await
    }

    pub async fn delete(&mut self) -> Result<(), IndexedDBError> {
        if self.done {
            return Err(IndexedDBError::NotFound);
        }
        let cmd = pb::cursor_command::Command::Delete(true);
        self.send_mutation(cmd).await
    }

    pub async fn update(&mut self, value: Record) -> Result<(), IndexedDBError> {
        if self.done {
            return Err(IndexedDBError::NotFound);
        }
        let cmd = pb::cursor_command::Command::Update(record_to_pb_record(value));
        self.send_mutation(cmd).await
    }

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
        if let Some(resp) = self.stream.message().await.map_err(map_status)? {
            if let Some(pb::cursor_response::Result::Entry(entry)) = resp.result {
                self.entry = Some(entry);
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
    client: &mut IndexedDbClient<Channel>,
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
    let _ack = stream.message().await.map_err(map_status)?;

    Ok(Cursor {
        tx,
        stream,
        keys_only,
        entry: None,
        done: false,
        index_cursor: is_index,
    })
}

pub struct IndexedDB {
    client: IndexedDbClient<Channel>,
}

impl IndexedDB {
    pub async fn connect() -> Result<Self, IndexedDBError> {
        let socket_path = std::env::var(ENV_INDEXEDDB_SOCKET)
            .map_err(|_| IndexedDBError::Env(format!("{ENV_INDEXEDDB_SOCKET} is not set")))?;

        let channel = Endpoint::try_from("http://[::]:50051")?
            .connect_with_connector(service_fn(move |_: Uri| {
                let path = socket_path.clone();
                async move {
                    tokio::net::UnixStream::connect(path)
                        .await
                        .map(TokioIo::new)
                }
            }))
            .await?;

        Ok(Self {
            client: IndexedDbClient::new(channel),
        })
    }

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

    pub async fn delete_object_store(&mut self, name: &str) -> Result<(), IndexedDBError> {
        self.client
            .delete_object_store(pb::DeleteObjectStoreRequest {
                name: name.to_string(),
            })
            .await
            .map_err(map_status)?;
        Ok(())
    }

    pub fn object_store(&self, name: &str) -> ObjectStore {
        ObjectStore {
            client: self.client.clone(),
            store: name.to_string(),
        }
    }
}

pub struct ObjectStore {
    client: IndexedDbClient<Channel>,
    store: String,
}

impl ObjectStore {
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

    pub async fn clear(&mut self) -> Result<(), IndexedDBError> {
        self.client
            .clear(pb::ObjectStoreNameRequest {
                store: self.store.clone(),
            })
            .await
            .map_err(map_status)?;
        Ok(())
    }

    pub fn index(&self, name: &str) -> IndexClient {
        IndexClient {
            client: self.client.clone(),
            store: self.store.clone(),
            index: name.to_string(),
        }
    }

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

pub struct IndexClient {
    client: IndexedDbClient<Channel>,
    store: String,
    index: String,
}

impl IndexClient {
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

    pub async fn get_all(
        &mut self,
        values: &[serde_json::Value],
    ) -> Result<Vec<Record>, IndexedDBError> {
        let resp = self
            .client
            .index_get_all(pb::IndexQueryRequest {
                store: self.store.clone(),
                index: self.index.clone(),
                values: values.iter().map(json_to_typed_value).collect(),
                range: None,
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
