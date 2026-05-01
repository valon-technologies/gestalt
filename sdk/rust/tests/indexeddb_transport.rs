#[allow(dead_code)]
mod helpers;

use std::io::{BufRead, BufReader};
use std::net::TcpListener;
use std::process::{Command, Stdio};

use gestalt::indexeddb::{
    CursorDirection, ENV_INDEXEDDB_SOCKET, IndexSchema, IndexedDB, IndexedDBError, KeyRange,
    ObjectStoreSchema, Record, TransactionMode, TransactionOptions, indexeddb_socket_env,
    indexeddb_socket_token_env,
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

async fn start_harness(socket_name: &str) -> Harness {
    let repo_root = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .unwrap()
        .parent()
        .unwrap();

    let tmp = std::env::temp_dir();
    let binary = tmp.join("indexeddbtransportd");

    let build = Command::new("go")
        .arg("build")
        .arg("-o")
        .arg(&binary)
        .arg("./internal/testutil/testdata/cmd/indexeddbtransportd/")
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

    let env_guard = helpers::EnvGuard::set(ENV_INDEXEDDB_SOCKET, socket.as_os_str());
    Harness {
        child,
        _env_guard: env_guard,
    }
}

async fn start_tcp_harness(expect_token: Option<&str>) -> Harness {
    let repo_root = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .unwrap()
        .parent()
        .unwrap();

    let tmp = std::env::temp_dir();
    let binary = tmp.join("indexeddbtransportd-tcp");

    let build = Command::new("go")
        .arg("build")
        .arg("-o")
        .arg(&binary)
        .arg("./internal/testutil/testdata/cmd/indexeddbtransportd/")
        .current_dir(repo_root.join("gestaltd"))
        .output()
        .expect("go build");
    assert!(
        build.status.success(),
        "go build failed: {}",
        String::from_utf8_lossy(&build.stderr)
    );

    let listener = TcpListener::bind("127.0.0.1:0").expect("reserve tcp address");
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

    let env_guard = helpers::EnvGuard::set(ENV_INDEXEDDB_SOCKET, format!("tcp://{address}"));
    Harness {
        child,
        _env_guard: env_guard,
    }
}

fn make_record(pairs: &[(&str, serde_json::Value)]) -> Record {
    pairs
        .iter()
        .map(|(k, v)| (k.to_string(), v.clone()))
        .collect()
}

#[tokio::test]
async fn nested_json_round_trip() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-nested.sock").await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store("nested_store", ObjectStoreSchema { indexes: vec![] })
        .await
        .expect("create store");

    let mut store = db.object_store("nested_store");
    let record = make_record(&[
        ("id", serde_json::json!("rec1")),
        ("meta", serde_json::json!({"role": "admin", "level": 5})),
        ("tags", serde_json::json!(["alpha", "beta"])),
    ]);
    store.put(record).await.expect("put");

    let got = store.get("rec1").await.expect("get");
    assert!(
        got["meta"].is_object(),
        "meta should be object, got: {:?}",
        got["meta"]
    );
    assert_eq!(got["meta"]["role"], serde_json::json!("admin"));
    assert!(
        got["tags"].is_array(),
        "tags should be array, got: {:?}",
        got["tags"]
    );
    assert_eq!(got["tags"][0], serde_json::json!("alpha"));
}

#[tokio::test]
async fn named_unix_target_round_trip() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-named.sock").await;
    let _named_env = helpers::EnvGuard::set(
        indexeddb_socket_env("named"),
        format!(
            "unix://{}",
            std::env::var(ENV_INDEXEDDB_SOCKET).expect("default socket")
        ),
    );

    let mut db = IndexedDB::connect_named("named").await.expect("connect");
    db.create_object_store("named_socket_env", ObjectStoreSchema { indexes: vec![] })
        .await
        .expect("create store");

    let mut store = db.object_store("named_socket_env");
    store
        .put(make_record(&[
            ("id", serde_json::json!("row-1")),
            ("value", serde_json::json!("named")),
        ]))
        .await
        .expect("put");

    let got = store.get("row-1").await.expect("get");
    assert_eq!(got["value"], serde_json::json!("named"));
}

#[tokio::test]
async fn tcp_target_with_token_round_trip() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_tcp_harness(Some("relay-token-rust")).await;
    let _token_env = helpers::EnvGuard::set(indexeddb_socket_token_env(""), "relay-token-rust");

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store(
        "tcp_target_token_env",
        ObjectStoreSchema { indexes: vec![] },
    )
    .await
    .expect("create store");

    let mut store = db.object_store("tcp_target_token_env");
    store
        .put(make_record(&[
            ("id", serde_json::json!("row-1")),
            ("value", serde_json::json!("token")),
        ]))
        .await
        .expect("put");

    let got = store.get("row-1").await.expect("get");
    assert_eq!(got["value"], serde_json::json!("token"));
}

#[tokio::test]
async fn object_store_bulk_helpers() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-store-helpers.sock").await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store("store_helpers", ObjectStoreSchema { indexes: vec![] })
        .await
        .expect("create store");

    let mut store = db.object_store("store_helpers");
    for id in ["a", "b", "c", "d"] {
        store
            .put(make_record(&[
                ("id", serde_json::json!(id)),
                ("label", serde_json::json!(format!("label-{id}"))),
            ]))
            .await
            .expect("put");
    }

    let key = store.get_key("b").await.expect("get_key");
    assert_eq!(key, "b");

    let all = store.get_all(None).await.expect("get_all");
    assert_eq!(all.len(), 4);

    let ranged = store
        .get_all(Some(KeyRange {
            lower: Some(serde_json::json!("b")),
            upper: Some(serde_json::json!("c")),
            lower_open: false,
            upper_open: false,
        }))
        .await
        .expect("get_all with range");
    let ranged_ids: Vec<_> = ranged
        .iter()
        .map(|record| record["id"].as_str().expect("id").to_string())
        .collect();
    assert_eq!(ranged_ids, vec!["b", "c"]);

    let all_keys = store.get_all_keys(None).await.expect("get_all_keys");
    assert_eq!(all_keys, vec!["a", "b", "c", "d"]);

    let count = store.count(None).await.expect("count");
    assert_eq!(count, 4);

    let deleted = store
        .delete_range(KeyRange {
            lower: Some(serde_json::json!("b")),
            upper: Some(serde_json::json!("c")),
            lower_open: false,
            upper_open: false,
        })
        .await
        .expect("delete_range");
    assert_eq!(deleted, 2);
    assert_eq!(
        store.count(None).await.expect("count after delete_range"),
        2
    );
    assert_eq!(
        store.get_all_keys(None).await.expect("remaining keys"),
        vec!["a", "d"]
    );
}

#[tokio::test]
async fn transaction_readwrite_commits_and_reads_own_writes() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-transaction-commit.sock").await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store("transaction_commit", ObjectStoreSchema { indexes: vec![] })
        .await
        .expect("create store");

    let mut tx = db
        .transaction(
            &["transaction_commit"],
            TransactionMode::Readwrite,
            TransactionOptions::default(),
        )
        .await
        .expect("open transaction");
    {
        let mut tx_store = tx.object_store("transaction_commit");
        tx_store
            .put(make_record(&[
                ("id", serde_json::json!("row-1")),
                ("value", serde_json::json!("initial")),
            ]))
            .await
            .expect("transaction put");
        let got = tx_store.get("row-1").await.expect("transaction get");
        assert_eq!(got["value"], serde_json::json!("initial"));
        tx_store
            .put(make_record(&[
                ("id", serde_json::json!("row-1")),
                ("value", serde_json::json!("updated")),
            ]))
            .await
            .expect("transaction update");
        assert_eq!(tx_store.count(None).await.expect("transaction count"), 1);
    }
    tx.commit().await.expect("commit");

    let mut store = db.object_store("transaction_commit");
    let got = store.get("row-1").await.expect("get row");
    assert_eq!(got["value"], serde_json::json!("updated"));
}

#[tokio::test]
async fn transaction_abort_rolls_back() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-transaction-abort.sock").await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store("transaction_abort", ObjectStoreSchema { indexes: vec![] })
        .await
        .expect("create store");

    let mut tx = db
        .transaction(
            &["transaction_abort"],
            TransactionMode::Readwrite,
            TransactionOptions::default(),
        )
        .await
        .expect("open transaction");
    {
        let mut tx_store = tx.object_store("transaction_abort");
        tx_store
            .put(make_record(&[
                ("id", serde_json::json!("row-1")),
                ("value", serde_json::json!("pending")),
            ]))
            .await
            .expect("transaction put");
    }
    tx.abort("test rollback").await.expect("abort");

    let mut store = db.object_store("transaction_abort");
    assert!(matches!(
        store.get("row-1").await,
        Err(IndexedDBError::NotFound)
    ));
}

#[tokio::test]
async fn readonly_transaction_rejects_writes() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-transaction-readonly.sock").await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store(
        "transaction_readonly",
        ObjectStoreSchema { indexes: vec![] },
    )
    .await
    .expect("create store");
    let mut store = db.object_store("transaction_readonly");
    store
        .put(make_record(&[
            ("id", serde_json::json!("row-1")),
            ("value", serde_json::json!("kept")),
        ]))
        .await
        .expect("put");

    let mut tx = db
        .transaction(
            &["transaction_readonly"],
            TransactionMode::Readonly,
            TransactionOptions::default(),
        )
        .await
        .expect("open transaction");
    {
        let mut tx_store = tx.object_store("transaction_readonly");
        let got = tx_store.get("row-1").await.expect("transaction get");
        assert_eq!(got["value"], serde_json::json!("kept"));
        let err = tx_store
            .put(make_record(&[
                ("id", serde_json::json!("row-2")),
                ("value", serde_json::json!("blocked")),
            ]))
            .await
            .expect_err("readonly put should fail");
        assert!(matches!(err, IndexedDBError::Transaction(_)));
    }

    assert!(matches!(
        store.get("row-2").await,
        Err(IndexedDBError::NotFound)
    ));
}

#[tokio::test]
async fn transaction_operation_error_rolls_back() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-transaction-error.sock").await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store(
        "transaction_error_rollback",
        ObjectStoreSchema { indexes: vec![] },
    )
    .await
    .expect("create store");

    let mut tx = db
        .transaction(
            &["transaction_error_rollback"],
            TransactionMode::Readwrite,
            TransactionOptions::default(),
        )
        .await
        .expect("open transaction");
    {
        let mut tx_store = tx.object_store("transaction_error_rollback");
        tx_store
            .add(make_record(&[
                ("id", serde_json::json!("row-1")),
                ("value", serde_json::json!("pending")),
            ]))
            .await
            .expect("transaction add");
        let err = tx_store
            .add(make_record(&[
                ("id", serde_json::json!("row-1")),
                ("value", serde_json::json!("duplicate")),
            ]))
            .await
            .expect_err("duplicate add should fail");
        assert!(matches!(err, IndexedDBError::AlreadyExists));
    }

    let mut store = db.object_store("transaction_error_rollback");
    assert!(matches!(
        store.get("row-1").await,
        Err(IndexedDBError::NotFound)
    ));
}

#[tokio::test]
async fn transaction_index_operations_and_bulk_deletes_roll_back() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-transaction-index-bulk.sock").await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store(
        "transaction_index_bulk_rollback",
        ObjectStoreSchema {
            indexes: vec![IndexSchema {
                name: "by_status".to_string(),
                key_path: vec!["status".to_string()],
                unique: false,
            }],
        },
    )
    .await
    .expect("create store");
    let mut store = db.object_store("transaction_index_bulk_rollback");
    for (id, status) in [
        ("a", "active"),
        ("b", "active"),
        ("c", "inactive"),
        ("d", "active"),
    ] {
        store
            .add(make_record(&[
                ("id", serde_json::json!(id)),
                ("status", serde_json::json!(status)),
            ]))
            .await
            .expect("seed row");
    }

    let mut tx = db
        .transaction(
            &["transaction_index_bulk_rollback"],
            TransactionMode::Readwrite,
            TransactionOptions::default(),
        )
        .await
        .expect("open transaction");
    {
        let mut tx_store = tx.object_store("transaction_index_bulk_rollback");
        {
            let mut idx = tx_store.index("by_status");
            assert_eq!(
                idx.count(&[serde_json::json!("active")], None)
                    .await
                    .expect("index count"),
                3
            );
            assert_eq!(
                idx.get_all_keys(&[serde_json::json!("active")], None)
                    .await
                    .expect("index keys")
                    .len(),
                3
            );
        }
        assert_eq!(
            tx_store
                .delete_range(KeyRange {
                    lower: Some(serde_json::json!("b")),
                    upper: Some(serde_json::json!("c")),
                    lower_open: false,
                    upper_open: false,
                })
                .await
                .expect("delete range"),
            2
        );
        {
            let mut idx = tx_store.index("by_status");
            assert_eq!(
                idx.delete(&[serde_json::json!("active")])
                    .await
                    .expect("index delete"),
                2
            );
        }
        tx_store.clear().await.expect("clear");
        assert_eq!(tx_store.count(None).await.expect("count after clear"), 0);
    }
    tx.abort("rollback").await.expect("abort");

    assert_eq!(store.count(None).await.expect("public count"), 4);
    assert_eq!(
        store
            .index("by_status")
            .count(&[serde_json::json!("inactive")], None)
            .await
            .expect("public inactive count"),
        1
    );
}

#[tokio::test]
async fn tcp_target_round_trip() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_tcp_harness(None).await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store("tcp_target_env", ObjectStoreSchema { indexes: vec![] })
        .await
        .expect("create store");

    let mut store = db.object_store("tcp_target_env");
    store
        .put(make_record(&[
            ("id", serde_json::json!("row-1")),
            ("value", serde_json::json!("tcp")),
        ]))
        .await
        .expect("put");

    let got = store.get("row-1").await.expect("get");
    assert_eq!(got["value"], serde_json::json!("tcp"));
}

#[tokio::test]
async fn cursor_happy_path() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-cursor-happy.sock").await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store("cursor_happy", ObjectStoreSchema { indexes: vec![] })
        .await
        .expect("create store");

    let mut store = db.object_store("cursor_happy");
    for name in ["alice", "bob", "carol", "dave"] {
        store
            .put(make_record(&[
                ("id", serde_json::json!(name)),
                ("label", serde_json::json!(name)),
            ]))
            .await
            .expect("put");
    }

    let mut cursor = store
        .open_cursor(None, CursorDirection::Next)
        .await
        .expect("open cursor");

    let mut collected = vec![];
    while cursor.continue_next().await.expect("continue_next") {
        collected.push(cursor.primary_key().to_string());
    }

    assert_eq!(collected, vec!["alice", "bob", "carol", "dave"]);
}

#[tokio::test]
async fn empty_cursor() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-empty-cursor.sock").await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store("empty_cursor", ObjectStoreSchema { indexes: vec![] })
        .await
        .expect("create store");

    let mut store = db.object_store("empty_cursor");
    let mut cursor = store
        .open_cursor(None, CursorDirection::Next)
        .await
        .expect("open cursor");

    assert!(
        !cursor
            .continue_next()
            .await
            .expect("continue_next on empty")
    );
}

#[tokio::test]
async fn keys_only_cursor() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-keysonly.sock").await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store("keys_only_store", ObjectStoreSchema { indexes: vec![] })
        .await
        .expect("create store");

    let mut store = db.object_store("keys_only_store");
    store
        .put(make_record(&[("id", serde_json::json!("k1"))]))
        .await
        .expect("put");

    let mut cursor = store
        .open_key_cursor(None, CursorDirection::Next)
        .await
        .expect("open key cursor");

    assert!(cursor.continue_next().await.expect("continue_next"));
    match cursor.value() {
        Err(IndexedDBError::KeysOnly) => {}
        other => panic!("expected KeysOnly, got: {:?}", other),
    }
}

#[tokio::test]
async fn cursor_exhaustion() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-exhaust.sock").await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store("exhaust_store", ObjectStoreSchema { indexes: vec![] })
        .await
        .expect("create store");

    let mut store = db.object_store("exhaust_store");
    store
        .put(make_record(&[("id", serde_json::json!("only"))]))
        .await
        .expect("put");

    let mut cursor = store
        .open_cursor(None, CursorDirection::Next)
        .await
        .expect("open cursor");

    assert!(cursor.continue_next().await.expect("first"));
    assert_eq!(cursor.primary_key(), "only");
    assert!(!cursor.continue_next().await.expect("past last row"));
}

#[tokio::test]
async fn continue_to_key_beyond_end() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-ctk-end.sock").await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store("ctk_store", ObjectStoreSchema { indexes: vec![] })
        .await
        .expect("create store");

    let mut store = db.object_store("ctk_store");
    store
        .put(make_record(&[("id", serde_json::json!("aaa"))]))
        .await
        .expect("put");

    let mut cursor = store
        .open_cursor(None, CursorDirection::Next)
        .await
        .expect("open cursor");

    assert!(cursor.continue_next().await.expect("position"));
    let ok = cursor
        .continue_to_key(serde_json::json!("zzz"))
        .await
        .expect("continue_to_key beyond end");
    assert!(!ok);
}

#[tokio::test]
async fn advance_past_end() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-advance.sock").await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store("advance_store", ObjectStoreSchema { indexes: vec![] })
        .await
        .expect("create store");

    let mut store = db.object_store("advance_store");
    store
        .put(make_record(&[("id", serde_json::json!("one"))]))
        .await
        .expect("put");

    let mut cursor = store
        .open_cursor(None, CursorDirection::Next)
        .await
        .expect("open cursor");

    assert!(cursor.continue_next().await.expect("position"));
    let ok = cursor.advance(100).await.expect("advance past end");
    assert!(!ok);
}

#[tokio::test]
async fn advance_rejects_non_positive_counts() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-advance-invalid.sock").await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store(
        "advance_invalid_store",
        ObjectStoreSchema { indexes: vec![] },
    )
    .await
    .expect("create store");

    let mut store = db.object_store("advance_invalid_store");
    store
        .put(make_record(&[("id", serde_json::json!("one"))]))
        .await
        .expect("put");

    let mut cursor = store
        .open_cursor(None, CursorDirection::Next)
        .await
        .expect("open cursor");

    match cursor.advance(0).await {
        Err(IndexedDBError::Status(status)) => {
            assert_eq!(status.code(), tonic::Code::InvalidArgument);
        }
        other => panic!("expected InvalidArgument from advance(0), got: {:?}", other),
    }
}

#[tokio::test]
async fn post_exhaustion() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-post-exhaust.sock").await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store("post_exhaust_store", ObjectStoreSchema { indexes: vec![] })
        .await
        .expect("create store");

    let mut store = db.object_store("post_exhaust_store");
    store
        .put(make_record(&[("id", serde_json::json!("x"))]))
        .await
        .expect("put");

    let mut cursor = store
        .open_cursor(None, CursorDirection::Next)
        .await
        .expect("open cursor");

    assert!(cursor.continue_next().await.expect("first"));
    assert!(!cursor.continue_next().await.expect("exhaust"));
    assert!(
        !cursor
            .continue_next()
            .await
            .expect("post-exhaust continue_next")
    );
    match cursor.delete().await {
        Err(IndexedDBError::NotFound) => {}
        other => panic!(
            "expected NotFound from delete after exhaustion, got: {:?}",
            other
        ),
    }
}

#[tokio::test]
async fn index_cursor() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-index.sock").await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store(
        "index_store",
        ObjectStoreSchema {
            indexes: vec![IndexSchema {
                name: "by_status".to_string(),
                key_path: vec!["status".to_string()],
                unique: false,
            }],
        },
    )
    .await
    .expect("create store");

    let mut store = db.object_store("index_store");
    let records = vec![
        ("u1", "active"),
        ("u2", "inactive"),
        ("u3", "active"),
        ("u4", "active"),
        ("u5", "inactive"),
    ];
    for (id, status) in records {
        store
            .put(make_record(&[
                ("id", serde_json::json!(id)),
                ("status", serde_json::json!(status)),
            ]))
            .await
            .expect("put");
    }

    let mut idx = store.index("by_status");
    let all_active = idx
        .get_all(&[serde_json::json!("active")], None)
        .await
        .expect("get_all active");
    assert_eq!(all_active.len(), 3, "expected 3 active records");
    for rec in &all_active {
        assert_eq!(rec["status"], serde_json::json!("active"));
    }
}

#[tokio::test]
async fn index_query_helpers() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-index-helpers.sock").await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store(
        "index_helpers_store",
        ObjectStoreSchema {
            indexes: vec![IndexSchema {
                name: "by_num".to_string(),
                key_path: vec!["n".to_string()],
                unique: false,
            }],
        },
    )
    .await
    .expect("create store");

    let mut store = db.object_store("index_helpers_store");
    for (id, n) in [("a", 1), ("b", 2), ("c", 2), ("d", 4)] {
        store
            .put(make_record(&[
                ("id", serde_json::json!(id)),
                ("n", serde_json::json!(n)),
            ]))
            .await
            .expect("put");
    }

    let mut idx = store.index("by_num");
    let key = idx
        .get_key(&[serde_json::json!(2)])
        .await
        .expect("index get_key");
    assert_eq!(key, "b");

    let keys = idx
        .get_all_keys(&[], None)
        .await
        .expect("index get_all_keys");
    assert_eq!(keys, vec!["a", "b", "c", "d"]);

    let ranged_keys = idx
        .get_all_keys(
            &[],
            Some(KeyRange {
                lower: Some(serde_json::json!(2)),
                upper: Some(serde_json::json!(2)),
                lower_open: false,
                upper_open: false,
            }),
        )
        .await
        .expect("index get_all_keys with range");
    assert_eq!(ranged_keys, vec!["b", "c"]);

    let count = idx.count(&[], None).await.expect("index count");
    assert_eq!(count, 4);

    let exact_count = idx
        .count(&[serde_json::json!(2)], None)
        .await
        .expect("index count exact");
    assert_eq!(exact_count, 2);
}

#[tokio::test]
async fn index_continue_to_key_round_trip() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-index-seek.sock").await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store(
        "index_seek_store",
        ObjectStoreSchema {
            indexes: vec![IndexSchema {
                name: "by_num".to_string(),
                key_path: vec!["n".to_string()],
                unique: false,
            }],
        },
    )
    .await
    .expect("create store");

    let mut store = db.object_store("index_seek_store");
    for (id, n) in [("a", 1), ("b", 2), ("c", 3)] {
        store
            .put(make_record(&[
                ("id", serde_json::json!(id)),
                ("n", serde_json::json!(n)),
            ]))
            .await
            .expect("put");
    }

    let mut cursor = store
        .index("by_num")
        .open_cursor(&[], None, CursorDirection::Next)
        .await
        .expect("open cursor");

    assert!(cursor.continue_next().await.expect("first"));
    assert_eq!(cursor.key(), Some(serde_json::json!([1])));
    assert!(
        cursor
            .continue_to_key(cursor.key().expect("cursor key"))
            .await
            .expect("continue_to_key")
    );
    assert_eq!(cursor.primary_key(), "b");
}

#[tokio::test]
async fn error_mapping() {
    let _lock = helpers::env_lock().lock().await;
    let _harness = start_harness("idb-errors.sock").await;

    let mut db = IndexedDB::connect().await.expect("connect");
    db.create_object_store("error_store", ObjectStoreSchema { indexes: vec![] })
        .await
        .expect("create store");

    let mut store = db.object_store("error_store");

    match store.get("nonexistent").await {
        Err(IndexedDBError::NotFound) => {}
        other => panic!("expected NotFound for missing key, got: {:?}", other),
    }

    store
        .add(make_record(&[("id", serde_json::json!("dup"))]))
        .await
        .expect("first add");
    match store
        .add(make_record(&[("id", serde_json::json!("dup"))]))
        .await
    {
        Err(IndexedDBError::AlreadyExists) => {}
        other => panic!("expected AlreadyExists for duplicate add, got: {:?}", other),
    }
}
