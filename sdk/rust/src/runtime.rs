use std::env;
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

use crate::env::{ENV_PLUGIN_NAME, ENV_PLUGIN_PARENT_PID, ENV_PLUGIN_SOCKET, ENV_WRITE_CATALOG};
use crate::error::{Error, Result};
#[cfg(unix)]
use crate::generated::v1::provider_plugin_server::ProviderPluginServer;
use crate::{Provider, ProviderServer, Router, write_catalog};

pub fn run_provider<P>(provider: Arc<P>, router: Router<P>) -> Result<()>
where
    P: Provider,
{
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .map_err(|error| Error::internal(error.to_string()))?;
    runtime.block_on(serve_provider(provider, router))
}

pub fn write_catalog_path<P>(router: &Router<P>, path: impl AsRef<Path>) -> Result<()> {
    write_catalog(&router.catalog(), path)
}

pub fn maybe_write_catalog<P>(router: &Router<P>) -> Result<bool> {
    let Some(path) = env::var_os(ENV_WRITE_CATALOG) else {
        return Ok(false);
    };

    let router = if let Ok(name) = env::var(ENV_PLUGIN_NAME) {
        (*router).clone().with_name(name)
    } else {
        (*router).clone()
    };

    write_catalog(&router.catalog(), PathBuf::from(path))?;
    Ok(true)
}

#[cfg(unix)]
pub async fn serve_provider<P>(provider: Arc<P>, router: Router<P>) -> Result<()>
where
    P: Provider,
{
    if maybe_write_catalog(&router)? {
        return Ok(());
    }

    let socket = env::var_os(ENV_PLUGIN_SOCKET)
        .ok_or_else(|| Error::internal(format!("{ENV_PLUGIN_SOCKET} is required")))?;
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
    let server = ProviderServer::new(Arc::clone(&provider), router);
    let serve_result = Server::builder()
        .add_service(ProviderPluginServer::new(server))
        .serve_with_incoming_shutdown(incoming, shutdown_signal(parent_pid()))
        .await
        .map_err(Error::from);

    let close_result = provider.close().await;
    let _ = remove_socket(&socket);

    serve_result?;
    close_result
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
fn parent_pid() -> Option<u32> {
    env::var(ENV_PLUGIN_PARENT_PID)
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
