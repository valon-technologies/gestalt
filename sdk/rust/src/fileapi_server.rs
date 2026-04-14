use std::pin::Pin;
use std::sync::Arc;

use tokio_stream::Stream;
use tokio_stream::StreamExt;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::fileapi::FileAPIProvider;
use crate::generated::v1::file_api_server::FileApi as FileAPIGrpc;
use crate::generated::v1::{
    BytesResponse, CreateBlobRequest, CreateFileRequest, CreateObjectUrlRequest, FileObjectRequest,
    FileObjectResponse, ObjectUrlRequest, ObjectUrlResponse, ReadChunk, ReadStreamRequest,
    SliceRequest,
};
use crate::rpc_status::rpc_status;

#[derive(Clone)]
pub struct FileAPIServer<P> {
    fileapi: Arc<P>,
}

impl<P> FileAPIServer<P> {
    pub fn new(fileapi: Arc<P>) -> Self {
        Self { fileapi }
    }
}

#[tonic::async_trait]
impl<P> FileAPIGrpc for FileAPIServer<P>
where
    P: FileAPIProvider,
{
    type OpenReadStreamStream =
        Pin<Box<dyn Stream<Item = std::result::Result<ReadChunk, Status>> + Send + 'static>>;

    async fn create_blob(
        &self,
        request: GrpcRequest<CreateBlobRequest>,
    ) -> std::result::Result<GrpcResponse<FileObjectResponse>, Status> {
        let response = self
            .fileapi
            .create_blob(request.into_inner())
            .await
            .map_err(|error| rpc_status("create blob", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn create_file(
        &self,
        request: GrpcRequest<CreateFileRequest>,
    ) -> std::result::Result<GrpcResponse<FileObjectResponse>, Status> {
        let response = self
            .fileapi
            .create_file(request.into_inner())
            .await
            .map_err(|error| rpc_status("create file", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn stat(
        &self,
        request: GrpcRequest<FileObjectRequest>,
    ) -> std::result::Result<GrpcResponse<FileObjectResponse>, Status> {
        let response = self
            .fileapi
            .stat(request.into_inner())
            .await
            .map_err(|error| rpc_status("stat", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn slice(
        &self,
        request: GrpcRequest<SliceRequest>,
    ) -> std::result::Result<GrpcResponse<FileObjectResponse>, Status> {
        let response = self
            .fileapi
            .slice(request.into_inner())
            .await
            .map_err(|error| rpc_status("slice", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn read_bytes(
        &self,
        request: GrpcRequest<FileObjectRequest>,
    ) -> std::result::Result<GrpcResponse<BytesResponse>, Status> {
        let response = self
            .fileapi
            .read_bytes(request.into_inner())
            .await
            .map_err(|error| rpc_status("read bytes", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn open_read_stream(
        &self,
        request: GrpcRequest<ReadStreamRequest>,
    ) -> std::result::Result<GrpcResponse<Self::OpenReadStreamStream>, Status> {
        let stream = self
            .fileapi
            .open_read_stream(request.into_inner())
            .await
            .map_err(|error| rpc_status("open read stream", error))?;
        Ok(GrpcResponse::new(Box::pin(stream.map(|result| {
            result.map_err(|error| rpc_status("open read stream", error))
        }))))
    }

    async fn create_object_url(
        &self,
        request: GrpcRequest<CreateObjectUrlRequest>,
    ) -> std::result::Result<GrpcResponse<ObjectUrlResponse>, Status> {
        let response = self
            .fileapi
            .create_object_url(request.into_inner())
            .await
            .map_err(|error| rpc_status("create object url", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn resolve_object_url(
        &self,
        request: GrpcRequest<ObjectUrlRequest>,
    ) -> std::result::Result<GrpcResponse<FileObjectResponse>, Status> {
        let response = self
            .fileapi
            .resolve_object_url(request.into_inner())
            .await
            .map_err(|error| rpc_status("resolve object url", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn revoke_object_url(
        &self,
        request: GrpcRequest<ObjectUrlRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.fileapi
            .revoke_object_url(request.into_inner())
            .await
            .map_err(|error| rpc_status("revoke object url", error))?;
        Ok(GrpcResponse::new(()))
    }
}
