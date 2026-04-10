use std::collections::BTreeMap;

use hyper_util::rt::TokioIo;
use tonic::transport::{Channel, Endpoint, Uri};
use tower::service_fn;

use crate::generated::v1::{self as pb, indexed_db_client::IndexedDbClient};

pub const ENV_INDEXEDDB_SOCKET: &str = "GESTALT_INDEXEDDB_SOCKET";

#[derive(Debug, thiserror::Error)]
pub enum IndexedDBError {
    #[error("not found")]
    NotFound,
    #[error("already exists")]
    AlreadyExists,
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
        Ok(prost_struct_to_record(resp.into_inner().record))
    }

    pub async fn add(&mut self, record: Record) -> Result<(), IndexedDBError> {
        self.client
            .add(pb::RecordRequest {
                store: self.store.clone(),
                record: Some(record_to_prost_struct(record)),
            })
            .await
            .map_err(map_status)?;
        Ok(())
    }

    pub async fn put(&mut self, record: Record) -> Result<(), IndexedDBError> {
        self.client
            .put(pb::RecordRequest {
                store: self.store.clone(),
                record: Some(record_to_prost_struct(record)),
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
                values: values.iter().map(json_to_prost_value).collect(),
                range: None,
            })
            .await
            .map_err(map_status)?;
        Ok(prost_struct_to_record(resp.into_inner().record))
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
                values: values.iter().map(json_to_prost_value).collect(),
                range: None,
            })
            .await
            .map_err(map_status)?;
        Ok(resp
            .into_inner()
            .records
            .into_iter()
            .map(|s| prost_struct_to_record(Some(s)))
            .collect())
    }

    pub async fn delete(&mut self, values: &[serde_json::Value]) -> Result<i64, IndexedDBError> {
        let resp = self
            .client
            .index_delete(pb::IndexQueryRequest {
                store: self.store.clone(),
                index: self.index.clone(),
                values: values.iter().map(json_to_prost_value).collect(),
                range: None,
            })
            .await
            .map_err(map_status)?;
        Ok(resp.into_inner().deleted)
    }
}

fn map_status(err: tonic::Status) -> IndexedDBError {
    match err.code() {
        tonic::Code::NotFound => IndexedDBError::NotFound,
        tonic::Code::AlreadyExists => IndexedDBError::AlreadyExists,
        _ => IndexedDBError::Status(err),
    }
}

fn record_to_prost_struct(record: Record) -> prost_types::Struct {
    prost_types::Struct {
        fields: record
            .into_iter()
            .map(|(k, v)| (k, json_to_prost_value(&v)))
            .collect(),
    }
}

fn prost_struct_to_record(s: Option<prost_types::Struct>) -> Record {
    match s {
        Some(s) => s
            .fields
            .into_iter()
            .map(|(k, v)| (k, prost_value_to_json(v)))
            .collect(),
        None => BTreeMap::new(),
    }
}

fn json_to_prost_value(v: &serde_json::Value) -> prost_types::Value {
    use prost_types::value::Kind;
    let kind = match v {
        serde_json::Value::Null => Kind::NullValue(0),
        serde_json::Value::Bool(b) => Kind::BoolValue(*b),
        serde_json::Value::Number(n) => Kind::NumberValue(n.as_f64().unwrap_or(0.0)),
        serde_json::Value::String(s) => Kind::StringValue(s.clone()),
        _ => Kind::StringValue(v.to_string()),
    };
    prost_types::Value { kind: Some(kind) }
}

fn prost_value_to_json(v: prost_types::Value) -> serde_json::Value {
    use prost_types::value::Kind;
    match v.kind {
        Some(Kind::NullValue(_)) => serde_json::Value::Null,
        Some(Kind::BoolValue(b)) => serde_json::Value::Bool(b),
        Some(Kind::NumberValue(n)) => serde_json::json!(n),
        Some(Kind::StringValue(s)) => serde_json::Value::String(s),
        _ => serde_json::Value::Null,
    }
}
