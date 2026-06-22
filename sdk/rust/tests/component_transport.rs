#[path = "../src/generated.rs"]
mod generated;

#[allow(dead_code)]
mod helpers;

use std::collections::BTreeMap;
use std::path::Path;
use std::sync::{Arc, Mutex};

use generated::v1::identity_client::IdentityClient;
use generated::v1::provider_lifecycle_client::ProviderLifecycleClient;
use generated::v1::s3_client::S3Client;
use generated::v1::{
    AuthorizeRequest as ProtoAuthorizeRequest, ConfigureProviderRequest,
    HeadObjectRequest as ProtoHeadObjectRequest, IntrospectRequest as ProtoIntrospectRequest,
    ListObjectsRequest as ProtoListObjectsRequest, ProviderKind,
    ReadObjectRequest as ProtoReadObjectRequest, S3ObjectRef, TokenRequest as ProtoTokenRequest,
    WriteObjectRequest as ProtoWriteObjectRequest,
};
use gestalt::identity::{
    AuthorizeRequest, AuthorizeResponse, GetGrantRequest, GetGrantResponse, IntrospectRequest,
    IntrospectResponse, ListGrantsRequest, ListGrantsResponse, RevokeGrantRequest,
    RevokeGrantResponse, TokenRequest, TokenResponse,
};
use gestalt::s3_provider::{
    CopyObjectRequest, CopyObjectResponse, DeleteObjectRequest, HeadObjectRequest,
    HeadObjectResponse, ListObjectsRequest, ListObjectsResponse, ObjectMeta, ObjectRef,
    PresignObjectRequest, PresignObjectResponse, ReadObjectRequest, S3ReadObjectFrame,
    S3WriteObjectFrame, WriteObjectResponse,
};
use gestalt::{IdentityProvider, RuntimeMetadata};
use hyper_util::rt::tokio::TokioIo;
use tokio::net::UnixStream;
use tokio_stream::iter as stream_iter;
use tonic::codegen::async_trait;
use tonic::transport::Endpoint;
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
impl IdentityProvider for TestAuthProvider {
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
            description: "Test authentication provider".to_string(),
            version: "0.1.0".to_string(),
        })
    }

    fn warnings(&self) -> Vec<String> {
        vec!["set OIDC_BASE_URL".to_string()]
    }

    async fn authorize(&self, req: AuthorizeRequest) -> gestalt::Result<AuthorizeResponse> {
        Ok(AuthorizeResponse {
            redirect_uri: format!(
                "https://example.com/callback?code=fixture-code&state={}",
                req.state
            ),
        })
    }

    async fn token(&self, req: TokenRequest) -> gestalt::Result<TokenResponse> {
        if req.code != "fixture-code" {
            return Err(gestalt::Error::bad_request("invalid code"));
        }
        Ok(TokenResponse {
            access_token: "fixture-access-token".to_string(),
            token_type: "Bearer".to_string(),
            expires_in: 3600,
            refresh_token: String::new(),
            scope: "openid email".to_string(),
            grant_id: "grant-fixture".to_string(),
        })
    }

    async fn introspect(&self, req: IntrospectRequest) -> gestalt::Result<IntrospectResponse> {
        if req.token == "fixture-access-token" {
            return Ok(IntrospectResponse {
                active: true,
                subject: "user:fixture".to_string(),
                scope: "openid email".to_string(),
                client_id: "gestaltd".to_string(),
                audience: vec!["https://issuer".to_string()],
            });
        }
        Ok(IntrospectResponse {
            active: false,
            subject: String::new(),
            scope: String::new(),
            client_id: String::new(),
            audience: Vec::new(),
        })
    }

    async fn user_info(
        &self,
        call: gestalt::IdentityCallContext,
        _req: gestalt::identity::UserInfoRequest,
    ) -> gestalt::Result<gestalt::identity::UserInfoResponse> {
        if call.caller_bearer_token == "fixture-access-token" {
            return Ok(gestalt::identity::UserInfoResponse {
                subject_id: "user:fixture".to_string(),
                email: "fixture@example.com".to_string(),
                name: "Fixture User".to_string(),
            });
        }
        Err(gestalt::Error::not_found("userinfo not found"))
    }

    async fn list_grants(
        &self,
        _call: gestalt::IdentityCallContext,
        _req: ListGrantsRequest,
    ) -> gestalt::Result<ListGrantsResponse> {
        Ok(ListGrantsResponse {
            grant_ids: vec!["grant-fixture".to_string()],
        })
    }

    async fn get_grant(
        &self,
        _call: gestalt::IdentityCallContext,
        req: GetGrantRequest,
    ) -> gestalt::Result<GetGrantResponse> {
        if req.grant_id != "grant-fixture" {
            return Err(gestalt::Error::not_found("grant not found"));
        }
        Ok(GetGrantResponse {
            scopes: vec![gestalt::identity::GrantScope {
                scope: "openid".to_string(),
                resource: Vec::new(),
            }],
            created_at: 1_700_000_000,
            expires_at: 1_800_000_000,
        })
    }

    async fn revoke_grant(
        &self,
        _call: gestalt::IdentityCallContext,
        _req: RevokeGrantRequest,
    ) -> gestalt::Result<RevokeGrantResponse> {
        Ok(RevokeGrantResponse {})
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

    async fn head_object(&self, request: HeadObjectRequest) -> gestalt::Result<HeadObjectResponse> {
        let reference = request
            .reference
            .ok_or_else(|| gestalt::Error::bad_request("missing ref"))?;
        let key = reference.key.clone();
        let objects = self.objects.lock().expect("lock objects");
        let body = objects
            .get(&key)
            .ok_or_else(|| gestalt::Error::not_found("object not found"))?;
        Ok(HeadObjectResponse {
            meta: Some(object_meta(
                reference,
                body.len() as i64,
                "application/octet-stream",
            )),
        })
    }

    async fn read_object(
        &self,
        request: ReadObjectRequest,
    ) -> gestalt::Result<gestalt::S3ReadObjectStream> {
        let reference = request
            .reference
            .ok_or_else(|| gestalt::Error::bad_request("missing ref"))?;
        let key = reference.key.clone();
        let objects = self.objects.lock().expect("lock objects");
        let body = objects
            .get(&key)
            .cloned()
            .ok_or_else(|| gestalt::Error::not_found("object not found"))?;
        drop(objects);

        let mut messages = vec![Ok(S3ReadObjectFrame::Meta(object_meta(
            reference,
            body.len() as i64,
            "application/octet-stream",
        )))];
        if !body.is_empty() {
            messages.push(Ok(S3ReadObjectFrame::Data(body)));
        }

        Ok(Box::pin(stream_iter(messages)))
    }

    async fn write_object(
        &self,
        mut stream: gestalt::S3WriteObjectStream,
    ) -> gestalt::Result<WriteObjectResponse> {
        let mut reference = None;
        let mut content_type = String::new();
        let mut body = Vec::new();

        while let Some(message) = stream.message().await? {
            match message {
                S3WriteObjectFrame::Open(open) => {
                    reference = open.reference;
                    content_type = open.content_type;
                }
                S3WriteObjectFrame::Data(chunk) => {
                    body.extend_from_slice(&chunk);
                }
                S3WriteObjectFrame::Empty => {}
            }
        }

        let reference =
            reference.ok_or_else(|| gestalt::Error::bad_request("missing open frame"))?;
        self.objects
            .lock()
            .expect("lock objects")
            .insert(reference.key.clone(), body.clone());

        Ok(WriteObjectResponse {
            meta: Some(object_meta(reference, body.len() as i64, &content_type)),
        })
    }

    async fn delete_object(&self, request: DeleteObjectRequest) -> gestalt::Result<()> {
        let reference = request
            .reference
            .ok_or_else(|| gestalt::Error::bad_request("missing ref"))?;
        self.objects
            .lock()
            .expect("lock objects")
            .remove(&reference.key);
        Ok(())
    }

    async fn list_objects(
        &self,
        request: ListObjectsRequest,
    ) -> gestalt::Result<ListObjectsResponse> {
        let objects = self.objects.lock().expect("lock objects");
        let mut metas = Vec::new();
        for (key, body) in objects.iter() {
            if !request.prefix.is_empty() && !key.starts_with(&request.prefix) {
                continue;
            }
            metas.push(object_meta(
                ObjectRef {
                    key: key.to_string(),
                    version_id: String::new(),
                },
                body.len() as i64,
                "application/octet-stream",
            ));
        }
        Ok(ListObjectsResponse {
            objects: metas,
            ..ListObjectsResponse::default()
        })
    }

    async fn copy_object(&self, request: CopyObjectRequest) -> gestalt::Result<CopyObjectResponse> {
        let source = request
            .source
            .ok_or_else(|| gestalt::Error::bad_request("missing source"))?;
        let destination = request
            .destination
            .ok_or_else(|| gestalt::Error::bad_request("missing destination"))?;
        let mut objects = self.objects.lock().expect("lock objects");
        let body = objects
            .get(&source.key)
            .cloned()
            .ok_or_else(|| gestalt::Error::not_found("object not found"))?;
        objects.insert(destination.key.clone(), body.clone());
        Ok(CopyObjectResponse {
            meta: Some(object_meta(
                destination,
                body.len() as i64,
                "application/octet-stream",
            )),
        })
    }

    async fn presign_object(
        &self,
        request: PresignObjectRequest,
    ) -> gestalt::Result<PresignObjectResponse> {
        let reference = request
            .reference
            .ok_or_else(|| gestalt::Error::bad_request("missing ref"))?;
        Ok(PresignObjectResponse {
            url: format!("https://example.invalid/{}", reference.key),
            method: request.method,
            expires_at: None,
            headers: request.headers,
        })
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
        gestalt::runtime_impl::serve_identity_provider(serve_provider)
            .await
            .expect("serve authentication provider");
    });

    helpers::wait_for_socket(&socket).await;

    let channel = connect_unix(&socket).await;
    let mut runtime = ProviderLifecycleClient::new(channel.clone());
    let mut auth = IdentityClient::new(channel);

    let metadata = runtime
        .get_provider_identity(())
        .await
        .expect("get provider identity")
        .into_inner();
    assert_eq!(
        ProviderKind::try_from(metadata.kind)
            .expect("valid provider kind")
            .as_str_name(),
        "PROVIDER_KIND_IDENTITY"
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

    let authorize = auth
        .authorize(ProtoAuthorizeRequest {
            response_type: "code".to_string(),
            client_id: "gestaltd".to_string(),
            redirect_uri: "https://host/callback".to_string(),
            scope: "openid".to_string(),
            state: "host-state".to_string(),
        })
        .await
        .expect("authorize")
        .into_inner();
    assert!(authorize.redirect_uri.contains("host-state"));

    let token = auth
        .token(ProtoTokenRequest {
            grant_type: "authorization_code".to_string(),
            code: "fixture-code".to_string(),
            redirect_uri: "https://host/callback".to_string(),
            client_id: "gestaltd".to_string(),
            state: "host-state".to_string(),
            scope: String::new(),
            subject_token: String::new(),
            subject_token_type: String::new(),
            expires_in: 0,
        })
        .await
        .expect("token")
        .into_inner();
    assert_eq!(token.access_token, "fixture-access-token");

    let introspected = auth
        .introspect(ProtoIntrospectRequest {
            token: "fixture-access-token".to_string(),
            token_type_hint: String::new(),
        })
        .await
        .expect("introspect")
        .into_inner();
    assert!(introspected.active);
    assert_eq!(introspected.subject, "user:fixture");

    let inactive = auth
        .introspect(ProtoIntrospectRequest {
            token: "missing-token".to_string(),
            token_type_hint: String::new(),
        })
        .await
        .expect("introspect inactive")
        .into_inner();
    assert!(!inactive.active);

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
        gestalt::runtime_impl::serve_s3_provider(serve_provider)
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
        key: "docs/example.txt".to_string(),
        version_id: String::new(),
    };
    s3.write_object(stream_iter(vec![
        ProtoWriteObjectRequest {
            msg: Some(generated::v1::write_object_request::Msg::Open(
                generated::v1::WriteObjectOpen {
                    r#ref: Some(reference.clone()),
                    ..generated::v1::WriteObjectOpen::default()
                },
            )),
        },
        ProtoWriteObjectRequest {
            msg: Some(generated::v1::write_object_request::Msg::Data(
                b"hello".to_vec(),
            )),
        },
    ]))
    .await
    .expect("write object");

    let head = s3
        .head_object(ProtoHeadObjectRequest {
            r#ref: Some(reference.clone()),
        })
        .await
        .expect("head object")
        .into_inner();
    assert_eq!(head.meta.expect("meta").size, 5);

    let listed = s3
        .list_objects(ProtoListObjectsRequest {
            prefix: "docs/".to_string(),
            ..ProtoListObjectsRequest::default()
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
        .read_object(ProtoReadObjectRequest {
            r#ref: Some(reference),
            ..ProtoReadObjectRequest::default()
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
        Some(generated::v1::read_object_chunk::Result::Meta(_))
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
                generated::v1::read_object_chunk::Result::Data(data) => Some(data),
                _ => None,
            })
            .expect("data payload"),
        b"hello".to_vec()
    );

    serve_task.abort();
    let _ = serve_task.await;
}

fn object_meta(reference: ObjectRef, size: i64, content_type: &str) -> ObjectMeta {
    ObjectMeta {
        reference,
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
