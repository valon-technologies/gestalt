//! Tests for the sync gRPC transport (SyncGrpcTransport).

use std::sync::Arc;

use gestalt::public::auth::NoAuth;
use gestalt::public::generated::app_client::ExternalCredentialsClient;
use gestalt::external_credential::{
    CreateExternalCredentialRequest, ExternalCredential,
};
use gestalt::public::grpc_transport::SyncGrpcTransport;
use gestalt::rpc_support::gestalt_error_code;

#[path = "../src/generated.rs"]
mod generated;

use generated::v1::external_credentials_server::{ExternalCredentials, ExternalCredentialsServer};
use generated::v1::{
    CreateExternalCredentialRequest as WireCreateRequest,
    DeleteExternalCredentialRequest as WireDeleteRequest,
    ExchangeExternalCredentialRequest as WireExchangeRequest,
    ExchangeExternalCredentialResponse as WireExchangeResponse,
    ExternalCredential as WireCredential, GetExternalCredentialRequest as WireGetRequest,
    ListExternalCredentialsRequest as WireListRequest,
    ListExternalCredentialsResponse as WireListResponse,
    ResolveExternalCredentialRequest as WireResolveRequest,
    ResolveExternalCredentialResponse as WireResolveResponse,
    UpsertExternalCredentialRequest as WireUpsertRequest,
    ValidateExternalCredentialConfigRequest as WireValidateRequest,
};
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::Server;
use tonic::{Request, Response, Status};

struct StubExternalCredentials;

#[tonic::async_trait]
impl ExternalCredentials for StubExternalCredentials {
    async fn create_credential(
        &self,
        _request: Request<WireCreateRequest>,
    ) -> Result<Response<WireCredential>, Status> {
        Ok(Response::new(WireCredential {
            id: "cred-1".into(),
            subject: "user-1".into(),
            ..Default::default()
        }))
    }

    async fn upsert_credential(
        &self,
        _request: Request<WireUpsertRequest>,
    ) -> Result<Response<WireCredential>, Status> {
        Err(Status::unimplemented("unused"))
    }

    async fn get_credential(
        &self,
        _request: Request<WireGetRequest>,
    ) -> Result<Response<WireCredential>, Status> {
        Err(Status::unimplemented("unused"))
    }

    async fn list_credentials(
        &self,
        _request: Request<WireListRequest>,
    ) -> Result<Response<WireListResponse>, Status> {
        Err(Status::unimplemented("unused"))
    }

    async fn delete_credential(
        &self,
        _request: Request<WireDeleteRequest>,
    ) -> Result<Response<()>, Status> {
        Err(Status::unimplemented("unused"))
    }

    async fn validate_credential_config(
        &self,
        _request: Request<WireValidateRequest>,
    ) -> Result<Response<()>, Status> {
        Err(Status::unimplemented("unused"))
    }

    async fn resolve_credential(
        &self,
        _request: Request<WireResolveRequest>,
    ) -> Result<Response<WireResolveResponse>, Status> {
        Err(Status::unimplemented("unused"))
    }

    async fn exchange_credential(
        &self,
        _request: Request<WireExchangeRequest>,
    ) -> Result<Response<WireExchangeResponse>, Status> {
        Err(Status::unimplemented("unused"))
    }
}

struct StubUnauthenticated;

#[tonic::async_trait]
impl ExternalCredentials for StubUnauthenticated {
    async fn create_credential(
        &self,
        _request: Request<WireCreateRequest>,
    ) -> Result<Response<WireCredential>, Status> {
        Err(Status::unauthenticated("missing bearer"))
    }

    async fn upsert_credential(
        &self,
        _request: Request<WireUpsertRequest>,
    ) -> Result<Response<WireCredential>, Status> {
        Err(Status::unimplemented("unused"))
    }

    async fn get_credential(
        &self,
        _request: Request<WireGetRequest>,
    ) -> Result<Response<WireCredential>, Status> {
        Err(Status::unimplemented("unused"))
    }

    async fn list_credentials(
        &self,
        _request: Request<WireListRequest>,
    ) -> Result<Response<WireListResponse>, Status> {
        Err(Status::unimplemented("unused"))
    }

    async fn delete_credential(
        &self,
        _request: Request<WireDeleteRequest>,
    ) -> Result<Response<()>, Status> {
        Err(Status::unimplemented("unused"))
    }

    async fn validate_credential_config(
        &self,
        _request: Request<WireValidateRequest>,
    ) -> Result<Response<()>, Status> {
        Err(Status::unimplemented("unused"))
    }

    async fn resolve_credential(
        &self,
        _request: Request<WireResolveRequest>,
    ) -> Result<Response<WireResolveResponse>, Status> {
        Err(Status::unimplemented("unused"))
    }

    async fn exchange_credential(
        &self,
        _request: Request<WireExchangeRequest>,
    ) -> Result<Response<WireExchangeResponse>, Status> {
        Err(Status::unimplemented("unused"))
    }
}

// Boot a tonic gRPC server on a one-shot tokio runtime, then return the
// address. The server runs in a background thread for the lifetime of the
// returned guard.
struct ServerGuard {
    _runtime: tokio::runtime::Runtime,
    addr: std::net::SocketAddr,
}

fn start_server<S: ExternalCredentials + 'static>(service: S) -> ServerGuard {
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .expect("tokio runtime");
    let listener = runtime
        .block_on(tokio::net::TcpListener::bind("127.0.0.1:0"))
        .expect("bind");
    let addr = listener.local_addr().expect("addr");
    runtime.spawn(async move {
        Server::builder()
            .add_service(ExternalCredentialsServer::new(service))
            .serve_with_incoming(TcpListenerStream::new(listener))
            .await
            .expect("serve");
    });
    ServerGuard {
        _runtime: runtime,
        addr,
    }
}

fn sync_client(guard: &ServerGuard) -> ExternalCredentialsClient<SyncGrpcTransport> {
    let endpoint = tonic::transport::Endpoint::from_shared(format!("http://{}", guard.addr))
        .expect("endpoint");
    let transport = SyncGrpcTransport::from_endpoint(endpoint, Arc::new(NoAuth));
    ExternalCredentialsClient::new(transport)
}

#[test]
fn sync_grpc_transport_create_credential_success() {
    let guard = start_server(StubExternalCredentials);
    let client = sync_client(&guard);

    let response = client
        .create_credential_sync(CreateExternalCredentialRequest {
            credential: Some(ExternalCredential {
                id: "req-1".to_string(),
                subject: "user-1".to_string(),
                ..Default::default()
            }),
        })
        .expect("create_credential_sync should succeed");

    assert_eq!(response.id, "cred-1");
    assert_eq!(response.subject, "user-1");
}

#[test]
fn sync_grpc_transport_maps_unauthenticated_error() {
    let guard = start_server(StubUnauthenticated);
    let client = sync_client(&guard);

    let err = client
        .create_credential_sync(CreateExternalCredentialRequest::default())
        .expect_err("create_credential_sync should fail");

    assert_eq!(err.code, gestalt_error_code::UNAUTHENTICATED);
}
