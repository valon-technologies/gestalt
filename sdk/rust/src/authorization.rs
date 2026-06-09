use std::time::SystemTime;

use serde::Serialize;
use tonic::codegen::async_trait;

use crate::Request;
use crate::env::{ENV_HOST_SERVICE_SOCKET, ENV_HOST_SERVICE_TOKEN};
use crate::generated::v1::{
    self as pb,
    authorization_provider_client::AuthorizationProviderClient as ProtoAuthorizationProviderClient,
};
use crate::host_service::{self, HostServiceError};
use crate::protocol;

/// Native JSON object used by Authorization properties fields.
pub type JsonObject = serde_json::Map<String, serde_json::Value>;

type AuthorizationTransport = host_service::Transport;

#[derive(Debug, thiserror::Error)]
/// Errors returned by the Authorization client.
pub enum AuthorizationError {
    /// The host-service transport could not be created.
    #[error("{0}")]
    Transport(#[from] tonic::transport::Error),
    /// The host-service RPC returned a gRPC status.
    #[error("{0}")]
    Status(#[from] tonic::Status),
    /// Required environment or target configuration was invalid.
    #[error("{0}")]
    Env(String),
    /// JSON object conversion failed.
    #[error("{0}")]
    Json(#[from] serde_json::Error),
    /// The host returned a protocol value the SDK could not represent.
    #[error("{0}")]
    Protocol(String),
}

impl From<HostServiceError> for AuthorizationError {
    fn from(error: HostServiceError) -> Self {
        match error {
            HostServiceError::Transport(error) => Self::Transport(error),
            HostServiceError::Env(error) => Self::Env(error),
        }
    }
}

/// Converts any serializable value that serializes to a JSON object into a native
/// Authorization JSON object.
pub fn json_object<T: Serialize>(value: T) -> Result<JsonObject, AuthorizationError> {
    match protocol::json_value_from_serializable(value)? {
        serde_json::Value::Object(fields) => Ok(fields),
        _ => Err(AuthorizationError::Protocol(
            "authorization: expected JSON object".to_string(),
        )),
    }
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct Subject {
    pub r#type: String,
    pub id: String,
    pub properties: Option<JsonObject>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct Action {
    pub name: String,
    pub properties: Option<JsonObject>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct Resource {
    pub r#type: String,
    pub id: String,
    pub properties: Option<JsonObject>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct CheckAccessRequest {
    pub subject: Option<Subject>,
    pub action: Option<Action>,
    pub resource: Option<Resource>,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct CheckAccessResponse {
    pub allowed: bool,
    pub model_id: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct CheckAccessManyRequest {
    pub requests: Vec<CheckAccessRequest>,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct CheckAccessManyResponse {
    pub decisions: Vec<CheckAccessResponse>,
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq, Hash)]
pub enum RelationshipTargetType {
    #[default]
    Unspecified,
    Subject,
    Resource,
    SubjectSet,
    Unknown(i32),
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq, Hash)]
pub enum SourceLayer {
    #[default]
    Unspecified,
    StaticConfig,
    Runtime,
    Unknown(i32),
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq, Hash)]
pub enum DefaultAccessPolicy {
    #[default]
    Deny,
    Allow,
    Unknown(i32),
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct RelationshipFilter {
    pub target: Option<RelationshipTarget>,
    pub relation: String,
    pub resource: Option<Resource>,
    pub target_type: RelationshipTargetType,
    pub target_entity_type: String,
    pub resource_type: String,
    pub source_layer: SourceLayer,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct ListRelationshipsRequest {
    pub filter: Option<RelationshipFilter>,
    pub page_size: i32,
    pub page_token: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct ListRelationshipsResponse {
    pub relationships: Vec<Relationship>,
    pub next_page_token: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct AddRelationshipRequest {
    pub relationship: Option<Relationship>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct AddRelationshipResponse {
    pub relationship: Option<Relationship>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct DeleteRelationshipRequest {
    pub relationship_tuple: Option<RelationshipTuple>,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct DeleteRelationshipResponse {}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct SetAuthorizationStateRequest {
    pub model: Option<Model>,
    pub relationships: Vec<Relationship>,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct SetAuthorizationStateResponse {
    pub active_model: Option<ModelRef>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct Relationship {
    pub tuple: Option<RelationshipTuple>,
    pub properties: Option<JsonObject>,
    pub source_layer: SourceLayer,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct RelationshipTuple {
    pub target: Option<RelationshipTarget>,
    pub relation: String,
    pub resource: Option<Resource>,
}

#[derive(Clone, Debug, PartialEq)]
pub enum RelationshipTarget {
    Subject(Subject),
    Resource(Resource),
    SubjectSet(SubjectSet),
    Unset,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct SubjectSet {
    pub resource: Option<Resource>,
    pub relation: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct Model {
    pub id: String,
    pub version: String,
    pub resource_types: Vec<ModelResourceType>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct ModelResourceType {
    pub name: String,
    pub relations: Vec<ModelRelation>,
    pub actions: Vec<ModelAction>,
    pub source_layer: SourceLayer,
    pub default_access_policy: DefaultAccessPolicy,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct ModelRelation {
    pub name: String,
    pub allowed_targets: Vec<ModelAllowedTarget>,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct ModelAction {
    pub name: String,
    pub relations: Vec<String>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ModelAllowedTarget {
    SubjectType(String),
    ResourceType(String),
    SubjectSetType(SubjectSetType),
    Unset,
}

#[derive(Clone, Debug, Default, Eq, PartialEq, Hash)]
pub struct SubjectSetType {
    pub resource_type: String,
    pub relation: String,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct ModelRef {
    pub id: String,
    pub version: String,
    pub created_at: Option<SystemTime>,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct GetActiveModelRefResponse {
    pub model: Option<ModelRef>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct SetActiveModelRequest {
    pub model: Option<Model>,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct SetActiveModelResponse {
    pub model: Option<ModelRef>,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct ModelResourceTypeFilter {
    pub name: String,
    pub source_layer: SourceLayer,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct ListActiveModelResourceTypesRequest {
    pub filter: Option<ModelResourceTypeFilter>,
    pub page_size: i32,
    pub page_token: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct ListActiveModelResourceTypesResponse {
    pub resource_types: Vec<ModelResourceType>,
    pub next_page_token: String,
    pub model_id: String,
}

#[async_trait]
/// Fakeable client contract for Authorization host-service calls.
pub trait AuthorizationContract: Send {
    async fn check_access(
        &mut self,
        request: CheckAccessRequest,
    ) -> Result<CheckAccessResponse, AuthorizationError>;

    async fn check_access_many(
        &mut self,
        request: CheckAccessManyRequest,
    ) -> Result<CheckAccessManyResponse, AuthorizationError>;

    async fn list_relationships(
        &mut self,
        request: ListRelationshipsRequest,
    ) -> Result<ListRelationshipsResponse, AuthorizationError>;

    async fn add_relationship(
        &mut self,
        request: AddRelationshipRequest,
    ) -> Result<AddRelationshipResponse, AuthorizationError>;

    async fn delete_relationship(
        &mut self,
        request: DeleteRelationshipRequest,
    ) -> Result<DeleteRelationshipResponse, AuthorizationError>;

    async fn set_authorization_state(
        &mut self,
        request: SetAuthorizationStateRequest,
    ) -> Result<SetAuthorizationStateResponse, AuthorizationError>;

    async fn get_active_model_ref(
        &mut self,
    ) -> Result<GetActiveModelRefResponse, AuthorizationError>;

    async fn set_active_model(
        &mut self,
        request: SetActiveModelRequest,
    ) -> Result<SetActiveModelResponse, AuthorizationError>;

    async fn list_active_model_resource_types(
        &mut self,
        request: ListActiveModelResourceTypesRequest,
    ) -> Result<ListActiveModelResourceTypesResponse, AuthorizationError>;
}

/// Async client for Gestalt Authorization host-service calls.
pub struct Client {
    client: ProtoAuthorizationProviderClient<AuthorizationTransport>,
}

impl Client {
    /// Connects to the Authorization host service using Gestalt environment variables.
    pub async fn connect(_request: &Request) -> Result<Self, AuthorizationError> {
        let target = std::env::var(ENV_HOST_SERVICE_SOCKET).map_err(|_| {
            AuthorizationError::Env(format!("{ENV_HOST_SERVICE_SOCKET} is not set"))
        })?;
        let relay_token = std::env::var(ENV_HOST_SERVICE_TOKEN).unwrap_or_default();
        Self::connect_target(&target, relay_token.trim()).await
    }

    /// Connects to an explicit host-service target.
    pub async fn connect_target(
        target: &str,
        relay_token: &str,
    ) -> Result<Self, AuthorizationError> {
        Ok(Self {
            client: ProtoAuthorizationProviderClient::new(
                host_service::connect("authorization", target, relay_token, None).await?,
            ),
        })
    }

    pub async fn check_access(
        &mut self,
        request: CheckAccessRequest,
    ) -> Result<CheckAccessResponse, AuthorizationError> {
        let response = self
            .client
            .check_access(check_access_request_to_proto(request))
            .await?
            .into_inner();
        check_access_response_from_proto(response)
    }

    pub async fn check_access_many(
        &mut self,
        request: CheckAccessManyRequest,
    ) -> Result<CheckAccessManyResponse, AuthorizationError> {
        let response = self
            .client
            .check_access_many(check_access_many_request_to_proto(request))
            .await?
            .into_inner();
        check_access_many_response_from_proto(response)
    }

    pub async fn list_relationships(
        &mut self,
        request: ListRelationshipsRequest,
    ) -> Result<ListRelationshipsResponse, AuthorizationError> {
        let response = self
            .client
            .list_relationships(list_relationships_request_to_proto(request))
            .await?
            .into_inner();
        list_relationships_response_from_proto(response)
    }

    pub async fn add_relationship(
        &mut self,
        request: AddRelationshipRequest,
    ) -> Result<AddRelationshipResponse, AuthorizationError> {
        let response = self
            .client
            .add_relationship(add_relationship_request_to_proto(request))
            .await?
            .into_inner();
        add_relationship_response_from_proto(response)
    }

    pub async fn delete_relationship(
        &mut self,
        request: DeleteRelationshipRequest,
    ) -> Result<DeleteRelationshipResponse, AuthorizationError> {
        self.client
            .delete_relationship(delete_relationship_request_to_proto(request))
            .await?;
        Ok(DeleteRelationshipResponse {})
    }

    pub async fn set_authorization_state(
        &mut self,
        request: SetAuthorizationStateRequest,
    ) -> Result<SetAuthorizationStateResponse, AuthorizationError> {
        let response = self
            .client
            .set_authorization_state(set_authorization_state_request_to_proto(request))
            .await?
            .into_inner();
        set_authorization_state_response_from_proto(response)
    }

    pub async fn get_active_model_ref(
        &mut self,
    ) -> Result<GetActiveModelRefResponse, AuthorizationError> {
        let response = self.client.get_active_model_ref(()).await?.into_inner();
        get_active_model_ref_response_from_proto(response)
    }

    pub async fn set_active_model(
        &mut self,
        request: SetActiveModelRequest,
    ) -> Result<SetActiveModelResponse, AuthorizationError> {
        let response = self
            .client
            .set_active_model(set_active_model_request_to_proto(request))
            .await?
            .into_inner();
        set_active_model_response_from_proto(response)
    }

    pub async fn list_active_model_resource_types(
        &mut self,
        request: ListActiveModelResourceTypesRequest,
    ) -> Result<ListActiveModelResourceTypesResponse, AuthorizationError> {
        let response = self
            .client
            .list_active_model_resource_types(list_active_model_resource_types_request_to_proto(
                request,
            ))
            .await?
            .into_inner();
        list_active_model_resource_types_response_from_proto(response)
    }
}

#[async_trait]
impl AuthorizationContract for Client {
    async fn check_access(
        &mut self,
        request: CheckAccessRequest,
    ) -> Result<CheckAccessResponse, AuthorizationError> {
        Client::check_access(self, request).await
    }

    async fn check_access_many(
        &mut self,
        request: CheckAccessManyRequest,
    ) -> Result<CheckAccessManyResponse, AuthorizationError> {
        Client::check_access_many(self, request).await
    }

    async fn list_relationships(
        &mut self,
        request: ListRelationshipsRequest,
    ) -> Result<ListRelationshipsResponse, AuthorizationError> {
        Client::list_relationships(self, request).await
    }

    async fn add_relationship(
        &mut self,
        request: AddRelationshipRequest,
    ) -> Result<AddRelationshipResponse, AuthorizationError> {
        Client::add_relationship(self, request).await
    }

    async fn delete_relationship(
        &mut self,
        request: DeleteRelationshipRequest,
    ) -> Result<DeleteRelationshipResponse, AuthorizationError> {
        Client::delete_relationship(self, request).await
    }

    async fn set_authorization_state(
        &mut self,
        request: SetAuthorizationStateRequest,
    ) -> Result<SetAuthorizationStateResponse, AuthorizationError> {
        Client::set_authorization_state(self, request).await
    }

    async fn get_active_model_ref(
        &mut self,
    ) -> Result<GetActiveModelRefResponse, AuthorizationError> {
        Client::get_active_model_ref(self).await
    }

    async fn set_active_model(
        &mut self,
        request: SetActiveModelRequest,
    ) -> Result<SetActiveModelResponse, AuthorizationError> {
        Client::set_active_model(self, request).await
    }

    async fn list_active_model_resource_types(
        &mut self,
        request: ListActiveModelResourceTypesRequest,
    ) -> Result<ListActiveModelResourceTypesResponse, AuthorizationError> {
        Client::list_active_model_resource_types(self, request).await
    }
}

fn check_access_request_to_proto(value: CheckAccessRequest) -> pb::CheckAccessRequest {
    pb::CheckAccessRequest {
        subject: value.subject.map(subject_to_proto),
        action: value.action.map(action_to_proto),
        resource: value.resource.map(resource_to_proto),
    }
}

fn check_access_response_from_proto(
    value: pb::CheckAccessResponse,
) -> Result<CheckAccessResponse, AuthorizationError> {
    Ok(CheckAccessResponse {
        allowed: value.allowed,
        model_id: value.model_id,
    })
}

fn check_access_many_request_to_proto(value: CheckAccessManyRequest) -> pb::CheckAccessManyRequest {
    pb::CheckAccessManyRequest {
        requests: value
            .requests
            .into_iter()
            .map(check_access_request_to_proto)
            .collect(),
    }
}

fn check_access_many_response_from_proto(
    value: pb::CheckAccessManyResponse,
) -> Result<CheckAccessManyResponse, AuthorizationError> {
    Ok(CheckAccessManyResponse {
        decisions: value
            .decisions
            .into_iter()
            .map(check_access_response_from_proto)
            .collect::<Result<Vec<_>, _>>()?,
    })
}

fn list_relationships_request_to_proto(
    value: ListRelationshipsRequest,
) -> pb::ListRelationshipsRequest {
    pb::ListRelationshipsRequest {
        filter: value.filter.map(relationship_filter_to_proto),
        page_size: value.page_size,
        page_token: value.page_token,
    }
}

fn list_relationships_response_from_proto(
    value: pb::ListRelationshipsResponse,
) -> Result<ListRelationshipsResponse, AuthorizationError> {
    Ok(ListRelationshipsResponse {
        relationships: value
            .relationships
            .into_iter()
            .map(relationship_from_proto)
            .collect::<Result<Vec<_>, _>>()?,
        next_page_token: value.next_page_token,
    })
}

fn add_relationship_request_to_proto(value: AddRelationshipRequest) -> pb::AddRelationshipRequest {
    pb::AddRelationshipRequest {
        relationship: value.relationship.map(relationship_to_proto),
    }
}

fn add_relationship_response_from_proto(
    value: pb::AddRelationshipResponse,
) -> Result<AddRelationshipResponse, AuthorizationError> {
    Ok(AddRelationshipResponse {
        relationship: value
            .relationship
            .map(relationship_from_proto)
            .transpose()?,
    })
}

fn delete_relationship_request_to_proto(
    value: DeleteRelationshipRequest,
) -> pb::DeleteRelationshipRequest {
    pb::DeleteRelationshipRequest {
        relationship_tuple: value.relationship_tuple.map(relationship_tuple_to_proto),
    }
}

fn set_authorization_state_request_to_proto(
    value: SetAuthorizationStateRequest,
) -> pb::SetAuthorizationStateRequest {
    pb::SetAuthorizationStateRequest {
        model: value.model.map(model_to_proto),
        relationships: value
            .relationships
            .into_iter()
            .map(relationship_to_proto)
            .collect(),
    }
}

fn set_authorization_state_response_from_proto(
    value: pb::SetAuthorizationStateResponse,
) -> Result<SetAuthorizationStateResponse, AuthorizationError> {
    Ok(SetAuthorizationStateResponse {
        active_model: value.active_model.map(model_ref_from_proto).transpose()?,
    })
}

fn get_active_model_ref_response_from_proto(
    value: pb::GetActiveModelRefResponse,
) -> Result<GetActiveModelRefResponse, AuthorizationError> {
    Ok(GetActiveModelRefResponse {
        model: value.model.map(model_ref_from_proto).transpose()?,
    })
}

fn set_active_model_request_to_proto(value: SetActiveModelRequest) -> pb::SetActiveModelRequest {
    pb::SetActiveModelRequest {
        model: value.model.map(model_to_proto),
    }
}

fn set_active_model_response_from_proto(
    value: pb::SetActiveModelResponse,
) -> Result<SetActiveModelResponse, AuthorizationError> {
    Ok(SetActiveModelResponse {
        model: value.model.map(model_ref_from_proto).transpose()?,
    })
}

fn list_active_model_resource_types_request_to_proto(
    value: ListActiveModelResourceTypesRequest,
) -> pb::ListActiveModelResourceTypesRequest {
    pb::ListActiveModelResourceTypesRequest {
        filter: value.filter.map(model_resource_type_filter_to_proto),
        page_size: value.page_size,
        page_token: value.page_token,
    }
}

fn list_active_model_resource_types_response_from_proto(
    value: pb::ListActiveModelResourceTypesResponse,
) -> Result<ListActiveModelResourceTypesResponse, AuthorizationError> {
    Ok(ListActiveModelResourceTypesResponse {
        resource_types: value
            .resource_types
            .into_iter()
            .map(model_resource_type_from_proto)
            .collect::<Result<Vec<_>, _>>()?,
        next_page_token: value.next_page_token,
        model_id: value.model_id,
    })
}

fn subject_to_proto(value: Subject) -> pb::Subject {
    pb::Subject {
        r#type: value.r#type,
        id: value.id,
        properties: value.properties.map(object_to_struct),
    }
}

fn subject_from_proto(value: pb::Subject) -> Subject {
    Subject {
        r#type: value.r#type,
        id: value.id,
        properties: value.properties.as_ref().map(object_from_struct),
    }
}

fn action_to_proto(value: Action) -> pb::Action {
    pb::Action {
        name: value.name,
        properties: value.properties.map(object_to_struct),
    }
}

fn resource_to_proto(value: Resource) -> pb::Resource {
    pb::Resource {
        r#type: value.r#type,
        id: value.id,
        properties: value.properties.map(object_to_struct),
    }
}

fn resource_from_proto(value: pb::Resource) -> Resource {
    Resource {
        r#type: value.r#type,
        id: value.id,
        properties: value.properties.as_ref().map(object_from_struct),
    }
}

fn relationship_filter_to_proto(value: RelationshipFilter) -> pb::RelationshipFilter {
    pb::RelationshipFilter {
        target: value.target.map(relationship_target_to_proto),
        relation: value.relation,
        resource: value.resource.map(resource_to_proto),
        target_type: relationship_target_type_to_proto(value.target_type),
        target_entity_type: value.target_entity_type,
        resource_type: value.resource_type,
        source_layer: source_layer_to_proto(value.source_layer),
    }
}

fn relationship_to_proto(value: Relationship) -> pb::Relationship {
    pb::Relationship {
        tuple: value.tuple.map(relationship_tuple_to_proto),
        properties: value.properties.map(object_to_struct),
        source_layer: source_layer_to_proto(value.source_layer),
    }
}

fn relationship_from_proto(value: pb::Relationship) -> Result<Relationship, AuthorizationError> {
    Ok(Relationship {
        tuple: value.tuple.map(relationship_tuple_from_proto).transpose()?,
        properties: value.properties.as_ref().map(object_from_struct),
        source_layer: source_layer_from_proto(value.source_layer),
    })
}

fn relationship_tuple_to_proto(value: RelationshipTuple) -> pb::RelationshipTuple {
    pb::RelationshipTuple {
        target: value.target.map(relationship_target_to_proto),
        relation: value.relation,
        resource: value.resource.map(resource_to_proto),
    }
}

fn relationship_tuple_from_proto(
    value: pb::RelationshipTuple,
) -> Result<RelationshipTuple, AuthorizationError> {
    Ok(RelationshipTuple {
        target: value
            .target
            .map(relationship_target_from_proto)
            .transpose()?,
        relation: value.relation,
        resource: value.resource.map(resource_from_proto),
    })
}

fn relationship_target_to_proto(value: RelationshipTarget) -> pb::RelationshipTarget {
    let kind = match value {
        RelationshipTarget::Subject(value) => {
            pb::relationship_target::Kind::Subject(subject_to_proto(value))
        }
        RelationshipTarget::Resource(value) => {
            pb::relationship_target::Kind::Resource(resource_to_proto(value))
        }
        RelationshipTarget::SubjectSet(value) => {
            pb::relationship_target::Kind::SubjectSet(subject_set_to_proto(value))
        }
        RelationshipTarget::Unset => return pb::RelationshipTarget { kind: None },
    };
    pb::RelationshipTarget { kind: Some(kind) }
}

fn relationship_target_from_proto(
    value: pb::RelationshipTarget,
) -> Result<RelationshipTarget, AuthorizationError> {
    match value.kind {
        Some(pb::relationship_target::Kind::Subject(value)) => {
            Ok(RelationshipTarget::Subject(subject_from_proto(value)))
        }
        Some(pb::relationship_target::Kind::Resource(value)) => {
            Ok(RelationshipTarget::Resource(resource_from_proto(value)))
        }
        Some(pb::relationship_target::Kind::SubjectSet(value)) => Ok(
            RelationshipTarget::SubjectSet(subject_set_from_proto(value)),
        ),
        None => Ok(RelationshipTarget::Unset),
    }
}

fn subject_set_to_proto(value: SubjectSet) -> pb::SubjectSet {
    pb::SubjectSet {
        resource: value.resource.map(resource_to_proto),
        relation: value.relation,
    }
}

fn subject_set_from_proto(value: pb::SubjectSet) -> SubjectSet {
    SubjectSet {
        resource: value.resource.map(resource_from_proto),
        relation: value.relation,
    }
}

fn model_to_proto(value: Model) -> pb::AuthorizationModel {
    pb::AuthorizationModel {
        id: value.id,
        version: value.version,
        resource_types: value
            .resource_types
            .into_iter()
            .map(model_resource_type_to_proto)
            .collect(),
    }
}

fn model_resource_type_to_proto(value: ModelResourceType) -> pb::AuthorizationModelResourceType {
    pb::AuthorizationModelResourceType {
        name: value.name,
        relations: value
            .relations
            .into_iter()
            .map(model_relation_to_proto)
            .collect(),
        actions: value
            .actions
            .into_iter()
            .map(model_action_to_proto)
            .collect(),
        source_layer: source_layer_to_proto(value.source_layer),
        default_access_policy: default_access_policy_to_proto(value.default_access_policy),
    }
}

fn model_resource_type_from_proto(
    value: pb::AuthorizationModelResourceType,
) -> Result<ModelResourceType, AuthorizationError> {
    Ok(ModelResourceType {
        name: value.name,
        relations: value
            .relations
            .into_iter()
            .map(model_relation_from_proto)
            .collect::<Result<Vec<_>, _>>()?,
        actions: value
            .actions
            .into_iter()
            .map(model_action_from_proto)
            .collect(),
        source_layer: source_layer_from_proto(value.source_layer),
        default_access_policy: default_access_policy_from_proto(value.default_access_policy),
    })
}

fn model_relation_to_proto(value: ModelRelation) -> pb::ModelRelation {
    pb::ModelRelation {
        name: value.name,
        allowed_targets: value
            .allowed_targets
            .into_iter()
            .map(model_allowed_target_to_proto)
            .collect(),
    }
}

fn model_relation_from_proto(
    value: pb::ModelRelation,
) -> Result<ModelRelation, AuthorizationError> {
    Ok(ModelRelation {
        name: value.name,
        allowed_targets: value
            .allowed_targets
            .into_iter()
            .map(model_allowed_target_from_proto)
            .collect::<Result<Vec<_>, _>>()?,
    })
}

fn model_action_to_proto(value: ModelAction) -> pb::ModelAction {
    pb::ModelAction {
        name: value.name,
        relations: value.relations,
    }
}

fn model_action_from_proto(value: pb::ModelAction) -> ModelAction {
    ModelAction {
        name: value.name,
        relations: value.relations,
    }
}

fn model_allowed_target_to_proto(value: ModelAllowedTarget) -> pb::ModelAllowedTarget {
    let kind = match value {
        ModelAllowedTarget::SubjectType(value) => {
            pb::model_allowed_target::Kind::SubjectType(value)
        }
        ModelAllowedTarget::ResourceType(value) => {
            pb::model_allowed_target::Kind::ResourceType(value)
        }
        ModelAllowedTarget::SubjectSetType(value) => {
            pb::model_allowed_target::Kind::SubjectSetType(subject_set_type_to_proto(value))
        }
        ModelAllowedTarget::Unset => return pb::ModelAllowedTarget { kind: None },
    };
    pb::ModelAllowedTarget { kind: Some(kind) }
}

fn model_allowed_target_from_proto(
    value: pb::ModelAllowedTarget,
) -> Result<ModelAllowedTarget, AuthorizationError> {
    match value.kind {
        Some(pb::model_allowed_target::Kind::SubjectType(value)) => {
            Ok(ModelAllowedTarget::SubjectType(value))
        }
        Some(pb::model_allowed_target::Kind::ResourceType(value)) => {
            Ok(ModelAllowedTarget::ResourceType(value))
        }
        Some(pb::model_allowed_target::Kind::SubjectSetType(value)) => Ok(
            ModelAllowedTarget::SubjectSetType(subject_set_type_from_proto(value)),
        ),
        None => Ok(ModelAllowedTarget::Unset),
    }
}

fn subject_set_type_to_proto(value: SubjectSetType) -> pb::SubjectSetType {
    pb::SubjectSetType {
        resource_type: value.resource_type,
        relation: value.relation,
    }
}

fn subject_set_type_from_proto(value: pb::SubjectSetType) -> SubjectSetType {
    SubjectSetType {
        resource_type: value.resource_type,
        relation: value.relation,
    }
}

fn model_ref_from_proto(value: pb::AuthorizationModelRef) -> Result<ModelRef, AuthorizationError> {
    Ok(ModelRef {
        id: value.id,
        version: value.version,
        created_at: value
            .created_at
            .as_ref()
            .map(protocol::system_time_from_timestamp)
            .transpose()
            .map_err(|err| AuthorizationError::Protocol(err.to_string()))?,
    })
}

fn model_resource_type_filter_to_proto(
    value: ModelResourceTypeFilter,
) -> pb::AuthorizationModelResourceTypeFilter {
    pb::AuthorizationModelResourceTypeFilter {
        name: value.name,
        source_layer: source_layer_to_proto(value.source_layer),
    }
}

fn object_to_struct(value: JsonObject) -> prost_types::Struct {
    protocol::struct_from_map(value)
}

fn object_from_struct(value: &prost_types::Struct) -> JsonObject {
    match protocol::json_from_struct(value) {
        serde_json::Value::Object(fields) => fields,
        _ => JsonObject::default(),
    }
}

fn relationship_target_type_to_proto(value: RelationshipTargetType) -> i32 {
    match value {
        RelationshipTargetType::Unspecified => pb::RelationshipTargetType::Unspecified as i32,
        RelationshipTargetType::Subject => pb::RelationshipTargetType::Subject as i32,
        RelationshipTargetType::Resource => pb::RelationshipTargetType::Resource as i32,
        RelationshipTargetType::SubjectSet => pb::RelationshipTargetType::SubjectSet as i32,
        RelationshipTargetType::Unknown(value) => value,
    }
}

fn source_layer_to_proto(value: SourceLayer) -> i32 {
    match value {
        SourceLayer::Unspecified => pb::SourceLayer::Unspecified as i32,
        SourceLayer::StaticConfig => pb::SourceLayer::StaticConfig as i32,
        SourceLayer::Runtime => pb::SourceLayer::Runtime as i32,
        SourceLayer::Unknown(value) => value,
    }
}

fn source_layer_from_proto(value: i32) -> SourceLayer {
    match pb::SourceLayer::try_from(value) {
        Ok(pb::SourceLayer::Unspecified) => SourceLayer::Unspecified,
        Ok(pb::SourceLayer::StaticConfig) => SourceLayer::StaticConfig,
        Ok(pb::SourceLayer::Runtime) => SourceLayer::Runtime,
        Err(_) => SourceLayer::Unknown(value),
    }
}

fn default_access_policy_to_proto(value: DefaultAccessPolicy) -> i32 {
    match value {
        DefaultAccessPolicy::Deny => pb::DefaultAccessPolicy::Deny as i32,
        DefaultAccessPolicy::Allow => pb::DefaultAccessPolicy::Allow as i32,
        DefaultAccessPolicy::Unknown(value) => value,
    }
}

fn default_access_policy_from_proto(value: i32) -> DefaultAccessPolicy {
    match pb::DefaultAccessPolicy::try_from(value) {
        Ok(pb::DefaultAccessPolicy::Deny) => DefaultAccessPolicy::Deny,
        Ok(pb::DefaultAccessPolicy::Allow) => DefaultAccessPolicy::Allow,
        Err(_) => DefaultAccessPolicy::Unknown(value),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn preserves_unknown_enum_values() {
        assert_eq!(source_layer_from_proto(99), SourceLayer::Unknown(99));
        assert_eq!(
            default_access_policy_to_proto(DefaultAccessPolicy::Unknown(42)),
            42
        );
        assert_eq!(
            relationship_target_type_to_proto(RelationshipTargetType::Unknown(77)),
            77
        );
    }

    #[test]
    fn converts_relationship_target_oneof() {
        let target = RelationshipTarget::Subject(Subject {
            r#type: "user".to_string(),
            id: "u1".to_string(),
            properties: None,
        });

        let roundtrip =
            relationship_target_from_proto(relationship_target_to_proto(target.clone()))
                .expect("relationship target should convert");

        assert_eq!(roundtrip, target);
    }

    #[test]
    fn converts_model_allowed_target_oneof() {
        let target = ModelAllowedTarget::SubjectSetType(SubjectSetType {
            resource_type: "document".to_string(),
            relation: "owner".to_string(),
        });

        let roundtrip =
            model_allowed_target_from_proto(model_allowed_target_to_proto(target.clone()))
                .expect("allowed target should convert");

        assert_eq!(roundtrip, target);
    }

    #[test]
    fn preserves_absent_struct_properties() {
        let subject = Subject {
            r#type: "user".to_string(),
            id: "u1".to_string(),
            properties: None,
        };

        let wire = subject_to_proto(subject);
        assert!(wire.properties.is_none());

        let native = subject_from_proto(wire);
        assert_eq!(native.properties, None);
    }

    #[test]
    fn converts_unset_oneofs() {
        assert_eq!(
            relationship_target_from_proto(
                relationship_target_to_proto(RelationshipTarget::Unset,)
            )
            .expect("unset relationship target should convert"),
            RelationshipTarget::Unset,
        );
        assert_eq!(
            model_allowed_target_from_proto(model_allowed_target_to_proto(
                ModelAllowedTarget::Unset,
            ))
            .expect("unset allowed target should convert"),
            ModelAllowedTarget::Unset,
        );
    }

    #[test]
    fn json_object_rejects_non_object_values() {
        let error = json_object(["not", "an", "object"]).expect_err("array should fail");
        assert!(matches!(error, AuthorizationError::Protocol(_)));
    }
}
