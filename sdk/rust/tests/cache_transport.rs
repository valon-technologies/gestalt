#[allow(dead_code)]
mod helpers;

use std::collections::BTreeMap;
use std::path::Path;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use gestalt::proto::v1::cache_server::{Cache as ProtoCache, CacheServer};
use gestalt::proto::v1::provider_lifecycle_client::ProviderLifecycleClient;
use gestalt::proto::v1::{
    CacheDeleteManyRequest, CacheDeleteManyResponse, CacheDeleteRequest, CacheDeleteResponse,
    CacheGetManyRequest, CacheGetManyResponse, CacheGetRequest, CacheGetResponse, CacheResult,
    CacheSetManyRequest, CacheSetRequest, CacheTouchRequest, CacheTouchResponse,
    ConfigureProviderRequest, ProviderKind,
};
use gestalt::{
    CacheEntry, CacheProvider, CacheSetOptions, ENV_CACHE_SOCKET, ENV_CACHE_SOCKET_TOKEN,
    RuntimeMetadata,
};
use hyper_util::rt::tokio::TokioIo;
use tokio::net::{TcpListener, UnixStream};
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::{Endpoint, Server};
use tonic::{Code, Request as GrpcRequest, Response as GrpcResponse, Status};
use tower::service_fn;

const CACHE_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

#[derive(Default)]
struct TestCacheProvider {
    configured_name: Mutex<String>,
    namespace: Mutex<String>,
    entries: Mutex<BTreeMap<String, Vec<u8>>>,
    ttl_by_key: Mutex<BTreeMap<String, Duration>>,
    seen_relay_tokens: Mutex<Vec<String>>,
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

    fn record_relay_tokens(&self, metadata: &tonic::metadata::MetadataMap) {
        let relay_tokens = metadata
            .get_all(CACHE_RELAY_TOKEN_HEADER)
            .iter()
            .filter_map(|value| value.to_str().ok())
            .map(ToOwned::to_owned)
            .collect::<Vec<_>>();
        self.seen_relay_tokens
            .lock()
            .expect("seen_relay_tokens lock")
            .extend(relay_tokens);
    }
}

#[derive(Clone)]
struct ObservedCacheServer {
    provider: Arc<TestCacheProvider>,
}

#[gestalt::async_trait]
impl ProtoCache for ObservedCacheServer {
    async fn get(
        &self,
        request: GrpcRequest<CacheGetRequest>,
    ) -> std::result::Result<GrpcResponse<CacheGetResponse>, Status> {
        self.provider.record_relay_tokens(request.metadata());
        let request = request.into_inner();
        let value = self
            .provider
            .get(&request.key)
            .await
            .map_err(|error| Status::internal(error.to_string()))?;
        match value {
            Some(value) => Ok(GrpcResponse::new(CacheGetResponse { found: true, value })),
            None => Ok(GrpcResponse::new(CacheGetResponse {
                found: false,
                value: Vec::new(),
            })),
        }
    }

    async fn get_many(
        &self,
        request: GrpcRequest<CacheGetManyRequest>,
    ) -> std::result::Result<GrpcResponse<CacheGetManyResponse>, Status> {
        self.provider.record_relay_tokens(request.metadata());
        let request = request.into_inner();
        let values = self
            .provider
            .get_many(&request.keys)
            .await
            .map_err(|error| Status::internal(error.to_string()))?;
        let entries = request
            .keys
            .into_iter()
            .map(|key| match values.get(&key) {
                Some(value) => CacheResult {
                    key,
                    found: true,
                    value: value.clone(),
                },
                None => CacheResult {
                    key,
                    found: false,
                    value: Vec::new(),
                },
            })
            .collect();
        Ok(GrpcResponse::new(CacheGetManyResponse { entries }))
    }

    async fn set(
        &self,
        request: GrpcRequest<CacheSetRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider.record_relay_tokens(request.metadata());
        let request = request.into_inner();
        self.provider
            .set(
                &request.key,
                &request.value,
                CacheSetOptions {
                    ttl: duration_from_proto(request.ttl)?,
                },
            )
            .await
            .map_err(|error| Status::internal(error.to_string()))?;
        Ok(GrpcResponse::new(()))
    }

    async fn set_many(
        &self,
        request: GrpcRequest<CacheSetManyRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider.record_relay_tokens(request.metadata());
        let request = request.into_inner();
        let entries = request
            .entries
            .into_iter()
            .map(|entry| CacheEntry {
                key: entry.key,
                value: entry.value,
            })
            .collect::<Vec<_>>();
        self.provider
            .set_many(
                &entries,
                CacheSetOptions {
                    ttl: duration_from_proto(request.ttl)?,
                },
            )
            .await
            .map_err(|error| Status::internal(error.to_string()))?;
        Ok(GrpcResponse::new(()))
    }

    async fn delete(
        &self,
        request: GrpcRequest<CacheDeleteRequest>,
    ) -> std::result::Result<GrpcResponse<CacheDeleteResponse>, Status> {
        self.provider.record_relay_tokens(request.metadata());
        let request = request.into_inner();
        let deleted = self
            .provider
            .delete(&request.key)
            .await
            .map_err(|error| Status::internal(error.to_string()))?;
        Ok(GrpcResponse::new(CacheDeleteResponse { deleted }))
    }

    async fn delete_many(
        &self,
        request: GrpcRequest<CacheDeleteManyRequest>,
    ) -> std::result::Result<GrpcResponse<CacheDeleteManyResponse>, Status> {
        self.provider.record_relay_tokens(request.metadata());
        let deleted = self
            .provider
            .delete_many(&request.into_inner().keys)
            .await
            .map_err(|error| Status::internal(error.to_string()))?;
        Ok(GrpcResponse::new(CacheDeleteManyResponse { deleted }))
    }

    async fn touch(
        &self,
        request: GrpcRequest<CacheTouchRequest>,
    ) -> std::result::Result<GrpcResponse<CacheTouchResponse>, Status> {
        self.provider.record_relay_tokens(request.metadata());
        let request = request.into_inner();
        let touched = self
            .provider
            .touch(
                &request.key,
                duration_from_proto(request.ttl)?.unwrap_or_default(),
            )
            .await
            .map_err(|error| Status::internal(error.to_string()))?;
        Ok(GrpcResponse::new(CacheTouchResponse { touched }))
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
    assert_eq!(
        metadata.min_protocol_version,
        gestalt::CURRENT_PROTOCOL_VERSION
    );
    assert_eq!(
        metadata.max_protocol_version,
        gestalt::CURRENT_PROTOCOL_VERSION
    );

    let err = runtime
        .configure_provider(ConfigureProviderRequest {
            name: "cache-runtime".to_string(),
            config: Some(helpers::struct_from_json(
                serde_json::json!({ "namespace": "tenant-a" }),
            )),
            protocol_version: gestalt::CURRENT_PROTOCOL_VERSION + 1,
        })
        .await
        .expect_err("configure provider should reject mismatched protocol version");
    assert_eq!(err.code(), Code::FailedPrecondition);
    assert_eq!(
        provider
            .configured_name
            .lock()
            .expect("configured_name lock")
            .as_str(),
        "",
        "provider should not be configured on protocol mismatch"
    );

    let configured = runtime
        .configure_provider(ConfigureProviderRequest {
            name: "cache-runtime".to_string(),
            config: Some(helpers::struct_from_json(
                serde_json::json!({ "namespace": "tenant-a" }),
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

#[tokio::test]
async fn cache_connects_over_tcp_and_forwards_relay_token() {
    let _env_lock = helpers::env_lock().lock().await;
    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .expect("bind tcp listener");
    let address = listener.local_addr().expect("local addr");
    let _socket_guard = helpers::EnvGuard::set(ENV_CACHE_SOCKET, format!("tcp://{address}"));
    let _token_guard = helpers::EnvGuard::set(ENV_CACHE_SOCKET_TOKEN, "relay-token-rust");

    let provider = Arc::new(TestCacheProvider::default());
    let server = ObservedCacheServer {
        provider: Arc::clone(&provider),
    };
    let serve_task = tokio::spawn(async move {
        Server::builder()
            .add_service(CacheServer::new(server))
            .serve_with_incoming(TcpListenerStream::new(listener))
            .await
            .expect("serve cache over tcp");
    });

    let mut cache = gestalt::Cache::connect().await.expect("connect cache");
    cache
        .set(
            "tcp-alpha",
            b"over-tcp",
            CacheSetOptions {
                ttl: Some(Duration::from_secs(45)),
            },
        )
        .await
        .expect("set over tcp");
    assert_eq!(
        cache.get("tcp-alpha").await.expect("get over tcp"),
        Some(b"over-tcp".to_vec())
    );

    assert_eq!(
        provider
            .seen_relay_tokens
            .lock()
            .expect("seen_relay_tokens lock")
            .clone(),
        vec![
            "relay-token-rust".to_string(),
            "relay-token-rust".to_string()
        ]
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

fn duration_from_proto(
    ttl: Option<prost_types::Duration>,
) -> std::result::Result<Option<Duration>, Status> {
    let Some(ttl) = ttl else {
        return Ok(None);
    };
    if ttl.seconds < 0 {
        return Err(Status::invalid_argument("ttl must be non-negative"));
    }
    if ttl.nanos < 0 || ttl.nanos >= 1_000_000_000 {
        return Err(Status::invalid_argument("ttl nanos must be in [0, 1e9)"));
    }
    let seconds =
        u64::try_from(ttl.seconds).map_err(|_| Status::invalid_argument("ttl is too large"))?;
    Ok(Some(Duration::new(seconds, ttl.nanos as u32)))
}
