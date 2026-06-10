#[allow(dead_code)]
mod helpers;

use std::collections::BTreeMap;
use std::io::{BufRead, BufReader};
use std::net::TcpListener;
use std::process::{Command, Stdio};

use gestalt::ENV_HOST_SERVICE_SOCKET;
use gestalt::rpc_support::{GestaltError, gestalt_error_code};
use gestalt::s3::{
    CreateObjectAccessURLRequest, PresignObjectRequest, ReadObjectRequest, S3, S3ObjectAccess,
    S3ObjectMeta, S3ObjectRef, WriteObjectOpen, presign_method,
};
use hyper_util::rt::tokio::TokioIo;
use tokio::net::UnixStream;
use tonic::transport::Endpoint;
use tower::service_fn;

struct Harness {
    child: std::process::Child,
    _env_guard: helpers::EnvGuard,
}

impl Drop for Harness {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

async fn start_harness(socket_name: &str, env_name: &str) -> Harness {
    let repo_root = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .unwrap()
        .parent()
        .unwrap();

    let tmp = std::env::temp_dir();
    let binary = tmp.join("s3transportd");

    let build = Command::new("go")
        .arg("build")
        .arg("-o")
        .arg(&binary)
        .arg("./internal/testutil/testdata/cmd/s3transportd/")
        .current_dir(repo_root.join("gestaltd"))
        .output()
        .expect("go build");
    assert!(
        build.status.success(),
        "go build failed: {}",
        String::from_utf8_lossy(&build.stderr)
    );

    let socket = helpers::temp_socket(socket_name);
    let mut child = Command::new(&binary)
        .arg("--socket")
        .arg(&socket)
        .stdout(Stdio::piped())
        .stderr(Stdio::inherit())
        .spawn()
        .expect("spawn harness");

    let stdout = child.stdout.take().unwrap();
    let mut reader = BufReader::new(stdout);
    let mut line = String::new();
    reader.read_line(&mut line).expect("read READY");
    assert!(
        line.trim() == "READY",
        "expected READY, got: {:?}",
        line.trim()
    );

    let env_guard = helpers::EnvGuard::set(env_name.to_string(), socket.as_os_str());
    Harness {
        child,
        _env_guard: env_guard,
    }
}

async fn start_tcp_harness(expect_token: Option<&str>, env_name: &str) -> Harness {
    let repo_root = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .unwrap()
        .parent()
        .unwrap();

    let tmp = std::env::temp_dir();
    let binary = tmp.join("s3transportd");

    let build = Command::new("go")
        .arg("build")
        .arg("-o")
        .arg(&binary)
        .arg("./internal/testutil/testdata/cmd/s3transportd/")
        .current_dir(repo_root.join("gestaltd"))
        .output()
        .expect("go build");
    assert!(
        build.status.success(),
        "go build failed: {}",
        String::from_utf8_lossy(&build.stderr)
    );

    let listener = TcpListener::bind("127.0.0.1:0").expect("bind tcp listener");
    let address = listener.local_addr().expect("tcp local addr");
    drop(listener);

    let mut command = Command::new(&binary);
    command.arg("--tcp").arg(address.to_string());
    if let Some(token) = expect_token {
        command.arg("--expect-token").arg(token);
    }
    let mut child = command
        .stdout(Stdio::piped())
        .stderr(Stdio::inherit())
        .spawn()
        .expect("spawn tcp harness");

    let stdout = child.stdout.take().unwrap();
    let mut reader = BufReader::new(stdout);
    let mut line = String::new();
    reader.read_line(&mut line).expect("read READY");
    assert!(
        line.trim() == "READY",
        "expected READY, got: {:?}",
        line.trim()
    );

    let env_guard = helpers::EnvGuard::set(env_name.to_string(), format!("tcp://{address}"));
    Harness {
        child,
        _env_guard: env_guard,
    }
}

fn object_ref(key: &str) -> Option<S3ObjectRef> {
    Some(S3ObjectRef {
        key: key.to_string(),
        version_id: String::new(),
    })
}

fn open_frame(key: &str) -> WriteObjectOpen {
    WriteObjectOpen {
        r#ref: object_ref(key),
        ..WriteObjectOpen::default()
    }
}

async fn write_bytes(
    s3: &mut S3,
    open: WriteObjectOpen,
    body: Vec<u8>,
) -> Result<S3ObjectMeta, GestaltError> {
    let chunks = body
        .chunks(64 * 1024)
        .map(<[u8]>::to_vec)
        .collect::<Vec<_>>();
    write_chunks(s3, open, chunks).await
}

async fn write_chunks(
    s3: &mut S3,
    open: WriteObjectOpen,
    chunks: Vec<Vec<u8>>,
) -> Result<S3ObjectMeta, GestaltError> {
    let response = s3.write_object(open, tokio_stream::iter(chunks)).await?;
    Ok(response.meta.expect("write object response metadata"))
}

async fn read_bytes(
    s3: &mut S3,
    request: ReadObjectRequest,
) -> Result<(S3ObjectMeta, Vec<u8>), GestaltError> {
    let (meta, mut data) = s3.read_object(request).await?;
    let mut body = Vec::new();
    while let Some(chunk) = data.recv().await? {
        body.extend_from_slice(&chunk);
    }
    Ok((meta, body))
}

async fn stat(s3: &mut S3, key: &str) -> Result<S3ObjectMeta, GestaltError> {
    let response = s3.head_object(object_ref(key)).await?;
    Ok(response.meta.expect("head object response metadata"))
}

#[tokio::test]
async fn write_read_and_stat_round_trip() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("s3-round-trip.sock", ENV_HOST_SERVICE_SOCKET).await;

    let mut s3 = S3::connect().await.expect("connect");
    let meta = write_bytes(
        &mut s3,
        WriteObjectOpen {
            r#ref: object_ref("docs/hello.txt"),
            content_type: "text/plain".to_string(),
            metadata: BTreeMap::from([("owner".to_string(), "sdk".to_string())]),
            ..WriteObjectOpen::default()
        },
        b"hello".to_vec(),
    )
    .await
    .expect("write");

    assert_eq!(
        meta.r#ref.as_ref().expect("write ref").key,
        "docs/hello.txt"
    );
    assert_eq!(meta.size, 5);
    assert_eq!(meta.content_type, "text/plain");
    assert_eq!(meta.metadata.get("owner"), Some(&"sdk".to_string()));
    assert!(meta.last_modified.is_some());

    let fetched = stat(&mut s3, "docs/hello.txt").await.expect("stat");
    assert_eq!(fetched.etag, meta.etag);

    let (read_meta, body) = read_bytes(
        &mut s3,
        ReadObjectRequest {
            r#ref: object_ref("docs/hello.txt"),
            ..ReadObjectRequest::default()
        },
    )
    .await
    .expect("read");
    assert_eq!(read_meta.size, 5);
    assert_eq!(body, b"hello");
    assert_eq!(String::from_utf8(body).expect("utf-8 body"), "hello");
}

#[tokio::test]
async fn large_in_memory_write_bytes_round_trip() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("s3-large-write.sock", ENV_HOST_SERVICE_SOCKET).await;

    let mut s3 = S3::connect().await.expect("connect");
    let payload = vec![b'x'; 5 * 1024 * 1024];
    let meta = write_bytes(&mut s3, open_frame("docs/large.bin"), payload.clone())
        .await
        .expect("write large bytes");

    assert_eq!(meta.size, payload.len() as i64);
    assert_eq!(
        stat(&mut s3, "docs/large.bin").await.expect("stat").size,
        payload.len() as i64
    );
    let (_, body) = read_bytes(
        &mut s3,
        ReadObjectRequest {
            r#ref: object_ref("docs/large.bin"),
            ..ReadObjectRequest::default()
        },
    )
    .await
    .expect("read large bytes");
    assert_eq!(body, payload);
}

#[tokio::test]
async fn named_socket_json_and_preconditions() {
    let _lock = helpers::env_lock().lock().await;
    let env_name = ENV_HOST_SERVICE_SOCKET;
    let _harness = start_harness("s3-named.sock", env_name).await;

    let mut s3 = S3::connect_named("reports").await.expect("connect");
    let meta = write_bytes(
        &mut s3,
        WriteObjectOpen {
            r#ref: object_ref("reports/summary.json"),
            content_type: "application/json".to_string(),
            if_none_match: "*".to_string(),
            ..WriteObjectOpen::default()
        },
        serde_json::to_vec(&serde_json::json!({ "ok": true, "count": 2 })).expect("encode json"),
    )
    .await
    .expect("write json");
    assert_eq!(meta.content_type, "application/json");

    let (_, body) = read_bytes(
        &mut s3,
        ReadObjectRequest {
            r#ref: object_ref("reports/summary.json"),
            ..ReadObjectRequest::default()
        },
    )
    .await
    .expect("read json");
    let value = serde_json::from_slice::<serde_json::Value>(&body).expect("json decode");
    assert_eq!(value, serde_json::json!({ "ok": true, "count": 2 }));

    let error = write_bytes(
        &mut s3,
        WriteObjectOpen {
            r#ref: object_ref("reports/summary.json"),
            if_none_match: "*".to_string(),
            ..WriteObjectOpen::default()
        },
        b"again".to_vec(),
    )
    .await
    .expect_err("conditional rewrite should fail");
    assert_eq!(error.code, gestalt_error_code::FAILED_PRECONDITION);
}

#[tokio::test]
async fn tcp_target_round_trip() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_tcp_harness(None, ENV_HOST_SERVICE_SOCKET).await;

    let mut s3 = S3::connect().await.expect("connect");
    write_bytes(&mut s3, open_frame("docs/tcp.txt"), b"tcp".to_vec())
        .await
        .expect("write tcp target");

    let (_, body) = read_bytes(
        &mut s3,
        ReadObjectRequest {
            r#ref: object_ref("docs/tcp.txt"),
            ..ReadObjectRequest::default()
        },
    )
    .await
    .expect("read tcp target");
    assert_eq!(body, b"tcp");
}

#[tokio::test]
async fn tcp_target_with_token_round_trip() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_tcp_harness(Some("relay-token-rust"), ENV_HOST_SERVICE_SOCKET).await;
    let _token_env = helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_TOKEN, "relay-token-rust");

    let mut s3 = S3::connect().await.expect("connect");
    write_bytes(
        &mut s3,
        open_frame("docs/tcp-token.txt"),
        b"tcp-token".to_vec(),
    )
    .await
    .expect("write tcp token target");

    let (_, body) = read_bytes(
        &mut s3,
        ReadObjectRequest {
            r#ref: object_ref("docs/tcp-token.txt"),
            ..ReadObjectRequest::default()
        },
    )
    .await
    .expect("read tcp token target");
    assert_eq!(body, b"tcp-token");
}

#[tokio::test]
async fn named_tcp_target_uses_named_token_env() {
    let _lock = helpers::env_lock().lock().await;
    let env_name = ENV_HOST_SERVICE_SOCKET;
    let _harness = start_tcp_harness(Some("named-relay-token-rust"), env_name).await;
    let _token_env =
        helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_TOKEN, "named-relay-token-rust");

    let mut s3 = S3::connect_named("reports").await.expect("connect");
    write_bytes(
        &mut s3,
        open_frame("reports/named-tcp.txt"),
        b"named-tcp".to_vec(),
    )
    .await
    .expect("write named tcp target");

    let (_, body) = read_bytes(
        &mut s3,
        ReadObjectRequest {
            r#ref: object_ref("reports/named-tcp.txt"),
            ..ReadObjectRequest::default()
        },
    )
    .await
    .expect("read named tcp target");
    assert_eq!(body, b"named-tcp");
}

#[tokio::test]
async fn chunked_write_range_read_and_error_mapping() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("s3-range.sock", ENV_HOST_SERVICE_SOCKET).await;

    let mut s3 = S3::connect().await.expect("connect");
    write_chunks(
        &mut s3,
        WriteObjectOpen {
            r#ref: object_ref("docs/chunked.txt"),
            content_type: "text/plain".to_string(),
            ..WriteObjectOpen::default()
        },
        vec![b"he".to_vec(), b"llo".to_vec()],
    )
    .await
    .expect("write chunks");

    let (meta, body) = read_bytes(
        &mut s3,
        ReadObjectRequest {
            r#ref: object_ref("docs/chunked.txt"),
            range: Some(gestalt::s3::ByteRange {
                start: Some(1),
                end: Some(3),
            }),
            ..ReadObjectRequest::default()
        },
    )
    .await
    .expect("range read");
    assert_eq!(meta.size, 5);
    assert_eq!(String::from_utf8(body).expect("utf-8 body"), "ell");

    let error = read_bytes(
        &mut s3,
        ReadObjectRequest {
            r#ref: object_ref("docs/chunked.txt"),
            range: Some(gestalt::s3::ByteRange {
                start: Some(10),
                end: None,
            }),
            ..ReadObjectRequest::default()
        },
    )
    .await
    .expect_err("out-of-range read should fail");
    assert_eq!(error.code, gestalt_error_code::OUT_OF_RANGE);

    let error = stat(&mut s3, "missing.txt")
        .await
        .expect_err("missing object should fail");
    assert_eq!(error.code, gestalt_error_code::NOT_FOUND);
}

#[tokio::test]
async fn zero_byte_objects_round_trip() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("s3-empty.sock", ENV_HOST_SERVICE_SOCKET).await;

    let mut s3 = S3::connect().await.expect("connect");
    let meta = write_chunks(&mut s3, open_frame("docs/empty.bin"), Vec::new())
        .await
        .expect("write empty");

    assert_eq!(meta.size, 0);
    let (read_meta, body) = read_bytes(
        &mut s3,
        ReadObjectRequest {
            r#ref: object_ref("docs/empty.bin"),
            ..ReadObjectRequest::default()
        },
    )
    .await
    .expect("read empty");
    assert_eq!(read_meta.size, 0);
    assert_eq!(body, Vec::<u8>::new());
}

#[tokio::test]
async fn list_copy_delete_and_exists() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("s3-list.sock", ENV_HOST_SERVICE_SOCKET).await;

    let mut s3 = S3::connect().await.expect("connect");
    for (key, body) in [
        ("docs/a.txt", "A"),
        ("docs/b.txt", "B"),
        ("docs/folder/c.txt", "C"),
        ("docs/folder/d.txt", "D"),
    ] {
        write_bytes(&mut s3, open_frame(key), body.as_bytes().to_vec())
            .await
            .expect("write");
    }

    let listed = s3
        .list_objects(
            "docs/".to_string(),
            "/".to_string(),
            String::new(),
            String::new(),
            0,
        )
        .await
        .expect("list with delimiter");
    let listed_keys: Vec<_> = listed
        .objects
        .iter()
        .map(|meta| meta.r#ref.as_ref().expect("listed ref").key.clone())
        .collect();
    assert_eq!(listed_keys, vec!["docs/a.txt", "docs/b.txt"]);
    assert_eq!(listed.common_prefixes, vec!["docs/folder/"]);

    let page_one = s3
        .list_objects(
            "docs/".to_string(),
            String::new(),
            String::new(),
            String::new(),
            2,
        )
        .await
        .expect("list page one");
    assert!(page_one.has_more);
    assert_eq!(
        page_one
            .objects
            .iter()
            .map(|meta| meta.r#ref.as_ref().expect("page one ref").key.clone())
            .collect::<Vec<_>>(),
        vec!["docs/a.txt", "docs/b.txt"]
    );

    let page_two = s3
        .list_objects(
            "docs/".to_string(),
            String::new(),
            page_one.next_continuation_token,
            String::new(),
            2,
        )
        .await
        .expect("list page two");
    assert_eq!(
        page_two
            .objects
            .iter()
            .map(|meta| meta.r#ref.as_ref().expect("page two ref").key.clone())
            .collect::<Vec<_>>(),
        vec!["docs/folder/c.txt", "docs/folder/d.txt"]
    );

    let copied = s3
        .copy_object(
            String::new(),
            String::new(),
            object_ref("docs/a.txt"),
            object_ref("archive/a.txt"),
        )
        .await
        .expect("copy")
        .meta
        .expect("copy metadata");
    assert_eq!(
        copied.r#ref.as_ref().expect("copied ref").key,
        "archive/a.txt"
    );

    stat(&mut s3, "archive/a.txt")
        .await
        .expect("exists after copy");
    let (_, body) = read_bytes(
        &mut s3,
        ReadObjectRequest {
            r#ref: object_ref("archive/a.txt"),
            ..ReadObjectRequest::default()
        },
    )
    .await
    .expect("archived read");
    assert_eq!(body, b"A");

    s3.delete_object(object_ref("archive/a.txt"))
        .await
        .expect("delete");
    let error = stat(&mut s3, "archive/a.txt")
        .await
        .expect_err("deleted object should be missing");
    assert_eq!(error.code, gestalt_error_code::NOT_FOUND);
}

#[tokio::test]
async fn presign_round_trip() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("s3-presign.sock", ENV_HOST_SERVICE_SOCKET).await;

    let mut s3 = S3::connect().await.expect("connect");
    write_bytes(&mut s3, open_frame("docs/presign.txt"), b"presign".to_vec())
        .await
        .expect("seed object");

    let presigned = s3
        .presign_object_raw(PresignObjectRequest {
            r#ref: object_ref("docs/presign.txt"),
            method: presign_method::PRESIGN_METHOD_PUT,
            expires_seconds: 300,
            content_type: "text/plain".to_string(),
            headers: BTreeMap::from([("x-test".to_string(), "1".to_string())]),
            ..PresignObjectRequest::default()
        })
        .await
        .expect("presign");

    assert_eq!(presigned.method, presign_method::PRESIGN_METHOD_PUT);
    assert!(presigned.url.contains("docs%2Fpresign.txt"));
    assert!(presigned.url.contains("method=PUT"));
    assert_eq!(presigned.headers.get("x-test"), Some(&"1".to_string()));
    assert!(presigned.expires_at.is_some());

    let socket = std::env::var(ENV_HOST_SERVICE_SOCKET).expect("host service socket env");
    let channel = Endpoint::try_from("http://[::]:50051")
        .expect("endpoint")
        .connect_with_connector(service_fn(move |_| {
            let socket = socket.clone();
            async move { UnixStream::connect(socket).await.map(TokioIo::new) }
        }))
        .await
        .expect("connect object access channel");
    let mut object_access = S3ObjectAccess::new(channel);
    let access_url = object_access
        .create_object_access_url_raw(CreateObjectAccessURLRequest {
            r#ref: object_ref("docs/presign.txt"),
            method: presign_method::PRESIGN_METHOD_PUT,
            expires_seconds: 300,
            headers: BTreeMap::from([("Content-Length".to_string(), "5".to_string())]),
            ..CreateObjectAccessURLRequest::default()
        })
        .await
        .expect("create access URL");

    assert_eq!(access_url.method, presign_method::PRESIGN_METHOD_PUT);
    assert!(
        access_url
            .url
            .starts_with("https://gestalt.example.test/api/v1/s3/object-access/")
    );
    assert!(!access_url.url.contains("docs/presign.txt"));
    assert_eq!(
        access_url.headers.get("Content-Length"),
        Some(&"5".to_string())
    );
    assert!(access_url.expires_at.is_some());
}
