use hyper_util::rt::TokioIo;
use tokio::net::UnixStream;
use tonic::Request;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, ClientTlsConfig, Endpoint, Uri};
use tower::service_fn;

use crate::generated::v1::{
    self as pb, authorization_provider_client::AuthorizationProviderClient,
};
use crate::protocol;

type AuthorizationTransport = InterceptedService<Channel, RelayTokenInterceptor>;

/// Environment variable containing the authorization host-service target.
pub const ENV_AUTHORIZATION_SOCKET: &str = "GESTALT_AUTHORIZATION_SOCKET";
/// Environment variable containing the optional authorization relay token.
pub const ENV_AUTHORIZATION_SOCKET_TOKEN: &str = "GESTALT_AUTHORIZATION_SOCKET_TOKEN";
const AUTHORIZATION_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";
/// Subject type used for canonical Gestalt subject ids in managed grants.
pub const AUTHORIZATION_SUBJECT_TYPE_SUBJECT: &str = "subject";
/// Managed authorization resource type for agent sessions.
pub const AGENT_SESSION_RESOURCE_TYPE: &str = "agent_session";
/// Relation that grants view and edit access to an agent session.
pub const AGENT_SESSION_RELATION_EDITOR: &str = "editor";
/// Action checked when reading a shared agent session.
pub const AGENT_SESSION_ACTION_VIEW: &str = "view";
/// Action checked when creating turns or resolving interactions in a session.
pub const AGENT_SESSION_ACTION_EDIT: &str = "edit";

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
/// Resources are objects protected by authorization checks, such as an
/// `agent_session` with a concrete session id.
pub struct AuthorizationResource {
    /// Resource type, such as `agent_session`.
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

    /// Creates the managed authorization resource for an agent session.
    pub fn agent_session(session_id: impl Into<String>) -> Self {
        Self::new(AGENT_SESSION_RESOURCE_TYPE, session_id)
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
/// Relationship tuple stored in the authorization graph.
///
/// A relationship grants a subject a relation on a resource, for example
/// `subject:user:123` has `editor` on `agent_session:sess_123`.
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

    /// Creates the relationship that shares an agent session with a canonical
    /// Gestalt subject id such as `user:123`.
    ///
    /// The editor relation grants both view and edit actions in the
    /// host-managed authorization model. The returned relationship mirrors the
    /// subject into both the legacy `subject` field and generalized `target`
    /// field so it remains compatible with mixed host/provider versions.
    pub fn agent_session_editor(
        subject_id: impl Into<String>,
        session_id: impl Into<String>,
    ) -> Self {
        let subject = AuthorizationSubject::new(AUTHORIZATION_SUBJECT_TYPE_SUBJECT, subject_id);
        Self {
            subject: subject.clone(),
            target: Some(AuthorizationRelationshipTarget::Subject(subject)),
            relation: AGENT_SESSION_RELATION_EDITOR.to_string(),
            resource: AuthorizationResource::agent_session(session_id),
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

    /// Creates a write request that shares an agent session with a canonical
    /// Gestalt subject id.
    pub fn agent_session_editor(
        subject_id: impl Into<String>,
        session_id: impl Into<String>,
    ) -> Self {
        Self::writes([Relationship::agent_session_editor(subject_id, session_id)])
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

/// Client for the host-configured authorization provider.
///
/// The client exposes typed SDK values and performs all protobuf conversion
/// internally. Providers can use it to evaluate access, search relationships,
/// and write relationship grants without importing generated protocol modules.
pub struct Authorization {
    client: AuthorizationProviderClient<AuthorizationTransport>,
}

impl Authorization {
    /// Connects to the authorization host-service target from the environment.
    pub async fn connect() -> std::result::Result<Self, AuthorizationError> {
        let target = std::env::var(ENV_AUTHORIZATION_SOCKET).map_err(|_| {
            AuthorizationError::Env(format!("{ENV_AUTHORIZATION_SOCKET} is not set"))
        })?;
        let relay_token = std::env::var(ENV_AUTHORIZATION_SOCKET_TOKEN).unwrap_or_default();
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
        requests: impl IntoIterator<Item = AccessEvaluationRequest>,
    ) -> std::result::Result<Vec<AccessDecision>, AuthorizationError> {
        let response = self
            .client
            .evaluate_many(pb::AccessEvaluationsRequest {
                requests: requests.into_iter().map(Into::into).collect(),
            })
            .await?
            .into_inner();
        Ok(response.decisions.into_iter().map(Into::into).collect())
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

    /// Grants a canonical Gestalt subject id editor access to an agent session.
    pub async fn grant_agent_session_editor(
        &mut self,
        subject_id: impl Into<String>,
        session_id: impl Into<String>,
    ) -> std::result::Result<(), AuthorizationError> {
        self.write_relationships(WriteRelationshipsRequest::agent_session_editor(
            subject_id, session_id,
        ))
        .await
    }

    /// Returns host authorization provider metadata.
    pub async fn get_metadata(
        &mut self,
    ) -> std::result::Result<AuthorizationMetadata, AuthorizationError> {
        Ok(self.client.get_metadata(()).await?.into_inner().into())
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

impl From<pb::AccessDecision> for AccessDecision {
    fn from(value: pb::AccessDecision) -> Self {
        Self {
            allowed: value.allowed,
            context: map_from_struct_option(value.context),
            model_id: value.model_id,
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

impl From<pb::ActionSearchResponse> for ActionSearchResponse {
    fn from(value: pb::ActionSearchResponse) -> Self {
        Self {
            actions: value.actions.into_iter().map(Into::into).collect(),
            next_page_token: value.next_page_token,
            model_id: value.model_id,
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

impl From<pb::ExpandNode> for ExpandNode {
    fn from(value: pb::ExpandNode) -> Self {
        Self {
            target: value.target.map(Into::into),
            relation: value.relation,
            children: value.children.into_iter().map(Into::into).collect(),
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
