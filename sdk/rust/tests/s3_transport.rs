#[allow(dead_code)]
mod helpers;

use std::collections::BTreeMap;
use std::io::{BufRead, BufReader};
use std::net::TcpListener;
use std::process::{Command, Stdio};
use std::time::Duration;

use gestalt::s3::{
    ByteRange, ENV_S3_SOCKET, ENV_S3_SOCKET_TOKEN, ListOptions, PresignMethod, PresignOptions,
    ReadOptions, S3, S3Error, WriteOptions, s3_socket_env, s3_socket_token_env,
};

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
        .arg("./internal/testutil/cmd/s3transportd/")
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
        .arg("./internal/testutil/cmd/s3transportd/")
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

#[tokio::test]
async fn write_read_and_stat_round_trip() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("s3-round-trip.sock", ENV_S3_SOCKET).await;

    let s3 = S3::connect().await.expect("connect");
    let mut object = s3.object("bucket", "docs/hello.txt");
    let meta = object
        .write_bytes(
            b"hello",
            Some(WriteOptions {
                content_type: "text/plain".to_string(),
                metadata: BTreeMap::from([("owner".to_string(), "sdk".to_string())]),
                ..WriteOptions::default()
            }),
        )
        .await
        .expect("write");

    assert_eq!(meta.reference.bucket, "bucket");
    assert_eq!(meta.reference.key, "docs/hello.txt");
    assert_eq!(meta.size, 5);
    assert_eq!(meta.content_type, "text/plain");
    assert_eq!(meta.metadata.get("owner"), Some(&"sdk".to_string()));
    assert!(meta.last_modified.is_some());

    let stat = object.stat().await.expect("stat");
    assert_eq!(stat.etag, meta.etag);
    assert_eq!(object.bytes(None).await.expect("bytes"), b"hello");
    assert_eq!(object.text(None).await.expect("text"), "hello");
}

#[tokio::test]
async fn large_in_memory_write_bytes_round_trip() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("s3-large-write.sock", ENV_S3_SOCKET).await;

    let s3 = S3::connect().await.expect("connect");
    let mut object = s3.object("bucket", "docs/large.bin");
    let payload = vec![b'x'; 5 * 1024 * 1024];
    let meta = object
        .write_bytes(payload.as_slice(), None)
        .await
        .expect("write large bytes");

    assert_eq!(meta.size, payload.len() as i64);
    assert_eq!(
        object.stat().await.expect("stat").size,
        payload.len() as i64
    );
    assert_eq!(object.bytes(None).await.expect("bytes"), payload);
}

#[tokio::test]
async fn named_socket_json_and_preconditions() {
    let _lock = helpers::env_lock().lock().await;
    let env_name = s3_socket_env("reports");
    let _harness = start_harness("s3-named.sock", &env_name).await;

    let s3 = S3::connect_named("reports").await.expect("connect");
    let mut object = s3.object("bucket", "reports/summary.json");
    let meta = object
        .write_json(
            &serde_json::json!({ "ok": true, "count": 2 }),
            Some(WriteOptions {
                if_none_match: "*".to_string(),
                ..WriteOptions::default()
            }),
        )
        .await
        .expect("write json");
    assert_eq!(meta.content_type, "application/json");

    let value = object
        .json::<serde_json::Value>(None)
        .await
        .expect("json decode");
    assert_eq!(value, serde_json::json!({ "ok": true, "count": 2 }));

    match object
        .write_bytes(
            b"again",
            Some(WriteOptions {
                if_none_match: "*".to_string(),
                ..WriteOptions::default()
            }),
        )
        .await
    {
        Err(S3Error::PreconditionFailed) => {}
        other => panic!("expected PreconditionFailed, got: {other:?}"),
    }
}

#[tokio::test]
async fn tcp_target_round_trip() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_tcp_harness(None, ENV_S3_SOCKET).await;

    let s3 = S3::connect().await.expect("connect");
    let mut object = s3.object("bucket", "docs/tcp.txt");
    object
        .write_string("tcp", None)
        .await
        .expect("write tcp target");

    assert_eq!(object.text(None).await.expect("read tcp target"), "tcp");
}

#[tokio::test]
async fn tcp_target_with_token_round_trip() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_tcp_harness(Some("relay-token-rust"), ENV_S3_SOCKET).await;
    let _token_env = helpers::EnvGuard::set(ENV_S3_SOCKET_TOKEN, "relay-token-rust");

    let s3 = S3::connect().await.expect("connect");
    let mut object = s3.object("bucket", "docs/tcp-token.txt");
    object
        .write_string("tcp-token", None)
        .await
        .expect("write tcp token target");

    assert_eq!(
        object.text(None).await.expect("read tcp token target"),
        "tcp-token"
    );
}

#[tokio::test]
async fn named_tcp_target_uses_named_token_env() {
    let _lock = helpers::env_lock().lock().await;
    let env_name = s3_socket_env("reports");
    let _harness = start_tcp_harness(Some("named-relay-token-rust"), &env_name).await;
    let _token_env =
        helpers::EnvGuard::set(s3_socket_token_env("reports"), "named-relay-token-rust");

    let s3 = S3::connect_named("reports").await.expect("connect");
    let mut object = s3.object("bucket", "reports/named-tcp.txt");
    object
        .write_string("named-tcp", None)
        .await
        .expect("write named tcp target");

    assert_eq!(
        object.text(None).await.expect("read named tcp target"),
        "named-tcp"
    );
}

#[tokio::test]
async fn chunked_write_range_read_and_error_mapping() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("s3-range.sock", ENV_S3_SOCKET).await;

    let s3 = S3::connect().await.expect("connect");
    let mut object = s3.object("bucket", "docs/chunked.txt");
    object
        .write_chunks(
            vec![b"he".to_vec(), b"llo".to_vec()],
            Some(WriteOptions {
                content_type: "text/plain".to_string(),
                ..WriteOptions::default()
            }),
        )
        .await
        .expect("write chunks");

    let reader = object
        .stream(Some(ReadOptions {
            range: Some(ByteRange {
                start: Some(1),
                end: Some(3),
            }),
            ..ReadOptions::default()
        }))
        .await
        .expect("stream");
    assert_eq!(reader.meta().size, 5);
    assert_eq!(reader.text().await.expect("range text"), "ell");

    match object
        .stream(Some(ReadOptions {
            range: Some(ByteRange {
                start: Some(10),
                end: None,
            }),
            ..ReadOptions::default()
        }))
        .await
    {
        Err(S3Error::InvalidRange) => {}
        Ok(_) => panic!("expected InvalidRange, got success"),
        Err(error) => panic!("expected InvalidRange, got {error}"),
    }

    let mut missing = s3.object("bucket", "missing.txt");
    match missing.stat().await {
        Err(S3Error::NotFound) => {}
        other => panic!("expected NotFound, got: {other:?}"),
    }
}

#[tokio::test]
async fn zero_byte_objects_round_trip() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("s3-empty.sock", ENV_S3_SOCKET).await;

    let s3 = S3::connect().await.expect("connect");
    let mut object = s3.object("bucket", "docs/empty.bin");
    let meta = object
        .write_bytes(Vec::<u8>::new(), None)
        .await
        .expect("write empty");

    assert_eq!(meta.size, 0);
    assert_eq!(
        object.bytes(None).await.expect("empty bytes"),
        Vec::<u8>::new()
    );
    assert_eq!(object.text(None).await.expect("empty text"), "");
}

#[tokio::test]
async fn list_copy_delete_and_exists() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("s3-list.sock", ENV_S3_SOCKET).await;

    let mut s3 = S3::connect().await.expect("connect");
    for (key, body) in [
        ("docs/a.txt", "A"),
        ("docs/b.txt", "B"),
        ("docs/folder/c.txt", "C"),
        ("docs/folder/d.txt", "D"),
    ] {
        let mut object = s3.object("bucket", key);
        object.write_string(body, None).await.expect("write");
    }

    let listed = s3
        .list_objects(ListOptions {
            bucket: "bucket".to_string(),
            prefix: "docs/".to_string(),
            delimiter: "/".to_string(),
            ..ListOptions::default()
        })
        .await
        .expect("list with delimiter");
    let listed_keys: Vec<_> = listed
        .objects
        .iter()
        .map(|meta| meta.reference.key.clone())
        .collect();
    assert_eq!(listed_keys, vec!["docs/a.txt", "docs/b.txt"]);
    assert_eq!(listed.common_prefixes, vec!["docs/folder/"]);

    let page_one = s3
        .list_objects(ListOptions {
            bucket: "bucket".to_string(),
            prefix: "docs/".to_string(),
            max_keys: 2,
            ..ListOptions::default()
        })
        .await
        .expect("list page one");
    assert!(page_one.has_more);
    assert_eq!(
        page_one
            .objects
            .iter()
            .map(|meta| meta.reference.key.clone())
            .collect::<Vec<_>>(),
        vec!["docs/a.txt", "docs/b.txt"]
    );

    let page_two = s3
        .list_objects(ListOptions {
            bucket: "bucket".to_string(),
            prefix: "docs/".to_string(),
            continuation_token: page_one.next_continuation_token,
            max_keys: 2,
            ..ListOptions::default()
        })
        .await
        .expect("list page two");
    assert_eq!(
        page_two
            .objects
            .iter()
            .map(|meta| meta.reference.key.clone())
            .collect::<Vec<_>>(),
        vec!["docs/folder/c.txt", "docs/folder/d.txt"]
    );

    let copied = s3
        .copy_object(
            gestalt::s3::ObjectRef {
                bucket: "bucket".to_string(),
                key: "docs/a.txt".to_string(),
                version_id: String::new(),
            },
            gestalt::s3::ObjectRef {
                bucket: "bucket".to_string(),
                key: "archive/a.txt".to_string(),
                version_id: String::new(),
            },
            None,
        )
        .await
        .expect("copy");
    assert_eq!(copied.reference.key, "archive/a.txt");

    let mut archived = s3.object("bucket", "archive/a.txt");
    assert!(archived.exists().await.expect("exists after copy"));
    assert_eq!(archived.text(None).await.expect("archived text"), "A");
    archived.delete().await.expect("delete");
    assert!(!archived.exists().await.expect("exists after delete"));
}

#[tokio::test]
async fn presign_round_trip() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("s3-presign.sock", ENV_S3_SOCKET).await;

    let s3 = S3::connect().await.expect("connect");
    let mut object = s3.object("bucket", "docs/presign.txt");
    object
        .write_string("presign", None)
        .await
        .expect("seed object");

    let presigned = object
        .presign(Some(PresignOptions {
            method: PresignMethod::Put,
            expires: Duration::from_secs(300),
            content_type: "text/plain".to_string(),
            headers: BTreeMap::from([("x-test".to_string(), "1".to_string())]),
            ..PresignOptions::default()
        }))
        .await
        .expect("presign");

    assert_eq!(presigned.method, PresignMethod::Put);
    assert!(presigned.url.contains("bucket/docs%2Fpresign.txt"));
    assert!(presigned.url.contains("method=PUT"));
    assert_eq!(presigned.headers.get("x-test"), Some(&"1".to_string()));
    assert!(presigned.expires_at.is_some());
}
