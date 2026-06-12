use std::collections::BTreeMap;
use std::pin::Pin;
use std::sync::Arc;
use std::time::{Duration, SystemTime};

use tokio_stream::{Stream, StreamExt};
use tonic::codegen::async_trait;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::api::RuntimeMetadata;
use crate::error::Result as ProviderResult;
use crate::generated::v1 as pb;
use crate::protocol;
use crate::rpc_status::rpc_status;

/// Server-streamed object read chunks returned by [`S3Provider::read_object`].
pub type S3ReadObjectStream =
    Pin<Box<dyn Stream<Item = ProviderResult<S3ReadObjectFrame>> + Send + 'static>>;
type S3RpcReadObjectStream =
    Pin<Box<dyn Stream<Item = std::result::Result<pb::ReadObjectChunk, Status>> + Send + 'static>>;

#[derive(Clone, Debug, PartialEq)]
/// One frame in a provider-authored object read stream.
pub enum S3ReadObjectFrame {
    /// The `Meta` variant.
    Meta(ObjectMeta),
    /// The `Data` variant.
    Data(Vec<u8>),
}

#[derive(Clone, Debug, PartialEq)]
/// One frame in a provider-backed object write stream.
pub enum S3WriteObjectFrame {
    /// The `Open` variant.
    Open(Box<S3WriteObjectOpen>),
    /// The `Data` variant.
    Data(Vec<u8>),
    /// The `Empty` variant.
    Empty,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Metadata frame that starts an object write stream.
pub struct S3WriteObjectOpen {
    /// The `reference` field.
    pub reference: Option<ObjectRef>,
    /// The `content_type` field.
    pub content_type: String,
    /// The `cache_control` field.
    pub cache_control: String,
    /// The `content_disposition` field.
    pub content_disposition: String,
    /// The `content_encoding` field.
    pub content_encoding: String,
    /// The `content_language` field.
    pub content_language: String,
    /// The `metadata` field.
    pub metadata: BTreeMap<String, String>,
    /// The `if_match` field.
    pub if_match: String,
    /// The `if_none_match` field.
    pub if_none_match: String,
}

/// Client-streamed object write chunks passed to [`S3Provider::write_object`].
pub struct S3WriteObjectStream {
    inner: tonic::Streaming<pb::WriteObjectRequest>,
}

impl S3WriteObjectStream {
    pub(crate) fn new(inner: tonic::Streaming<pb::WriteObjectRequest>) -> Self {
        Self { inner }
    }

    /// Reads the next write request frame from the upload stream.
    pub async fn message(&mut self) -> ProviderResult<Option<S3WriteObjectFrame>> {
        self.inner
            .message()
            .await
            .map_err(|error| crate::Error::new(error.to_string()))?
            .map(write_object_frame_from_proto)
            .transpose()
    }
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Identifies one object or object version.
pub struct ObjectRef {
    /// Object key.
    pub key: String,
    /// Optional object version id.
    pub version_id: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Describes an object returned by the provider.
pub struct ObjectMeta {
    /// Object reference, including version when returned by the provider.
    pub reference: ObjectRef,
    /// Entity tag returned by storage.
    pub etag: String,
    /// Object size in bytes.
    pub size: i64,
    /// MIME content type.
    pub content_type: String,
    /// Last-modified timestamp, when known.
    pub last_modified: Option<SystemTime>,
    /// User metadata associated with the object.
    pub metadata: BTreeMap<String, String>,
    /// Storage class reported by the provider.
    pub storage_class: String,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Requests a half-open slice of an object's bytes.
pub struct ByteRange {
    /// Inclusive starting byte offset.
    pub start: Option<i64>,
    /// Inclusive ending byte offset.
    pub end: Option<i64>,
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
/// Identifies the HTTP verb encoded into a presigned URL.
pub enum PresignMethod {
    /// Let the provider choose the default method.
    #[default]
    Unspecified,
    /// HTTP GET.
    Get,
    /// HTTP PUT.
    Put,
    /// HTTP DELETE.
    Delete,
    /// HTTP HEAD.
    Head,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
/// Fetches metadata for one object.
pub struct HeadObjectRequest {
    /// The `reference` field.
    pub reference: Option<ObjectRef>,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Returns object metadata.
pub struct HeadObjectResponse {
    /// The `meta` field.
    pub meta: Option<ObjectMeta>,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Opens a streaming object read.
pub struct ReadObjectRequest {
    /// The `reference` field.
    pub reference: Option<ObjectRef>,
    /// The `range` field.
    pub range: Option<ByteRange>,
    /// The `if_match` field.
    pub if_match: String,
    /// The `if_none_match` field.
    pub if_none_match: String,
    /// The `if_modified_since` field.
    pub if_modified_since: Option<SystemTime>,
    /// The `if_unmodified_since` field.
    pub if_unmodified_since: Option<SystemTime>,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Returns metadata for a committed object write.
pub struct WriteObjectResponse {
    /// The `meta` field.
    pub meta: Option<ObjectMeta>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
/// Deletes one object.
pub struct DeleteObjectRequest {
    /// The `reference` field.
    pub reference: Option<ObjectRef>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
/// Lists objects in the provider's configured bucket.
pub struct ListObjectsRequest {
    /// The `prefix` field.
    pub prefix: String,
    /// The `delimiter` field.
    pub delimiter: String,
    /// The `continuation_token` field.
    pub continuation_token: String,
    /// The `start_after` field.
    pub start_after: String,
    /// The `max_keys` field.
    pub max_keys: i32,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// One page of list-objects results.
pub struct ListObjectsResponse {
    /// The `objects` field.
    pub objects: Vec<ObjectMeta>,
    /// The `common_prefixes` field.
    pub common_prefixes: Vec<String>,
    /// The `next_continuation_token` field.
    pub next_continuation_token: String,
    /// The `has_more` field.
    pub has_more: bool,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
/// Copies one object to another location.
pub struct CopyObjectRequest {
    /// The `source` field.
    pub source: Option<ObjectRef>,
    /// The `destination` field.
    pub destination: Option<ObjectRef>,
    /// The `if_match` field.
    pub if_match: String,
    /// The `if_none_match` field.
    pub if_none_match: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Returns metadata for a copied object.
pub struct CopyObjectResponse {
    /// The `meta` field.
    pub meta: Option<ObjectMeta>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
/// Asks the provider to mint a presigned URL.
pub struct PresignObjectRequest {
    /// The `reference` field.
    pub reference: Option<ObjectRef>,
    /// The `method` field.
    pub method: PresignMethod,
    /// The `expires` field.
    pub expires: Duration,
    /// The `content_type` field.
    pub content_type: String,
    /// The `content_disposition` field.
    pub content_disposition: String,
    /// The `headers` field.
    pub headers: BTreeMap<String, String>,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Returns a presigned URL plus required headers.
pub struct PresignObjectResponse {
    /// The `url` field.
    pub url: String,
    /// The `method` field.
    pub method: PresignMethod,
    /// The `expires_at` field.
    pub expires_at: Option<SystemTime>,
    /// The `headers` field.
    pub headers: BTreeMap<String, String>,
}

#[async_trait]
/// Lifecycle and RPC contract for S3-compatible providers.
pub trait S3Provider: Send + Sync + 'static {
    /// Configures the provider before it starts serving requests.
    async fn configure(
        &self,
        _name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> ProviderResult<()> {
        Ok(())
    }

    /// Returns runtime metadata that should augment the static manifest.
    fn metadata(&self) -> Option<RuntimeMetadata> {
        None
    }

    /// Returns non-fatal warnings the host should surface to users.
    fn warnings(&self) -> Vec<String> {
        Vec::new()
    }

    /// Performs an optional health check.
    async fn health_check(&self) -> ProviderResult<()> {
        Ok(())
    }

    /// Starts provider-owned background work after configuration.
    async fn start(&self) -> ProviderResult<()> {
        Ok(())
    }

    /// Shuts the provider down before the runtime exits.
    async fn close(&self) -> ProviderResult<()> {
        Ok(())
    }

    /// Returns object metadata without reading the object body.
    async fn head_object(&self, _request: HeadObjectRequest) -> ProviderResult<HeadObjectResponse> {
        Err(crate::Error::unimplemented(
            "s3 head object is not implemented",
        ))
    }

    /// Streams object metadata followed by object data chunks.
    async fn read_object(&self, _request: ReadObjectRequest) -> ProviderResult<S3ReadObjectStream> {
        Err(crate::Error::unimplemented(
            "s3 read object is not implemented",
        ))
    }

    /// Consumes a streamed upload and returns metadata for the written object.
    async fn write_object(
        &self,
        _request: S3WriteObjectStream,
    ) -> ProviderResult<WriteObjectResponse> {
        Err(crate::Error::unimplemented(
            "s3 write object is not implemented",
        ))
    }

    /// Deletes one object or object version.
    async fn delete_object(&self, _request: DeleteObjectRequest) -> ProviderResult<()> {
        Err(crate::Error::unimplemented(
            "s3 delete object is not implemented",
        ))
    }

    /// Lists objects in the provider's configured bucket using S3-style pagination and delimiters.
    async fn list_objects(
        &self,
        _request: ListObjectsRequest,
    ) -> ProviderResult<ListObjectsResponse> {
        Err(crate::Error::unimplemented(
            "s3 list objects is not implemented",
        ))
    }

    /// Copies one object to another object reference.
    async fn copy_object(&self, _request: CopyObjectRequest) -> ProviderResult<CopyObjectResponse> {
        Err(crate::Error::unimplemented(
            "s3 copy object is not implemented",
        ))
    }

    /// Creates a presigned URL for direct object access.
    async fn presign_object(
        &self,
        _request: PresignObjectRequest,
    ) -> ProviderResult<PresignObjectResponse> {
        Err(crate::Error::unimplemented(
            "s3 presign object is not implemented",
        ))
    }
}

#[derive(Clone)]
pub(crate) struct S3RpcServer<P> {
    provider: Arc<P>,
}

impl<P> S3RpcServer<P> {
    pub(crate) fn new(provider: Arc<P>) -> Self {
        Self { provider }
    }
}

#[async_trait]
impl<P> pb::s3_server::S3 for S3RpcServer<P>
where
    P: S3Provider,
{
    type ReadObjectStream = S3RpcReadObjectStream;

    async fn head_object(
        &self,
        request: GrpcRequest<pb::HeadObjectRequest>,
    ) -> std::result::Result<GrpcResponse<pb::HeadObjectResponse>, Status> {
        let response = self
            .provider
            .head_object(head_object_request_from_proto(request.into_inner()))
            .await
            .map_err(|error| rpc_status("s3 head object", error))?;
        Ok(GrpcResponse::new(
            head_object_response_to_proto(response)
                .map_err(|error| rpc_status("s3 head object", error))?,
        ))
    }

    async fn read_object(
        &self,
        request: GrpcRequest<pb::ReadObjectRequest>,
    ) -> std::result::Result<GrpcResponse<Self::ReadObjectStream>, Status> {
        let stream = self
            .provider
            .read_object(
                read_object_request_from_proto(request.into_inner())
                    .map_err(|error| rpc_status("s3 read object", error))?,
            )
            .await
            .map_err(|error| rpc_status("s3 read object", error))?
            .map(|chunk| {
                chunk
                    .and_then(read_object_frame_to_proto)
                    .map_err(|error| rpc_status("s3 read object stream", error))
            });
        Ok(GrpcResponse::new(Box::pin(stream)))
    }

    async fn write_object(
        &self,
        request: GrpcRequest<tonic::Streaming<pb::WriteObjectRequest>>,
    ) -> std::result::Result<GrpcResponse<pb::WriteObjectResponse>, Status> {
        let response = self
            .provider
            .write_object(S3WriteObjectStream::new(request.into_inner()))
            .await
            .map_err(|error| rpc_status("s3 write object", error))?;
        Ok(GrpcResponse::new(
            write_object_response_to_proto(response)
                .map_err(|error| rpc_status("s3 write object", error))?,
        ))
    }

    async fn delete_object(
        &self,
        request: GrpcRequest<pb::DeleteObjectRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider
            .delete_object(delete_object_request_from_proto(request.into_inner()))
            .await
            .map_err(|error| rpc_status("s3 delete object", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn list_objects(
        &self,
        request: GrpcRequest<pb::ListObjectsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListObjectsResponse>, Status> {
        let response = self
            .provider
            .list_objects(list_objects_request_from_proto(request.into_inner()))
            .await
            .map_err(|error| rpc_status("s3 list objects", error))?;
        Ok(GrpcResponse::new(
            list_objects_response_to_proto(response)
                .map_err(|error| rpc_status("s3 list objects", error))?,
        ))
    }

    async fn copy_object(
        &self,
        request: GrpcRequest<pb::CopyObjectRequest>,
    ) -> std::result::Result<GrpcResponse<pb::CopyObjectResponse>, Status> {
        let response = self
            .provider
            .copy_object(copy_object_request_from_proto(request.into_inner()))
            .await
            .map_err(|error| rpc_status("s3 copy object", error))?;
        Ok(GrpcResponse::new(
            copy_object_response_to_proto(response)
                .map_err(|error| rpc_status("s3 copy object", error))?,
        ))
    }

    async fn presign_object(
        &self,
        request: GrpcRequest<pb::PresignObjectRequest>,
    ) -> std::result::Result<GrpcResponse<pb::PresignObjectResponse>, Status> {
        let response = self
            .provider
            .presign_object(presign_object_request_from_proto(request.into_inner()))
            .await
            .map_err(|error| rpc_status("s3 presign object", error))?;
        Ok(GrpcResponse::new(presign_object_response_to_proto(
            response,
        )))
    }
}

fn object_ref_to_proto(reference: ObjectRef) -> pb::S3ObjectRef {
    pb::S3ObjectRef {
        key: reference.key,
        version_id: reference.version_id,
    }
}

fn object_ref_from_proto(reference: pb::S3ObjectRef) -> ObjectRef {
    ObjectRef {
        key: reference.key,
        version_id: reference.version_id,
    }
}

fn object_meta_to_proto(meta: ObjectMeta) -> ProviderResult<pb::S3ObjectMeta> {
    Ok(pb::S3ObjectMeta {
        r#ref: Some(object_ref_to_proto(meta.reference)),
        etag: meta.etag,
        size: meta.size,
        content_type: meta.content_type,
        last_modified: meta.last_modified.map(protocol::timestamp_from_system_time),
        metadata: meta.metadata,
        storage_class: meta.storage_class,
    })
}

fn head_object_request_from_proto(request: pb::HeadObjectRequest) -> HeadObjectRequest {
    HeadObjectRequest {
        reference: request.r#ref.map(object_ref_from_proto),
    }
}

fn head_object_response_to_proto(
    response: HeadObjectResponse,
) -> ProviderResult<pb::HeadObjectResponse> {
    Ok(pb::HeadObjectResponse {
        meta: response.meta.map(object_meta_to_proto).transpose()?,
    })
}

fn read_object_request_from_proto(
    request: pb::ReadObjectRequest,
) -> ProviderResult<ReadObjectRequest> {
    Ok(ReadObjectRequest {
        reference: request.r#ref.map(object_ref_from_proto),
        range: request.range.map(|range| ByteRange {
            start: range.start,
            end: range.end,
        }),
        if_match: request.if_match,
        if_none_match: request.if_none_match,
        if_modified_since: request
            .if_modified_since
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()?,
        if_unmodified_since: request
            .if_unmodified_since
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()?,
    })
}

fn read_object_frame_to_proto(frame: S3ReadObjectFrame) -> ProviderResult<pb::ReadObjectChunk> {
    let result = match frame {
        S3ReadObjectFrame::Meta(meta) => {
            pb::read_object_chunk::Result::Meta(object_meta_to_proto(meta)?)
        }
        S3ReadObjectFrame::Data(data) => pb::read_object_chunk::Result::Data(data),
    };
    Ok(pb::ReadObjectChunk {
        result: Some(result),
    })
}

fn write_object_frame_from_proto(
    frame: pb::WriteObjectRequest,
) -> ProviderResult<S3WriteObjectFrame> {
    Ok(match frame.msg {
        Some(pb::write_object_request::Msg::Open(open)) => {
            S3WriteObjectFrame::Open(Box::new(S3WriteObjectOpen {
                reference: open.r#ref.map(object_ref_from_proto),
                content_type: open.content_type,
                cache_control: open.cache_control,
                content_disposition: open.content_disposition,
                content_encoding: open.content_encoding,
                content_language: open.content_language,
                metadata: open.metadata,
                if_match: open.if_match,
                if_none_match: open.if_none_match,
            }))
        }
        Some(pb::write_object_request::Msg::Data(data)) => S3WriteObjectFrame::Data(data),
        None => S3WriteObjectFrame::Empty,
    })
}

fn write_object_response_to_proto(
    response: WriteObjectResponse,
) -> ProviderResult<pb::WriteObjectResponse> {
    Ok(pb::WriteObjectResponse {
        meta: response.meta.map(object_meta_to_proto).transpose()?,
    })
}

fn delete_object_request_from_proto(request: pb::DeleteObjectRequest) -> DeleteObjectRequest {
    DeleteObjectRequest {
        reference: request.r#ref.map(object_ref_from_proto),
    }
}

fn list_objects_request_from_proto(request: pb::ListObjectsRequest) -> ListObjectsRequest {
    ListObjectsRequest {
        prefix: request.prefix,
        delimiter: request.delimiter,
        continuation_token: request.continuation_token,
        start_after: request.start_after,
        max_keys: request.max_keys,
    }
}

fn list_objects_response_to_proto(
    response: ListObjectsResponse,
) -> ProviderResult<pb::ListObjectsResponse> {
    Ok(pb::ListObjectsResponse {
        objects: response
            .objects
            .into_iter()
            .map(object_meta_to_proto)
            .collect::<ProviderResult<Vec<_>>>()?,
        common_prefixes: response.common_prefixes,
        next_continuation_token: response.next_continuation_token,
        has_more: response.has_more,
    })
}

fn copy_object_request_from_proto(request: pb::CopyObjectRequest) -> CopyObjectRequest {
    CopyObjectRequest {
        source: request.source.map(object_ref_from_proto),
        destination: request.destination.map(object_ref_from_proto),
        if_match: request.if_match,
        if_none_match: request.if_none_match,
    }
}

fn copy_object_response_to_proto(
    response: CopyObjectResponse,
) -> ProviderResult<pb::CopyObjectResponse> {
    Ok(pb::CopyObjectResponse {
        meta: response.meta.map(object_meta_to_proto).transpose()?,
    })
}

fn presign_object_request_from_proto(request: pb::PresignObjectRequest) -> PresignObjectRequest {
    PresignObjectRequest {
        reference: request.r#ref.map(object_ref_from_proto),
        method: presign_method_from_proto(request.method),
        expires: Duration::from_secs(u64::try_from(request.expires_seconds).unwrap_or_default()),
        content_type: request.content_type,
        content_disposition: request.content_disposition,
        headers: request.headers,
    }
}

fn presign_object_response_to_proto(response: PresignObjectResponse) -> pb::PresignObjectResponse {
    pb::PresignObjectResponse {
        url: response.url,
        method: presign_method_to_proto(response.method) as i32,
        expires_at: response
            .expires_at
            .map(protocol::timestamp_from_system_time),
        headers: response.headers,
    }
}

fn presign_method_to_proto(method: PresignMethod) -> pb::PresignMethod {
    match method {
        PresignMethod::Unspecified => pb::PresignMethod::Unspecified,
        PresignMethod::Get => pb::PresignMethod::Get,
        PresignMethod::Put => pb::PresignMethod::Put,
        PresignMethod::Delete => pb::PresignMethod::Delete,
        PresignMethod::Head => pb::PresignMethod::Head,
    }
}

fn presign_method_from_proto(method: i32) -> PresignMethod {
    match pb::PresignMethod::try_from(method).unwrap_or(pb::PresignMethod::Unspecified) {
        pb::PresignMethod::Get => PresignMethod::Get,
        pb::PresignMethod::Put => PresignMethod::Put,
        pb::PresignMethod::Delete => PresignMethod::Delete,
        pb::PresignMethod::Head => PresignMethod::Head,
        pb::PresignMethod::Unspecified => PresignMethod::Unspecified,
    }
}

#[cfg(test)]
mod tests {
    use tonic::Code;

    use super::*;

    struct EmptyS3Provider;

    #[async_trait]
    impl S3Provider for EmptyS3Provider {}

    struct HiddenErrorS3Provider;

    #[async_trait]
    impl S3Provider for HiddenErrorS3Provider {
        async fn head_object(
            &self,
            _request: HeadObjectRequest,
        ) -> ProviderResult<HeadObjectResponse> {
            Err(crate::Error::hidden_internal("backend detail"))
        }
    }

    #[tokio::test]
    async fn default_provider_methods_map_to_unimplemented_status() {
        let server = S3RpcServer::new(Arc::new(EmptyS3Provider));
        let status = <S3RpcServer<EmptyS3Provider> as pb::s3_server::S3>::head_object(
            &server,
            GrpcRequest::new(pb::HeadObjectRequest::default()),
        )
        .await
        .expect_err("default head_object should be unimplemented");

        assert_eq!(status.code(), Code::Unimplemented);
        assert_eq!(status.message(), "s3 head object is not implemented");
    }

    #[tokio::test]
    async fn provider_errors_are_sanitized_at_rpc_boundary() {
        let server = S3RpcServer::new(Arc::new(HiddenErrorS3Provider));
        let status = <S3RpcServer<HiddenErrorS3Provider> as pb::s3_server::S3>::head_object(
            &server,
            GrpcRequest::new(pb::HeadObjectRequest::default()),
        )
        .await
        .expect_err("hidden provider error should fail");

        assert_eq!(status.code(), Code::Unknown);
        assert_eq!(status.message(), "s3 head object: internal error");
    }
}
