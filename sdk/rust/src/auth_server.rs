use std::sync::Arc;

use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::auth::{AuthCallContext, AuthenticationProvider, caller_bearer_token_from_metadata};
use crate::authentication::{
    AuthorizeRequest, GetGrantRequest, IntrospectRequest, ListGrantsRequest, RevokeGrantRequest,
    TokenRequest, UserInfoRequest, UserInfoResponse,
};
use crate::authentication::{
    GetGrantResponse, IntrospectResponse, ListGrantsResponse, TokenResponse,
};
use crate::generated::v1::authentication_server::Authentication as AuthenticationProviderGrpc;
use crate::generated::v1::{
    AuthorizeRequest as ProtoAuthorizeRequest, AuthorizeResponse as ProtoAuthorizeResponse,
    GetGrantRequest as ProtoGetGrantRequest, GetGrantResponse as ProtoGetGrantResponse,
    GrantScope as ProtoGrantScope, IntrospectRequest as ProtoIntrospectRequest,
    IntrospectResponse as ProtoIntrospectResponse, ListGrantsRequest as ProtoListGrantsRequest,
    ListGrantsResponse as ProtoListGrantsResponse, RevokeGrantRequest as ProtoRevokeGrantRequest,
    RevokeGrantResponse as ProtoRevokeGrantResponse, TokenRequest as ProtoTokenRequest,
    TokenResponse as ProtoTokenResponse, UserInfoRequest as ProtoUserInfoRequest,
    UserInfoResponse as ProtoUserInfoResponse,
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

fn user_info_response_to_proto(value: UserInfoResponse) -> ProtoUserInfoResponse {
    ProtoUserInfoResponse {
        subject_id: value.subject_id,
        email: value.email,
        name: value.name,
    }
}

fn token_response_to_proto(value: TokenResponse) -> ProtoTokenResponse {
    ProtoTokenResponse {
        access_token: value.access_token,
        token_type: value.token_type,
        expires_in: value.expires_in,
        refresh_token: value.refresh_token,
        scope: value.scope,
        grant_id: value.grant_id,
    }
}

fn introspect_response_to_proto(value: IntrospectResponse) -> ProtoIntrospectResponse {
    ProtoIntrospectResponse {
        active: value.active,
        subject: value.subject,
        scope: value.scope,
        client_id: value.client_id,
        audience: value.audience,
    }
}

fn list_grants_response_to_proto(value: ListGrantsResponse) -> ProtoListGrantsResponse {
    ProtoListGrantsResponse {
        grant_ids: value.grant_ids,
    }
}

fn get_grant_response_to_proto(value: GetGrantResponse) -> ProtoGetGrantResponse {
    ProtoGetGrantResponse {
        scopes: value
            .scopes
            .into_iter()
            .map(|scope| ProtoGrantScope {
                scope: scope.scope,
                resource: scope.resource,
            })
            .collect(),
        created_at: value.created_at,
        expires_at: value.expires_at,
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
        Ok(GrpcResponse::new(token_response_to_proto(response)))
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
        Ok(GrpcResponse::new(introspect_response_to_proto(response)))
    }

    async fn user_info(
        &self,
        request: GrpcRequest<ProtoUserInfoRequest>,
    ) -> std::result::Result<GrpcResponse<ProtoUserInfoResponse>, Status> {
        let call = AuthCallContext {
            caller_bearer_token: caller_bearer_token_from_metadata(request.metadata()),
        };
        let response = self
            .provider
            .user_info(call, UserInfoRequest {})
            .await
            .map_err(|error| rpc_status("userinfo", error))?;
        Ok(GrpcResponse::new(user_info_response_to_proto(response)))
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
        Ok(GrpcResponse::new(list_grants_response_to_proto(response)))
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
        Ok(GrpcResponse::new(get_grant_response_to_proto(response)))
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
        Ok(GrpcResponse::new(ProtoRevokeGrantResponse {}))
    }
}
