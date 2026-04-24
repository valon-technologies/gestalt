use std::ffi::OsString;
use std::path::{Path, PathBuf};
use std::sync::OnceLock;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use prost_types::{Struct, Timestamp, Value};

pub fn env_lock() -> &'static tokio::sync::Mutex<()> {
    static ENV_LOCK: OnceLock<tokio::sync::Mutex<()>> = OnceLock::new();
    ENV_LOCK.get_or_init(|| tokio::sync::Mutex::new(()))
}

pub struct EnvGuard {
    key: String,
    previous: Option<OsString>,
}

impl EnvGuard {
    pub fn set(key: impl Into<String>, value: impl AsRef<std::ffi::OsStr>) -> Self {
        let key = key.into();
        let previous = std::env::var_os(&key);
        unsafe {
            std::env::set_var(&key, value);
        }
        Self { key, previous }
    }
}

impl Drop for EnvGuard {
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

pub fn struct_from_json(value: serde_json::Value) -> Struct {
    let object = value.as_object().expect("json object");
    Struct {
        fields: object
            .iter()
            .map(|(key, value)| (key.clone(), json_to_prost(value)))
            .collect(),
    }
}

pub fn json_to_prost(value: &serde_json::Value) -> Value {
    use prost_types::value::Kind;

    let kind = match value {
        serde_json::Value::Null => Kind::NullValue(0),
        serde_json::Value::Bool(boolean) => Kind::BoolValue(*boolean),
        serde_json::Value::Number(number) => Kind::NumberValue(number.as_f64().expect("f64")),
        serde_json::Value::String(string) => Kind::StringValue(string.clone()),
        serde_json::Value::Array(items) => Kind::ListValue(prost_types::ListValue {
            values: items.iter().map(json_to_prost).collect(),
        }),
        serde_json::Value::Object(object) => Kind::StructValue(Struct {
            fields: object
                .iter()
                .map(|(key, value)| (key.clone(), json_to_prost(value)))
                .collect(),
        }),
    };
    Value { kind: Some(kind) }
}

pub fn timestamp_now() -> Timestamp {
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("unix epoch");
    Timestamp {
        seconds: now.as_secs() as i64,
        nanos: now.subsec_nanos() as i32,
    }
}

pub async fn wait_for_socket(path: &Path) {
    let deadline = tokio::time::Instant::now() + Duration::from_secs(3);
    while tokio::time::Instant::now() < deadline {
        if path.exists() {
            return;
        }
        tokio::time::sleep(Duration::from_millis(25)).await;
    }
    panic!("socket {} was not created", path.display());
}

pub fn temp_socket(name: &str) -> PathBuf {
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("unix epoch")
        .as_nanos();
    std::env::temp_dir().join(format!("{nanos}-{name}"))
}
