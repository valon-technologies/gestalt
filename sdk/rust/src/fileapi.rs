use std::pin::Pin;

use tokio_stream::Stream;
use tonic::codegen::async_trait;

use crate::api::RuntimeMetadata;
use crate::error::Result;
pub use crate::generated::v1::{
    BlobOptions, BlobPart, BytesResponse, CreateBlobRequest, CreateFileRequest,
    CreateObjectUrlRequest, FileObject, FileObjectRequest, FileObjectResponse, FileOptions,
    ObjectUrlRequest, ObjectUrlResponse, ReadChunk, ReadStreamRequest, SliceRequest,
};

pub type FileAPIReadStream = Pin<Box<dyn Stream<Item = Result<ReadChunk>> + Send + 'static>>;

#[async_trait]
pub trait FileAPIProvider: Send + Sync + 'static {
    async fn configure(
        &self,
        _name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> Result<()> {
        Ok(())
    }

    fn metadata(&self) -> Option<RuntimeMetadata> {
        None
    }

    fn warnings(&self) -> Vec<String> {
        Vec::new()
    }

    async fn health_check(&self) -> Result<()> {
        Ok(())
    }

    async fn close(&self) -> Result<()> {
        Ok(())
    }

    async fn create_blob(&self, req: CreateBlobRequest) -> Result<FileObjectResponse>;

    async fn create_file(&self, req: CreateFileRequest) -> Result<FileObjectResponse>;

    async fn stat(&self, req: FileObjectRequest) -> Result<FileObjectResponse>;

    async fn slice(&self, req: SliceRequest) -> Result<FileObjectResponse>;

    async fn read_bytes(&self, req: FileObjectRequest) -> Result<BytesResponse>;

    async fn open_read_stream(&self, req: ReadStreamRequest) -> Result<FileAPIReadStream>;

    async fn create_object_url(&self, req: CreateObjectUrlRequest) -> Result<ObjectUrlResponse>;

    async fn resolve_object_url(&self, req: ObjectUrlRequest) -> Result<FileObjectResponse>;

    async fn revoke_object_url(&self, req: ObjectUrlRequest) -> Result<()>;
}
