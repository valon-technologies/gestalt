#[allow(dead_code)]
mod helpers;

use std::collections::BTreeMap;
use std::path::Path;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use gestalt::proto::v1::auth_provider_client::AuthProviderClient;
use gestalt::proto::v1::provider_lifecycle_client::ProviderLifecycleClient;
use gestalt::proto::v1::s3_client::S3Client;
use gestalt::proto::v1::s3_server::S3 as ProtoS3;
use gestalt::proto::v1::{
    BeginLoginRequest, CompleteLoginRequest, ConfigureProviderRequest, CopyObjectRequest,
    CopyObjectResponse, DeleteObjectRequest, HeadObjectRequest, HeadObjectResponse,
    ListObjectsRequest, ListObjectsResponse, PresignObjectRequest, PresignObjectResponse,
    ProviderKind, ReadObjectChunk, ReadObjectRequest, S3ObjectMeta, S3ObjectRef,
    ValidateExternalTokenRequest, WriteObjectRequest, WriteObjectResponse,
};
use gestalt::{AuthProvider, RuntimeMetadata};
use hyper_util::rt::tokio::TokioIo;
use tokio::net::UnixStream;
use tokio_stream::iter as stream_iter;
use tonic::Code;
use tonic::codegen::async_trait;
use tonic::transport::Endpoint;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};
use tower::service_fn;

struct TestAuthProvider {
    configured_name: Mutex<String>,
}

impl Default for TestAuthProvider {
    fn default() -> Self {
        Self {
            configured_name: Mutex::new(String::new()),
        }
    }
}

#[async_trait]
impl AuthProvider for TestAuthProvider {
    async fn configure(
        &self,
        name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> gestalt::Result<()> {
        *self.configured_name.lock().expect("lock configured_name") = name.to_string();
        Ok(())
    }

    fn metadata(&self) -> Option<RuntimeMetadata> {
        Some(RuntimeMetadata {
            name: "auth-example".to_string(),
            display_name: "Auth Example".to_string(),
            description: "Test auth provider".to_string(),
            version: "0.1.0".to_string(),
        })
    }

    fn warnings(&self) -> Vec<String> {
        vec!["set OIDC_BASE_URL".to_string()]
    }

    async fn begin_login(
        &self,
        req: BeginLoginRequest,
    ) -> gestalt::Result<gestalt::BeginLoginResponse> {
        Ok(gestalt::BeginLoginResponse {
            authorization_url: format!("https://example.com/login?state={}", req.host_state),
            provider_state: b"provider-state".to_vec(),
        })
    }

    async fn complete_login(
        &self,
        req: CompleteLoginRequest,
    ) -> gestalt::Result<gestalt::AuthenticatedUser> {
        Ok(gestalt::AuthenticatedUser {
            subject: "sub_123".to_string(),
            email: req
                .query
                .get("email")
                .cloned()
                .unwrap_or_else(|| "sdk@example.com".to_string()),
            email_verified: true,
            display_name: "SDK User".to_string(),
            avatar_url: String::new(),
            claims: BTreeMap::from([("source".to_string(), "complete_login".to_string())]),
        })
    }

    async fn validate_external_token(
        &self,
        token: &str,
    ) -> gestalt::Result<Option<gestalt::AuthenticatedUser>> {
        if token == "external-token" {
            return Ok(Some(gestalt::AuthenticatedUser {
                subject: "sub_external".to_string(),
                email: "external@example.com".to_string(),
                email_verified: true,
                display_name: "External User".to_string(),
                avatar_url: String::new(),
                claims: BTreeMap::new(),
            }));
        }
        Ok(None)
    }

    fn session_ttl(&self) -> Option<Duration> {
        Some(Duration::from_secs(7200))
    }
}

#[derive(Default)]
struct TestS3Provider {
    configured_name: Mutex<String>,
    objects: Mutex<BTreeMap<String, Vec<u8>>>,
}

#[async_trait]
impl gestalt::S3Provider for TestS3Provider {
    async fn configure(
        &self,
        name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> gestalt::Result<()> {
        *self.configured_name.lock().expect("lock configured_name") = name.to_string();
        Ok(())
    }

    fn metadata(&self) -> Option<RuntimeMetadata> {
        Some(RuntimeMetadata {
            name: "s3-example".to_string(),
            display_name: "S3 Example".to_string(),
            description: "Test s3 provider".to_string(),
            version: "0.1.0".to_string(),
        })
    }

    fn warnings(&self) -> Vec<String> {
        vec!["set STORAGE_BUCKET".to_string()]
    }
}

#[tonic::async_trait]
impl ProtoS3 for TestS3Provider {
    type ReadObjectStream =
        tokio_stream::Iter<std::vec::IntoIter<std::result::Result<ReadObjectChunk, Status>>>;

    async fn head_object(
        &self,
        request: GrpcRequest<HeadObjectRequest>,
    ) -> std::result::Result<GrpcResponse<HeadObjectResponse>, Status> {
        let reference = request
            .into_inner()
            .r#ref
            .ok_or_else(|| Status::invalid_argument("missing ref"))?;
        let key = object_key(&reference.bucket, &reference.key);
        let objects = self.objects.lock().expect("lock objects");
        let body = objects
            .get(&key)
            .ok_or_else(|| Status::not_found("object not found"))?;
        Ok(GrpcResponse::new(HeadObjectResponse {
            meta: Some(object_meta(
                reference,
                body.len() as i64,
                "application/octet-stream",
            )),
        }))
    }

    async fn read_object(
        &self,
        request: GrpcRequest<ReadObjectRequest>,
    ) -> std::result::Result<GrpcResponse<Self::ReadObjectStream>, Status> {
        let reference = request
            .into_inner()
            .r#ref
            .ok_or_else(|| Status::invalid_argument("missing ref"))?;
        let key = object_key(&reference.bucket, &reference.key);
        let objects = self.objects.lock().expect("lock objects");
        let body = objects
            .get(&key)
            .cloned()
            .ok_or_else(|| Status::not_found("object not found"))?;
        drop(objects);

        let mut messages = vec![Ok(ReadObjectChunk {
            result: Some(gestalt::proto::v1::read_object_chunk::Result::Meta(
                object_meta(reference, body.len() as i64, "application/octet-stream"),
            )),
        })];
        if !body.is_empty() {
            messages.push(Ok(ReadObjectChunk {
                result: Some(gestalt::proto::v1::read_object_chunk::Result::Data(body)),
            }));
        }

        Ok(GrpcResponse::new(stream_iter(messages)))
    }

    async fn write_object(
        &self,
        request: GrpcRequest<tonic::Streaming<WriteObjectRequest>>,
    ) -> std::result::Result<GrpcResponse<WriteObjectResponse>, Status> {
        let mut stream = request.into_inner();
        let mut reference = None;
        let mut content_type = String::new();
        let mut body = Vec::new();

        while let Some(message) = stream.message().await? {
            match message.msg {
                Some(gestalt::proto::v1::write_object_request::Msg::Open(open)) => {
                    reference = open.r#ref;
                    content_type = open.content_type;
                }
                Some(gestalt::proto::v1::write_object_request::Msg::Data(chunk)) => {
                    body.extend_from_slice(&chunk);
                }
                None => {}
            }
        }

        let reference = reference.ok_or_else(|| Status::invalid_argument("missing open frame"))?;
        self.objects
            .lock()
            .expect("lock objects")
            .insert(object_key(&reference.bucket, &reference.key), body.clone());

        Ok(GrpcResponse::new(WriteObjectResponse {
            meta: Some(object_meta(reference, body.len() as i64, &content_type)),
        }))
    }

    async fn delete_object(
        &self,
        request: GrpcRequest<DeleteObjectRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        let reference = request
            .into_inner()
            .r#ref
            .ok_or_else(|| Status::invalid_argument("missing ref"))?;
        self.objects
            .lock()
            .expect("lock objects")
            .remove(&object_key(&reference.bucket, &reference.key));
        Ok(GrpcResponse::new(()))
    }

    async fn list_objects(
        &self,
        request: GrpcRequest<ListObjectsRequest>,
    ) -> std::result::Result<GrpcResponse<ListObjectsResponse>, Status> {
        let request = request.into_inner();
        let objects = self.objects.lock().expect("lock objects");
        let mut metas = Vec::new();
        for (key, body) in objects.iter() {
            let Some((bucket, object_key)) = key.split_once('/') else {
                continue;
            };
            if bucket != request.bucket {
                continue;
            }
            if !request.prefix.is_empty() && !object_key.starts_with(&request.prefix) {
                continue;
            }
            metas.push(object_meta(
                S3ObjectRef {
                    bucket: bucket.to_string(),
                    key: object_key.to_string(),
                    version_id: String::new(),
                },
                body.len() as i64,
                "application/octet-stream",
            ));
        }
        Ok(GrpcResponse::new(ListObjectsResponse {
            objects: metas,
            ..ListObjectsResponse::default()
        }))
    }

    async fn copy_object(
        &self,
        request: GrpcRequest<CopyObjectRequest>,
    ) -> std::result::Result<GrpcResponse<CopyObjectResponse>, Status> {
        let request = request.into_inner();
        let source = request
            .source
            .ok_or_else(|| Status::invalid_argument("missing source"))?;
        let destination = request
            .destination
            .ok_or_else(|| Status::invalid_argument("missing destination"))?;
        let mut objects = self.objects.lock().expect("lock objects");
        let body = objects
            .get(&object_key(&source.bucket, &source.key))
            .cloned()
            .ok_or_else(|| Status::not_found("object not found"))?;
        objects.insert(
            object_key(&destination.bucket, &destination.key),
            body.clone(),
        );
        Ok(GrpcResponse::new(CopyObjectResponse {
            meta: Some(object_meta(
                destination,
                body.len() as i64,
                "application/octet-stream",
            )),
        }))
    }

    async fn presign_object(
        &self,
        request: GrpcRequest<PresignObjectRequest>,
    ) -> std::result::Result<GrpcResponse<PresignObjectResponse>, Status> {
        let request = request.into_inner();
        let reference = request
            .r#ref
            .ok_or_else(|| Status::invalid_argument("missing ref"))?;
        Ok(GrpcResponse::new(PresignObjectResponse {
            url: format!(
                "https://example.invalid/{}/{}",
                reference.bucket, reference.key
            ),
            method: request.method,
            expires_at: None,
            headers: request.headers,
        }))
    }
}

#[tokio::test]
async fn serves_auth_provider_and_runtime_over_unix_socket() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-auth.sock");
    let _socket_guard = helpers::EnvGuard::set(gestalt::ENV_PROVIDER_SOCKET, socket.as_os_str());

    let provider = Arc::new(TestAuthProvider::default());
    let serve_provider = Arc::clone(&provider);
    let serve_task = tokio::spawn(async move {
        gestalt::runtime::serve_auth_provider(serve_provider)
            .await
            .expect("serve auth provider");
    });

    helpers::wait_for_socket(&socket).await;

    let channel = connect_unix(&socket).await;
    let mut runtime = ProviderLifecycleClient::new(channel.clone());
    let mut auth = AuthProviderClient::new(channel);

    let metadata = runtime
        .get_provider_identity(())
        .await
        .expect("get provider identity")
        .into_inner();
    assert_eq!(
        ProviderKind::try_from(metadata.kind)
            .expect("valid provider kind")
            .as_str_name(),
        "PROVIDER_KIND_AUTH"
    );
    assert_eq!(metadata.name, "auth-example");
    assert_eq!(metadata.warnings, vec!["set OIDC_BASE_URL"]);

    let configured = runtime
        .configure_provider(ConfigureProviderRequest {
            name: "auth-runtime".to_string(),
            config: Some(helpers::struct_from_json(
                serde_json::json!({ "issuer": "https://issuer" }),
            )),
            protocol_version: gestalt::CURRENT_PROTOCOL_VERSION,
        })
        .await
        .expect("configure provider")
        .into_inner();
    assert_eq!(
        configured.protocol_version,
        gestalt::CURRENT_PROTOCOL_VERSION
    );

    let begin = auth
        .begin_login(BeginLoginRequest {
            callback_url: "https://host/callback".to_string(),
            host_state: "host-state".to_string(),
            scopes: vec!["openid".to_string()],
            options: BTreeMap::new(),
        })
        .await
        .expect("begin login")
        .into_inner();
    assert!(begin.authorization_url.contains("host-state"));
    assert_eq!(begin.provider_state, b"provider-state");

    let completed = auth
        .complete_login(CompleteLoginRequest {
            query: BTreeMap::from([("email".to_string(), "complete@example.com".to_string())]),
            provider_state: b"provider-state".to_vec(),
            callback_url: "https://host/callback".to_string(),
        })
        .await
        .expect("complete login")
        .into_inner();
    assert_eq!(completed.email, "complete@example.com");

    let validated = auth
        .validate_external_token(ValidateExternalTokenRequest {
            token: "external-token".to_string(),
        })
        .await
        .expect("validate external token")
        .into_inner();
    assert_eq!(validated.subject, "sub_external");

    let err = auth
        .validate_external_token(ValidateExternalTokenRequest {
            token: "missing-token".to_string(),
        })
        .await
        .expect_err("unknown token should return not found");
    assert_eq!(err.code(), Code::NotFound);

    let session_settings = auth
        .get_session_settings(())
        .await
        .expect("get session settings")
        .into_inner();
    assert_eq!(session_settings.session_ttl_seconds, 7200);

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn serves_s3_provider_and_runtime_over_unix_socket() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-s3.sock");
    let _socket_guard = helpers::EnvGuard::set(gestalt::ENV_PROVIDER_SOCKET, socket.as_os_str());

    let provider = Arc::new(TestS3Provider::default());
    let serve_provider = Arc::clone(&provider);
    let serve_task = tokio::spawn(async move {
        gestalt::runtime::serve_s3_provider(serve_provider)
            .await
            .expect("serve s3 provider");
    });

    helpers::wait_for_socket(&socket).await;

    let channel = connect_unix(&socket).await;
    let mut runtime = ProviderLifecycleClient::new(channel.clone());
    let mut s3 = S3Client::new(channel);

    let metadata = runtime
        .get_provider_identity(())
        .await
        .expect("get provider identity")
        .into_inner();
    assert_eq!(
        ProviderKind::try_from(metadata.kind)
            .expect("valid provider kind")
            .as_str_name(),
        "PROVIDER_KIND_S3"
    );
    assert_eq!(metadata.name, "s3-example");
    assert_eq!(metadata.warnings, vec!["set STORAGE_BUCKET"]);

    runtime
        .configure_provider(ConfigureProviderRequest {
            name: "s3-runtime".to_string(),
            config: Some(helpers::struct_from_json(
                serde_json::json!({ "bucket": "sdk-bucket" }),
            )),
            protocol_version: gestalt::CURRENT_PROTOCOL_VERSION,
        })
        .await
        .expect("configure provider");
    assert_eq!(
        *provider
            .configured_name
            .lock()
            .expect("lock configured_name"),
        "s3-runtime"
    );

    let reference = S3ObjectRef {
        bucket: "bucket".to_string(),
        key: "docs/example.txt".to_string(),
        version_id: String::new(),
    };
    s3.write_object(stream_iter(vec![
        WriteObjectRequest {
            msg: Some(gestalt::proto::v1::write_object_request::Msg::Open(
                gestalt::proto::v1::WriteObjectOpen {
                    r#ref: Some(reference.clone()),
                    ..gestalt::proto::v1::WriteObjectOpen::default()
                },
            )),
        },
        WriteObjectRequest {
            msg: Some(gestalt::proto::v1::write_object_request::Msg::Data(
                b"hello".to_vec(),
            )),
        },
    ]))
    .await
    .expect("write object");

    let head = s3
        .head_object(HeadObjectRequest {
            r#ref: Some(reference.clone()),
        })
        .await
        .expect("head object")
        .into_inner();
    assert_eq!(head.meta.expect("meta").size, 5);

    let listed = s3
        .list_objects(ListObjectsRequest {
            bucket: "bucket".to_string(),
            prefix: "docs/".to_string(),
            ..ListObjectsRequest::default()
        })
        .await
        .expect("list objects")
        .into_inner();
    assert_eq!(listed.objects.len(), 1);
    assert_eq!(
        listed.objects[0].r#ref.as_ref().expect("ref").key,
        "docs/example.txt"
    );

    let mut stream = s3
        .read_object(ReadObjectRequest {
            r#ref: Some(reference),
            ..ReadObjectRequest::default()
        })
        .await
        .expect("read object")
        .into_inner();
    let first = stream
        .message()
        .await
        .expect("recv meta")
        .expect("meta frame");
    assert!(matches!(
        first.result,
        Some(gestalt::proto::v1::read_object_chunk::Result::Meta(_))
    ));
    let second = stream
        .message()
        .await
        .expect("recv data")
        .expect("data frame");
    assert_eq!(
        second
            .result
            .and_then(|result| match result {
                gestalt::proto::v1::read_object_chunk::Result::Data(data) => Some(data),
                _ => None,
            })
            .expect("data payload"),
        b"hello".to_vec()
    );

    serve_task.abort();
    let _ = serve_task.await;
}

fn object_key(bucket: &str, key: &str) -> String {
    format!("{bucket}/{key}")
}

fn object_meta(reference: S3ObjectRef, size: i64, content_type: &str) -> S3ObjectMeta {
    S3ObjectMeta {
        r#ref: Some(reference),
        etag: String::new(),
        size,
        content_type: content_type.to_string(),
        last_modified: None,
        metadata: BTreeMap::new(),
        storage_class: String::new(),
    }
}

async fn connect_unix(path: &Path) -> tonic::transport::Channel {
    Endpoint::try_from("http://[::]:50051")
        .expect("endpoint")
        .connect_with_connector(service_fn({
            let path = path.to_path_buf();
            move |_| {
                let path = path.clone();
                async move { UnixStream::connect(path).await.map(TokioIo::new) }
            }
        }))
        .await
        .expect("connect channel")
}
