mod support;

use std::os::unix::fs::PermissionsExt;

use gestalt::config::ConfigStore;
use gestalt::credentials::{CredentialStore, Credentials, normalize_origin};
use serde_json::Value;
use support::{EnvGuard, env_lock};
use tempfile::TempDir;

struct TestStore {
    store: CredentialStore,
    credentials_path: std::path::PathBuf,
    _dir: TempDir,
    _env: EnvGuard,
    _lock: std::sync::MutexGuard<'static, ()>,
}

impl TestStore {
    fn new() -> Self {
        let _lock = env_lock();
        let dir = TempDir::new().unwrap();
        let _env = EnvGuard::new(dir.path());
        std::fs::create_dir_all(gestalt::paths::gestalt_config_dir().unwrap()).unwrap();
        let store = CredentialStore::new().unwrap();
        let credentials_path = gestalt::paths::gestalt_config_dir()
            .unwrap()
            .join("credentials.json");
        Self {
            store,
            credentials_path,
            _dir: dir,
            _env,
            _lock,
        }
    }

    fn write_config_url(&self, url: &str) {
        ConfigStore::new().unwrap().set("url", url).unwrap();
    }

    fn write_raw(&self, json: &str) {
        std::fs::write(&self.credentials_path, json).unwrap();
    }

    fn read_raw_json(&self) -> Value {
        serde_json::from_str(&std::fs::read_to_string(&self.credentials_path).unwrap()).unwrap()
    }

    fn permissions(&self) -> u32 {
        std::fs::metadata(&self.credentials_path)
            .unwrap()
            .permissions()
            .mode()
            & 0o777
    }
}

fn sample_credentials(token: &str) -> Credentials {
    Credentials {
        api_token: token.to_string(),
        api_token_id: format!("{token}-id"),
    }
}

#[test]
fn round_trip_new_format() {
    let test = TestStore::new();
    let creds = sample_credentials("token-a");
    test.store
        .save_for_origin("https://origin-a.example.test", &creds)
        .unwrap();

    let loaded = test
        .store
        .load_for_origin("https://origin-a.example.test/")
        .unwrap()
        .unwrap();
    assert_eq!(loaded, creds);

    let json = test.read_raw_json();
    assert_eq!(
        json["servers"]["https://origin-a.example.test"]["api_token"],
        "token-a"
    );
}

#[test]
fn per_origin_isolation() {
    let test = TestStore::new();
    test.store
        .save_for_origin(
            "https://origin-a.example.test",
            &sample_credentials("prod-token"),
        )
        .unwrap();
    test.store
        .save_for_origin(
            "https://origin-b.example.test",
            &sample_credentials("dev-token"),
        )
        .unwrap();

    assert_eq!(
        test.store
            .load_for_origin("https://origin-a.example.test")
            .unwrap()
            .unwrap()
            .api_token,
        "prod-token"
    );
    assert_eq!(
        test.store
            .load_for_origin("https://origin-b.example.test")
            .unwrap()
            .unwrap()
            .api_token,
        "dev-token"
    );

    test.store
        .delete_for_origin("https://origin-a.example.test")
        .unwrap();
    assert!(
        test.store
            .load_for_origin("https://origin-a.example.test")
            .unwrap()
            .is_none()
    );
    assert_eq!(
        test.store
            .load_for_origin("https://origin-b.example.test")
            .unwrap()
            .unwrap()
            .api_token,
        "dev-token"
    );
}

#[test]
fn legacy_credentials_readable_without_migrating_on_load() {
    let test = TestStore::new();
    test.write_config_url("https://origin-a.example.test");
    test.write_raw(r#"{"api_token":"legacy-token","api_token_id":"legacy-id"}"#);

    let loaded = test
        .store
        .load_for_origin("https://origin-a.example.test")
        .unwrap()
        .unwrap();
    assert_eq!(loaded.api_token, "legacy-token");
    assert_eq!(
        std::fs::read_to_string(&test.credentials_path).unwrap(),
        r#"{"api_token":"legacy-token","api_token_id":"legacy-id"}"#
    );

    test.store
        .save_for_origin("https://origin-a.example.test", &loaded)
        .unwrap();

    let json = test.read_raw_json();
    assert!(json.get("api_token").is_none());
    assert_eq!(
        json["servers"]["https://origin-a.example.test"]["api_token"],
        "legacy-token"
    );
    assert_eq!(test.permissions(), 0o600);
}

#[test]
fn legacy_without_config_url_matches_nothing_and_preserves_file() {
    let test = TestStore::new();
    test.write_raw(r#"{"api_token":"legacy-token","api_token_id":"legacy-id"}"#);

    assert!(
        test.store
            .load_for_origin("https://origin-a.example.test")
            .unwrap()
            .is_none()
    );
    assert_eq!(
        std::fs::read_to_string(&test.credentials_path).unwrap(),
        r#"{"api_token":"legacy-token","api_token_id":"legacy-id"}"#
    );
}

#[test]
fn save_for_origin_rewrites_legacy_without_config_url() {
    let test = TestStore::new();
    test.write_raw(r#"{"api_token":"legacy-token","api_token_id":"legacy-id"}"#);

    test.store
        .save_for_origin(
            "https://origin-b.example.test",
            &sample_credentials("dev-token"),
        )
        .unwrap();

    let json = test.read_raw_json();
    assert!(json.get("api_token").is_none());
    assert_eq!(
        json["servers"]["https://origin-b.example.test"]["api_token"],
        "dev-token"
    );
    assert!(
        json["servers"]
            .get("https://origin-a.example.test")
            .is_none()
    );
}

#[test]
fn origin_lookup_is_case_insensitive_for_host() {
    let test = TestStore::new();
    test.store
        .save_for_origin(
            "https://Origin-A.example.test",
            &sample_credentials("token"),
        )
        .unwrap();

    let loaded = test
        .store
        .load_for_origin("https://origin-a.example.test")
        .unwrap()
        .unwrap();
    assert_eq!(loaded.api_token, "token");
}

#[test]
fn normalize_origin_omits_default_ports_and_brackets_ipv6() {
    assert_eq!(
        normalize_origin("HTTPS://Origin-A.Example.Test/"),
        "https://origin-a.example.test"
    );
    assert_eq!(
        normalize_origin("https://origin-a.example.test:443/"),
        "https://origin-a.example.test"
    );
    assert_eq!(normalize_origin("http://localhost:80"), "http://localhost");
    assert_eq!(normalize_origin("http://[::1]:8080"), "http://[::1]:8080");
}

#[test]
fn empty_server_token_is_ignored() {
    let test = TestStore::new();
    test.write_raw(
        r#"{"servers":{"https://origin-a.example.test":{"api_token":"","api_token_id":"id"}}}"#,
    );

    assert!(
        test.store
            .load_for_origin("https://origin-a.example.test")
            .unwrap()
            .is_none()
    );
}

#[test]
fn empty_legacy_credentials_are_rejected() {
    let test = TestStore::new();
    test.write_raw(r#"{"api_token":"","api_token_id":""}"#);

    let err = test
        .store
        .load_for_origin("https://origin-a.example.test")
        .unwrap_err();
    assert!(
        err.to_string()
            .contains("unrecognized credentials file format")
    );
}
