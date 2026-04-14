#![allow(dead_code)]

use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::{SystemTime, UNIX_EPOCH};

use gestalt::proto::v1::{self as pb, file_api_server::FileApi};
use gestalt::{FileAPIProvider, RuntimeMetadata};
use tokio::sync::mpsc;
use tokio_stream::wrappers::ReceiverStream;
use tonic::{Request, Response, Status};

#[derive(Clone)]
pub struct InMemoryFileAPIProvider {
    state: Arc<Mutex<State>>,
}

#[derive(Default)]
struct State {
    configured_name: String,
    next_object_id: usize,
    next_object_url: usize,
    objects: HashMap<String, StoredObject>,
    object_urls: HashMap<String, String>,
}

#[derive(Clone)]
struct StoredObject {
    kind: Kind,
    data: Vec<u8>,
    mime_type: String,
    name: String,
    last_modified: i64,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Kind {
    Blob,
    File,
}

impl Default for InMemoryFileAPIProvider {
    fn default() -> Self {
        Self {
            state: Arc::new(Mutex::new(State::default())),
        }
    }
}

impl InMemoryFileAPIProvider {
    pub fn configured_name(&self) -> String {
        self.state
            .lock()
            .expect("lock state")
            .configured_name
            .clone()
    }

    fn create_object(
        state: &mut State,
        kind: Kind,
        data: Vec<u8>,
        mime_type: String,
        name: String,
        last_modified: i64,
    ) -> pb::FileObject {
        state.next_object_id += 1;
        let id = format!(
            "{}-{}",
            if kind == Kind::File { "file" } else { "blob" },
            state.next_object_id
        );
        state.objects.insert(
            id.clone(),
            StoredObject {
                kind,
                data: data.clone(),
                mime_type: mime_type.clone(),
                name: name.clone(),
                last_modified,
            },
        );
        file_object_proto(
            &id,
            kind,
            data.len() as i64,
            &mime_type,
            &name,
            last_modified,
        )
    }

    fn object(state: &State, id: &str) -> Result<StoredObject, Status> {
        state
            .objects
            .get(id)
            .cloned()
            .ok_or_else(|| Status::not_found("object not found"))
    }

    fn materialize_parts(
        state: &State,
        parts: &[pb::BlobPart],
        endings: pb::LineEndings,
    ) -> Result<Vec<u8>, Status> {
        let mut data = Vec::new();
        for part in parts {
            match part.kind.as_ref() {
                Some(pb::blob_part::Kind::StringData(value)) => {
                    data.extend(convert_string_part(value, endings));
                }
                Some(pb::blob_part::Kind::BytesData(value)) => data.extend(value),
                Some(pb::blob_part::Kind::BlobId(id)) => {
                    data.extend(Self::object(state, id)?.data);
                }
                None => {}
            }
        }
        Ok(data)
    }
}

#[gestalt::async_trait]
impl FileAPIProvider for InMemoryFileAPIProvider {
    async fn configure(
        &self,
        name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> gestalt::Result<()> {
        self.state.lock().expect("lock state").configured_name = name.to_string();
        Ok(())
    }

    fn metadata(&self) -> Option<RuntimeMetadata> {
        Some(RuntimeMetadata {
            name: "fileapi-example".to_string(),
            display_name: "FileAPI Example".to_string(),
            description: "In-memory FileAPI test provider".to_string(),
            version: "0.1.0".to_string(),
        })
    }

    fn warnings(&self) -> Vec<String> {
        vec!["ephemeral storage".to_string()]
    }
}

#[tonic::async_trait]
impl FileApi for InMemoryFileAPIProvider {
    type OpenReadStreamStream = ReceiverStream<Result<pb::ReadChunk, Status>>;

    async fn create_blob(
        &self,
        request: Request<pb::CreateBlobRequest>,
    ) -> Result<Response<pb::FileObjectResponse>, Status> {
        let request = request.into_inner();
        let options = request.options.unwrap_or_default();
        let mut state = self.state.lock().expect("lock state");
        let data = Self::materialize_parts(
            &state,
            &request.parts,
            pb::LineEndings::try_from(options.endings).unwrap_or(pb::LineEndings::Transparent),
        )?;
        let object = Self::create_object(
            &mut state,
            Kind::Blob,
            data,
            normalize_type(&options.mime_type),
            String::new(),
            0,
        );
        Ok(Response::new(pb::FileObjectResponse {
            object: Some(object),
        }))
    }

    async fn create_file(
        &self,
        request: Request<pb::CreateFileRequest>,
    ) -> Result<Response<pb::FileObjectResponse>, Status> {
        let request = request.into_inner();
        let options = request.options.unwrap_or_default();
        let mut state = self.state.lock().expect("lock state");
        let data = Self::materialize_parts(
            &state,
            &request.file_bits,
            pb::LineEndings::try_from(options.endings).unwrap_or(pb::LineEndings::Transparent),
        )?;
        let object = Self::create_object(
            &mut state,
            Kind::File,
            data,
            normalize_type(&options.mime_type),
            request.file_name,
            resolve_last_modified(options.last_modified),
        );
        Ok(Response::new(pb::FileObjectResponse {
            object: Some(object),
        }))
    }

    async fn stat(
        &self,
        request: Request<pb::FileObjectRequest>,
    ) -> Result<Response<pb::FileObjectResponse>, Status> {
        let request = request.into_inner();
        let state = self.state.lock().expect("lock state");
        let object = Self::object(&state, &request.id)?;
        Ok(Response::new(pb::FileObjectResponse {
            object: Some(file_object_proto(
                &request.id,
                object.kind,
                object.data.len() as i64,
                &object.mime_type,
                &object.name,
                object.last_modified,
            )),
        }))
    }

    async fn slice(
        &self,
        request: Request<pb::SliceRequest>,
    ) -> Result<Response<pb::FileObjectResponse>, Status> {
        let request = request.into_inner();
        let mut state = self.state.lock().expect("lock state");
        let source = Self::object(&state, &request.id)?;
        let bytes = slice_bytes(&source.data, request.start, request.end);
        let object = Self::create_object(
            &mut state,
            Kind::Blob,
            bytes,
            normalize_type(&request.content_type),
            String::new(),
            0,
        );
        Ok(Response::new(pb::FileObjectResponse {
            object: Some(object),
        }))
    }

    async fn read_bytes(
        &self,
        request: Request<pb::FileObjectRequest>,
    ) -> Result<Response<pb::BytesResponse>, Status> {
        let request = request.into_inner();
        let state = self.state.lock().expect("lock state");
        let object = Self::object(&state, &request.id)?;
        Ok(Response::new(pb::BytesResponse { data: object.data }))
    }

    async fn open_read_stream(
        &self,
        request: Request<pb::ReadStreamRequest>,
    ) -> Result<Response<Self::OpenReadStreamStream>, Status> {
        let request = request.into_inner();
        let state = self.state.lock().expect("lock state");
        let data = Self::object(&state, &request.id)?.data;
        let (tx, rx) = mpsc::channel(4);
        tokio::spawn(async move {
            for chunk in data.chunks(3) {
                if tx
                    .send(Ok(pb::ReadChunk {
                        data: chunk.to_vec(),
                    }))
                    .await
                    .is_err()
                {
                    break;
                }
            }
        });
        Ok(Response::new(ReceiverStream::new(rx)))
    }

    async fn create_object_url(
        &self,
        request: Request<pb::CreateObjectUrlRequest>,
    ) -> Result<Response<pb::ObjectUrlResponse>, Status> {
        let request = request.into_inner();
        let mut state = self.state.lock().expect("lock state");
        let _ = Self::object(&state, &request.id)?;
        state.next_object_url += 1;
        let url = format!("blob:test-{}", state.next_object_url);
        state.object_urls.insert(url.clone(), request.id);
        Ok(Response::new(pb::ObjectUrlResponse { url }))
    }

    async fn resolve_object_url(
        &self,
        request: Request<pb::ObjectUrlRequest>,
    ) -> Result<Response<pb::FileObjectResponse>, Status> {
        let request = request.into_inner();
        let state = self.state.lock().expect("lock state");
        let id = state
            .object_urls
            .get(&request.url)
            .cloned()
            .ok_or_else(|| Status::not_found("object URL not found"))?;
        let object = Self::object(&state, &id)?;
        Ok(Response::new(pb::FileObjectResponse {
            object: Some(file_object_proto(
                &id,
                object.kind,
                object.data.len() as i64,
                &object.mime_type,
                &object.name,
                object.last_modified,
            )),
        }))
    }

    async fn revoke_object_url(
        &self,
        request: Request<pb::ObjectUrlRequest>,
    ) -> Result<Response<()>, Status> {
        let request = request.into_inner();
        self.state
            .lock()
            .expect("lock state")
            .object_urls
            .remove(&request.url);
        Ok(Response::new(()))
    }
}

fn file_object_proto(
    id: &str,
    kind: Kind,
    size: i64,
    mime_type: &str,
    name: &str,
    last_modified: i64,
) -> pb::FileObject {
    pb::FileObject {
        id: id.to_string(),
        kind: match kind {
            Kind::Blob => pb::FileObjectKind::Blob as i32,
            Kind::File => pb::FileObjectKind::File as i32,
        },
        size,
        r#type: mime_type.to_string(),
        name: name.to_string(),
        last_modified,
    }
}

fn normalize_type(value: &str) -> String {
    if value.is_empty() {
        return String::new();
    }
    if !value.bytes().all(|byte| (0x20..=0x7e).contains(&byte)) {
        return String::new();
    }
    value.to_ascii_lowercase()
}

fn resolve_last_modified(last_modified: i64) -> i64 {
    if last_modified > 0 {
        return last_modified;
    }
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("unix epoch")
        .as_millis() as i64
}

fn convert_string_part(value: &str, endings: pb::LineEndings) -> Vec<u8> {
    if endings != pb::LineEndings::Native {
        return value.as_bytes().to_vec();
    }
    let normalized = value.replace("\r\n", "\n").replace('\r', "\n");
    if cfg!(windows) {
        return normalized.replace('\n', "\r\n").into_bytes();
    }
    normalized.into_bytes()
}

fn slice_bytes(data: &[u8], start: Option<i64>, end: Option<i64>) -> Vec<u8> {
    let size = data.len() as i64;
    let start = match start {
        Some(value) if value < 0 => (size + value).max(0),
        Some(value) => value.min(size),
        None => 0,
    };
    let end = match end {
        Some(value) if value < 0 => (size + value).max(0),
        Some(value) => value.min(size),
        None => size,
    }
    .max(start);
    data[start as usize..end as usize].to_vec()
}
