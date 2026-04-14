#[path = "support/fileapi_testutil.rs"]
mod fileapi_testutil;
#[allow(dead_code)]
mod helpers;

use std::ffi::{OsStr, OsString};
use std::sync::Arc;

use gestalt::{
    BlobPart, BlobPropertyBag, ENV_FILEAPI_SOCKET, ENV_PROVIDER_SOCKET, FileAPI, FileAPIError,
    FileObjectHandle, FilePropertyBag, fileapi_socket_env,
};

struct DynamicEnvGuard {
    key: String,
    previous: Option<OsString>,
}

impl DynamicEnvGuard {
    fn set(key: impl Into<String>, value: impl AsRef<OsStr>) -> Self {
        let key = key.into();
        let previous = std::env::var_os(&key);
        unsafe {
            std::env::set_var(&key, value);
        }
        Self { key, previous }
    }
}

impl Drop for DynamicEnvGuard {
    fn drop(&mut self) {
        unsafe {
            if let Some(previous) = &self.previous {
                std::env::set_var(&self.key, previous);
            } else {
                std::env::remove_var(&self.key);
            }
        }
    }
}

#[tokio::test]
async fn named_socket_env_round_trip() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-fileapi-named.sock");
    let _provider_socket = helpers::EnvGuard::set(ENV_PROVIDER_SOCKET, socket.as_os_str());
    let _client_socket = helpers::EnvGuard::set(ENV_FILEAPI_SOCKET, socket.as_os_str());
    let _named_socket = DynamicEnvGuard::set(fileapi_socket_env("named"), socket.as_os_str());

    let provider = Arc::new(fileapi_testutil::InMemoryFileAPIProvider::default());
    let serve_provider = Arc::clone(&provider);
    let serve_task = tokio::spawn(async move {
        gestalt::runtime::serve_fileapi_provider(serve_provider)
            .await
            .expect("serve fileapi provider");
    });

    helpers::wait_for_socket(&socket).await;

    let mut client = FileAPI::connect_named("named")
        .await
        .expect("connect named");
    let blob = client
        .create_blob(
            vec![BlobPart::from("named")],
            BlobPropertyBag {
                mime_type: "text/plain".to_string(),
                ..Default::default()
            },
        )
        .await
        .expect("create blob");
    assert_eq!(blob.text().await.expect("blob text"), "named");

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn blob_helpers_round_trip_over_transport() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-fileapi-blob.sock");
    let _provider_socket = helpers::EnvGuard::set(ENV_PROVIDER_SOCKET, socket.as_os_str());
    let _client_socket = helpers::EnvGuard::set(ENV_FILEAPI_SOCKET, socket.as_os_str());

    let provider = Arc::new(fileapi_testutil::InMemoryFileAPIProvider::default());
    let serve_provider = Arc::clone(&provider);
    let serve_task = tokio::spawn(async move {
        gestalt::runtime::serve_fileapi_provider(serve_provider)
            .await
            .expect("serve fileapi provider");
    });

    helpers::wait_for_socket(&socket).await;

    let mut client = FileAPI::connect().await.expect("connect");
    let blob = client
        .create_blob(
            vec![BlobPart::from("hello"), BlobPart::from(&b" world"[..])],
            BlobPropertyBag {
                mime_type: "text/plain".to_string(),
                ..Default::default()
            },
        )
        .await
        .expect("create blob");

    assert_eq!(blob.size, 11);
    assert_eq!(blob.mime_type, "text/plain");
    assert_eq!(blob.bytes().await.expect("blob bytes"), b"hello world");
    assert_eq!(blob.text().await.expect("blob text"), "hello world");
    assert_eq!(
        blob.data_url().await.expect("blob data url"),
        "data:text/plain;base64,aGVsbG8gd29ybGQ="
    );

    let stat = client.stat(&blob.id).await.expect("stat blob");
    match stat {
        FileObjectHandle::Blob(stat_blob) => assert_eq!(stat_blob.id, blob.id),
        FileObjectHandle::File(_) => panic!("expected blob handle"),
    }

    let stream_data = read_stream(blob.open_read_stream().await.expect("open stream"))
        .await
        .expect("read stream");
    assert_eq!(stream_data, b"hello world");

    let url = blob.create_object_url().await.expect("create object url");
    let resolved = client
        .resolve_object_url(&url)
        .await
        .expect("resolve object url");
    match resolved {
        FileObjectHandle::Blob(resolved_blob) => assert_eq!(resolved_blob.id, blob.id),
        FileObjectHandle::File(_) => panic!("expected blob from object URL"),
    }
    client
        .revoke_object_url(&url)
        .await
        .expect("revoke object url");
    match client.resolve_object_url(&url).await {
        Err(FileAPIError::NotFound) => {}
        Ok(_) => panic!("expected NotFound after revoke"),
        Err(error) => panic!("expected NotFound after revoke, got {error}"),
    }

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn slice_uses_file_api_byte_ranges() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-fileapi-slice.sock");
    let _provider_socket = helpers::EnvGuard::set(ENV_PROVIDER_SOCKET, socket.as_os_str());
    let _client_socket = helpers::EnvGuard::set(ENV_FILEAPI_SOCKET, socket.as_os_str());

    let provider = Arc::new(fileapi_testutil::InMemoryFileAPIProvider::default());
    let serve_provider = Arc::clone(&provider);
    let serve_task = tokio::spawn(async move {
        gestalt::runtime::serve_fileapi_provider(serve_provider)
            .await
            .expect("serve fileapi provider");
    });

    helpers::wait_for_socket(&socket).await;

    let mut client = FileAPI::connect().await.expect("connect");
    let blob = client
        .create_blob(
            vec![BlobPart::from(&b"abcdef"[..])],
            BlobPropertyBag {
                mime_type: "application/octet-stream".to_string(),
                ..Default::default()
            },
        )
        .await
        .expect("create blob");

    assert_eq!(
        blob.slice(Some(1), Some(4), "")
            .await
            .expect("slice")
            .bytes()
            .await
            .expect("slice bytes"),
        b"bcd"
    );
    assert_eq!(
        blob.slice(Some(-2), None, "")
            .await
            .expect("slice")
            .bytes()
            .await
            .expect("slice bytes"),
        b"ef"
    );
    assert_eq!(
        blob.slice(Some(0), Some(-1), "")
            .await
            .expect("slice")
            .bytes()
            .await
            .expect("slice bytes"),
        b"abcde"
    );

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn file_round_trip_over_transport() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-fileapi-file.sock");
    let _provider_socket = helpers::EnvGuard::set(ENV_PROVIDER_SOCKET, socket.as_os_str());
    let _client_socket = helpers::EnvGuard::set(ENV_FILEAPI_SOCKET, socket.as_os_str());

    let provider = Arc::new(fileapi_testutil::InMemoryFileAPIProvider::default());
    let serve_provider = Arc::clone(&provider);
    let serve_task = tokio::spawn(async move {
        gestalt::runtime::serve_fileapi_provider(serve_provider)
            .await
            .expect("serve fileapi provider");
    });

    helpers::wait_for_socket(&socket).await;

    let mut client = FileAPI::connect().await.expect("connect");
    let file = client
        .create_file(
            vec![BlobPart::from(&b"payload"[..])],
            "report.txt",
            FilePropertyBag {
                mime_type: "text/plain".to_string(),
                last_modified: 1_714_566_600_000,
                ..Default::default()
            },
        )
        .await
        .expect("create file");

    assert_eq!(file.name, "report.txt");
    assert_eq!(file.mime_type, "text/plain");
    assert_eq!(file.last_modified, 1_714_566_600_000);
    assert_eq!(file.text().await.expect("file text"), "payload");

    let stat = client.stat(&file.id).await.expect("stat file");
    match stat {
        FileObjectHandle::File(stat_file) => {
            assert_eq!(stat_file.name, "report.txt");
            assert_eq!(stat_file.last_modified, 1_714_566_600_000);
        }
        FileObjectHandle::Blob(_) => panic!("expected file handle"),
    }

    serve_task.abort();
    let _ = serve_task.await;
}

async fn read_stream(
    mut stream: tonic::Streaming<gestalt::proto::v1::ReadChunk>,
) -> Result<Vec<u8>, tonic::Status> {
    let mut data = Vec::new();
    while let Some(chunk) = stream.message().await? {
        data.extend(chunk.data);
    }
    Ok(data)
}
