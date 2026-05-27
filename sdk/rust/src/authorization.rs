use hyper_util::rt::TokioIo;
use std::sync::Arc;
use std::time::SystemTime;
use tokio::net::UnixStream;

use tonic::codegen::async_trait;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, ClientTlsConfig, Endpoint, Uri};
use tonic::{Request, Request as GrpcRequest, Response as GrpcResponse, Status};
use tower::service_fn;

use crate::api::RuntimeMetadata;
use crate::env::{ENV_HOST_SERVICE_SOCKET, ENV_HOST_SERVICE_TOKEN};
use crate::error::Result as ProviderResult;
use crate::generated::v1::{
    self as pb, authorization_provider_client::AuthorizationProviderClient,
};
use crate::protocol;
use crate::rpc_status::rpc_status;

type AuthorizationTransport = InterceptedService<Channel, RelayTokenInterceptor>;

const AUTHORIZATION_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";
/// Subject type used for canonical Gestalt subject ids in managed grants.
pub const AUTHORIZATION_SUBJECT_TYPE_SUBJECT: &str = "subject";

#[derive(Debug, thiserror::Error)]
/// Errors returned by the authorization host-service client.
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
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Subject in the authorization graph.
///
/// Subjects identify callers or principals, such as `subject:user:123`.
/// Properties are optional JSON metadata passed to authorization providers that
/// support contextual decisions.
pub struct AuthorizationSubject {
    /// Subject type, such as `subject`, `user`, or `service_account`.
    pub r#type: String,
    /// Stable subject identifier within the subject type.
    pub id: String,
    /// Optional JSON metadata associated with the subject.
    pub properties: serde_json::Map<String, serde_json::Value>,
}

impl AuthorizationSubject {
    /// Creates an authorization subject without extra properties.
    pub fn new(r#type: impl Into<String>, id: impl Into<String>) -> Self {
        Self {
            r#type: r#type.into(),
            id: id.into(),
            properties: serde_json::Map::new(),
        }
    }
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Resource in the authorization graph.
///
/// Resources are objects protected by authorization checks, such as a `team`
/// with a concrete team id.
pub struct AuthorizationResource {
    /// Resource type, such as `team`.
    pub r#type: String,
    /// Stable resource identifier within the resource type.
    pub id: String,
    /// Optional JSON metadata associated with the resource.
    pub properties: serde_json::Map<String, serde_json::Value>,
}

impl AuthorizationResource {
    /// Creates an authorization resource without extra properties.
    pub fn new(r#type: impl Into<String>, id: impl Into<String>) -> Self {
        Self {
            r#type: r#type.into(),
            id: id.into(),
            properties: serde_json::Map::new(),
        }
    }
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Subject set in the authorization graph.
pub struct AuthorizationSubjectSet {
    /// Resource that owns the relation.
    pub resource: AuthorizationResource,
    /// Relation on the resource.
    pub relation: String,
}

impl AuthorizationSubjectSet {
    /// Creates an authorization subject set.
    pub fn new(resource: AuthorizationResource, relation: impl Into<String>) -> Self {
        Self {
            resource,
            relation: relation.into(),
        }
    }
}

#[derive(Clone, Debug, PartialEq)]
/// Target on the left side of an authorization relationship.
pub enum AuthorizationRelationshipTarget {
    /// Concrete subject target.
    Subject(AuthorizationSubject),
    /// Resource target.
    Resource(AuthorizationResource),
    /// Subject-set target.
    SubjectSet(AuthorizationSubjectSet),
}

impl Default for AuthorizationRelationshipTarget {
    fn default() -> Self {
        Self::Subject(AuthorizationSubject::default())
    }
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Action requested against an authorization resource.
pub struct AuthorizationAction {
    /// Action name, such as `view`, `edit`, or `assume`.
    pub name: String,
    /// Optional JSON metadata associated with the action.
    pub properties: serde_json::Map<String, serde_json::Value>,
}

impl AuthorizationAction {
    /// Creates an authorization action without extra properties.
    pub fn new(name: impl Into<String>) -> Self {
        Self {
            name: name.into(),
            properties: serde_json::Map::new(),
        }
    }
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Request asking whether a subject can perform an action on a resource.
pub struct AccessEvaluationRequest {
    /// Subject requesting access.
    pub subject: AuthorizationSubject,
    /// Action the subject wants to perform.
    pub action: AuthorizationAction,
    /// Resource being protected.
    pub resource: AuthorizationResource,
    /// Optional JSON context for providers that support contextual decisions.
    pub context: serde_json::Map<String, serde_json::Value>,
}

impl AccessEvaluationRequest {
    /// Creates an access-evaluation request without contextual metadata.
    pub fn new(
        subject: AuthorizationSubject,
        action: AuthorizationAction,
        resource: AuthorizationResource,
    ) -> Self {
        Self {
            subject,
            action,
            resource,
            context: serde_json::Map::new(),
        }
    }
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Result of evaluating one authorization request.
pub struct AccessDecision {
    /// Whether the authorization provider allowed the request.
    pub allowed: bool,
    /// Optional JSON context returned by the provider.
    pub context: serde_json::Map<String, serde_json::Value>,
    /// Authorization model id used to evaluate the request, when reported.
    pub model_id: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Batch of access-evaluation requests.
pub struct AccessEvaluationsRequest {
    /// Requests to evaluate.
    pub requests: Vec<AccessEvaluationRequest>,
}

impl From<Vec<AccessEvaluationRequest>> for AccessEvaluationsRequest {
    fn from(requests: Vec<AccessEvaluationRequest>) -> Self {
        Self { requests }
    }
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Batch of access-evaluation decisions.
pub struct AccessEvaluationsResponse {
    /// Decisions in request order.
    pub decisions: Vec<AccessDecision>,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Request searching resources visible to a subject for an action.
pub struct ResourceSearchRequest {
    /// Subject whose visible resources are being searched.
    pub subject: AuthorizationSubject,
    /// Action to test against matching resources.
    pub action: AuthorizationAction,
    /// Resource type to return.
    pub resource_type: String,
    /// Optional JSON context for providers that support contextual searches.
    pub context: serde_json::Map<String, serde_json::Value>,
    /// Maximum number of resources to return.
    pub page_size: i32,
    /// Opaque page token returned by a previous search.
    pub page_token: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Page of resources returned by [`Authorization::search_resources`].
pub struct ResourceSearchResponse {
    /// Matching resources.
    pub resources: Vec<AuthorizationResource>,
    /// Opaque token for the next result page.
    pub next_page_token: String,
    /// Authorization model id used for the search, when reported.
    pub model_id: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Request searching subjects related to a resource and action.
pub struct SubjectSearchRequest {
    /// Resource used as the search anchor.
    pub resource: AuthorizationResource,
    /// Action to test against matching subjects.
    pub action: AuthorizationAction,
    /// Subject type to return.
    pub subject_type: String,
    /// Optional JSON context for providers that support contextual searches.
    pub context: serde_json::Map<String, serde_json::Value>,
    /// Maximum number of subjects to return.
    pub page_size: i32,
    /// Opaque page token returned by a previous search.
    pub page_token: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Page of subjects returned by [`Authorization::search_subjects`].
pub struct SubjectSearchResponse {
    /// Matching subjects.
    pub subjects: Vec<AuthorizationSubject>,
    /// Opaque token for the next result page.
    pub next_page_token: String,
    /// Authorization model id used for the search, when reported.
    pub model_id: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Request searching effective subjects or subject sets for a resource and action.
pub struct EffectiveSubjectSearchRequest {
    /// Resource used as the search anchor.
    pub resource: AuthorizationResource,
    /// Action to test against matching targets.
    pub action: AuthorizationAction,
    /// Optional JSON context for providers that support contextual searches.
    pub context: serde_json::Map<String, serde_json::Value>,
    /// Maximum number of targets to return.
    pub page_size: i32,
    /// Opaque page token returned by a previous search.
    pub page_token: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Page of effective relationship targets returned by [`Authorization::effective_search_subjects`].
pub struct EffectiveSubjectSearchResponse {
    /// Matching subjects, resources, or subject sets.
    pub targets: Vec<AuthorizationRelationshipTarget>,
    /// Opaque token for the next result page.
    pub next_page_token: String,
    /// Authorization model id used for the search, when reported.
    pub model_id: String,
    /// Whether the provider truncated the target set.
    pub truncated: bool,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Request searching actions available between a subject and resource.
pub struct ActionSearchRequest {
    /// Subject used as the search anchor.
    pub subject: AuthorizationSubject,
    /// Resource used as the search anchor.
    pub resource: AuthorizationResource,
    /// Optional JSON context for providers that support contextual searches.
    pub context: serde_json::Map<String, serde_json::Value>,
    /// Maximum number of actions to return.
    pub page_size: i32,
    /// Opaque page token returned by a previous search.
    pub page_token: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Page of actions returned by [`Authorization::search_actions`].
pub struct ActionSearchResponse {
    /// Matching actions.
    pub actions: Vec<AuthorizationAction>,
    /// Opaque token for the next result page.
    pub next_page_token: String,
    /// Authorization model id used for the search, when reported.
    pub model_id: String,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Metadata reported by the host authorization provider.
pub struct AuthorizationMetadata {
    /// Provider capabilities, such as supported relationship operations.
    pub capabilities: Vec<String>,
    /// Active authorization model id, when reported by the provider.
    pub active_model_id: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Authorization model definition.
pub struct AuthorizationModel {
    /// Authorization model schema version.
    pub version: i32,
    /// Resource types described by the model.
    pub resource_types: Vec<AuthorizationModelResourceType>,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Authorization model resource type.
pub struct AuthorizationModelResourceType {
    /// Resource type name.
    pub name: String,
    /// Relations defined for the resource type.
    pub relations: Vec<AuthorizationModelRelation>,
    /// Actions defined for the resource type.
    pub actions: Vec<AuthorizationModelAction>,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Authorization model relation.
pub struct AuthorizationModelRelation {
    /// Relation name.
    pub name: String,
    /// Legacy subject type names allowed by the relation.
    pub subject_types: Vec<String>,
    /// Structured allowed target definitions.
    pub allowed_targets: Vec<AuthorizationModelAllowedTarget>,
    /// Optional rewrite used to compute the relation.
    pub rewrite: Option<AuthorizationModelRewrite>,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Authorization model action.
pub struct AuthorizationModelAction {
    /// Action name.
    pub name: String,
    /// Relations that imply this action.
    pub relations: Vec<String>,
    /// Optional rewrite used to compute the action.
    pub rewrite: Option<AuthorizationModelRewrite>,
}

#[derive(Clone, Debug, PartialEq)]
/// Authorization model allowed target.
pub enum AuthorizationModelAllowedTarget {
    /// Concrete subject type target.
    SubjectType(String),
    /// Concrete resource type target.
    ResourceType(String),
    /// Subject-set target.
    SubjectSet(AuthorizationModelSubjectSetTarget),
}

impl Default for AuthorizationModelAllowedTarget {
    fn default() -> Self {
        Self::SubjectType(String::new())
    }
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Authorization model subject-set target.
pub struct AuthorizationModelSubjectSetTarget {
    /// Target resource type.
    pub resource_type: String,
    /// Target relation.
    pub relation: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Authorization model rewrite expression.
pub enum AuthorizationModelRewrite {
    /// Directly related targets.
    #[default]
    This,
    /// Computed userset on the same resource.
    ComputedUserset(AuthorizationModelComputedUserset),
    /// Tuple-to-userset rewrite.
    TupleToUserset(AuthorizationModelTupleToUserset),
    /// Union of rewrite branches.
    Union(AuthorizationModelRewriteUnion),
}

/// Authorization model `this` rewrite leaf.
pub type AuthorizationModelRewriteThis = ();

#[derive(Clone, Debug, Default, PartialEq)]
/// Authorization model computed-userset rewrite.
pub struct AuthorizationModelComputedUserset {
    /// Relation to compute on the same resource.
    pub relation: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Authorization model tuple-to-userset rewrite.
pub struct AuthorizationModelTupleToUserset {
    /// Relation used to select intermediate tuples.
    pub tupleset_relation: String,
    /// Relation computed on each intermediate resource.
    pub computed_relation: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Authorization model union rewrite.
pub struct AuthorizationModelRewriteUnion {
    /// Child rewrite branches.
    pub children: Vec<AuthorizationModelRewrite>,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Stored authorization model reference.
pub struct AuthorizationModelRef {
    /// Model id.
    pub id: String,
    /// Model version.
    pub version: String,
    /// Creation time.
    pub created_at: Option<SystemTime>,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Active authorization model response.
pub struct GetActiveModelResponse {
    /// Active model reference.
    pub model: Option<AuthorizationModelRef>,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// List authorization models request.
pub struct ListModelsRequest {
    /// Maximum number of models to return.
    pub page_size: i32,
    /// Opaque page token from a previous list request.
    pub page_token: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// List authorization models response.
pub struct ListModelsResponse {
    /// Matching model references.
    pub models: Vec<AuthorizationModelRef>,
    /// Opaque token for the next page.
    pub next_page_token: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Write authorization model request.
pub struct WriteModelRequest {
    /// Model to store.
    pub model: Option<AuthorizationModel>,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Relationship tuple stored in the authorization graph.
///
/// A relationship grants a subject a relation on a resource, for example
/// `subject:user:123` has `member` on `team:servicing`.
pub struct Relationship {
    /// Subject receiving the relation.
    pub subject: AuthorizationSubject,
    /// Generalized relationship target. When set, providers should use this
    /// instead of `subject`.
    pub target: Option<AuthorizationRelationshipTarget>,
    /// Relation granted to the subject.
    pub relation: String,
    /// Resource on which the relation is granted.
    pub resource: AuthorizationResource,
    /// Optional JSON metadata stored with the relationship.
    pub properties: serde_json::Map<String, serde_json::Value>,
}

impl Relationship {
    /// Creates an authorization relationship without extra properties.
    pub fn new(
        subject: AuthorizationSubject,
        relation: impl Into<String>,
        resource: AuthorizationResource,
    ) -> Self {
        Self {
            subject,
            target: None,
            relation: relation.into(),
            resource,
            properties: serde_json::Map::new(),
        }
    }

    /// Creates a generalized authorization relationship without extra properties.
    pub fn with_target(
        target: AuthorizationRelationshipTarget,
        relation: impl Into<String>,
        resource: AuthorizationResource,
    ) -> Self {
        Self {
            subject: AuthorizationSubject::default(),
            target: Some(target),
            relation: relation.into(),
            resource,
            properties: serde_json::Map::new(),
        }
    }
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Key identifying a relationship tuple to delete.
pub struct RelationshipKey {
    /// Subject on the relationship being deleted.
    pub subject: AuthorizationSubject,
    /// Generalized target on the relationship being deleted.
    pub target: Option<AuthorizationRelationshipTarget>,
    /// Relation on the relationship being deleted.
    pub relation: String,
    /// Resource on the relationship being deleted.
    pub resource: AuthorizationResource,
}

impl RelationshipKey {
    /// Creates an authorization relationship-delete key.
    pub fn new(
        subject: AuthorizationSubject,
        relation: impl Into<String>,
        resource: AuthorizationResource,
    ) -> Self {
        Self {
            subject,
            target: None,
            relation: relation.into(),
            resource,
        }
    }

    /// Creates a generalized authorization relationship-delete key.
    pub fn with_target(
        target: AuthorizationRelationshipTarget,
        relation: impl Into<String>,
        resource: AuthorizationResource,
    ) -> Self {
        Self {
            subject: AuthorizationSubject::default(),
            target: Some(target),
            relation: relation.into(),
            resource,
        }
    }
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Request selecting authorization relationships to read.
pub struct ReadRelationshipsRequest {
    /// Optional subject filter.
    pub subject: Option<AuthorizationSubject>,
    /// Optional generalized target filter.
    pub target: Option<AuthorizationRelationshipTarget>,
    /// Optional relation filter.
    pub relation: String,
    /// Optional resource filter.
    pub resource: Option<AuthorizationResource>,
    /// Maximum number of relationships to return.
    pub page_size: i32,
    /// Opaque page token returned by a previous read.
    pub page_token: String,
    /// Authorization model id to read from. Empty uses the provider default.
    pub model_id: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Page of relationships returned by [`Authorization::read_relationships`].
pub struct ReadRelationshipsResponse {
    /// Matching relationship tuples.
    pub relationships: Vec<Relationship>,
    /// Opaque token for the next result page.
    pub next_page_token: String,
    /// Authorization model id used for the read, when reported.
    pub model_id: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Relationship mutation request.
///
/// `writes` are upserts. `deletes` remove exact relationship keys. `model_id`
/// may be left empty to use the provider's active model.
pub struct WriteRelationshipsRequest {
    /// Relationships to write or upsert.
    pub writes: Vec<Relationship>,
    /// Relationship keys to delete.
    pub deletes: Vec<RelationshipKey>,
    /// Authorization model id to mutate. Empty uses the provider default.
    pub model_id: String,
}

impl WriteRelationshipsRequest {
    /// Creates a write request containing only relationship writes.
    pub fn writes(writes: impl IntoIterator<Item = Relationship>) -> Self {
        Self {
            writes: writes.into_iter().collect(),
            deletes: Vec::new(),
            model_id: String::new(),
        }
    }
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Request expanding one resource relation into contributing targets.
pub struct ExpandRequest {
    /// Resource used as the expansion root.
    pub resource: AuthorizationResource,
    /// Relation to expand.
    pub relation: String,
    /// Optional JSON context for providers that support contextual expansion.
    pub context: serde_json::Map<String, serde_json::Value>,
    /// Maximum expansion depth.
    pub max_depth: i32,
    /// Authorization model id to expand. Empty uses the provider default.
    pub model_id: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Node in an expanded authorization relationship graph.
pub struct ExpandNode {
    /// Target represented by this node.
    pub target: Option<AuthorizationRelationshipTarget>,
    /// Relation used at this node.
    pub relation: String,
    /// Child expansion nodes.
    pub children: Vec<ExpandNode>,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Expanded authorization relationship graph.
pub struct ExpandResponse {
    /// Root expansion node.
    pub root: Option<ExpandNode>,
    /// Whether the provider truncated the response.
    pub truncated: bool,
    /// Whether the provider detected a graph cycle.
    pub cycle_detected: bool,
    /// Whether expansion stopped at `max_depth`.
    pub max_depth_reached: bool,
    /// Authorization model id used for expansion, when reported.
    pub model_id: String,
}

#[async_trait]
/// Fakeable client contract for host authorization calls.
pub trait AuthorizationApi: Send {
    async fn evaluate(
        &mut self,
        request: AccessEvaluationRequest,
    ) -> std::result::Result<AccessDecision, AuthorizationError>;

    async fn evaluate_many(
        &mut self,
        request: AccessEvaluationsRequest,
    ) -> std::result::Result<AccessEvaluationsResponse, AuthorizationError>;

    async fn search_resources(
        &mut self,
        request: ResourceSearchRequest,
    ) -> std::result::Result<ResourceSearchResponse, AuthorizationError>;

    async fn search_subjects(
        &mut self,
        request: SubjectSearchRequest,
    ) -> std::result::Result<SubjectSearchResponse, AuthorizationError>;

    async fn effective_search_resources(
        &mut self,
        request: ResourceSearchRequest,
    ) -> std::result::Result<ResourceSearchResponse, AuthorizationError>;

    async fn effective_search_subjects(
        &mut self,
        request: EffectiveSubjectSearchRequest,
    ) -> std::result::Result<EffectiveSubjectSearchResponse, AuthorizationError>;

    async fn search_actions(
        &mut self,
        request: ActionSearchRequest,
    ) -> std::result::Result<ActionSearchResponse, AuthorizationError>;

    async fn expand(
        &mut self,
        request: ExpandRequest,
    ) -> std::result::Result<ExpandResponse, AuthorizationError>;

    async fn read_relationships(
        &mut self,
        request: ReadRelationshipsRequest,
    ) -> std::result::Result<ReadRelationshipsResponse, AuthorizationError>;

    async fn write_relationships(
        &mut self,
        request: WriteRelationshipsRequest,
    ) -> std::result::Result<(), AuthorizationError>;

    async fn get_metadata(
        &mut self,
    ) -> std::result::Result<AuthorizationMetadata, AuthorizationError>;

    async fn get_active_model(
        &mut self,
    ) -> std::result::Result<GetActiveModelResponse, AuthorizationError>;

    async fn list_models(
        &mut self,
        request: ListModelsRequest,
    ) -> std::result::Result<ListModelsResponse, AuthorizationError>;

    async fn write_model(
        &mut self,
        request: WriteModelRequest,
    ) -> std::result::Result<AuthorizationModelRef, AuthorizationError>;
}

/// Client for the host-configured authorization provider.
///
/// The client exposes typed SDK values and keeps transport conversion inside
/// the SDK. Providers can use it to evaluate access, search relationships, and
/// write relationship grants.
pub struct Authorization {
    client: AuthorizationProviderClient<AuthorizationTransport>,
}

impl Authorization {
    /// Connects to the authorization host-service target from the environment.
    pub async fn connect() -> std::result::Result<Self, AuthorizationError> {
        let target = std::env::var(ENV_HOST_SERVICE_SOCKET).map_err(|_| {
            AuthorizationError::Env(format!("{ENV_HOST_SERVICE_SOCKET} is not set"))
        })?;
        let relay_token = std::env::var(ENV_HOST_SERVICE_TOKEN).unwrap_or_default();
        Self::connect_target(target, relay_token).await
    }

    /// Connects to an explicit authorization host-service target.
    ///
    /// `target` may be a Unix socket path, `unix://` path, `tcp://host:port`, or
    /// `tls://host:port`. `relay_token` is attached as host-service metadata
    /// when non-empty.
    pub async fn connect_target(
        target: impl AsRef<str>,
        relay_token: impl AsRef<str>,
    ) -> std::result::Result<Self, AuthorizationError> {
        let channel = match parse_authorization_target(target.as_ref())? {
            AuthorizationTarget::Unix(path) => {
                Endpoint::try_from("http://[::]:50051")?
                    .connect_with_connector(service_fn(move |_: Uri| {
                        let path = path.clone();
                        async move { UnixStream::connect(path).await.map(TokioIo::new) }
                    }))
                    .await?
            }
            AuthorizationTarget::Tcp(address) => {
                Endpoint::from_shared(format!("http://{address}"))?
                    .connect()
                    .await?
            }
            AuthorizationTarget::Tls(address) => {
                Endpoint::from_shared(format!("https://{address}"))?
                    .tls_config(ClientTlsConfig::new().with_native_roots())?
                    .connect()
                    .await?
            }
        };

        Ok(Self {
            client: AuthorizationProviderClient::with_interceptor(
                channel,
                relay_token_interceptor(relay_token.as_ref().trim())?,
            ),
        })
    }

    /// Evaluates one access request.
    pub async fn evaluate(
        &mut self,
        request: AccessEvaluationRequest,
    ) -> std::result::Result<AccessDecision, AuthorizationError> {
        Ok(self
            .client
            .evaluate(pb::AccessEvaluationRequest::from(request))
            .await?
            .into_inner()
            .into())
    }

    /// Evaluates multiple access requests in one RPC.
    pub async fn evaluate_many(
        &mut self,
        request: AccessEvaluationsRequest,
    ) -> std::result::Result<AccessEvaluationsResponse, AuthorizationError> {
        Ok(self
            .client
            .evaluate_many(pb::AccessEvaluationsRequest::from(request))
            .await?
            .into_inner()
            .into())
    }

    /// Searches resources visible to a subject for an action.
    pub async fn search_resources(
        &mut self,
        request: ResourceSearchRequest,
    ) -> std::result::Result<ResourceSearchResponse, AuthorizationError> {
        Ok(self
            .client
            .search_resources(pb::ResourceSearchRequest::from(request))
            .await?
            .into_inner()
            .into())
    }

    /// Searches subjects related to a resource and action.
    pub async fn search_subjects(
        &mut self,
        request: SubjectSearchRequest,
    ) -> std::result::Result<SubjectSearchResponse, AuthorizationError> {
        Ok(self
            .client
            .search_subjects(pb::SubjectSearchRequest::from(request))
            .await?
            .into_inner()
            .into())
    }

    /// Searches resources visible to a subject through computed usersets and inherited relationships.
    pub async fn effective_search_resources(
        &mut self,
        request: ResourceSearchRequest,
    ) -> std::result::Result<ResourceSearchResponse, AuthorizationError> {
        Ok(self
            .client
            .effective_search_resources(pb::ResourceSearchRequest::from(request))
            .await?
            .into_inner()
            .into())
    }

    /// Searches effective subjects or subject sets related to a resource and action.
    pub async fn effective_search_subjects(
        &mut self,
        request: EffectiveSubjectSearchRequest,
    ) -> std::result::Result<EffectiveSubjectSearchResponse, AuthorizationError> {
        Ok(self
            .client
            .effective_search_subjects(pb::EffectiveSubjectSearchRequest::from(request))
            .await?
            .into_inner()
            .into())
    }

    /// Searches actions available between a subject and resource.
    pub async fn search_actions(
        &mut self,
        request: ActionSearchRequest,
    ) -> std::result::Result<ActionSearchResponse, AuthorizationError> {
        Ok(self
            .client
            .search_actions(pb::ActionSearchRequest::from(request))
            .await?
            .into_inner()
            .into())
    }

    /// Expands one resource relation into contributing relationship targets.
    pub async fn expand(
        &mut self,
        request: ExpandRequest,
    ) -> std::result::Result<ExpandResponse, AuthorizationError> {
        Ok(self
            .client
            .expand(pb::ExpandRequest::from(request))
            .await?
            .into_inner()
            .into())
    }

    /// Reads authorization relationships matching the supplied filters.
    pub async fn read_relationships(
        &mut self,
        request: ReadRelationshipsRequest,
    ) -> std::result::Result<ReadRelationshipsResponse, AuthorizationError> {
        Ok(self
            .client
            .read_relationships(pb::ReadRelationshipsRequest::from(request))
            .await?
            .into_inner()
            .into())
    }

    /// Writes and deletes authorization relationships.
    pub async fn write_relationships(
        &mut self,
        request: WriteRelationshipsRequest,
    ) -> std::result::Result<(), AuthorizationError> {
        self.client
            .write_relationships(pb::WriteRelationshipsRequest::from(request))
            .await?;
        Ok(())
    }

    /// Returns host authorization provider metadata.
    pub async fn get_metadata(
        &mut self,
    ) -> std::result::Result<AuthorizationMetadata, AuthorizationError> {
        Ok(self.client.get_metadata(()).await?.into_inner().into())
    }

    /// Returns the active authorization model.
    pub async fn get_active_model(
        &mut self,
    ) -> std::result::Result<GetActiveModelResponse, AuthorizationError> {
        Ok(self.client.get_active_model(()).await?.into_inner().into())
    }

    /// Lists authorization models.
    pub async fn list_models(
        &mut self,
        request: ListModelsRequest,
    ) -> std::result::Result<ListModelsResponse, AuthorizationError> {
        Ok(self
            .client
            .list_models(pb::ListModelsRequest::from(request))
            .await?
            .into_inner()
            .into())
    }

    /// Writes an authorization model and returns its reference.
    pub async fn write_model(
        &mut self,
        request: WriteModelRequest,
    ) -> std::result::Result<AuthorizationModelRef, AuthorizationError> {
        Ok(self
            .client
            .write_model(pb::WriteModelRequest::from(request))
            .await?
            .into_inner()
            .into())
    }
}

#[async_trait]
impl AuthorizationApi for Authorization {
    async fn evaluate(
        &mut self,
        request: AccessEvaluationRequest,
    ) -> std::result::Result<AccessDecision, AuthorizationError> {
        Authorization::evaluate(self, request).await
    }

    async fn evaluate_many(
        &mut self,
        request: AccessEvaluationsRequest,
    ) -> std::result::Result<AccessEvaluationsResponse, AuthorizationError> {
        Authorization::evaluate_many(self, request).await
    }

    async fn search_resources(
        &mut self,
        request: ResourceSearchRequest,
    ) -> std::result::Result<ResourceSearchResponse, AuthorizationError> {
        Authorization::search_resources(self, request).await
    }

    async fn search_subjects(
        &mut self,
        request: SubjectSearchRequest,
    ) -> std::result::Result<SubjectSearchResponse, AuthorizationError> {
        Authorization::search_subjects(self, request).await
    }

    async fn effective_search_resources(
        &mut self,
        request: ResourceSearchRequest,
    ) -> std::result::Result<ResourceSearchResponse, AuthorizationError> {
        Authorization::effective_search_resources(self, request).await
    }

    async fn effective_search_subjects(
        &mut self,
        request: EffectiveSubjectSearchRequest,
    ) -> std::result::Result<EffectiveSubjectSearchResponse, AuthorizationError> {
        Authorization::effective_search_subjects(self, request).await
    }

    async fn search_actions(
        &mut self,
        request: ActionSearchRequest,
    ) -> std::result::Result<ActionSearchResponse, AuthorizationError> {
        Authorization::search_actions(self, request).await
    }

    async fn expand(
        &mut self,
        request: ExpandRequest,
    ) -> std::result::Result<ExpandResponse, AuthorizationError> {
        Authorization::expand(self, request).await
    }

    async fn read_relationships(
        &mut self,
        request: ReadRelationshipsRequest,
    ) -> std::result::Result<ReadRelationshipsResponse, AuthorizationError> {
        Authorization::read_relationships(self, request).await
    }

    async fn write_relationships(
        &mut self,
        request: WriteRelationshipsRequest,
    ) -> std::result::Result<(), AuthorizationError> {
        Authorization::write_relationships(self, request).await
    }

    async fn get_metadata(
        &mut self,
    ) -> std::result::Result<AuthorizationMetadata, AuthorizationError> {
        Authorization::get_metadata(self).await
    }

    async fn get_active_model(
        &mut self,
    ) -> std::result::Result<GetActiveModelResponse, AuthorizationError> {
        Authorization::get_active_model(self).await
    }

    async fn list_models(
        &mut self,
        request: ListModelsRequest,
    ) -> std::result::Result<ListModelsResponse, AuthorizationError> {
        Authorization::list_models(self, request).await
    }

    async fn write_model(
        &mut self,
        request: WriteModelRequest,
    ) -> std::result::Result<AuthorizationModelRef, AuthorizationError> {
        Authorization::write_model(self, request).await
    }
}

impl From<AuthorizationSubject> for pb::Subject {
    fn from(value: AuthorizationSubject) -> Self {
        Self {
            r#type: value.r#type,
            id: value.id,
            properties: struct_option_from_map(value.properties),
        }
    }
}

impl From<pb::Subject> for AuthorizationSubject {
    fn from(value: pb::Subject) -> Self {
        Self {
            r#type: value.r#type,
            id: value.id,
            properties: map_from_struct_option(value.properties),
        }
    }
}

impl From<AuthorizationResource> for pb::Resource {
    fn from(value: AuthorizationResource) -> Self {
        Self {
            r#type: value.r#type,
            id: value.id,
            properties: struct_option_from_map(value.properties),
        }
    }
}

impl From<pb::Resource> for AuthorizationResource {
    fn from(value: pb::Resource) -> Self {
        Self {
            r#type: value.r#type,
            id: value.id,
            properties: map_from_struct_option(value.properties),
        }
    }
}

impl From<AuthorizationSubjectSet> for pb::SubjectSet {
    fn from(value: AuthorizationSubjectSet) -> Self {
        Self {
            resource: Some(value.resource.into()),
            relation: value.relation,
        }
    }
}

impl From<pb::SubjectSet> for AuthorizationSubjectSet {
    fn from(value: pb::SubjectSet) -> Self {
        Self {
            resource: value.resource.map(Into::into).unwrap_or_default(),
            relation: value.relation,
        }
    }
}

impl From<AuthorizationRelationshipTarget> for pb::RelationshipTarget {
    fn from(value: AuthorizationRelationshipTarget) -> Self {
        let kind = match value {
            AuthorizationRelationshipTarget::Subject(subject) => {
                pb::relationship_target::Kind::Subject(subject.into())
            }
            AuthorizationRelationshipTarget::Resource(resource) => {
                pb::relationship_target::Kind::Resource(resource.into())
            }
            AuthorizationRelationshipTarget::SubjectSet(subject_set) => {
                pb::relationship_target::Kind::SubjectSet(subject_set.into())
            }
        };
        Self { kind: Some(kind) }
    }
}

impl From<pb::RelationshipTarget> for AuthorizationRelationshipTarget {
    fn from(value: pb::RelationshipTarget) -> Self {
        match value.kind {
            Some(pb::relationship_target::Kind::Subject(subject)) => Self::Subject(subject.into()),
            Some(pb::relationship_target::Kind::Resource(resource)) => {
                Self::Resource(resource.into())
            }
            Some(pb::relationship_target::Kind::SubjectSet(subject_set)) => {
                Self::SubjectSet(subject_set.into())
            }
            None => Self::default(),
        }
    }
}

impl From<AuthorizationAction> for pb::Action {
    fn from(value: AuthorizationAction) -> Self {
        Self {
            name: value.name,
            properties: struct_option_from_map(value.properties),
        }
    }
}

impl From<pb::Action> for AuthorizationAction {
    fn from(value: pb::Action) -> Self {
        Self {
            name: value.name,
            properties: map_from_struct_option(value.properties),
        }
    }
}

impl From<AccessEvaluationRequest> for pb::AccessEvaluationRequest {
    fn from(value: AccessEvaluationRequest) -> Self {
        Self {
            subject: Some(value.subject.into()),
            action: Some(value.action.into()),
            resource: Some(value.resource.into()),
            context: struct_option_from_map(value.context),
        }
    }
}

impl From<pb::AccessEvaluationRequest> for AccessEvaluationRequest {
    fn from(value: pb::AccessEvaluationRequest) -> Self {
        Self {
            subject: value.subject.map(Into::into).unwrap_or_default(),
            action: value.action.map(Into::into).unwrap_or_default(),
            resource: value.resource.map(Into::into).unwrap_or_default(),
            context: map_from_struct_option(value.context),
        }
    }
}

impl From<AccessDecision> for pb::AccessDecision {
    fn from(value: AccessDecision) -> Self {
        Self {
            allowed: value.allowed,
            context: struct_option_from_map(value.context),
            model_id: value.model_id,
        }
    }
}

impl From<pb::AccessDecision> for AccessDecision {
    fn from(value: pb::AccessDecision) -> Self {
        Self {
            allowed: value.allowed,
            context: map_from_struct_option(value.context),
            model_id: value.model_id,
        }
    }
}

impl From<AccessEvaluationsRequest> for pb::AccessEvaluationsRequest {
    fn from(value: AccessEvaluationsRequest) -> Self {
        Self {
            requests: value.requests.into_iter().map(Into::into).collect(),
        }
    }
}

impl From<pb::AccessEvaluationsRequest> for AccessEvaluationsRequest {
    fn from(value: pb::AccessEvaluationsRequest) -> Self {
        Self {
            requests: value.requests.into_iter().map(Into::into).collect(),
        }
    }
}

impl From<AccessEvaluationsResponse> for pb::AccessEvaluationsResponse {
    fn from(value: AccessEvaluationsResponse) -> Self {
        Self {
            decisions: value.decisions.into_iter().map(Into::into).collect(),
        }
    }
}

impl From<pb::AccessEvaluationsResponse> for AccessEvaluationsResponse {
    fn from(value: pb::AccessEvaluationsResponse) -> Self {
        Self {
            decisions: value.decisions.into_iter().map(Into::into).collect(),
        }
    }
}

impl From<ResourceSearchRequest> for pb::ResourceSearchRequest {
    fn from(value: ResourceSearchRequest) -> Self {
        Self {
            subject: Some(value.subject.into()),
            action: Some(value.action.into()),
            resource_type: value.resource_type,
            context: struct_option_from_map(value.context),
            page_size: value.page_size,
            page_token: value.page_token,
        }
    }
}

impl From<pb::ResourceSearchRequest> for ResourceSearchRequest {
    fn from(value: pb::ResourceSearchRequest) -> Self {
        Self {
            subject: value.subject.map(Into::into).unwrap_or_default(),
            action: value.action.map(Into::into).unwrap_or_default(),
            resource_type: value.resource_type,
            context: map_from_struct_option(value.context),
            page_size: value.page_size,
            page_token: value.page_token,
        }
    }
}

impl From<ResourceSearchResponse> for pb::ResourceSearchResponse {
    fn from(value: ResourceSearchResponse) -> Self {
        Self {
            resources: value.resources.into_iter().map(Into::into).collect(),
            next_page_token: value.next_page_token,
            model_id: value.model_id,
        }
    }
}

impl From<pb::ResourceSearchResponse> for ResourceSearchResponse {
    fn from(value: pb::ResourceSearchResponse) -> Self {
        Self {
            resources: value.resources.into_iter().map(Into::into).collect(),
            next_page_token: value.next_page_token,
            model_id: value.model_id,
        }
    }
}

impl From<SubjectSearchRequest> for pb::SubjectSearchRequest {
    fn from(value: SubjectSearchRequest) -> Self {
        Self {
            resource: Some(value.resource.into()),
            action: Some(value.action.into()),
            subject_type: value.subject_type,
            context: struct_option_from_map(value.context),
            page_size: value.page_size,
            page_token: value.page_token,
        }
    }
}

impl From<pb::SubjectSearchRequest> for SubjectSearchRequest {
    fn from(value: pb::SubjectSearchRequest) -> Self {
        Self {
            resource: value.resource.map(Into::into).unwrap_or_default(),
            action: value.action.map(Into::into).unwrap_or_default(),
            subject_type: value.subject_type,
            context: map_from_struct_option(value.context),
            page_size: value.page_size,
            page_token: value.page_token,
        }
    }
}

impl From<SubjectSearchResponse> for pb::SubjectSearchResponse {
    fn from(value: SubjectSearchResponse) -> Self {
        Self {
            subjects: value.subjects.into_iter().map(Into::into).collect(),
            next_page_token: value.next_page_token,
            model_id: value.model_id,
        }
    }
}

impl From<pb::SubjectSearchResponse> for SubjectSearchResponse {
    fn from(value: pb::SubjectSearchResponse) -> Self {
        Self {
            subjects: value.subjects.into_iter().map(Into::into).collect(),
            next_page_token: value.next_page_token,
            model_id: value.model_id,
        }
    }
}

impl From<EffectiveSubjectSearchRequest> for pb::EffectiveSubjectSearchRequest {
    fn from(value: EffectiveSubjectSearchRequest) -> Self {
        Self {
            resource: Some(value.resource.into()),
            action: Some(value.action.into()),
            context: struct_option_from_map(value.context),
            page_size: value.page_size,
            page_token: value.page_token,
        }
    }
}

impl From<pb::EffectiveSubjectSearchRequest> for EffectiveSubjectSearchRequest {
    fn from(value: pb::EffectiveSubjectSearchRequest) -> Self {
        Self {
            resource: value.resource.map(Into::into).unwrap_or_default(),
            action: value.action.map(Into::into).unwrap_or_default(),
            context: map_from_struct_option(value.context),
            page_size: value.page_size,
            page_token: value.page_token,
        }
    }
}

impl From<EffectiveSubjectSearchResponse> for pb::EffectiveSubjectSearchResponse {
    fn from(value: EffectiveSubjectSearchResponse) -> Self {
        Self {
            targets: value.targets.into_iter().map(Into::into).collect(),
            next_page_token: value.next_page_token,
            model_id: value.model_id,
            truncated: value.truncated,
        }
    }
}

impl From<pb::EffectiveSubjectSearchResponse> for EffectiveSubjectSearchResponse {
    fn from(value: pb::EffectiveSubjectSearchResponse) -> Self {
        Self {
            targets: value.targets.into_iter().map(Into::into).collect(),
            next_page_token: value.next_page_token,
            model_id: value.model_id,
            truncated: value.truncated,
        }
    }
}

impl From<ActionSearchRequest> for pb::ActionSearchRequest {
    fn from(value: ActionSearchRequest) -> Self {
        Self {
            subject: Some(value.subject.into()),
            resource: Some(value.resource.into()),
            context: struct_option_from_map(value.context),
            page_size: value.page_size,
            page_token: value.page_token,
        }
    }
}

impl From<pb::ActionSearchRequest> for ActionSearchRequest {
    fn from(value: pb::ActionSearchRequest) -> Self {
        Self {
            subject: value.subject.map(Into::into).unwrap_or_default(),
            resource: value.resource.map(Into::into).unwrap_or_default(),
            context: map_from_struct_option(value.context),
            page_size: value.page_size,
            page_token: value.page_token,
        }
    }
}

impl From<ActionSearchResponse> for pb::ActionSearchResponse {
    fn from(value: ActionSearchResponse) -> Self {
        Self {
            actions: value.actions.into_iter().map(Into::into).collect(),
            next_page_token: value.next_page_token,
            model_id: value.model_id,
        }
    }
}

impl From<pb::ActionSearchResponse> for ActionSearchResponse {
    fn from(value: pb::ActionSearchResponse) -> Self {
        Self {
            actions: value.actions.into_iter().map(Into::into).collect(),
            next_page_token: value.next_page_token,
            model_id: value.model_id,
        }
    }
}

impl From<AuthorizationMetadata> for pb::AuthorizationMetadata {
    fn from(value: AuthorizationMetadata) -> Self {
        Self {
            capabilities: value.capabilities,
            active_model_id: value.active_model_id,
        }
    }
}

impl From<pb::AuthorizationMetadata> for AuthorizationMetadata {
    fn from(value: pb::AuthorizationMetadata) -> Self {
        Self {
            capabilities: value.capabilities,
            active_model_id: value.active_model_id,
        }
    }
}

impl From<AuthorizationModel> for pb::AuthorizationModel {
    fn from(value: AuthorizationModel) -> Self {
        Self {
            version: value.version,
            resource_types: value.resource_types.into_iter().map(Into::into).collect(),
        }
    }
}

impl From<pb::AuthorizationModel> for AuthorizationModel {
    fn from(value: pb::AuthorizationModel) -> Self {
        Self {
            version: value.version,
            resource_types: value.resource_types.into_iter().map(Into::into).collect(),
        }
    }
}

impl From<AuthorizationModelResourceType> for pb::AuthorizationModelResourceType {
    fn from(value: AuthorizationModelResourceType) -> Self {
        Self {
            name: value.name,
            relations: value.relations.into_iter().map(Into::into).collect(),
            actions: value.actions.into_iter().map(Into::into).collect(),
        }
    }
}

impl From<pb::AuthorizationModelResourceType> for AuthorizationModelResourceType {
    fn from(value: pb::AuthorizationModelResourceType) -> Self {
        Self {
            name: value.name,
            relations: value.relations.into_iter().map(Into::into).collect(),
            actions: value.actions.into_iter().map(Into::into).collect(),
        }
    }
}

impl From<AuthorizationModelRelation> for pb::AuthorizationModelRelation {
    fn from(value: AuthorizationModelRelation) -> Self {
        Self {
            name: value.name,
            subject_types: value.subject_types,
            allowed_targets: value.allowed_targets.into_iter().map(Into::into).collect(),
            rewrite: value.rewrite.map(Into::into),
        }
    }
}

impl From<pb::AuthorizationModelRelation> for AuthorizationModelRelation {
    fn from(value: pb::AuthorizationModelRelation) -> Self {
        Self {
            name: value.name,
            subject_types: value.subject_types,
            allowed_targets: value.allowed_targets.into_iter().map(Into::into).collect(),
            rewrite: value.rewrite.map(Into::into),
        }
    }
}

impl From<AuthorizationModelAction> for pb::AuthorizationModelAction {
    fn from(value: AuthorizationModelAction) -> Self {
        Self {
            name: value.name,
            relations: value.relations,
            rewrite: value.rewrite.map(Into::into),
        }
    }
}

impl From<pb::AuthorizationModelAction> for AuthorizationModelAction {
    fn from(value: pb::AuthorizationModelAction) -> Self {
        Self {
            name: value.name,
            relations: value.relations,
            rewrite: value.rewrite.map(Into::into),
        }
    }
}

impl From<AuthorizationModelAllowedTarget> for pb::AuthorizationModelAllowedTarget {
    fn from(value: AuthorizationModelAllowedTarget) -> Self {
        let kind = match value {
            AuthorizationModelAllowedTarget::SubjectType(subject_type) => {
                pb::authorization_model_allowed_target::Kind::SubjectType(subject_type)
            }
            AuthorizationModelAllowedTarget::ResourceType(resource_type) => {
                pb::authorization_model_allowed_target::Kind::ResourceType(resource_type)
            }
            AuthorizationModelAllowedTarget::SubjectSet(subject_set) => {
                pb::authorization_model_allowed_target::Kind::SubjectSet(subject_set.into())
            }
        };
        Self { kind: Some(kind) }
    }
}

impl From<pb::AuthorizationModelAllowedTarget> for AuthorizationModelAllowedTarget {
    fn from(value: pb::AuthorizationModelAllowedTarget) -> Self {
        match value.kind {
            Some(pb::authorization_model_allowed_target::Kind::SubjectType(subject_type)) => {
                Self::SubjectType(subject_type)
            }
            Some(pb::authorization_model_allowed_target::Kind::ResourceType(resource_type)) => {
                Self::ResourceType(resource_type)
            }
            Some(pb::authorization_model_allowed_target::Kind::SubjectSet(subject_set)) => {
                Self::SubjectSet(subject_set.into())
            }
            None => Self::default(),
        }
    }
}

impl From<AuthorizationModelSubjectSetTarget> for pb::AuthorizationModelSubjectSetTarget {
    fn from(value: AuthorizationModelSubjectSetTarget) -> Self {
        Self {
            resource_type: value.resource_type,
            relation: value.relation,
        }
    }
}

impl From<pb::AuthorizationModelSubjectSetTarget> for AuthorizationModelSubjectSetTarget {
    fn from(value: pb::AuthorizationModelSubjectSetTarget) -> Self {
        Self {
            resource_type: value.resource_type,
            relation: value.relation,
        }
    }
}

impl From<AuthorizationModelRewrite> for pb::AuthorizationModelRewrite {
    fn from(value: AuthorizationModelRewrite) -> Self {
        let kind = match value {
            AuthorizationModelRewrite::This => {
                pb::authorization_model_rewrite::Kind::This(pb::AuthorizationModelRewriteThis {})
            }
            AuthorizationModelRewrite::ComputedUserset(computed_userset) => {
                pb::authorization_model_rewrite::Kind::ComputedUserset(computed_userset.into())
            }
            AuthorizationModelRewrite::TupleToUserset(tuple_to_userset) => {
                pb::authorization_model_rewrite::Kind::TupleToUserset(tuple_to_userset.into())
            }
            AuthorizationModelRewrite::Union(union) => {
                pb::authorization_model_rewrite::Kind::Union(union.into())
            }
        };
        Self { kind: Some(kind) }
    }
}

impl From<pb::AuthorizationModelRewrite> for AuthorizationModelRewrite {
    fn from(value: pb::AuthorizationModelRewrite) -> Self {
        match value.kind {
            Some(pb::authorization_model_rewrite::Kind::This(_)) => Self::This,
            Some(pb::authorization_model_rewrite::Kind::ComputedUserset(computed_userset)) => {
                Self::ComputedUserset(computed_userset.into())
            }
            Some(pb::authorization_model_rewrite::Kind::TupleToUserset(tuple_to_userset)) => {
                Self::TupleToUserset(tuple_to_userset.into())
            }
            Some(pb::authorization_model_rewrite::Kind::Union(union)) => Self::Union(union.into()),
            None => Self::default(),
        }
    }
}

impl From<AuthorizationModelComputedUserset> for pb::AuthorizationModelComputedUserset {
    fn from(value: AuthorizationModelComputedUserset) -> Self {
        Self {
            relation: value.relation,
        }
    }
}

impl From<pb::AuthorizationModelComputedUserset> for AuthorizationModelComputedUserset {
    fn from(value: pb::AuthorizationModelComputedUserset) -> Self {
        Self {
            relation: value.relation,
        }
    }
}

impl From<AuthorizationModelTupleToUserset> for pb::AuthorizationModelTupleToUserset {
    fn from(value: AuthorizationModelTupleToUserset) -> Self {
        Self {
            tupleset_relation: value.tupleset_relation,
            computed_relation: value.computed_relation,
        }
    }
}

impl From<pb::AuthorizationModelTupleToUserset> for AuthorizationModelTupleToUserset {
    fn from(value: pb::AuthorizationModelTupleToUserset) -> Self {
        Self {
            tupleset_relation: value.tupleset_relation,
            computed_relation: value.computed_relation,
        }
    }
}

impl From<AuthorizationModelRewriteUnion> for pb::AuthorizationModelRewriteUnion {
    fn from(value: AuthorizationModelRewriteUnion) -> Self {
        Self {
            children: value.children.into_iter().map(Into::into).collect(),
        }
    }
}

impl From<pb::AuthorizationModelRewriteUnion> for AuthorizationModelRewriteUnion {
    fn from(value: pb::AuthorizationModelRewriteUnion) -> Self {
        Self {
            children: value.children.into_iter().map(Into::into).collect(),
        }
    }
}

impl From<AuthorizationModelRef> for pb::AuthorizationModelRef {
    fn from(value: AuthorizationModelRef) -> Self {
        Self {
            id: value.id,
            version: value.version,
            created_at: value.created_at.map(protocol::timestamp_from_system_time),
        }
    }
}

impl From<pb::AuthorizationModelRef> for AuthorizationModelRef {
    fn from(value: pb::AuthorizationModelRef) -> Self {
        Self {
            id: value.id,
            version: value.version,
            created_at: value
                .created_at
                .and_then(|created_at| protocol::system_time_from_timestamp(&created_at).ok()),
        }
    }
}

impl From<GetActiveModelResponse> for pb::GetActiveModelResponse {
    fn from(value: GetActiveModelResponse) -> Self {
        Self {
            model: value.model.map(Into::into),
        }
    }
}

impl From<pb::GetActiveModelResponse> for GetActiveModelResponse {
    fn from(value: pb::GetActiveModelResponse) -> Self {
        Self {
            model: value.model.map(Into::into),
        }
    }
}

impl From<ListModelsRequest> for pb::ListModelsRequest {
    fn from(value: ListModelsRequest) -> Self {
        Self {
            page_size: value.page_size,
            page_token: value.page_token,
        }
    }
}

impl From<pb::ListModelsRequest> for ListModelsRequest {
    fn from(value: pb::ListModelsRequest) -> Self {
        Self {
            page_size: value.page_size,
            page_token: value.page_token,
        }
    }
}

impl From<ListModelsResponse> for pb::ListModelsResponse {
    fn from(value: ListModelsResponse) -> Self {
        Self {
            models: value.models.into_iter().map(Into::into).collect(),
            next_page_token: value.next_page_token,
        }
    }
}

impl From<pb::ListModelsResponse> for ListModelsResponse {
    fn from(value: pb::ListModelsResponse) -> Self {
        Self {
            models: value.models.into_iter().map(Into::into).collect(),
            next_page_token: value.next_page_token,
        }
    }
}

impl From<WriteModelRequest> for pb::WriteModelRequest {
    fn from(value: WriteModelRequest) -> Self {
        Self {
            model: value.model.map(Into::into),
        }
    }
}

impl From<pb::WriteModelRequest> for WriteModelRequest {
    fn from(value: pb::WriteModelRequest) -> Self {
        Self {
            model: value.model.map(Into::into),
        }
    }
}

impl From<Relationship> for pb::Relationship {
    fn from(value: Relationship) -> Self {
        let target = value.target.map(Into::into);
        let subject = if target.is_some() && value.subject == AuthorizationSubject::default() {
            None
        } else {
            Some(value.subject.into())
        };
        Self {
            subject,
            relation: value.relation,
            resource: Some(value.resource.into()),
            properties: struct_option_from_map(value.properties),
            target,
        }
    }
}

impl From<pb::Relationship> for Relationship {
    fn from(value: pb::Relationship) -> Self {
        Self {
            subject: value.subject.map(Into::into).unwrap_or_default(),
            target: value.target.map(Into::into),
            relation: value.relation,
            resource: value.resource.map(Into::into).unwrap_or_default(),
            properties: map_from_struct_option(value.properties),
        }
    }
}

impl From<RelationshipKey> for pb::RelationshipKey {
    fn from(value: RelationshipKey) -> Self {
        let target = value.target.map(Into::into);
        let subject = if target.is_some() && value.subject == AuthorizationSubject::default() {
            None
        } else {
            Some(value.subject.into())
        };
        Self {
            subject,
            relation: value.relation,
            resource: Some(value.resource.into()),
            target,
        }
    }
}

impl From<pb::RelationshipKey> for RelationshipKey {
    fn from(value: pb::RelationshipKey) -> Self {
        Self {
            subject: value.subject.map(Into::into).unwrap_or_default(),
            target: value.target.map(Into::into),
            relation: value.relation,
            resource: value.resource.map(Into::into).unwrap_or_default(),
        }
    }
}

impl From<ReadRelationshipsRequest> for pb::ReadRelationshipsRequest {
    fn from(value: ReadRelationshipsRequest) -> Self {
        Self {
            subject: value.subject.map(Into::into),
            relation: value.relation,
            resource: value.resource.map(Into::into),
            page_size: value.page_size,
            page_token: value.page_token,
            model_id: value.model_id,
            target: value.target.map(Into::into),
        }
    }
}

impl From<pb::ReadRelationshipsRequest> for ReadRelationshipsRequest {
    fn from(value: pb::ReadRelationshipsRequest) -> Self {
        Self {
            subject: value.subject.map(Into::into),
            target: value.target.map(Into::into),
            relation: value.relation,
            resource: value.resource.map(Into::into),
            page_size: value.page_size,
            page_token: value.page_token,
            model_id: value.model_id,
        }
    }
}

impl From<ReadRelationshipsResponse> for pb::ReadRelationshipsResponse {
    fn from(value: ReadRelationshipsResponse) -> Self {
        Self {
            relationships: value.relationships.into_iter().map(Into::into).collect(),
            next_page_token: value.next_page_token,
            model_id: value.model_id,
        }
    }
}

impl From<pb::ReadRelationshipsResponse> for ReadRelationshipsResponse {
    fn from(value: pb::ReadRelationshipsResponse) -> Self {
        Self {
            relationships: value.relationships.into_iter().map(Into::into).collect(),
            next_page_token: value.next_page_token,
            model_id: value.model_id,
        }
    }
}

impl From<WriteRelationshipsRequest> for pb::WriteRelationshipsRequest {
    fn from(value: WriteRelationshipsRequest) -> Self {
        Self {
            writes: value.writes.into_iter().map(Into::into).collect(),
            deletes: value.deletes.into_iter().map(Into::into).collect(),
            model_id: value.model_id,
        }
    }
}

impl From<pb::WriteRelationshipsRequest> for WriteRelationshipsRequest {
    fn from(value: pb::WriteRelationshipsRequest) -> Self {
        Self {
            writes: value.writes.into_iter().map(Into::into).collect(),
            deletes: value.deletes.into_iter().map(Into::into).collect(),
            model_id: value.model_id,
        }
    }
}

impl From<ExpandRequest> for pb::ExpandRequest {
    fn from(value: ExpandRequest) -> Self {
        Self {
            resource: Some(value.resource.into()),
            relation: value.relation,
            context: struct_option_from_map(value.context),
            max_depth: value.max_depth,
            model_id: value.model_id,
        }
    }
}

impl From<pb::ExpandRequest> for ExpandRequest {
    fn from(value: pb::ExpandRequest) -> Self {
        Self {
            resource: value.resource.map(Into::into).unwrap_or_default(),
            relation: value.relation,
            context: map_from_struct_option(value.context),
            max_depth: value.max_depth,
            model_id: value.model_id,
        }
    }
}

impl From<ExpandNode> for pb::ExpandNode {
    fn from(value: ExpandNode) -> Self {
        Self {
            target: value.target.map(Into::into),
            relation: value.relation,
            children: value.children.into_iter().map(Into::into).collect(),
        }
    }
}

impl From<pb::ExpandNode> for ExpandNode {
    fn from(value: pb::ExpandNode) -> Self {
        Self {
            target: value.target.map(Into::into),
            relation: value.relation,
            children: value.children.into_iter().map(Into::into).collect(),
        }
    }
}

impl From<ExpandResponse> for pb::ExpandResponse {
    fn from(value: ExpandResponse) -> Self {
        Self {
            root: value.root.map(Into::into),
            truncated: value.truncated,
            cycle_detected: value.cycle_detected,
            max_depth_reached: value.max_depth_reached,
            model_id: value.model_id,
        }
    }
}

impl From<pb::ExpandResponse> for ExpandResponse {
    fn from(value: pb::ExpandResponse) -> Self {
        Self {
            root: value.root.map(Into::into),
            truncated: value.truncated,
            cycle_detected: value.cycle_detected,
            max_depth_reached: value.max_depth_reached,
            model_id: value.model_id,
        }
    }
}

#[async_trait]
/// Provider trait for serving the Gestalt authorization-provider protocol.
pub trait AuthorizationProvider: Send + Sync + 'static {
    /// Configures the provider before it starts serving requests.
    async fn configure(
        &self,
        _name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> ProviderResult<()> {
        Ok(())
    }

    /// Returns runtime metadata that should augment the static manifest.
    fn metadata(&self) -> Option<RuntimeMetadata> {
        None
    }

    /// Returns non-fatal warnings the host should surface to users.
    fn warnings(&self) -> Vec<String> {
        Vec::new()
    }

    /// Performs an optional health check.
    async fn health_check(&self) -> ProviderResult<()> {
        Ok(())
    }

    /// Starts provider-owned background work after configuration.
    async fn start(&self) -> ProviderResult<()> {
        Ok(())
    }

    /// Shuts the provider down before the runtime exits.
    async fn close(&self) -> ProviderResult<()> {
        Ok(())
    }

    /// Reports whether effective resource/subject search RPCs are implemented.
    fn supports_effective_search(&self) -> bool {
        false
    }

    /// Reports whether graph expansion is implemented.
    fn supports_expand(&self) -> bool {
        false
    }

    /// Evaluates one access request.
    async fn evaluate(&self, _request: AccessEvaluationRequest) -> ProviderResult<AccessDecision> {
        Err(crate::Error::unimplemented(
            "authorization evaluate is not implemented",
        ))
    }

    /// Evaluates multiple access requests.
    async fn evaluate_many(
        &self,
        _request: AccessEvaluationsRequest,
    ) -> ProviderResult<AccessEvaluationsResponse> {
        Err(crate::Error::unimplemented(
            "authorization evaluate many is not implemented",
        ))
    }

    /// Searches directly stored resource relationships.
    async fn search_resources(
        &self,
        _request: ResourceSearchRequest,
    ) -> ProviderResult<ResourceSearchResponse> {
        Err(crate::Error::unimplemented(
            "authorization search resources is not implemented",
        ))
    }

    /// Searches directly stored subject relationships.
    async fn search_subjects(
        &self,
        _request: SubjectSearchRequest,
    ) -> ProviderResult<SubjectSearchResponse> {
        Err(crate::Error::unimplemented(
            "authorization search subjects is not implemented",
        ))
    }

    /// Searches effective resources through computed usersets and inheritance.
    async fn effective_search_resources(
        &self,
        _request: ResourceSearchRequest,
    ) -> ProviderResult<ResourceSearchResponse> {
        Err(crate::Error::unimplemented(
            "authorization effective search resources is not implemented",
        ))
    }

    /// Searches effective subjects through computed usersets and inheritance.
    async fn effective_search_subjects(
        &self,
        _request: EffectiveSubjectSearchRequest,
    ) -> ProviderResult<EffectiveSubjectSearchResponse> {
        Err(crate::Error::unimplemented(
            "authorization effective search subjects is not implemented",
        ))
    }

    /// Searches actions available between a subject and resource.
    async fn search_actions(
        &self,
        _request: ActionSearchRequest,
    ) -> ProviderResult<ActionSearchResponse> {
        Err(crate::Error::unimplemented(
            "authorization search actions is not implemented",
        ))
    }

    /// Expands one resource relation.
    async fn expand(&self, _request: ExpandRequest) -> ProviderResult<ExpandResponse> {
        Err(crate::Error::unimplemented(
            "authorization expand is not implemented",
        ))
    }

    /// Returns provider metadata.
    async fn get_metadata(&self) -> ProviderResult<AuthorizationMetadata> {
        Err(crate::Error::unimplemented(
            "authorization metadata is not implemented",
        ))
    }

    /// Reads stored relationships.
    async fn read_relationships(
        &self,
        _request: ReadRelationshipsRequest,
    ) -> ProviderResult<ReadRelationshipsResponse> {
        Err(crate::Error::unimplemented(
            "authorization read relationships is not implemented",
        ))
    }

    /// Writes and deletes relationships.
    async fn write_relationships(&self, _request: WriteRelationshipsRequest) -> ProviderResult<()> {
        Err(crate::Error::unimplemented(
            "authorization write relationships is not implemented",
        ))
    }

    /// Returns the active authorization model.
    async fn get_active_model(&self) -> ProviderResult<GetActiveModelResponse> {
        Err(crate::Error::unimplemented(
            "authorization get active model is not implemented",
        ))
    }

    /// Lists stored authorization models.
    async fn list_models(&self, _request: ListModelsRequest) -> ProviderResult<ListModelsResponse> {
        Err(crate::Error::unimplemented(
            "authorization list models is not implemented",
        ))
    }

    /// Writes a new authorization model.
    async fn write_model(
        &self,
        _request: WriteModelRequest,
    ) -> ProviderResult<AuthorizationModelRef> {
        Err(crate::Error::unimplemented(
            "authorization write model is not implemented",
        ))
    }
}

#[derive(Clone)]
pub(crate) struct AuthorizationServer<P> {
    provider: Arc<P>,
}

impl<P> AuthorizationServer<P> {
    pub(crate) fn new(provider: Arc<P>) -> Self {
        Self { provider }
    }
}

#[async_trait]
impl<P> pb::authorization_provider_server::AuthorizationProvider for AuthorizationServer<P>
where
    P: AuthorizationProvider,
{
    async fn evaluate(
        &self,
        request: GrpcRequest<pb::AccessEvaluationRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AccessDecision>, Status> {
        let decision = self
            .provider
            .evaluate(request.into_inner().into())
            .await
            .map_err(|error| rpc_status("authorization evaluate", error))?;
        Ok(GrpcResponse::new(decision.into()))
    }

    async fn evaluate_many(
        &self,
        request: GrpcRequest<pb::AccessEvaluationsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AccessEvaluationsResponse>, Status> {
        let response = self
            .provider
            .evaluate_many(request.into_inner().into())
            .await
            .map_err(|error| rpc_status("authorization evaluate many", error))?;
        Ok(GrpcResponse::new(response.into()))
    }

    async fn search_resources(
        &self,
        request: GrpcRequest<pb::ResourceSearchRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ResourceSearchResponse>, Status> {
        let response = self
            .provider
            .search_resources(request.into_inner().into())
            .await
            .map_err(|error| rpc_status("authorization search resources", error))?;
        Ok(GrpcResponse::new(response.into()))
    }

    async fn search_subjects(
        &self,
        request: GrpcRequest<pb::SubjectSearchRequest>,
    ) -> std::result::Result<GrpcResponse<pb::SubjectSearchResponse>, Status> {
        let response = self
            .provider
            .search_subjects(request.into_inner().into())
            .await
            .map_err(|error| rpc_status("authorization search subjects", error))?;
        Ok(GrpcResponse::new(response.into()))
    }

    async fn effective_search_resources(
        &self,
        request: GrpcRequest<pb::ResourceSearchRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ResourceSearchResponse>, Status> {
        if !self.provider.supports_effective_search() {
            return Err(Status::unimplemented(
                "authorization provider does not support effective search",
            ));
        }
        let response = self
            .provider
            .effective_search_resources(request.into_inner().into())
            .await
            .map_err(|error| rpc_status("authorization effective search resources", error))?;
        Ok(GrpcResponse::new(response.into()))
    }

    async fn effective_search_subjects(
        &self,
        request: GrpcRequest<pb::EffectiveSubjectSearchRequest>,
    ) -> std::result::Result<GrpcResponse<pb::EffectiveSubjectSearchResponse>, Status> {
        if !self.provider.supports_effective_search() {
            return Err(Status::unimplemented(
                "authorization provider does not support effective search",
            ));
        }
        let response = self
            .provider
            .effective_search_subjects(request.into_inner().into())
            .await
            .map_err(|error| rpc_status("authorization effective search subjects", error))?;
        Ok(GrpcResponse::new(response.into()))
    }

    async fn search_actions(
        &self,
        request: GrpcRequest<pb::ActionSearchRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ActionSearchResponse>, Status> {
        let response = self
            .provider
            .search_actions(request.into_inner().into())
            .await
            .map_err(|error| rpc_status("authorization search actions", error))?;
        Ok(GrpcResponse::new(response.into()))
    }

    async fn expand(
        &self,
        request: GrpcRequest<pb::ExpandRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ExpandResponse>, Status> {
        if !self.provider.supports_expand() {
            return Err(Status::unimplemented(
                "authorization provider does not support expansion",
            ));
        }
        let response = self
            .provider
            .expand(request.into_inner().into())
            .await
            .map_err(|error| rpc_status("authorization expand", error))?;
        Ok(GrpcResponse::new(response.into()))
    }

    async fn get_metadata(
        &self,
        _request: GrpcRequest<()>,
    ) -> std::result::Result<GrpcResponse<pb::AuthorizationMetadata>, Status> {
        let mut metadata = self
            .provider
            .get_metadata()
            .await
            .map_err(|error| rpc_status("authorization metadata", error))?;
        if self.provider.supports_effective_search() {
            push_capability(&mut metadata.capabilities, "effective_search_resources");
            push_capability(&mut metadata.capabilities, "effective_search_subjects");
        }
        if self.provider.supports_expand() {
            push_capability(&mut metadata.capabilities, "expand");
        }
        Ok(GrpcResponse::new(metadata.into()))
    }

    async fn read_relationships(
        &self,
        request: GrpcRequest<pb::ReadRelationshipsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ReadRelationshipsResponse>, Status> {
        let response = self
            .provider
            .read_relationships(request.into_inner().into())
            .await
            .map_err(|error| rpc_status("authorization read relationships", error))?;
        Ok(GrpcResponse::new(response.into()))
    }

    async fn write_relationships(
        &self,
        request: GrpcRequest<pb::WriteRelationshipsRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider
            .write_relationships(request.into_inner().into())
            .await
            .map_err(|error| rpc_status("authorization write relationships", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn get_active_model(
        &self,
        _request: GrpcRequest<()>,
    ) -> std::result::Result<GrpcResponse<pb::GetActiveModelResponse>, Status> {
        let response = self
            .provider
            .get_active_model()
            .await
            .map_err(|error| rpc_status("authorization get active model", error))?;
        Ok(GrpcResponse::new(response.into()))
    }

    async fn list_models(
        &self,
        request: GrpcRequest<pb::ListModelsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListModelsResponse>, Status> {
        let response = self
            .provider
            .list_models(request.into_inner().into())
            .await
            .map_err(|error| rpc_status("authorization list models", error))?;
        Ok(GrpcResponse::new(response.into()))
    }

    async fn write_model(
        &self,
        request: GrpcRequest<pb::WriteModelRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AuthorizationModelRef>, Status> {
        let response = self
            .provider
            .write_model(request.into_inner().into())
            .await
            .map_err(|error| rpc_status("authorization write model", error))?;
        Ok(GrpcResponse::new(response.into()))
    }
}

fn push_capability(capabilities: &mut Vec<String>, capability: &str) {
    if !capabilities.iter().any(|existing| existing == capability) {
        capabilities.push(capability.to_string());
    }
}

fn struct_option_from_map(
    value: serde_json::Map<String, serde_json::Value>,
) -> Option<prost_types::Struct> {
    if value.is_empty() {
        return None;
    }
    Some(protocol::struct_from_map(value))
}

fn map_from_struct_option(
    value: Option<prost_types::Struct>,
) -> serde_json::Map<String, serde_json::Value> {
    match value {
        Some(value) => match protocol::json_from_struct(&value) {
            serde_json::Value::Object(fields) => fields,
            _ => serde_json::Map::new(),
        },
        None => serde_json::Map::new(),
    }
}

#[derive(Clone)]
struct RelayTokenInterceptor {
    token: Option<MetadataValue<tonic::metadata::Ascii>>,
}

impl Interceptor for RelayTokenInterceptor {
    fn call(
        &mut self,
        mut request: Request<()>,
    ) -> std::result::Result<Request<()>, tonic::Status> {
        if let Some(token) = self.token.clone() {
            request
                .metadata_mut()
                .insert(AUTHORIZATION_RELAY_TOKEN_HEADER, token);
        }
        Ok(request)
    }
}

fn relay_token_interceptor(
    token: &str,
) -> std::result::Result<RelayTokenInterceptor, AuthorizationError> {
    let trimmed = token.trim();
    let token = if trimmed.is_empty() {
        None
    } else {
        Some(MetadataValue::try_from(trimmed).map_err(|err| {
            AuthorizationError::Env(format!(
                "authorization: invalid relay token metadata: {err}"
            ))
        })?)
    };
    Ok(RelayTokenInterceptor { token })
}

enum AuthorizationTarget {
    Unix(String),
    Tcp(String),
    Tls(String),
}

fn parse_authorization_target(
    raw: &str,
) -> std::result::Result<AuthorizationTarget, AuthorizationError> {
    let target = raw.trim();
    if target.is_empty() {
        return Err(AuthorizationError::Env(
            "authorization: transport target is required".to_string(),
        ));
    }
    if let Some(address) = target.strip_prefix("tcp://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(AuthorizationError::Env(format!(
                "authorization: tcp target {raw:?} is missing host:port"
            )));
        }
        return Ok(AuthorizationTarget::Tcp(address.to_string()));
    }
    if let Some(address) = target.strip_prefix("tls://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(AuthorizationError::Env(format!(
                "authorization: tls target {raw:?} is missing host:port"
            )));
        }
        return Ok(AuthorizationTarget::Tls(address.to_string()));
    }
    if let Some(path) = target.strip_prefix("unix://") {
        let path = path.trim();
        if path.is_empty() {
            return Err(AuthorizationError::Env(format!(
                "authorization: unix target {raw:?} is missing a socket path"
            )));
        }
        return Ok(AuthorizationTarget::Unix(path.to_string()));
    }
    if target.contains("://") {
        return Err(AuthorizationError::Env(format!(
            "authorization: unsupported target scheme in {raw:?}"
        )));
    }
    Ok(AuthorizationTarget::Unix(target.to_string()))
}
