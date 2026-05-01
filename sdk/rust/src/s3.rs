use std::collections::BTreeMap;
use std::sync::Arc;
use std::time::Duration;

use hyper_util::rt::TokioIo;
use serde::de::DeserializeOwned;
use tokio_stream::iter;
use tonic::codegen::async_trait;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, Endpoint, Uri};
use tower::service_fn;

use crate::api::RuntimeMetadata;
use crate::error::Result as ProviderResult;
use crate::generated::v1::{
    self as pb, s3_client::S3Client as ProtoS3Client,
    s3_object_access_client::S3ObjectAccessClient as ProtoS3ObjectAccessClient,
};

type ClientResult<T> = std::result::Result<T, S3Error>;
type S3Transport = InterceptedService<Channel, RelayTokenInterceptor>;

/// Default Unix-socket environment variable used by [`S3::connect`].
pub const ENV_S3_SOCKET: &str = "GESTALT_S3_SOCKET";
/// Suffix added to named S3 socket variables for relay-token variables.
pub const ENV_S3_SOCKET_TOKEN_SUFFIX: &str = "_TOKEN";
/// Default relay-token environment variable used by [`S3::connect`].
pub const ENV_S3_SOCKET_TOKEN: &str = "GESTALT_S3_SOCKET_TOKEN";
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
    pub last_modified: Option<prost_types::Timestamp>,
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
    pub if_modified_since: Option<prost_types::Timestamp>,
    /// Read only if the object has not changed since this timestamp.
    pub if_unmodified_since: Option<prost_types::Timestamp>,
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
    pub expires_at: Option<prost_types::Timestamp>,
    /// Headers clients must send with the URL.
    pub headers: BTreeMap<String, String>,
}

/// Options for creating a host-mediated object access URL.
pub type ObjectAccessURLOptions = PresignOptions;
/// Result returned when creating a host-mediated object access URL.
pub type ObjectAccessURL = PresignResult;

#[async_trait]
/// Lifecycle and RPC contract for S3-compatible providers.
pub trait S3Provider: pb::s3_server::S3 + Send + Sync + 'static {
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
}

#[async_trait]
impl<T> pb::s3_server::S3 for Arc<T>
where
    T: S3Provider,
{
    type ReadObjectStream = <T as pb::s3_server::S3>::ReadObjectStream;

    async fn head_object(
        &self,
        request: tonic::Request<pb::HeadObjectRequest>,
    ) -> std::result::Result<tonic::Response<pb::HeadObjectResponse>, tonic::Status> {
        <T as pb::s3_server::S3>::head_object(self.as_ref(), request).await
    }

    async fn read_object(
        &self,
        request: tonic::Request<pb::ReadObjectRequest>,
    ) -> std::result::Result<tonic::Response<Self::ReadObjectStream>, tonic::Status> {
        <T as pb::s3_server::S3>::read_object(self.as_ref(), request).await
    }

    async fn write_object(
        &self,
        request: tonic::Request<tonic::Streaming<pb::WriteObjectRequest>>,
    ) -> std::result::Result<tonic::Response<pb::WriteObjectResponse>, tonic::Status> {
        <T as pb::s3_server::S3>::write_object(self.as_ref(), request).await
    }

    async fn delete_object(
        &self,
        request: tonic::Request<pb::DeleteObjectRequest>,
    ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
        <T as pb::s3_server::S3>::delete_object(self.as_ref(), request).await
    }

    async fn list_objects(
        &self,
        request: tonic::Request<pb::ListObjectsRequest>,
    ) -> std::result::Result<tonic::Response<pb::ListObjectsResponse>, tonic::Status> {
        <T as pb::s3_server::S3>::list_objects(self.as_ref(), request).await
    }

    async fn copy_object(
        &self,
        request: tonic::Request<pb::CopyObjectRequest>,
    ) -> std::result::Result<tonic::Response<pb::CopyObjectResponse>, tonic::Status> {
        <T as pb::s3_server::S3>::copy_object(self.as_ref(), request).await
    }

    async fn presign_object(
        &self,
        request: tonic::Request<pb::PresignObjectRequest>,
    ) -> std::result::Result<tonic::Response<pb::PresignObjectResponse>, tonic::Status> {
        <T as pb::s3_server::S3>::presign_object(self.as_ref(), request).await
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
        let env_name = s3_socket_env(name);
        let target =
            std::env::var(&env_name).map_err(|_| S3Error::Env(format!("{env_name} is not set")))?;
        let token = std::env::var(s3_socket_token_env(name)).unwrap_or_default();

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
                    .connect()
                    .await?
            }
        };

        let interceptor = relay_token_interceptor(token.trim())?;
        Ok(Self {
            client: ProtoS3Client::with_interceptor(channel.clone(), interceptor.clone()),
            object_access_client: ProtoS3ObjectAccessClient::with_interceptor(channel, interceptor),
        })
    }

    /// Returns a convenience handle for one object key.
    pub fn object(&self, bucket: &str, key: &str) -> Object {
        Object {
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
    pub fn object_version(&self, bucket: &str, key: &str, version_id: &str) -> Object {
        Object {
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
                if_modified_since: options.if_modified_since,
                if_unmodified_since: options.if_unmodified_since,
            })
            .await
            .map_err(map_status)?
            .into_inner();

        let first =
            stream.message().await.map_err(map_status)?.ok_or_else(|| {
                S3Error::Protocol("read stream ended before metadata".to_string())
            })?;

        let meta = match first.result {
            Some(pb::read_object_chunk::Result::Meta(meta)) => object_meta_from_proto(meta),
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
        Ok(list_page_from_proto(response.into_inner()))
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
        Ok(presign_result_from_proto(
            response.into_inner(),
            options.method,
        ))
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
        Ok(object_access_url_from_proto(
            response.into_inner(),
            options.method,
        ))
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

/// Convenience wrapper around repeated operations on one object key.
pub struct Object {
    client: ProtoS3Client<S3Transport>,
    object_access_client: ProtoS3ObjectAccessClient<S3Transport>,
    reference: ObjectRef,
}

impl Object {
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

/// Streaming reader returned by [`S3::read_object`] and [`Object::stream`].
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

/// Returns the environment variable used for a named S3 socket.
pub fn s3_socket_env(name: &str) -> String {
    let trimmed = name.trim();
    if trimmed.is_empty() {
        return ENV_S3_SOCKET.to_string();
    }
    let mut env = String::from(ENV_S3_SOCKET);
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

/// Returns the environment variable used for a named S3 relay token.
pub fn s3_socket_token_env(name: &str) -> String {
    if name.trim().is_empty() {
        return ENV_S3_SOCKET_TOKEN.to_string();
    }
    format!("{}{}", s3_socket_env(name), ENV_S3_SOCKET_TOKEN_SUFFIX)
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

fn relay_token_interceptor(token: &str) -> Result<RelayTokenInterceptor, S3Error> {
    let header = if token.trim().is_empty() {
        None
    } else {
        Some(
            MetadataValue::try_from(token.to_string())
                .map_err(|err| S3Error::Env(format!("invalid S3 relay token metadata: {err}")))?,
        )
    };
    Ok(RelayTokenInterceptor { header })
}

#[derive(Clone)]
struct RelayTokenInterceptor {
    header: Option<MetadataValue<tonic::metadata::Ascii>>,
}

impl Interceptor for RelayTokenInterceptor {
    fn call(
        &mut self,
        mut request: tonic::Request<()>,
    ) -> std::result::Result<tonic::Request<()>, tonic::Status> {
        if let Some(header) = self.header.clone() {
            request.metadata_mut().insert(S3_RELAY_TOKEN_HEADER, header);
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

fn object_meta_from_proto(meta: pb::S3ObjectMeta) -> ObjectMeta {
    ObjectMeta {
        reference: meta
            .r#ref
            .map(|reference| ObjectRef {
                bucket: reference.bucket,
                key: reference.key,
                version_id: reference.version_id,
            })
            .unwrap_or_default(),
        etag: meta.etag,
        size: meta.size,
        content_type: meta.content_type,
        last_modified: meta.last_modified,
        metadata: meta.metadata,
        storage_class: meta.storage_class,
    }
}

fn required_object_meta(meta: Option<pb::S3ObjectMeta>, context: &str) -> ClientResult<ObjectMeta> {
    let meta = meta.ok_or_else(|| S3Error::Protocol(context.to_string()))?;
    Ok(object_meta_from_proto(meta))
}

fn byte_range_to_proto(range: ByteRange) -> pb::ByteRange {
    pb::ByteRange {
        start: range.start,
        end: range.end,
    }
}

fn list_page_from_proto(page: pb::ListObjectsResponse) -> ListPage {
    ListPage {
        objects: page
            .objects
            .into_iter()
            .map(object_meta_from_proto)
            .collect(),
        common_prefixes: page.common_prefixes,
        next_continuation_token: page.next_continuation_token,
        has_more: page.has_more,
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
) -> PresignResult {
    let method = presign_method_from_proto(result.method);
    PresignResult {
        url: result.url,
        method: if method == PresignMethod::Unspecified {
            requested_method
        } else {
            method
        },
        expires_at: result.expires_at,
        headers: result.headers,
    }
}

fn object_access_url_from_proto(
    result: pb::CreateObjectAccessUrlResponse,
    requested_method: PresignMethod,
) -> ObjectAccessURL {
    let method = presign_method_from_proto(result.method);
    ObjectAccessURL {
        url: result.url,
        method: if method == PresignMethod::Unspecified {
            requested_method
        } else {
            method
        },
        expires_at: result.expires_at,
        headers: result.headers,
    }
}
