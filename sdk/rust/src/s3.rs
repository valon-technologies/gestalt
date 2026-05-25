use std::collections::BTreeMap;
use std::pin::Pin;
use std::sync::Arc;
use std::time::{Duration, SystemTime};

use hyper_util::rt::TokioIo;
use serde::de::DeserializeOwned;
use tokio_stream::iter;
use tokio_stream::{Stream, StreamExt};
use tonic::codegen::async_trait;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, ClientTlsConfig, Endpoint, Uri};
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};
use tower::service_fn;

use crate::api::RuntimeMetadata;
use crate::env::{ENV_HOST_SERVICE_SOCKET, ENV_HOST_SERVICE_TOKEN, HOST_SERVICE_BINDING_HEADER};
use crate::error::Result as ProviderResult;
use crate::generated::v1::{
    self as pb, s3_client::S3Client as ProtoS3Client,
    s3_object_access_client::S3ObjectAccessClient as ProtoS3ObjectAccessClient,
};
use crate::protocol;
use crate::rpc_status::rpc_status;

type ClientResult<T> = std::result::Result<T, S3Error>;
type S3Transport = InterceptedService<Channel, RelayTokenInterceptor>;
/// Server-streamed object read chunks returned by [`S3Provider::read_object`].
pub type S3ReadObjectStream =
    Pin<Box<dyn Stream<Item = ProviderResult<S3ReadObjectFrame>> + Send + 'static>>;
type S3RpcReadObjectStream =
    Pin<Box<dyn Stream<Item = std::result::Result<pb::ReadObjectChunk, Status>> + Send + 'static>>;

#[derive(Clone, Debug, PartialEq)]
/// One frame in a provider-authored object read stream.
pub enum S3ReadObjectFrame {
    Meta(ObjectMeta),
    Data(Vec<u8>),
}

#[derive(Clone, Debug, PartialEq)]
/// One frame in a provider-backed object write stream.
pub enum S3WriteObjectFrame {
    Open(Box<S3WriteObjectOpen>),
    Data(Vec<u8>),
    Empty,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Metadata frame that starts an object write stream.
pub struct S3WriteObjectOpen {
    pub reference: Option<ObjectRef>,
    pub content_type: String,
    pub cache_control: String,
    pub content_disposition: String,
    pub content_encoding: String,
    pub content_language: String,
    pub metadata: BTreeMap<String, String>,
    pub if_match: String,
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

const S3_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";
const WRITE_CHUNK_SIZE: usize = 64 * 1024;

#[derive(Debug, thiserror::Error)]
/// Errors returned by the S3 transport client.
pub enum S3Error {
    /// The referenced object does not exist.
    #[error("not found")]
    NotFound,
    /// Conditional request headers failed.
    #[error("precondition failed")]
    PreconditionFailed,
    /// The requested byte range is invalid.
    #[error("invalid range")]
    InvalidRange,
    /// The host returned a protocol value the SDK could not represent.
    #[error("{0}")]
    Protocol(String),
    /// The host-service transport could not be created.
    #[error("{0}")]
    Transport(#[from] tonic::transport::Error),
    /// The host-service RPC returned a gRPC status.
    #[error("{0}")]
    Status(#[from] tonic::Status),
    /// Required environment or target configuration was invalid.
    #[error("{0}")]
    Env(String),
    /// JSON encoding or decoding failed.
    #[error("{0}")]
    Json(#[from] serde_json::Error),
    /// UTF-8 decoding failed.
    #[error("{0}")]
    Utf8(#[from] std::string::FromUtf8Error),
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Identifies one object or object version.
pub struct ObjectRef {
    /// Bucket name.
    pub bucket: String,
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

#[derive(Clone, Debug, Default, PartialEq)]
/// Configures conditional and ranged reads.
pub struct ReadOptions {
    /// Optional byte range to read.
    pub range: Option<ByteRange>,
    /// Read only if the current ETag matches this value.
    pub if_match: String,
    /// Read only if the current ETag does not match this value.
    pub if_none_match: String,
    /// Read only if the object has changed since this timestamp.
    pub if_modified_since: Option<SystemTime>,
    /// Read only if the object has not changed since this timestamp.
    pub if_unmodified_since: Option<SystemTime>,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Configures object writes.
pub struct WriteOptions {
    /// MIME content type.
    pub content_type: String,
    /// Cache-Control header value.
    pub cache_control: String,
    /// Content-Disposition header value.
    pub content_disposition: String,
    /// Content-Encoding header value.
    pub content_encoding: String,
    /// Content-Language header value.
    pub content_language: String,
    /// User metadata to store with the object.
    pub metadata: BTreeMap<String, String>,
    /// Write only if the current ETag matches this value.
    pub if_match: String,
    /// Write only if the object does not exist or has a different ETag.
    pub if_none_match: String,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Configures list-objects requests.
pub struct ListOptions {
    /// Bucket to list.
    pub bucket: String,
    /// Prefix filter.
    pub prefix: String,
    /// Delimiter for grouping common prefixes.
    pub delimiter: String,
    /// Continuation token from the previous page.
    pub continuation_token: String,
    /// Start listing after this key.
    pub start_after: String,
    /// Maximum number of keys to return.
    pub max_keys: i32,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Represents one page of list-objects results.
pub struct ListPage {
    /// Object metadata rows in this page.
    pub objects: Vec<ObjectMeta>,
    /// Common prefixes returned by delimiter grouping.
    pub common_prefixes: Vec<String>,
    /// Continuation token for the next page.
    pub next_continuation_token: String,
    /// Whether another page is available.
    pub has_more: bool,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Configures conditional copy requests.
pub struct CopyOptions {
    /// Copy only if the source ETag matches this value.
    pub if_match: String,
    /// Copy only if the source ETag does not match this value.
    pub if_none_match: String,
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

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Configures presigned URL generation.
pub struct PresignOptions {
    /// HTTP method encoded into the URL.
    pub method: PresignMethod,
    /// Requested URL lifetime.
    pub expires: Duration,
    /// Required content type for upload-style URLs.
    pub content_type: String,
    /// Required content disposition for upload-style URLs.
    pub content_disposition: String,
    /// Additional signed headers.
    pub headers: BTreeMap<String, String>,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Contains a presigned URL plus any required headers.
pub struct PresignResult {
    /// URL clients should use.
    pub url: String,
    /// HTTP method clients should use.
    pub method: PresignMethod,
    /// Expiration timestamp, when known.
    pub expires_at: Option<SystemTime>,
    /// Headers clients must send with the URL.
    pub headers: BTreeMap<String, String>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
/// Fetches metadata for one object.
pub struct HeadObjectRequest {
    pub reference: Option<ObjectRef>,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Returns object metadata.
pub struct HeadObjectResponse {
    pub meta: Option<ObjectMeta>,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Opens a streaming object read.
pub struct ReadObjectRequest {
    pub reference: Option<ObjectRef>,
    pub range: Option<ByteRange>,
    pub if_match: String,
    pub if_none_match: String,
    pub if_modified_since: Option<SystemTime>,
    pub if_unmodified_since: Option<SystemTime>,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Returns metadata for a committed object write.
pub struct WriteObjectResponse {
    pub meta: Option<ObjectMeta>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
/// Deletes one object.
pub struct DeleteObjectRequest {
    pub reference: Option<ObjectRef>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
/// Lists objects in a bucket.
pub struct ListObjectsRequest {
    pub bucket: String,
    pub prefix: String,
    pub delimiter: String,
    pub continuation_token: String,
    pub start_after: String,
    pub max_keys: i32,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// One page of list-objects results.
pub struct ListObjectsResponse {
    pub objects: Vec<ObjectMeta>,
    pub common_prefixes: Vec<String>,
    pub next_continuation_token: String,
    pub has_more: bool,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
/// Copies one object to another location.
pub struct CopyObjectRequest {
    pub source: Option<ObjectRef>,
    pub destination: Option<ObjectRef>,
    pub if_match: String,
    pub if_none_match: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Returns metadata for a copied object.
pub struct CopyObjectResponse {
    pub meta: Option<ObjectMeta>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
/// Asks the provider to mint a presigned URL.
pub struct PresignObjectRequest {
    pub reference: Option<ObjectRef>,
    pub method: PresignMethod,
    pub expires: Duration,
    pub content_type: String,
    pub content_disposition: String,
    pub headers: BTreeMap<String, String>,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Returns a presigned URL plus required headers.
pub struct PresignObjectResponse {
    pub url: String,
    pub method: PresignMethod,
    pub expires_at: Option<SystemTime>,
    pub headers: BTreeMap<String, String>,
}

/// Options for creating a host-mediated object access URL.
pub type ObjectAccessURLOptions = PresignOptions;
/// Result returned when creating a host-mediated object access URL.
pub type ObjectAccessURL = PresignResult;

#[async_trait]
/// Fakeable S3-compatible client contract.
pub trait S3Api: Send {
    type Object: S3ObjectApi;

    /// Returns a convenience handle for one object key.
    fn object(&self, bucket: &str, key: &str) -> Self::Object;

    /// Returns a convenience handle for one object version.
    fn object_version(&self, bucket: &str, key: &str, version_id: &str) -> Self::Object;

    /// Fetches metadata for one object.
    async fn head_object(
        &mut self,
        reference: ObjectRef,
    ) -> std::result::Result<ObjectMeta, S3Error>;

    /// Opens a streaming object reader.
    async fn read_object(
        &mut self,
        reference: ObjectRef,
        options: Option<ReadOptions>,
    ) -> std::result::Result<ObjectReader, S3Error>;

    /// Uploads an object from contiguous bytes.
    async fn write_object(
        &mut self,
        reference: ObjectRef,
        body: Vec<u8>,
        options: Option<WriteOptions>,
    ) -> std::result::Result<ObjectMeta, S3Error>;

    /// Deletes one object.
    async fn delete_object(&mut self, reference: ObjectRef) -> std::result::Result<(), S3Error>;

    /// Lists objects in a bucket.
    async fn list_objects(
        &mut self,
        options: ListOptions,
    ) -> std::result::Result<ListPage, S3Error>;

    /// Copies one object to another location.
    async fn copy_object(
        &mut self,
        source: ObjectRef,
        destination: ObjectRef,
        options: Option<CopyOptions>,
    ) -> std::result::Result<ObjectMeta, S3Error>;

    /// Creates a provider-generated presigned URL.
    async fn presign_object(
        &mut self,
        reference: ObjectRef,
        options: Option<PresignOptions>,
    ) -> std::result::Result<PresignResult, S3Error>;

    /// Creates a host-mediated object-access URL.
    async fn create_object_access_url(
        &mut self,
        reference: ObjectRef,
        options: Option<ObjectAccessURLOptions>,
    ) -> std::result::Result<ObjectAccessURL, S3Error>;

    /// Alias for creating a host-mediated object-access URL.
    async fn create_access_url(
        &mut self,
        reference: ObjectRef,
        options: Option<ObjectAccessURLOptions>,
    ) -> std::result::Result<ObjectAccessURL, S3Error>;
}

#[async_trait]
/// Fakeable convenience contract for one S3 object reference.
pub trait S3ObjectApi: Send {
    /// Returns the referenced object key and version.
    fn reference(&self) -> &ObjectRef;

    /// Fetches metadata for the current object.
    async fn stat(&mut self) -> std::result::Result<ObjectMeta, S3Error>;

    /// Reports whether the current object exists.
    async fn exists(&mut self) -> std::result::Result<bool, S3Error>;

    /// Opens a streaming reader for the current object.
    async fn stream(
        &mut self,
        options: Option<ReadOptions>,
    ) -> std::result::Result<ObjectReader, S3Error>;

    /// Reads the entire object into memory.
    async fn bytes(
        &mut self,
        options: Option<ReadOptions>,
    ) -> std::result::Result<Vec<u8>, S3Error>;

    /// Reads the entire object as UTF-8 text.
    async fn text(&mut self, options: Option<ReadOptions>) -> std::result::Result<String, S3Error>;

    /// Reads and decodes the entire object as JSON.
    async fn json<T>(&mut self, options: Option<ReadOptions>) -> std::result::Result<T, S3Error>
    where
        T: DeserializeOwned + Send;

    /// Uploads a new object body.
    async fn write(
        &mut self,
        body: Vec<u8>,
        options: Option<WriteOptions>,
    ) -> std::result::Result<ObjectMeta, S3Error>;

    /// Uploads raw bytes.
    async fn write_bytes(
        &mut self,
        body: Vec<u8>,
        options: Option<WriteOptions>,
    ) -> std::result::Result<ObjectMeta, S3Error>;

    /// Uploads UTF-8 text.
    async fn write_string(
        &mut self,
        body: String,
        options: Option<WriteOptions>,
    ) -> std::result::Result<ObjectMeta, S3Error>;

    /// Uploads JSON, defaulting the content type when omitted.
    async fn write_json<T>(
        &mut self,
        value: &T,
        options: Option<WriteOptions>,
    ) -> std::result::Result<ObjectMeta, S3Error>
    where
        T: serde::Serialize + Sync + ?Sized;

    /// Deletes the current object.
    async fn delete(&mut self) -> std::result::Result<(), S3Error>;

    /// Creates a presigned URL for the current object.
    async fn presign(
        &mut self,
        options: Option<PresignOptions>,
    ) -> std::result::Result<PresignResult, S3Error>;

    /// Creates a host-mediated object-access URL for the current object.
    async fn create_access_url(
        &mut self,
        options: Option<ObjectAccessURLOptions>,
    ) -> std::result::Result<ObjectAccessURL, S3Error>;
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

    /// Lists objects in a bucket using S3-style pagination and delimiters.
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

/// Client for a running S3 provider.
pub struct S3 {
    client: ProtoS3Client<S3Transport>,
    object_access_client: ProtoS3ObjectAccessClient<S3Transport>,
}

impl S3 {
    /// Connects to the default S3 transport socket.
    pub async fn connect() -> ClientResult<Self> {
        Self::connect_named("").await
    }

    /// Connects to a named S3 transport socket.
    pub async fn connect_named(name: &str) -> ClientResult<Self> {
        let target = std::env::var(ENV_HOST_SERVICE_SOCKET)
            .map_err(|_| S3Error::Env(format!("{ENV_HOST_SERVICE_SOCKET} is not set")))?;
        let token = std::env::var(ENV_HOST_SERVICE_TOKEN).unwrap_or_default();

        let channel = match parse_s3_target(&target)? {
            S3Target::Unix(path) => {
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
            S3Target::Tcp(address) => {
                Endpoint::from_shared(format!("http://{address}"))?
                    .connect()
                    .await?
            }
            S3Target::Tls(address) => {
                Endpoint::from_shared(format!("https://{address}"))?
                    .tls_config(ClientTlsConfig::new().with_native_roots())?
                    .connect()
                    .await?
            }
        };

        let interceptor = relay_token_interceptor(token.trim(), name)?;
        Ok(Self {
            client: ProtoS3Client::with_interceptor(channel.clone(), interceptor.clone()),
            object_access_client: ProtoS3ObjectAccessClient::with_interceptor(channel, interceptor),
        })
    }

    /// Returns a convenience handle for one object key.
    pub fn object(&self, bucket: &str, key: &str) -> S3Object {
        S3Object {
            client: self.client.clone(),
            object_access_client: self.object_access_client.clone(),
            reference: ObjectRef {
                bucket: bucket.to_string(),
                key: key.to_string(),
                version_id: String::new(),
            },
        }
    }

    /// Returns a convenience handle for one object version.
    pub fn object_version(&self, bucket: &str, key: &str, version_id: &str) -> S3Object {
        S3Object {
            client: self.client.clone(),
            object_access_client: self.object_access_client.clone(),
            reference: ObjectRef {
                bucket: bucket.to_string(),
                key: key.to_string(),
                version_id: version_id.to_string(),
            },
        }
    }

    /// Fetches metadata for one object.
    pub async fn head_object(&mut self, reference: ObjectRef) -> ClientResult<ObjectMeta> {
        let response = self
            .client
            .head_object(pb::HeadObjectRequest {
                r#ref: Some(object_ref_to_proto(reference)),
            })
            .await
            .map_err(map_status)?;
        required_object_meta(
            response.into_inner().meta,
            "head object response missing metadata",
        )
    }

    /// Opens a streaming object reader.
    pub async fn read_object(
        &mut self,
        reference: ObjectRef,
        options: Option<ReadOptions>,
    ) -> ClientResult<ObjectReader> {
        let options = options.unwrap_or_default();
        let mut stream = self
            .client
            .read_object(pb::ReadObjectRequest {
                r#ref: Some(object_ref_to_proto(reference)),
                range: options.range.map(byte_range_to_proto),
                if_match: options.if_match,
                if_none_match: options.if_none_match,
                if_modified_since: options
                    .if_modified_since
                    .map(protocol::timestamp_from_system_time),
                if_unmodified_since: options
                    .if_unmodified_since
                    .map(protocol::timestamp_from_system_time),
            })
            .await
            .map_err(map_status)?
            .into_inner();

        let first =
            stream.message().await.map_err(map_status)?.ok_or_else(|| {
                S3Error::Protocol("read stream ended before metadata".to_string())
            })?;

        let meta = match first.result {
            Some(pb::read_object_chunk::Result::Meta(meta)) => object_meta_from_proto(meta)?,
            Some(pb::read_object_chunk::Result::Data(_)) => {
                return Err(S3Error::Protocol(
                    "read stream started with data instead of metadata".to_string(),
                ));
            }
            None => {
                return Err(S3Error::Protocol(
                    "read stream started with an empty frame".to_string(),
                ));
            }
        };

        Ok(ObjectReader { meta, stream })
    }

    /// Uploads an object from a contiguous byte slice.
    pub async fn write_object<B>(
        &mut self,
        reference: ObjectRef,
        body: B,
        options: Option<WriteOptions>,
    ) -> ClientResult<ObjectMeta>
    where
        B: AsRef<[u8]>,
    {
        let options = options.unwrap_or_default();
        let open = pb::WriteObjectRequest {
            msg: Some(pb::write_object_request::Msg::Open(pb::WriteObjectOpen {
                r#ref: Some(object_ref_to_proto(reference)),
                content_type: options.content_type,
                cache_control: options.cache_control,
                content_disposition: options.content_disposition,
                content_encoding: options.content_encoding,
                content_language: options.content_language,
                metadata: options.metadata,
                if_match: options.if_match,
                if_none_match: options.if_none_match,
            })),
        };

        let body = body.as_ref();
        let data = body
            .chunks(WRITE_CHUNK_SIZE)
            .filter(|chunk| !chunk.is_empty())
            .map(|chunk| pb::WriteObjectRequest {
                msg: Some(pb::write_object_request::Msg::Data(chunk.to_vec())),
            })
            .collect::<Vec<_>>();

        let response = self
            .client
            .write_object(iter(std::iter::once(open).chain(data)))
            .await
            .map_err(map_status)?;
        required_object_meta(
            response.into_inner().meta,
            "write object response missing metadata",
        )
    }

    /// Uploads an object from multiple pre-chunked buffers.
    pub async fn write_object_chunks<I, B>(
        &mut self,
        reference: ObjectRef,
        chunks: I,
        options: Option<WriteOptions>,
    ) -> ClientResult<ObjectMeta>
    where
        I: IntoIterator<Item = B>,
        I::IntoIter: Send + 'static,
        B: AsRef<[u8]> + Send + 'static,
    {
        let options = options.unwrap_or_default();
        let open = std::iter::once(pb::WriteObjectRequest {
            msg: Some(pb::write_object_request::Msg::Open(pb::WriteObjectOpen {
                r#ref: Some(object_ref_to_proto(reference)),
                content_type: options.content_type,
                cache_control: options.cache_control,
                content_disposition: options.content_disposition,
                content_encoding: options.content_encoding,
                content_language: options.content_language,
                metadata: options.metadata,
                if_match: options.if_match,
                if_none_match: options.if_none_match,
            })),
        });

        let data = chunks.into_iter().filter_map(|chunk| {
            let bytes = chunk.as_ref();
            if bytes.is_empty() {
                return None;
            }
            Some(pb::WriteObjectRequest {
                msg: Some(pb::write_object_request::Msg::Data(bytes.to_vec())),
            })
        });

        let response = self
            .client
            .write_object(iter(open.chain(data)))
            .await
            .map_err(map_status)?;
        required_object_meta(
            response.into_inner().meta,
            "write object response missing metadata",
        )
    }

    /// Deletes one object.
    pub async fn delete_object(&mut self, reference: ObjectRef) -> ClientResult<()> {
        self.client
            .delete_object(pb::DeleteObjectRequest {
                r#ref: Some(object_ref_to_proto(reference)),
            })
            .await
            .map_err(map_status)?;
        Ok(())
    }

    /// Lists objects in a bucket.
    pub async fn list_objects(&mut self, options: ListOptions) -> ClientResult<ListPage> {
        let response = self
            .client
            .list_objects(pb::ListObjectsRequest {
                bucket: options.bucket,
                prefix: options.prefix,
                delimiter: options.delimiter,
                continuation_token: options.continuation_token,
                start_after: options.start_after,
                max_keys: options.max_keys,
            })
            .await
            .map_err(map_status)?;
        list_page_from_proto(response.into_inner())
    }

    /// Copies one object to another location.
    pub async fn copy_object(
        &mut self,
        source: ObjectRef,
        destination: ObjectRef,
        options: Option<CopyOptions>,
    ) -> ClientResult<ObjectMeta> {
        let options = options.unwrap_or_default();
        let response = self
            .client
            .copy_object(pb::CopyObjectRequest {
                source: Some(object_ref_to_proto(source)),
                destination: Some(object_ref_to_proto(destination)),
                if_match: options.if_match,
                if_none_match: options.if_none_match,
            })
            .await
            .map_err(map_status)?;
        required_object_meta(
            response.into_inner().meta,
            "copy object response missing metadata",
        )
    }

    /// Creates a provider-generated presigned URL.
    pub async fn presign_object(
        &mut self,
        reference: ObjectRef,
        options: Option<PresignOptions>,
    ) -> ClientResult<PresignResult> {
        let options = options.unwrap_or_default();
        let expires_seconds = i64::try_from(options.expires.as_secs()).unwrap_or(i64::MAX);
        let response = self
            .client
            .presign_object(pb::PresignObjectRequest {
                r#ref: Some(object_ref_to_proto(reference)),
                method: presign_method_to_proto(options.method) as i32,
                expires_seconds,
                content_type: options.content_type,
                content_disposition: options.content_disposition,
                headers: options.headers,
            })
            .await
            .map_err(map_status)?;
        presign_result_from_proto(response.into_inner(), options.method)
    }

    /// Creates a host-mediated object-access URL.
    pub async fn create_object_access_url(
        &mut self,
        reference: ObjectRef,
        options: Option<ObjectAccessURLOptions>,
    ) -> ClientResult<ObjectAccessURL> {
        let options = options.unwrap_or_default();
        let expires_seconds = i64::try_from(options.expires.as_secs()).unwrap_or(i64::MAX);
        let response = self
            .object_access_client
            .create_object_access_url(pb::CreateObjectAccessUrlRequest {
                r#ref: Some(object_ref_to_proto(reference)),
                method: presign_method_to_proto(options.method) as i32,
                expires_seconds,
                content_type: options.content_type,
                content_disposition: options.content_disposition,
                headers: options.headers,
            })
            .await
            .map_err(map_status)?;
        object_access_url_from_proto(response.into_inner(), options.method)
    }

    /// Alias for [`S3::create_object_access_url`].
    pub async fn create_access_url(
        &mut self,
        reference: ObjectRef,
        options: Option<ObjectAccessURLOptions>,
    ) -> ClientResult<ObjectAccessURL> {
        self.create_object_access_url(reference, options).await
    }
}

#[async_trait]
impl S3Api for S3 {
    type Object = S3Object;

    fn object(&self, bucket: &str, key: &str) -> S3Object {
        S3::object(self, bucket, key)
    }

    fn object_version(&self, bucket: &str, key: &str, version_id: &str) -> S3Object {
        S3::object_version(self, bucket, key, version_id)
    }

    async fn head_object(
        &mut self,
        reference: ObjectRef,
    ) -> std::result::Result<ObjectMeta, S3Error> {
        S3::head_object(self, reference).await
    }

    async fn read_object(
        &mut self,
        reference: ObjectRef,
        options: Option<ReadOptions>,
    ) -> std::result::Result<ObjectReader, S3Error> {
        S3::read_object(self, reference, options).await
    }

    async fn write_object(
        &mut self,
        reference: ObjectRef,
        body: Vec<u8>,
        options: Option<WriteOptions>,
    ) -> std::result::Result<ObjectMeta, S3Error> {
        S3::write_object(self, reference, body, options).await
    }

    async fn delete_object(&mut self, reference: ObjectRef) -> std::result::Result<(), S3Error> {
        S3::delete_object(self, reference).await
    }

    async fn list_objects(
        &mut self,
        options: ListOptions,
    ) -> std::result::Result<ListPage, S3Error> {
        S3::list_objects(self, options).await
    }

    async fn copy_object(
        &mut self,
        source: ObjectRef,
        destination: ObjectRef,
        options: Option<CopyOptions>,
    ) -> std::result::Result<ObjectMeta, S3Error> {
        S3::copy_object(self, source, destination, options).await
    }

    async fn presign_object(
        &mut self,
        reference: ObjectRef,
        options: Option<PresignOptions>,
    ) -> std::result::Result<PresignResult, S3Error> {
        S3::presign_object(self, reference, options).await
    }

    async fn create_object_access_url(
        &mut self,
        reference: ObjectRef,
        options: Option<ObjectAccessURLOptions>,
    ) -> std::result::Result<ObjectAccessURL, S3Error> {
        S3::create_object_access_url(self, reference, options).await
    }

    async fn create_access_url(
        &mut self,
        reference: ObjectRef,
        options: Option<ObjectAccessURLOptions>,
    ) -> std::result::Result<ObjectAccessURL, S3Error> {
        S3::create_access_url(self, reference, options).await
    }
}

/// Convenience wrapper around repeated operations on one object key.
pub struct S3Object {
    client: ProtoS3Client<S3Transport>,
    object_access_client: ProtoS3ObjectAccessClient<S3Transport>,
    reference: ObjectRef,
}

impl S3Object {
    /// Returns the referenced object key and version.
    pub fn reference(&self) -> &ObjectRef {
        &self.reference
    }

    /// Fetches metadata for the current object.
    pub async fn stat(&mut self) -> ClientResult<ObjectMeta> {
        let mut client = S3 {
            client: self.client.clone(),
            object_access_client: self.object_access_client.clone(),
        };
        client.head_object(self.reference.clone()).await
    }

    /// Reports whether the current object exists.
    pub async fn exists(&mut self) -> ClientResult<bool> {
        match self.stat().await {
            Ok(_) => Ok(true),
            Err(S3Error::NotFound) => Ok(false),
            Err(error) => Err(error),
        }
    }

    /// Opens a streaming reader for the current object.
    pub async fn stream(&mut self, options: Option<ReadOptions>) -> ClientResult<ObjectReader> {
        let mut client = S3 {
            client: self.client.clone(),
            object_access_client: self.object_access_client.clone(),
        };
        client.read_object(self.reference.clone(), options).await
    }

    /// Reads the entire object into memory.
    pub async fn bytes(&mut self, options: Option<ReadOptions>) -> ClientResult<Vec<u8>> {
        self.stream(options).await?.bytes().await
    }

    /// Reads the entire object as UTF-8 text.
    pub async fn text(&mut self, options: Option<ReadOptions>) -> ClientResult<String> {
        self.stream(options).await?.text().await
    }

    /// Reads and decodes the entire object as JSON.
    pub async fn json<T>(&mut self, options: Option<ReadOptions>) -> ClientResult<T>
    where
        T: DeserializeOwned,
    {
        self.stream(options).await?.json().await
    }

    /// Uploads a new object body.
    pub async fn write<B>(
        &mut self,
        body: B,
        options: Option<WriteOptions>,
    ) -> ClientResult<ObjectMeta>
    where
        B: AsRef<[u8]>,
    {
        let mut client = S3 {
            client: self.client.clone(),
            object_access_client: self.object_access_client.clone(),
        };
        client
            .write_object(self.reference.clone(), body, options)
            .await
    }

    /// Uploads a pre-chunked object body.
    pub async fn write_chunks<I, B>(
        &mut self,
        chunks: I,
        options: Option<WriteOptions>,
    ) -> ClientResult<ObjectMeta>
    where
        I: IntoIterator<Item = B>,
        I::IntoIter: Send + 'static,
        B: AsRef<[u8]> + Send + 'static,
    {
        let mut client = S3 {
            client: self.client.clone(),
            object_access_client: self.object_access_client.clone(),
        };
        client
            .write_object_chunks(self.reference.clone(), chunks, options)
            .await
    }

    /// Uploads raw bytes.
    pub async fn write_bytes(
        &mut self,
        body: impl AsRef<[u8]>,
        options: Option<WriteOptions>,
    ) -> ClientResult<ObjectMeta> {
        self.write(body, options).await
    }

    /// Uploads UTF-8 text.
    pub async fn write_string(
        &mut self,
        body: impl AsRef<str>,
        options: Option<WriteOptions>,
    ) -> ClientResult<ObjectMeta> {
        self.write(body.as_ref().as_bytes(), options).await
    }

    /// Uploads JSON, defaulting the content type when omitted.
    pub async fn write_json<T>(
        &mut self,
        value: &T,
        options: Option<WriteOptions>,
    ) -> ClientResult<ObjectMeta>
    where
        T: serde::Serialize + ?Sized,
    {
        let body = serde_json::to_vec(value)?;
        let options = match options {
            Some(mut options) => {
                if options.content_type.is_empty() {
                    options.content_type = "application/json".to_string();
                }
                Some(options)
            }
            None => Some(WriteOptions {
                content_type: "application/json".to_string(),
                ..WriteOptions::default()
            }),
        };
        self.write(body, options).await
    }

    /// Deletes the current object.
    pub async fn delete(&mut self) -> ClientResult<()> {
        let mut client = S3 {
            client: self.client.clone(),
            object_access_client: self.object_access_client.clone(),
        };
        client.delete_object(self.reference.clone()).await
    }

    /// Creates a presigned URL for the current object.
    pub async fn presign(
        &mut self,
        options: Option<PresignOptions>,
    ) -> ClientResult<PresignResult> {
        let mut client = S3 {
            client: self.client.clone(),
            object_access_client: self.object_access_client.clone(),
        };
        client.presign_object(self.reference.clone(), options).await
    }

    /// Creates a host-mediated object-access URL for the current object.
    pub async fn create_access_url(
        &mut self,
        options: Option<ObjectAccessURLOptions>,
    ) -> ClientResult<ObjectAccessURL> {
        let mut client = S3 {
            client: self.client.clone(),
            object_access_client: self.object_access_client.clone(),
        };
        client
            .create_object_access_url(self.reference.clone(), options)
            .await
    }
}

#[async_trait]
impl S3ObjectApi for S3Object {
    fn reference(&self) -> &ObjectRef {
        S3Object::reference(self)
    }

    async fn stat(&mut self) -> std::result::Result<ObjectMeta, S3Error> {
        S3Object::stat(self).await
    }

    async fn exists(&mut self) -> std::result::Result<bool, S3Error> {
        S3Object::exists(self).await
    }

    async fn stream(
        &mut self,
        options: Option<ReadOptions>,
    ) -> std::result::Result<ObjectReader, S3Error> {
        S3Object::stream(self, options).await
    }

    async fn bytes(
        &mut self,
        options: Option<ReadOptions>,
    ) -> std::result::Result<Vec<u8>, S3Error> {
        S3Object::bytes(self, options).await
    }

    async fn text(&mut self, options: Option<ReadOptions>) -> std::result::Result<String, S3Error> {
        S3Object::text(self, options).await
    }

    async fn json<T>(&mut self, options: Option<ReadOptions>) -> std::result::Result<T, S3Error>
    where
        T: DeserializeOwned + Send,
    {
        S3Object::json(self, options).await
    }

    async fn write(
        &mut self,
        body: Vec<u8>,
        options: Option<WriteOptions>,
    ) -> std::result::Result<ObjectMeta, S3Error> {
        S3Object::write(self, body, options).await
    }

    async fn write_bytes(
        &mut self,
        body: Vec<u8>,
        options: Option<WriteOptions>,
    ) -> std::result::Result<ObjectMeta, S3Error> {
        S3Object::write_bytes(self, body, options).await
    }

    async fn write_string(
        &mut self,
        body: String,
        options: Option<WriteOptions>,
    ) -> std::result::Result<ObjectMeta, S3Error> {
        S3Object::write_string(self, body, options).await
    }

    async fn write_json<T>(
        &mut self,
        value: &T,
        options: Option<WriteOptions>,
    ) -> std::result::Result<ObjectMeta, S3Error>
    where
        T: serde::Serialize + Sync + ?Sized,
    {
        S3Object::write_json(self, value, options).await
    }

    async fn delete(&mut self) -> std::result::Result<(), S3Error> {
        S3Object::delete(self).await
    }

    async fn presign(
        &mut self,
        options: Option<PresignOptions>,
    ) -> std::result::Result<PresignResult, S3Error> {
        S3Object::presign(self, options).await
    }

    async fn create_access_url(
        &mut self,
        options: Option<ObjectAccessURLOptions>,
    ) -> std::result::Result<ObjectAccessURL, S3Error> {
        S3Object::create_access_url(self, options).await
    }
}

/// Streaming reader returned by [`S3::read_object`] and [`S3Object::stream`].
pub struct ObjectReader {
    meta: ObjectMeta,
    stream: tonic::Streaming<pb::ReadObjectChunk>,
}

impl ObjectReader {
    /// Returns the metadata frame emitted at the start of the stream.
    pub fn meta(&self) -> &ObjectMeta {
        &self.meta
    }

    /// Returns the next non-empty body chunk.
    pub async fn next_chunk(&mut self) -> ClientResult<Option<Vec<u8>>> {
        loop {
            let Some(message) = self.stream.message().await.map_err(map_status)? else {
                return Ok(None);
            };

            match message.result {
                Some(pb::read_object_chunk::Result::Data(data)) => {
                    if data.is_empty() {
                        continue;
                    }
                    return Ok(Some(data));
                }
                Some(pb::read_object_chunk::Result::Meta(_)) => {
                    return Err(S3Error::Protocol(
                        "read stream emitted metadata after the initial frame".to_string(),
                    ));
                }
                None => continue,
            }
        }
    }

    /// Reads the remainder of the stream into memory.
    pub async fn bytes(mut self) -> ClientResult<Vec<u8>> {
        let mut body = Vec::new();
        while let Some(chunk) = self.next_chunk().await? {
            body.extend_from_slice(&chunk);
        }
        Ok(body)
    }

    /// Reads the remainder of the stream as UTF-8 text.
    pub async fn text(self) -> ClientResult<String> {
        Ok(String::from_utf8(self.bytes().await?)?)
    }

    /// Reads and decodes the remainder of the stream as JSON.
    pub async fn json<T>(self) -> ClientResult<T>
    where
        T: DeserializeOwned,
    {
        Ok(serde_json::from_slice(&self.bytes().await?)?)
    }
}

enum S3Target {
    Unix(String),
    Tcp(String),
    Tls(String),
}

fn parse_s3_target(raw_target: &str) -> Result<S3Target, S3Error> {
    let target = raw_target.trim();
    if target.is_empty() {
        return Err(S3Error::Env("S3 transport target is required".to_string()));
    }
    if let Some(address) = target.strip_prefix("tcp://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(S3Error::Env(format!(
                "S3 tcp target {raw_target:?} is missing host:port"
            )));
        }
        return Ok(S3Target::Tcp(address.to_string()));
    }
    if let Some(address) = target.strip_prefix("tls://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(S3Error::Env(format!(
                "S3 tls target {raw_target:?} is missing host:port"
            )));
        }
        return Ok(S3Target::Tls(address.to_string()));
    }
    if let Some(path) = target.strip_prefix("unix://") {
        let path = path.trim();
        if path.is_empty() {
            return Err(S3Error::Env(format!(
                "S3 unix target {raw_target:?} is missing a socket path"
            )));
        }
        return Ok(S3Target::Unix(path.to_string()));
    }
    if target.contains("://") {
        let scheme = target.split("://").next().unwrap_or_default();
        return Err(S3Error::Env(format!(
            "unsupported S3 target scheme {scheme:?}"
        )));
    }
    Ok(S3Target::Unix(target.to_string()))
}

fn relay_token_interceptor(token: &str, binding: &str) -> Result<RelayTokenInterceptor, S3Error> {
    let relay_token = if token.trim().is_empty() {
        None
    } else {
        Some(
            MetadataValue::try_from(token.to_string())
                .map_err(|err| S3Error::Env(format!("invalid S3 relay token metadata: {err}")))?,
        )
    };
    let binding = if binding.trim().is_empty() {
        None
    } else {
        Some(
            MetadataValue::try_from(binding.trim().to_string())
                .map_err(|err| S3Error::Env(format!("invalid S3 binding metadata: {err}")))?,
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
    fn call(
        &mut self,
        mut request: tonic::Request<()>,
    ) -> std::result::Result<tonic::Request<()>, tonic::Status> {
        if let Some(header) = self.relay_token.clone() {
            request.metadata_mut().insert(S3_RELAY_TOKEN_HEADER, header);
        }
        if let Some(header) = self.binding.clone() {
            request
                .metadata_mut()
                .insert(HOST_SERVICE_BINDING_HEADER, header);
        }
        Ok(request)
    }
}

fn map_status(err: tonic::Status) -> S3Error {
    match err.code() {
        tonic::Code::NotFound => S3Error::NotFound,
        tonic::Code::FailedPrecondition => S3Error::PreconditionFailed,
        tonic::Code::OutOfRange => S3Error::InvalidRange,
        _ => S3Error::Status(err),
    }
}

fn object_ref_to_proto(reference: ObjectRef) -> pb::S3ObjectRef {
    pb::S3ObjectRef {
        bucket: reference.bucket,
        key: reference.key,
        version_id: reference.version_id,
    }
}

fn object_ref_from_proto(reference: pb::S3ObjectRef) -> ObjectRef {
    ObjectRef {
        bucket: reference.bucket,
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

fn object_meta_from_proto(meta: pb::S3ObjectMeta) -> ClientResult<ObjectMeta> {
    Ok(ObjectMeta {
        reference: meta.r#ref.map(object_ref_from_proto).unwrap_or_default(),
        etag: meta.etag,
        size: meta.size,
        content_type: meta.content_type,
        last_modified: meta
            .last_modified
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()
            .map_err(|error| S3Error::Protocol(error.to_string()))?,
        metadata: meta.metadata,
        storage_class: meta.storage_class,
    })
}

fn required_object_meta(meta: Option<pb::S3ObjectMeta>, context: &str) -> ClientResult<ObjectMeta> {
    let meta = meta.ok_or_else(|| S3Error::Protocol(context.to_string()))?;
    object_meta_from_proto(meta)
}

fn byte_range_to_proto(range: ByteRange) -> pb::ByteRange {
    pb::ByteRange {
        start: range.start,
        end: range.end,
    }
}

fn list_page_from_proto(page: pb::ListObjectsResponse) -> ClientResult<ListPage> {
    Ok(ListPage {
        objects: page
            .objects
            .into_iter()
            .map(object_meta_from_proto)
            .collect::<ClientResult<Vec<_>>>()?,
        common_prefixes: page.common_prefixes,
        next_continuation_token: page.next_continuation_token,
        has_more: page.has_more,
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
        bucket: request.bucket,
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

fn presign_result_from_proto(
    result: pb::PresignObjectResponse,
    requested_method: PresignMethod,
) -> ClientResult<PresignResult> {
    let method = presign_method_from_proto(result.method);
    Ok(PresignResult {
        url: result.url,
        method: if method == PresignMethod::Unspecified {
            requested_method
        } else {
            method
        },
        expires_at: result
            .expires_at
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()
            .map_err(|error| S3Error::Protocol(error.to_string()))?,
        headers: result.headers,
    })
}

fn object_access_url_from_proto(
    result: pb::CreateObjectAccessUrlResponse,
    requested_method: PresignMethod,
) -> ClientResult<ObjectAccessURL> {
    let method = presign_method_from_proto(result.method);
    Ok(ObjectAccessURL {
        url: result.url,
        method: if method == PresignMethod::Unspecified {
            requested_method
        } else {
            method
        },
        expires_at: result
            .expires_at
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()
            .map_err(|error| S3Error::Protocol(error.to_string()))?,
        headers: result.headers,
    })
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
