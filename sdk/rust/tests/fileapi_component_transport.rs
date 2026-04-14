#[path = "support/fileapi_testutil.rs"]
mod fileapi_testutil;
#[allow(dead_code)]
mod helpers;

use std::path::Path;
use std::sync::Arc;

use gestalt::proto::v1::file_api_client::FileApiClient;
use gestalt::proto::v1::provider_lifecycle_client::ProviderLifecycleClient;
use gestalt::proto::v1::{ConfigureProviderRequest, CreateBlobRequest, ProviderKind};
use hyper_util::rt::tokio::TokioIo;
use tokio::net::UnixStream;
use tonic::transport::Endpoint;
use tower::service_fn;

#[tokio::test]
async fn serves_fileapi_provider_and_runtime_over_unix_socket() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-fileapi-runtime.sock");
    let _socket_guard = helpers::EnvGuard::set(gestalt::ENV_PROVIDER_SOCKET, socket.as_os_str());

    let provider = Arc::new(fileapi_testutil::InMemoryFileAPIProvider::default());
    let serve_provider = Arc::clone(&provider);
    let serve_task = tokio::spawn(async move {
        gestalt::runtime::serve_fileapi_provider(serve_provider)
            .await
            .expect("serve fileapi provider");
    });

    helpers::wait_for_socket(&socket).await;

    let channel = connect_unix(&socket).await;
    let mut runtime = ProviderLifecycleClient::new(channel.clone());
    let mut fileapi = FileApiClient::new(channel);

    let metadata = runtime
        .get_provider_identity(())
        .await
        .expect("get provider identity")
        .into_inner();
    assert_eq!(
        ProviderKind::try_from(metadata.kind)
            .expect("provider kind")
            .as_str_name(),
        "PROVIDER_KIND_FILEAPI"
    );
    assert_eq!(metadata.name, "fileapi-example");
    assert_eq!(metadata.display_name, "FileAPI Example");
    assert_eq!(metadata.warnings, vec!["ephemeral storage"]);

    let configured = runtime
        .configure_provider(ConfigureProviderRequest {
            name: "fileapi-runtime".to_string(),
            config: Some(helpers::struct_from_json(
                serde_json::json!({ "mode": "test" }),
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
    assert_eq!(provider.configured_name(), "fileapi-runtime");

    let health = runtime
        .health_check(())
        .await
        .expect("health check")
        .into_inner();
    assert!(health.ready);
    assert!(health.message.is_empty());

    let created = fileapi
        .create_blob(CreateBlobRequest {
            parts: vec![
                gestalt::proto::v1::BlobPart {
                    kind: Some(gestalt::proto::v1::blob_part::Kind::StringData(
                        "hello".to_string(),
                    )),
                },
                gestalt::proto::v1::BlobPart {
                    kind: Some(gestalt::proto::v1::blob_part::Kind::BytesData(
                        b" world".to_vec(),
                    )),
                },
            ],
            options: Some(gestalt::proto::v1::BlobOptions {
                mime_type: "text/plain".to_string(),
                endings: gestalt::proto::v1::LineEndings::Transparent as i32,
            }),
        })
        .await
        .expect("create blob")
        .into_inner();
    let object = created.object.expect("blob object");
    assert_eq!(object.r#type, "text/plain");
    assert_eq!(object.size, 11);

    let bytes = fileapi
        .read_bytes(gestalt::proto::v1::FileObjectRequest {
            id: object.id.clone(),
        })
        .await
        .expect("read bytes")
        .into_inner();
    assert_eq!(bytes.data, b"hello world");

    serve_task.abort();
    let _ = serve_task.await;
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
