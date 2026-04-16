use std::env;
#[cfg(unix)]
use std::future::Future;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::Duration;

#[cfg(unix)]
use tokio::net::UnixListener;
#[cfg(unix)]
use tokio::signal;
#[cfg(unix)]
use tokio::time::sleep;
#[cfg(unix)]
use tokio_stream::wrappers::UnixListenerStream;
#[cfg(unix)]
use tonic::transport::Server;

use crate::catalog::write_catalog;
use crate::env::{
    ENV_PROVIDER_NAME, ENV_PROVIDER_PARENT_PID, ENV_PROVIDER_SOCKET, ENV_WRITE_CATALOG,
};
use crate::error::{Error, Result};
#[cfg(unix)]
use crate::generated::v1::auth_provider_server::AuthProviderServer;
#[cfg(unix)]
use crate::generated::v1::cache_server::CacheServer;
#[cfg(unix)]
use crate::generated::v1::integration_provider_server::IntegrationProviderServer;
#[cfg(unix)]
use crate::generated::v1::provider_lifecycle_server::ProviderLifecycleServer;
#[cfg(unix)]
use crate::generated::v1::s3_server::S3Server;
#[cfg(unix)]
use crate::generated::v1::secrets_provider_server::SecretsProviderServer;
#[cfg(unix)]
use crate::generated::v1::workflow_provider_server::WorkflowProviderServer as WorkflowRpcServer;
use crate::provider_server::ProviderServer;
use crate::{
    AuthProvider, CacheProvider, Provider, Router, S3Provider, SecretsProvider, WorkflowProvider,
};
#[cfg(unix)]
use crate::{
    auth_server::AuthServer, cache_server::CacheRpcServer, runtime_server::RuntimeServer,
    secrets_server::SecretsServer, workflow::WorkflowServer,
};

fn build_runtime_and_block_on<F, Fut>(f: F) -> Result<()>
where
    F: FnOnce() -> Fut,
    Fut: std::future::Future<Output = Result<()>>,
{
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .map_err(|error| Error::internal(error.to_string()))?;
    runtime.block_on(f())
}

/// Runs an integration provider on the Unix socket exposed by `gestaltd`.
pub fn run_provider<P: Provider>(provider: Arc<P>, router: Router<P>) -> Result<()> {
    build_runtime_and_block_on(|| serve_provider(provider, router))
}

/// Runs an auth provider on the Unix socket exposed by `gestaltd`.
pub fn run_auth_provider<P: AuthProvider>(provider: Arc<P>) -> Result<()> {
    build_runtime_and_block_on(|| serve_auth_provider(provider))
}

/// Runs a cache provider on the Unix socket exposed by `gestaltd`.
pub fn run_cache_provider<P: CacheProvider>(provider: Arc<P>) -> Result<()> {
    build_runtime_and_block_on(|| serve_cache_provider(provider))
}

/// Runs a secrets provider on the Unix socket exposed by `gestaltd`.
pub fn run_secrets_provider<P: SecretsProvider>(provider: Arc<P>) -> Result<()> {
    build_runtime_and_block_on(|| serve_secrets_provider(provider))
}

/// Runs an S3 provider on the Unix socket exposed by `gestaltd`.
pub fn run_s3_provider<P: S3Provider>(provider: Arc<P>) -> Result<()> {
    build_runtime_and_block_on(|| serve_s3_provider(provider))
}

/// Runs a workflow provider on the Unix socket exposed by `gestaltd`.
pub fn run_workflow_provider<P: WorkflowProvider>(provider: Arc<P>) -> Result<()> {
    build_runtime_and_block_on(|| serve_workflow_provider(provider))
}

/// Writes the router's derived catalog to path.
pub fn write_catalog_path<P>(router: &Router<P>, path: impl AsRef<Path>) -> Result<()> {
    write_catalog(router.catalog(), path)
}

/// Writes the router's derived catalog when `GESTALT_PLUGIN_WRITE_CATALOG` is
/// set, returning whether anything was written.
pub fn maybe_write_catalog<P>(router: &Router<P>) -> Result<bool> {
    let Some(path) = env::var_os(ENV_WRITE_CATALOG) else {
        return Ok(false);
    };

    let catalog = if let Ok(name) = env::var(ENV_PROVIDER_NAME) {
        router.catalog().clone().with_name(name)
    } else {
        router.catalog().clone()
    };

    write_catalog(&catalog, PathBuf::from(path))?;
    Ok(true)
}

#[cfg(unix)]
/// Serves an integration provider over the configured Unix socket.
pub async fn serve_provider<P>(provider: Arc<P>, router: Router<P>) -> Result<()>
where
    P: Provider,
{
    if maybe_write_catalog(&router)? {
        return Ok(());
    }
    let server = ProviderServer::new(Arc::clone(&provider), router);
    serve_unix_provider(
        provider,
        move |incoming, provider| {
            Server::builder()
                .add_service(ProviderLifecycleServer::new(RuntimeServer::for_provider(
                    Arc::clone(&provider),
                )))
                .add_service(IntegrationProviderServer::new(server))
                .serve_with_incoming_shutdown(incoming, shutdown_signal(parent_pid()))
        },
        |provider| async move { provider.close().await },
    )
    .await
}

#[cfg(unix)]
/// Serves an auth provider over the configured Unix socket.
pub async fn serve_auth_provider<P>(provider: Arc<P>) -> Result<()>
where
    P: AuthProvider,
{
    serve_unix_provider(
        provider,
        move |incoming, provider| {
            Server::builder()
                .add_service(ProviderLifecycleServer::new(RuntimeServer::for_auth(
                    Arc::clone(&provider),
                )))
                .add_service(AuthProviderServer::new(AuthServer::new(Arc::clone(
                    &provider,
                ))))
                .serve_with_incoming_shutdown(incoming, shutdown_signal(parent_pid()))
        },
        |provider| async move { provider.close().await },
    )
    .await
}

#[cfg(unix)]
/// Serves a cache provider over the configured Unix socket.
pub async fn serve_cache_provider<P>(provider: Arc<P>) -> Result<()>
where
    P: CacheProvider,
{
    serve_unix_provider(
        provider,
        move |incoming, provider| {
            Server::builder()
                .add_service(ProviderLifecycleServer::new(RuntimeServer::for_cache(
                    Arc::clone(&provider),
                )))
                .add_service(CacheServer::new(CacheRpcServer::new(Arc::clone(&provider))))
                .serve_with_incoming_shutdown(incoming, shutdown_signal(parent_pid()))
        },
        |provider| async move { provider.close().await },
    )
    .await
}

#[cfg(unix)]
/// Serves a secrets provider over the configured Unix socket.
pub async fn serve_secrets_provider<P>(provider: Arc<P>) -> Result<()>
where
    P: SecretsProvider,
{
    serve_unix_provider(
        provider,
        move |incoming, provider| {
            Server::builder()
                .add_service(ProviderLifecycleServer::new(RuntimeServer::for_secrets(
                    Arc::clone(&provider),
                )))
                .add_service(SecretsProviderServer::new(SecretsServer::new(Arc::clone(
                    &provider,
                ))))
                .serve_with_incoming_shutdown(incoming, shutdown_signal(parent_pid()))
        },
        |provider| async move { provider.close().await },
    )
    .await
}

#[cfg(unix)]
/// Serves an S3 provider over the configured Unix socket.
pub async fn serve_s3_provider<P>(provider: Arc<P>) -> Result<()>
where
    P: S3Provider,
{
    serve_unix_provider(
        provider,
        move |incoming, provider| {
            Server::builder()
                .add_service(ProviderLifecycleServer::new(RuntimeServer::for_s3(
                    Arc::clone(&provider),
                )))
                .add_service(S3Server::new(Arc::clone(&provider)))
                .serve_with_incoming_shutdown(incoming, shutdown_signal(parent_pid()))
        },
        |provider| async move { provider.close().await },
    )
    .await
}

#[cfg(unix)]
pub async fn serve_workflow_provider<P>(provider: Arc<P>) -> Result<()>
where
    P: WorkflowProvider,
{
    serve_unix_provider(
        provider,
        move |incoming, provider| {
            Server::builder()
                .add_service(ProviderLifecycleServer::new(RuntimeServer::for_workflow(
                    Arc::clone(&provider),
                )))
                .add_service(WorkflowRpcServer::new(WorkflowServer::new(Arc::clone(
                    &provider,
                ))))
                .serve_with_incoming_shutdown(incoming, shutdown_signal(parent_pid()))
        },
        |provider| async move { provider.close().await },
    )
    .await
}

#[cfg(not(unix))]
pub async fn serve_provider<P>(_provider: Arc<P>, router: Router<P>) -> Result<()>
where
    P: Provider,
{
    if maybe_write_catalog(&router)? {
        return Ok(());
    }
    Err(Error::internal(
        "unix sockets are unsupported on this platform",
    ))
}

#[cfg(not(unix))]
pub async fn serve_auth_provider<P>(_provider: Arc<P>) -> Result<()>
where
    P: AuthProvider,
{
    Err(Error::internal(
        "unix sockets are unsupported on this platform",
    ))
}

#[cfg(not(unix))]
pub async fn serve_cache_provider<P>(_provider: Arc<P>) -> Result<()>
where
    P: CacheProvider,
{
    Err(Error::internal(
        "unix sockets are unsupported on this platform",
    ))
}

#[cfg(not(unix))]
pub async fn serve_secrets_provider<P>(_provider: Arc<P>) -> Result<()>
where
    P: SecretsProvider,
{
    Err(Error::internal(
        "unix sockets are unsupported on this platform",
    ))
}

#[cfg(not(unix))]
pub async fn serve_s3_provider<P>(_provider: Arc<P>) -> Result<()>
where
    P: S3Provider,
{
    Err(Error::internal(
        "unix sockets are unsupported on this platform",
    ))
}

#[cfg(not(unix))]
pub async fn serve_workflow_provider<P>(_provider: Arc<P>) -> Result<()>
where
    P: WorkflowProvider,
{
    Err(Error::internal(
        "unix sockets are unsupported on this platform",
    ))
}

#[cfg(unix)]
async fn shutdown_signal(parent_pid: Option<u32>) {
    let ctrl_c = async {
        let _ = signal::ctrl_c().await;
    };

    tokio::pin!(ctrl_c);

    if let Some(parent_pid) = parent_pid {
        tokio::select! {
            _ = &mut ctrl_c => {}
            _ = watch_parent(parent_pid) => {}
        }
        return;
    }

    ctrl_c.await;
}

#[cfg(unix)]
async fn serve_unix_provider<P, F, S, C, CF>(provider: Arc<P>, serve: F, close: C) -> Result<()>
where
    P: Send + Sync,
    F: FnOnce(UnixListenerStream, Arc<P>) -> S,
    S: Future<Output = std::result::Result<(), tonic::transport::Error>>,
    C: FnOnce(Arc<P>) -> CF,
    CF: Future<Output = Result<()>>,
{
    let socket = env::var_os(ENV_PROVIDER_SOCKET)
        .ok_or_else(|| Error::internal(format!("{ENV_PROVIDER_SOCKET} is required")))?;
    let socket = PathBuf::from(socket);
    if socket.exists() {
        std::fs::remove_file(&socket)?;
    }
    if let Some(parent) = socket.parent()
        && !parent.as_os_str().is_empty()
    {
        std::fs::create_dir_all(parent)?;
    }

    let listener = UnixListener::bind(&socket)?;
    let incoming = UnixListenerStream::new(listener);
    let serve_result = serve(incoming, Arc::clone(&provider))
        .await
        .map_err(Error::from);

    let close_result = close(provider).await;
    let _ = remove_socket(&socket);

    serve_result?;
    close_result
}

#[cfg(unix)]
fn parent_pid() -> Option<u32> {
    env::var(ENV_PROVIDER_PARENT_PID)
        .ok()
        .and_then(|value| value.parse::<u32>().ok())
        .filter(|pid| *pid > 0)
}

#[cfg(unix)]
async fn watch_parent(parent_pid: u32) {
    loop {
        if current_parent_pid() != parent_pid {
            break;
        }
        sleep(Duration::from_millis(500)).await;
    }
}

#[cfg(unix)]
fn current_parent_pid() -> u32 {
    unsafe { libc::getppid() as u32 }
}

#[cfg(unix)]
fn remove_socket(path: &Path) -> std::io::Result<()> {
    match std::fs::remove_file(path) {
        Ok(()) => Ok(()),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(error) => Err(error),
    }
}
