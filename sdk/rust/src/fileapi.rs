use std::ops::Deref;
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

use base64::Engine;
use hyper_util::rt::TokioIo;
use tonic::transport::{Channel, Endpoint, Uri};
use tower::service_fn;

use crate::api::RuntimeMetadata;
use crate::error::Result as GestaltResult;
use crate::generated::v1::{self as pb, file_api_client::FileApiClient};

pub const ENV_FILEAPI_SOCKET: &str = "GESTALT_FILEAPI_SOCKET";

#[derive(Debug, thiserror::Error)]
pub enum FileAPIError {
    #[error("not found")]
    NotFound,
    #[error("{0}")]
    Transport(#[from] tonic::transport::Error),
    #[error("{0}")]
    Status(#[from] tonic::Status),
    #[error("{0}")]
    Env(String),
    #[error("{0}")]
    InvalidResponse(String),
}

type ClientResult<T> = std::result::Result<T, FileAPIError>;

#[tonic::async_trait]
pub trait FileAPIProvider: pb::file_api_server::FileApi + Send + Sync + 'static {
    async fn configure(
        &self,
        _name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> GestaltResult<()> {
        Ok(())
    }

    fn metadata(&self) -> Option<RuntimeMetadata> {
        None
    }

    fn warnings(&self) -> Vec<String> {
        Vec::new()
    }

    async fn health_check(&self) -> GestaltResult<()> {
        Ok(())
    }

    async fn close(&self) -> GestaltResult<()> {
        Ok(())
    }
}

#[tonic::async_trait]
impl<T> pb::file_api_server::FileApi for Arc<T>
where
    T: pb::file_api_server::FileApi + ?Sized,
{
    type OpenReadStreamStream = T::OpenReadStreamStream;

    async fn create_blob(
        &self,
        request: tonic::Request<pb::CreateBlobRequest>,
    ) -> std::result::Result<tonic::Response<pb::FileObjectResponse>, tonic::Status> {
        T::create_blob(self.as_ref(), request).await
    }

    async fn create_file(
        &self,
        request: tonic::Request<pb::CreateFileRequest>,
    ) -> std::result::Result<tonic::Response<pb::FileObjectResponse>, tonic::Status> {
        T::create_file(self.as_ref(), request).await
    }

    async fn stat(
        &self,
        request: tonic::Request<pb::FileObjectRequest>,
    ) -> std::result::Result<tonic::Response<pb::FileObjectResponse>, tonic::Status> {
        T::stat(self.as_ref(), request).await
    }

    async fn slice(
        &self,
        request: tonic::Request<pb::SliceRequest>,
    ) -> std::result::Result<tonic::Response<pb::FileObjectResponse>, tonic::Status> {
        T::slice(self.as_ref(), request).await
    }

    async fn read_bytes(
        &self,
        request: tonic::Request<pb::FileObjectRequest>,
    ) -> std::result::Result<tonic::Response<pb::BytesResponse>, tonic::Status> {
        T::read_bytes(self.as_ref(), request).await
    }

    async fn open_read_stream(
        &self,
        request: tonic::Request<pb::ReadStreamRequest>,
    ) -> std::result::Result<tonic::Response<Self::OpenReadStreamStream>, tonic::Status> {
        T::open_read_stream(self.as_ref(), request).await
    }

    async fn create_object_url(
        &self,
        request: tonic::Request<pb::CreateObjectUrlRequest>,
    ) -> std::result::Result<tonic::Response<pb::ObjectUrlResponse>, tonic::Status> {
        T::create_object_url(self.as_ref(), request).await
    }

    async fn resolve_object_url(
        &self,
        request: tonic::Request<pb::ObjectUrlRequest>,
    ) -> std::result::Result<tonic::Response<pb::FileObjectResponse>, tonic::Status> {
        T::resolve_object_url(self.as_ref(), request).await
    }

    async fn revoke_object_url(
        &self,
        request: tonic::Request<pb::ObjectUrlRequest>,
    ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
        T::revoke_object_url(self.as_ref(), request).await
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FileObjectKind {
    Blob,
    File,
}

impl FileObjectKind {
    fn from_proto(kind: i32) -> Self {
        match pb::FileObjectKind::try_from(kind).unwrap_or(pb::FileObjectKind::Blob) {
            pb::FileObjectKind::File => Self::File,
            _ => Self::Blob,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum LineEndings {
    #[default]
    Transparent,
    Native,
}

impl LineEndings {
    fn to_proto(self) -> i32 {
        match self {
            Self::Transparent => pb::LineEndings::Transparent as i32,
            Self::Native => pb::LineEndings::Native as i32,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum BlobPart {
    Text(String),
    Bytes(Vec<u8>),
    BlobId(String),
}

impl BlobPart {
    fn to_proto(&self) -> pb::BlobPart {
        match self {
            Self::Text(value) => pb::BlobPart {
                kind: Some(pb::blob_part::Kind::StringData(value.clone())),
            },
            Self::Bytes(value) => pb::BlobPart {
                kind: Some(pb::blob_part::Kind::BytesData(value.clone())),
            },
            Self::BlobId(value) => pb::BlobPart {
                kind: Some(pb::blob_part::Kind::BlobId(value.clone())),
            },
        }
    }
}

impl From<String> for BlobPart {
    fn from(value: String) -> Self {
        Self::Text(value)
    }
}

impl From<&str> for BlobPart {
    fn from(value: &str) -> Self {
        Self::Text(value.to_string())
    }
}

impl From<Vec<u8>> for BlobPart {
    fn from(value: Vec<u8>) -> Self {
        Self::Bytes(value)
    }
}

impl From<&[u8]> for BlobPart {
    fn from(value: &[u8]) -> Self {
        Self::Bytes(value.to_vec())
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct BlobPropertyBag {
    pub mime_type: String,
    pub endings: LineEndings,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FilePropertyBag {
    pub mime_type: String,
    pub endings: LineEndings,
    pub last_modified: i64,
}

impl Default for FilePropertyBag {
    fn default() -> Self {
        Self {
            mime_type: String::new(),
            endings: LineEndings::Transparent,
            last_modified: current_time_millis(),
        }
    }
}

#[derive(Clone)]
pub struct FileAPI {
    client: FileApiClient<Channel>,
}

impl FileAPI {
    pub async fn connect() -> ClientResult<Self> {
        Self::connect_named("").await
    }

    pub async fn connect_named(name: &str) -> ClientResult<Self> {
        let env_name = fileapi_socket_env(name);
        let socket_path = std::env::var(&env_name)
            .map_err(|_| FileAPIError::Env(format!("{env_name} is not set")))?;

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
            client: FileApiClient::new(channel),
        })
    }

    pub async fn create_blob<I>(&mut self, parts: I, options: BlobPropertyBag) -> ClientResult<Blob>
    where
        I: IntoIterator<Item = BlobPart>,
    {
        let response = self
            .client
            .create_blob(pb::CreateBlobRequest {
                parts: parts.into_iter().map(|part| part.to_proto()).collect(),
                options: Some(pb::BlobOptions {
                    mime_type: normalize_type(&options.mime_type),
                    endings: options.endings.to_proto(),
                }),
            })
            .await
            .map_err(map_status)?
            .into_inner();
        let object = response
            .object
            .ok_or_else(|| FileAPIError::InvalidResponse("missing file object".to_string()))?;
        blob_from_proto(self.clone(), object)
    }

    pub async fn create_file<I>(
        &mut self,
        parts: I,
        name: &str,
        options: FilePropertyBag,
    ) -> ClientResult<File>
    where
        I: IntoIterator<Item = BlobPart>,
    {
        let response = self
            .client
            .create_file(pb::CreateFileRequest {
                file_bits: parts.into_iter().map(|part| part.to_proto()).collect(),
                file_name: name.to_string(),
                options: Some(pb::FileOptions {
                    mime_type: normalize_type(&options.mime_type),
                    endings: options.endings.to_proto(),
                    last_modified: resolve_last_modified(options.last_modified),
                }),
            })
            .await
            .map_err(map_status)?
            .into_inner();
        let object = response
            .object
            .ok_or_else(|| FileAPIError::InvalidResponse("missing file object".to_string()))?;
        file_from_proto(self.clone(), object)
    }

    pub async fn stat(&mut self, id: &str) -> ClientResult<FileObjectHandle> {
        let response = self
            .client
            .stat(pb::FileObjectRequest { id: id.to_string() })
            .await
            .map_err(map_status)?
            .into_inner();
        let object = response
            .object
            .ok_or_else(|| FileAPIError::InvalidResponse("missing file object".to_string()))?;
        object_from_proto(self.clone(), object)
    }

    pub async fn slice(
        &mut self,
        id: &str,
        start: Option<i64>,
        end: Option<i64>,
        content_type: &str,
    ) -> ClientResult<Blob> {
        let response = self
            .client
            .slice(pb::SliceRequest {
                id: id.to_string(),
                start,
                end,
                content_type: normalize_type(content_type),
            })
            .await
            .map_err(map_status)?
            .into_inner();
        let object = response
            .object
            .ok_or_else(|| FileAPIError::InvalidResponse("missing file object".to_string()))?;
        blob_from_proto(self.clone(), object)
    }

    pub async fn read_bytes(&mut self, id: &str) -> ClientResult<Vec<u8>> {
        let response = self
            .client
            .read_bytes(pb::FileObjectRequest { id: id.to_string() })
            .await
            .map_err(map_status)?
            .into_inner();
        Ok(response.data)
    }

    pub async fn read_text(&mut self, id: &str) -> ClientResult<String> {
        let bytes = self.read_bytes(id).await?;
        Ok(String::from_utf8_lossy(&bytes).into_owned())
    }

    pub async fn read_data_url(&mut self, id: &str) -> ClientResult<String> {
        let object = self.stat(id).await?;
        let mime_type = object.mime_type().to_string();
        let bytes = self.read_bytes(id).await?;
        Ok(package_data_url(&mime_type, &bytes))
    }

    pub async fn open_read_stream(
        &mut self,
        id: &str,
    ) -> ClientResult<tonic::Streaming<pb::ReadChunk>> {
        let response = self
            .client
            .open_read_stream(pb::ReadStreamRequest { id: id.to_string() })
            .await
            .map_err(map_status)?;
        Ok(response.into_inner())
    }

    pub async fn create_object_url(&mut self, id: &str) -> ClientResult<String> {
        let response = self
            .client
            .create_object_url(pb::CreateObjectUrlRequest { id: id.to_string() })
            .await
            .map_err(map_status)?
            .into_inner();
        Ok(response.url)
    }

    pub async fn resolve_object_url(&mut self, url: &str) -> ClientResult<FileObjectHandle> {
        let response = self
            .client
            .resolve_object_url(pb::ObjectUrlRequest {
                url: url.to_string(),
            })
            .await
            .map_err(map_status)?
            .into_inner();
        let object = response
            .object
            .ok_or_else(|| FileAPIError::InvalidResponse("missing file object".to_string()))?;
        object_from_proto(self.clone(), object)
    }

    pub async fn revoke_object_url(&mut self, url: &str) -> ClientResult<()> {
        self.client
            .revoke_object_url(pb::ObjectUrlRequest {
                url: url.to_string(),
            })
            .await
            .map_err(map_status)?;
        Ok(())
    }
}

pub enum FileObjectHandle {
    Blob(Blob),
    File(File),
}

impl FileObjectHandle {
    pub fn kind(&self) -> FileObjectKind {
        match self {
            Self::Blob(_) => FileObjectKind::Blob,
            Self::File(_) => FileObjectKind::File,
        }
    }

    pub fn id(&self) -> &str {
        match self {
            Self::Blob(blob) => &blob.id,
            Self::File(file) => &file.id,
        }
    }

    pub fn size(&self) -> i64 {
        match self {
            Self::Blob(blob) => blob.size,
            Self::File(file) => file.size,
        }
    }

    pub fn mime_type(&self) -> &str {
        match self {
            Self::Blob(blob) => &blob.mime_type,
            Self::File(file) => &file.mime_type,
        }
    }
}

#[derive(Clone)]
pub struct Blob {
    api: FileAPI,
    pub id: String,
    pub size: i64,
    pub mime_type: String,
}

impl Blob {
    pub fn kind(&self) -> FileObjectKind {
        FileObjectKind::Blob
    }

    pub async fn slice(
        &self,
        start: Option<i64>,
        end: Option<i64>,
        content_type: &str,
    ) -> ClientResult<Blob> {
        let mut api = self.api.clone();
        api.slice(&self.id, start, end, content_type).await
    }

    pub async fn bytes(&self) -> ClientResult<Vec<u8>> {
        let mut api = self.api.clone();
        api.read_bytes(&self.id).await
    }

    pub async fn text(&self) -> ClientResult<String> {
        let mut api = self.api.clone();
        api.read_text(&self.id).await
    }

    pub async fn data_url(&self) -> ClientResult<String> {
        Ok(package_data_url(&self.mime_type, &self.bytes().await?))
    }

    pub async fn open_read_stream(&self) -> ClientResult<tonic::Streaming<pb::ReadChunk>> {
        let mut api = self.api.clone();
        api.open_read_stream(&self.id).await
    }

    pub async fn create_object_url(&self) -> ClientResult<String> {
        let mut api = self.api.clone();
        api.create_object_url(&self.id).await
    }
}

#[derive(Clone)]
pub struct File {
    blob: Blob,
    pub name: String,
    pub last_modified: i64,
}

impl Deref for File {
    type Target = Blob;

    fn deref(&self) -> &Self::Target {
        &self.blob
    }
}

impl File {
    pub fn kind(&self) -> FileObjectKind {
        FileObjectKind::File
    }
}

impl From<&Blob> for BlobPart {
    fn from(value: &Blob) -> Self {
        Self::BlobId(value.id.clone())
    }
}

impl From<Blob> for BlobPart {
    fn from(value: Blob) -> Self {
        Self::BlobId(value.id)
    }
}

impl From<&File> for BlobPart {
    fn from(value: &File) -> Self {
        Self::BlobId(value.id.clone())
    }
}

impl From<File> for BlobPart {
    fn from(value: File) -> Self {
        Self::BlobId(value.id.clone())
    }
}

pub fn fileapi_socket_env(name: &str) -> String {
    let trimmed = name.trim();
    if trimmed.is_empty() {
        return ENV_FILEAPI_SOCKET.to_string();
    }
    let normalized = trimmed
        .chars()
        .map(|ch| {
            if ch.is_ascii_alphanumeric() {
                ch.to_ascii_uppercase()
            } else {
                '_'
            }
        })
        .collect::<String>();
    format!("{ENV_FILEAPI_SOCKET}_{normalized}")
}

fn object_from_proto(api: FileAPI, object: pb::FileObject) -> ClientResult<FileObjectHandle> {
    match FileObjectKind::from_proto(object.kind) {
        FileObjectKind::Blob => Ok(FileObjectHandle::Blob(Blob {
            api,
            id: object.id,
            size: object.size,
            mime_type: object.r#type,
        })),
        FileObjectKind::File => Ok(FileObjectHandle::File(File {
            blob: Blob {
                api,
                id: object.id,
                size: object.size,
                mime_type: object.r#type,
            },
            name: object.name,
            last_modified: object.last_modified,
        })),
    }
}

fn blob_from_proto(api: FileAPI, object: pb::FileObject) -> ClientResult<Blob> {
    match object_from_proto(api, object)? {
        FileObjectHandle::Blob(blob) => Ok(blob),
        FileObjectHandle::File(_) => Err(FileAPIError::InvalidResponse(
            "expected blob object, got file".to_string(),
        )),
    }
}

fn file_from_proto(api: FileAPI, object: pb::FileObject) -> ClientResult<File> {
    match object_from_proto(api, object)? {
        FileObjectHandle::File(file) => Ok(file),
        FileObjectHandle::Blob(_) => Err(FileAPIError::InvalidResponse(
            "expected file object, got blob".to_string(),
        )),
    }
}

fn map_status(err: tonic::Status) -> FileAPIError {
    match err.code() {
        tonic::Code::NotFound => FileAPIError::NotFound,
        _ => FileAPIError::Status(err),
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
        last_modified
    } else {
        current_time_millis()
    }
}

fn current_time_millis() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("unix epoch")
        .as_millis() as i64
}

fn package_data_url(mime_type: &str, data: &[u8]) -> String {
    let mime_type = normalize_type(mime_type);
    let payload = base64::engine::general_purpose::STANDARD.encode(data);
    if mime_type.is_empty() {
        return format!("data:;base64,{payload}");
    }
    format!("data:{mime_type};base64,{payload}")
}
