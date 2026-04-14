#[allow(dead_code)]
mod helpers;

use std::collections::BTreeMap;
use std::path::Path;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use gestalt::proto::v1::provider_lifecycle_client::ProviderLifecycleClient;
use gestalt::proto::v1::{ConfigureProviderRequest, ProviderKind};
use gestalt::{CacheEntry, CacheProvider, CacheSetOptions, RuntimeMetadata};
use hyper_util::rt::tokio::TokioIo;
use tokio::net::UnixStream;
use tonic::transport::Endpoint;
use tower::service_fn;

#[derive(Default)]
struct TestCacheProvider {
    configured_name: Mutex<String>,
    namespace: Mutex<String>,
    entries: Mutex<BTreeMap<String, Vec<u8>>>,
    ttl_by_key: Mutex<BTreeMap<String, Duration>>,
}

#[gestalt::async_trait]
impl CacheProvider for TestCacheProvider {
    async fn configure(
        &self,
        name: &str,
        config: serde_json::Map<String, serde_json::Value>,
    ) -> gestalt::Result<()> {
        *self.configured_name.lock().expect("configured_name lock") = name.to_string();
        *self.namespace.lock().expect("namespace lock") = config
            .get("namespace")
            .and_then(serde_json::Value::as_str)
            .unwrap_or_default()
            .to_string();
        Ok(())
    }

    fn metadata(&self) -> Option<RuntimeMetadata> {
        Some(RuntimeMetadata {
            name: "cache-example".to_string(),
            display_name: "Cache Example".to_string(),
            description: "Test cache provider".to_string(),
            version: "0.1.0".to_string(),
        })
    }

    fn warnings(&self) -> Vec<String> {
        vec!["set cache namespace".to_string()]
    }

    async fn get(&self, key: &str) -> gestalt::Result<Option<Vec<u8>>> {
        Ok(self
            .entries
            .lock()
            .expect("entries lock")
            .get(&self.namespaced(key))
            .cloned())
    }

    async fn set(&self, key: &str, value: &[u8], options: CacheSetOptions) -> gestalt::Result<()> {
        let namespaced = self.namespaced(key);
        self.entries
            .lock()
            .expect("entries lock")
            .insert(namespaced.clone(), value.to_vec());
        if let Some(ttl) = options.ttl {
            self.ttl_by_key
                .lock()
                .expect("ttl_by_key lock")
                .insert(namespaced, ttl);
        }
        Ok(())
    }

    async fn delete(&self, key: &str) -> gestalt::Result<bool> {
        Ok(self
            .entries
            .lock()
            .expect("entries lock")
            .remove(&self.namespaced(key))
            .is_some())
    }

    async fn touch(&self, key: &str, ttl: Duration) -> gestalt::Result<bool> {
        let namespaced = self.namespaced(key);
        let exists = self
            .entries
            .lock()
            .expect("entries lock")
            .contains_key(&namespaced);
        if exists {
            self.ttl_by_key
                .lock()
                .expect("ttl_by_key lock")
                .insert(namespaced, ttl);
        }
        Ok(exists)
    }
}

impl TestCacheProvider {
    fn namespaced(&self, key: &str) -> String {
        let namespace = self.namespace.lock().expect("namespace lock").clone();
        if namespace.is_empty() {
            return key.to_string();
        }
        format!("{namespace}:{key}")
    }
}

#[tokio::test]
async fn cache_runtime_and_client_round_trip_over_named_socket() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-cache.sock");
    let _provider_socket = helpers::EnvGuard::set(gestalt::ENV_PROVIDER_SOCKET, socket.as_os_str());
    let cache_env = gestalt::cache_socket_env("shared-cache");
    let _cache_socket = helpers::EnvGuard::set(cache_env, socket.as_os_str());

    let provider = Arc::new(TestCacheProvider::default());
    let serve_provider = Arc::clone(&provider);
    let serve_task = tokio::spawn(async move {
        gestalt::runtime::serve_cache_provider(serve_provider)
            .await
            .expect("serve cache provider");
    });

    helpers::wait_for_socket(&socket).await;

    let channel = connect_unix(&socket).await;
    let mut runtime = ProviderLifecycleClient::new(channel);
    let metadata = runtime
        .get_provider_identity(())
        .await
        .expect("get provider identity")
        .into_inner();
    assert_eq!(
        ProviderKind::try_from(metadata.kind)
            .expect("valid provider kind")
            .as_str_name(),
        "PROVIDER_KIND_CACHE"
    );
    assert_eq!(metadata.name, "cache-example");
    assert_eq!(metadata.warnings, vec!["set cache namespace"]);

    runtime
        .configure_provider(ConfigureProviderRequest {
            name: "cache-runtime".to_string(),
            config: Some(helpers::struct_from_json(
                serde_json::json!({ "namespace": "tenant-a" }),
            )),
            protocol_version: gestalt::CURRENT_PROTOCOL_VERSION,
        })
        .await
        .expect("configure provider");

    let mut cache = gestalt::Cache::connect_named("shared-cache")
        .await
        .expect("connect cache");
    cache
        .set(
            "alpha",
            b"one",
            CacheSetOptions {
                ttl: Some(Duration::from_secs(60)),
            },
        )
        .await
        .expect("set alpha");
    cache
        .set_many(
            &[
                CacheEntry {
                    key: "beta".to_string(),
                    value: b"two".to_vec(),
                },
                CacheEntry {
                    key: "gamma".to_string(),
                    value: b"three".to_vec(),
                },
            ],
            CacheSetOptions {
                ttl: Some(Duration::from_secs(120)),
            },
        )
        .await
        .expect("set many");

    assert_eq!(
        cache.get("alpha").await.expect("get alpha"),
        Some(b"one".to_vec())
    );

    let values = cache
        .get_many(&["alpha", "beta", "missing"])
        .await
        .expect("get many");
    assert_eq!(values.get("alpha").map(Vec::as_slice), Some(&b"one"[..]));
    assert_eq!(values.get("beta").map(Vec::as_slice), Some(&b"two"[..]));
    assert!(!values.contains_key("missing"));

    assert!(
        cache
            .touch("alpha", Duration::from_secs(30))
            .await
            .expect("touch alpha")
    );
    assert!(cache.delete("alpha").await.expect("delete alpha"));
    assert_eq!(
        cache
            .delete_many(&["beta", "missing", "beta"])
            .await
            .expect("delete many"),
        1
    );

    assert_eq!(
        *provider
            .configured_name
            .lock()
            .expect("configured_name lock"),
        "cache-runtime"
    );
    assert!(
        provider
            .entries
            .lock()
            .expect("entries lock")
            .contains_key("tenant-a:gamma")
    );
    assert_eq!(
        provider
            .ttl_by_key
            .lock()
            .expect("ttl_by_key lock")
            .get("tenant-a:alpha")
            .copied(),
        Some(Duration::from_secs(30))
    );

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
