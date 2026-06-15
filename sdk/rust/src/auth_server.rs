use std::sync::Arc;

use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::auth::{
    AuthCallContext, AuthenticationProvider, caller_bearer_token_from_metadata,
};
use crate::authentication::{
    AuthorizeRequest, GetGrantRequest, IntrospectRequest, ListGrantsRequest, RevokeGrantRequest,
    TokenRequest,
};
use crate::codec::authentication::{
    to_wire_get_grant_response, to_wire_introspect_response, to_wire_list_grants_response,
    to_wire_revoke_grant_response, to_wire_token_response,
};
use crate::generated::v1::authentication_server::Authentication as AuthenticationProviderGrpc;
use crate::generated::v1::{
    AuthorizeRequest as ProtoAuthorizeRequest, AuthorizeResponse as ProtoAuthorizeResponse,
    GetGrantRequest as ProtoGetGrantRequest, GetGrantResponse as ProtoGetGrantResponse,
    IntrospectRequest as ProtoIntrospectRequest, IntrospectResponse as ProtoIntrospectResponse,
    ListGrantsRequest as ProtoListGrantsRequest, ListGrantsResponse as ProtoListGrantsResponse,
    RevokeGrantRequest as ProtoRevokeGrantRequest, RevokeGrantResponse as ProtoRevokeGrantResponse,
    TokenRequest as ProtoTokenRequest, TokenResponse as ProtoTokenResponse,
};
use crate::rpc_status::rpc_status;

pub struct AuthenticationServer<P> {
    provider: Arc<P>,
}

impl<P> AuthenticationServer<P> {
    pub fn new(provider: Arc<P>) -> Self {
        Self { provider }
    }
}

impl<P> Clone for AuthenticationServer<P> {
    fn clone(&self) -> Self {
        Self {
            provider: Arc::clone(&self.provider),
        }
    }
}

fn authorize_request_from_proto(value: ProtoAuthorizeRequest) -> AuthorizeRequest {
    AuthorizeRequest {
        response_type: value.response_type,
        client_id: value.client_id,
        redirect_uri: value.redirect_uri,
        scope: value.scope,
        state: value.state,
    }
}

fn token_request_from_proto(value: ProtoTokenRequest) -> TokenRequest {
    TokenRequest {
        grant_type: value.grant_type,
        code: value.code,
        redirect_uri: value.redirect_uri,
        client_id: value.client_id,
        state: value.state,
        scope: value.scope,
        subject_token: value.subject_token,
        subject_token_type: value.subject_token_type,
    }
}

fn get_grant_request_from_proto(value: ProtoGetGrantRequest) -> GetGrantRequest {
    GetGrantRequest {
        grant_id: value.grant_id,
    }
}

fn revoke_grant_request_from_proto(value: ProtoRevokeGrantRequest) -> RevokeGrantRequest {
    RevokeGrantRequest {
        grant_id: value.grant_id,
    }
}

fn introspect_request_from_proto(value: ProtoIntrospectRequest) -> IntrospectRequest {
    IntrospectRequest {
        token: value.token,
        token_type_hint: value.token_type_hint,
    }
}

#[tonic::async_trait]
impl<P> AuthenticationProviderGrpc for AuthenticationServer<P>
where
    P: AuthenticationProvider,
{
    async fn authorize(
        &self,
        request: GrpcRequest<ProtoAuthorizeRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoAuthorizeResponse>, Status> {
        let response = self
            .provider
            .authorize(authorize_request_from_proto(request.into_inner()))
            .await
            .map_err(|error| rpc_status("authorize", error))?;
        Ok(GrpcResponse::new(ProtoAuthorizeResponse {
            redirect_uri: response.redirect_uri,
        }))
    }

    async fn token(
        &self,
        request: GrpcRequest<ProtoTokenRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoTokenResponse>, Status> {
        let response = self
            .provider
            .token(token_request_from_proto(request.into_inner()))
            .await
            .map_err(|error| rpc_status("token", error))?;
        Ok(GrpcResponse::new(to_wire_token_response(response)))
    }

    async fn introspect(
        &self,
        request: GrpcRequest<ProtoIntrospectRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoIntrospectResponse>, Status> {
        let response = self
            .provider
            .introspect(introspect_request_from_proto(request.into_inner()))
            .await
            .map_err(|error| rpc_status("introspect", error))?;
        Ok(GrpcResponse::new(to_wire_introspect_response(response)))
    }

    async fn list_grants(
        &self,
        request: GrpcRequest<ProtoListGrantsRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoListGrantsResponse>, Status> {
        let call = AuthCallContext {
            caller_bearer_token: caller_bearer_token_from_metadata(request.metadata()),
        };
        let response = self
            .provider
            .list_grants(call, ListGrantsRequest {})
            .await
            .map_err(|error| rpc_status("list grants", error))?;
        Ok(GrpcResponse::new(to_wire_list_grants_response(response)))
    }

    async fn get_grant(
        &self,
        request: GrpcRequest<ProtoGetGrantRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoGetGrantResponse>, Status> {
        let call = AuthCallContext {
            caller_bearer_token: caller_bearer_token_from_metadata(request.metadata()),
        };
        let response = self
            .provider
            .get_grant(call, get_grant_request_from_proto(request.into_inner()))
            .await
            .map_err(|error| rpc_status("get grant", error))?;
        Ok(GrpcResponse::new(to_wire_get_grant_response(response)))
    }

    async fn revoke_grant(
        &self,
        request: GrpcRequest<ProtoRevokeGrantRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoRevokeGrantResponse>, Status> {
        let call = AuthCallContext {
            caller_bearer_token: caller_bearer_token_from_metadata(request.metadata()),
        };
        self.provider
            .revoke_grant(call, revoke_grant_request_from_proto(request.into_inner()))
            .await
            .map_err(|error| rpc_status("revoke grant", error))?;
        Ok(GrpcResponse::new(to_wire_revoke_grant_response(
            crate::authentication::RevokeGrantResponse {},
        )))
    }
}
