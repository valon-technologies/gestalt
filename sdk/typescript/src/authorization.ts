import { connect } from "node:net";

import {
  create,
  type JsonObject,
  type MessageInitShape,
} from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import {
  Code,
  ConnectError,
  createClient,
  type Client,
  type Interceptor,
  type ServiceImpl,
} from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  type AccessDecision,
  type AccessEvaluationRequest,
  type AccessEvaluationsRequest,
  type AccessEvaluationsResponse,
  type ActionSearchRequest,
  type ActionSearchResponse,
  type AuthorizationMetadata,
  type AuthorizationModel,
  type AuthorizationModelAction,
  type AuthorizationModelAllowedTarget,
  type AuthorizationModelComputedUserset,
  type AuthorizationModelRef,
  type AuthorizationModelRelation,
  type AuthorizationModelResourceType,
  type AuthorizationModelRewrite,
  type AuthorizationModelRewriteThis,
  type AuthorizationModelRewriteUnion,
  type AuthorizationModelSubjectSetTarget,
  type AuthorizationModelTupleToUserset,
  AuthorizationProvider as AuthorizationProviderService,
  type EffectiveSubjectSearchRequest,
  type EffectiveSubjectSearchResponse,
  type ExpandRequest,
  type ExpandResponse,
  type GetActiveModelResponse,
  type ListModelsRequest,
  type ListModelsResponse,
  type ReadRelationshipsRequest,
  type ReadRelationshipsResponse,
  type Relationship,
  type RelationshipKey,
  type RelationshipTarget,
  type ResourceSearchRequest,
  type ResourceSearchResponse,
  type SubjectSet,
  type SubjectSearchRequest,
  type SubjectSearchResponse,
  type WriteModelRequest,
  type WriteRelationshipsRequest,
  AccessEvaluationRequestSchema,
  AccessDecisionSchema,
  AccessEvaluationsRequestSchema,
  AccessEvaluationsResponseSchema,
  ActionSearchRequestSchema,
  ActionSearchResponseSchema,
  ActionSchema,
  AuthorizationMetadataSchema,
  AuthorizationModelRefSchema,
  EffectiveSubjectSearchRequestSchema,
  EffectiveSubjectSearchResponseSchema,
  ExpandRequestSchema,
  ExpandResponseSchema,
  GetActiveModelResponseSchema,
  ListModelsRequestSchema,
  ListModelsResponseSchema,
  ReadRelationshipsRequestSchema,
  ReadRelationshipsResponseSchema,
  RelationshipKeySchema,
  RelationshipSchema,
  RelationshipTargetSchema,
  ResourceSchema,
  ResourceSearchRequestSchema,
  ResourceSearchResponseSchema,
  SubjectSchema,
  SubjectSetSchema,
  SubjectSearchRequestSchema,
  SubjectSearchResponseSchema,
  WriteModelRequestSchema,
  WriteRelationshipsRequestSchema,
} from "./internal/gen/v1/authorization_pb.ts";
import type { MaybePromise } from "./api.ts";
import { ProviderBase, type ProviderBaseOptions } from "./provider.ts";

/**
 * Environment variable containing the Unix socket path or relay target for the
 * host authorization client exposed to plugins.
 */
export const ENV_AUTHORIZATION_SOCKET = "GESTALT_AUTHORIZATION_SOCKET";
export const ENV_AUTHORIZATION_SOCKET_TOKEN = `${ENV_AUTHORIZATION_SOCKET}_TOKEN`;
const AUTHORIZATION_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token";

/** Subject type used for canonical Gestalt subject ids in managed grants. */
export const AUTHORIZATION_SUBJECT_TYPE_SUBJECT = "subject";
/** Managed authorization resource type for agent sessions. */
export const AGENT_SESSION_RESOURCE_TYPE = "agent_session";
/** Relation that grants view and edit access to an agent session. */
export const AGENT_SESSION_RELATION_EDITOR = "editor";
/** Action checked when reading a shared agent session. */
export const AGENT_SESSION_ACTION_VIEW = "view";
/** Action checked when creating turns or resolving interactions in a session. */
export const AGENT_SESSION_ACTION_EDIT = "edit";

export type AuthorizationEvaluateInput = MessageInitShape<
  typeof AccessEvaluationRequestSchema
>;
export type AuthorizationEvaluateManyInput = MessageInitShape<
  typeof AccessEvaluationsRequestSchema
>;
export type AuthorizationSearchResourcesInput = MessageInitShape<
  typeof ResourceSearchRequestSchema
>;
export type AuthorizationSearchSubjectsInput = MessageInitShape<
  typeof SubjectSearchRequestSchema
>;
export type AuthorizationEffectiveSearchSubjectsInput = MessageInitShape<
  typeof EffectiveSubjectSearchRequestSchema
>;
export type AuthorizationSearchActionsInput = MessageInitShape<
  typeof ActionSearchRequestSchema
>;
export type AuthorizationExpandInput = MessageInitShape<
  typeof ExpandRequestSchema
>;
export type AuthorizationReadRelationshipsInput = MessageInitShape<
  typeof ReadRelationshipsRequestSchema
>;
export type AuthorizationWriteRelationshipsInput = MessageInitShape<
  typeof WriteRelationshipsRequestSchema
>;
export type AuthorizationWriteModelInput = MessageInitShape<
  typeof WriteModelRequestSchema
>;
export type AuthorizationListModelsInput = MessageInitShape<
  typeof ListModelsRequestSchema
>;
export type AuthorizationSubject = MessageInitShape<typeof SubjectSchema>;
export type AuthorizationResource = MessageInitShape<typeof ResourceSchema>;
export type AuthorizationSubjectSet = MessageInitShape<typeof SubjectSetSchema>;
export type AuthorizationRelationshipTarget = MessageInitShape<
  typeof RelationshipTargetSchema
>;
export type AuthorizationAction = MessageInitShape<typeof ActionSchema>;
export type AuthorizationRelationship = MessageInitShape<
  typeof RelationshipSchema
>;
export type AuthorizationRelationshipKey = MessageInitShape<
  typeof RelationshipKeySchema
>;

export type AuthorizationDecisionMessage = AccessDecision;
export type AuthorizationEvaluationsRequestMessage = AccessEvaluationsRequest;
export type AuthorizationEvaluationsResponseMessage = AccessEvaluationsResponse;
export type AuthorizationMetadataMessage = AuthorizationMetadata;
export type AuthorizationModelMessage = AuthorizationModel;
export type AuthorizationModelActionMessage = AuthorizationModelAction;
export type AuthorizationModelAllowedTargetMessage =
  AuthorizationModelAllowedTarget;
export type AuthorizationModelComputedUsersetMessage =
  AuthorizationModelComputedUserset;
export type AuthorizationModelRefMessage = AuthorizationModelRef;
export type AuthorizationModelRelationMessage = AuthorizationModelRelation;
export type AuthorizationModelResourceTypeMessage =
  AuthorizationModelResourceType;
export type AuthorizationModelRewriteMessage = AuthorizationModelRewrite;
export type AuthorizationModelRewriteThisMessage =
  AuthorizationModelRewriteThis;
export type AuthorizationModelRewriteUnionMessage =
  AuthorizationModelRewriteUnion;
export type AuthorizationModelSubjectSetTargetMessage =
  AuthorizationModelSubjectSetTarget;
export type AuthorizationModelTupleToUsersetMessage =
  AuthorizationModelTupleToUserset;
export type AuthorizationResourceSearchMessage = ResourceSearchResponse;
export type AuthorizationSubjectSearchMessage = SubjectSearchResponse;
export type AuthorizationEffectiveSubjectSearchMessage =
  EffectiveSubjectSearchResponse;
export type AuthorizationActionSearchMessage = ActionSearchResponse;
export type AuthorizationExpandMessage = ExpandResponse;
export type AuthorizationReadRelationshipsMessage = ReadRelationshipsResponse;
export type AuthorizationRelationshipMessage = Relationship;
export type AuthorizationRelationshipKeyMessage = RelationshipKey;
export type AuthorizationRelationshipTargetMessage = RelationshipTarget;
export type AuthorizationSubjectSetMessage = SubjectSet;
export type AuthorizationGetActiveModelMessage = GetActiveModelResponse;
export type AuthorizationListModelsRequestMessage = ListModelsRequest;
export type AuthorizationListModelsResponseMessage = ListModelsResponse;
export type AuthorizationWriteModelRequestMessage = WriteModelRequest;

const sharedAuthorizationTransport: {
  target: string;
  token: string;
  client: AuthorizationClient | undefined;
} = {
  target: "",
  token: "",
  client: undefined,
};

/**
 * Client for the host-configured authorization provider.
 *
 * The client accepts plain SDK request objects and keeps transport message
 * construction inside the SDK.
 */
export class AuthorizationClient {
  private readonly client: Client<typeof AuthorizationProviderService>;

  constructor(
    socketTarget?: string,
    relayToken = process.env[ENV_AUTHORIZATION_SOCKET_TOKEN]?.trim() ?? "",
  ) {
    const resolvedTarget = resolveAuthorizationSocketTarget(socketTarget);
    const transportOptions = authorizationTransportOptions(resolvedTarget);
    const transport = createGrpcTransport({
      ...transportOptions,
      ...(transportOptions.nodeOptions
        ? {
            nodeOptions: {
              createConnection: () =>
                connect({ path: transportOptions.nodeOptions!.path }),
            },
          }
        : {}),
      interceptors: relayToken
        ? [authorizationRelayTokenInterceptor(relayToken)]
        : [],
    });
    this.client = createClient(AuthorizationProviderService, transport);
  }

  async evaluate(
    request: AuthorizationEvaluateInput,
  ): Promise<AuthorizationDecisionMessage> {
    return await this.client.evaluate(request);
  }

  async evaluateMany(
    request: AuthorizationEvaluateManyInput,
  ): Promise<AuthorizationEvaluationsResponseMessage> {
    return await this.client.evaluateMany(request);
  }

  async searchResources(
    request: AuthorizationSearchResourcesInput,
  ): Promise<AuthorizationResourceSearchMessage> {
    return await this.client.searchResources(request);
  }

  async searchSubjects(
    request: AuthorizationSearchSubjectsInput,
  ): Promise<AuthorizationSubjectSearchMessage> {
    return await this.client.searchSubjects(request);
  }

  async effectiveSearchResources(
    request: AuthorizationSearchResourcesInput,
  ): Promise<AuthorizationResourceSearchMessage> {
    return await this.client.effectiveSearchResources(request);
  }

  async effectiveSearchSubjects(
    request: AuthorizationEffectiveSearchSubjectsInput,
  ): Promise<AuthorizationEffectiveSubjectSearchMessage> {
    return await this.client.effectiveSearchSubjects(request);
  }

  async searchActions(
    request: AuthorizationSearchActionsInput,
  ): Promise<AuthorizationActionSearchMessage> {
    return await this.client.searchActions(request);
  }

  async expand(
    request: AuthorizationExpandInput,
  ): Promise<AuthorizationExpandMessage> {
    return await this.client.expand(request);
  }

  async readRelationships(
    request: AuthorizationReadRelationshipsInput,
  ): Promise<AuthorizationReadRelationshipsMessage> {
    return await this.client.readRelationships(request);
  }

  /** Writes and deletes authorization relationships. */
  async writeRelationships(
    request: AuthorizationWriteRelationshipsInput,
  ): Promise<void> {
    await this.client.writeRelationships(request);
  }

  /**
   * Grants a canonical Gestalt subject id editor access to an agent session.
   *
   * This writes the host-managed `agent_session` relationship through the SDK.
   */
  async grantAgentSessionEditor(
    subjectId: string,
    sessionId: string,
  ): Promise<void> {
    await this.writeRelationships(
      agentSessionEditorWriteRequest(subjectId, sessionId),
    );
  }

  async getMetadata(): Promise<AuthorizationMetadataMessage> {
    return await this.client.getMetadata({});
  }

  async getActiveModel(): Promise<AuthorizationGetActiveModelMessage> {
    return await this.client.getActiveModel({});
  }

  async listModels(
    request: AuthorizationListModelsInput = {},
  ): Promise<AuthorizationListModelsResponseMessage> {
    return await this.client.listModels(request);
  }

  async writeModel(
    request: AuthorizationWriteModelInput,
  ): Promise<AuthorizationModelRefMessage> {
    return await this.client.writeModel(request);
  }
}

export interface AuthorizationProviderOptions extends ProviderBaseOptions {
  evaluate: (
    request: AccessEvaluationRequest,
  ) => MaybePromise<MessageInitShape<typeof AccessDecisionSchema>>;
  evaluateMany: (
    request: AccessEvaluationsRequest,
  ) => MaybePromise<MessageInitShape<typeof AccessEvaluationsResponseSchema>>;
  searchResources: (
    request: ResourceSearchRequest,
  ) => MaybePromise<MessageInitShape<typeof ResourceSearchResponseSchema>>;
  searchSubjects: (
    request: SubjectSearchRequest,
  ) => MaybePromise<MessageInitShape<typeof SubjectSearchResponseSchema>>;
  effectiveSearchResources?: (
    request: ResourceSearchRequest,
  ) => MaybePromise<MessageInitShape<typeof ResourceSearchResponseSchema>>;
  effectiveSearchSubjects?: (
    request: EffectiveSubjectSearchRequest,
  ) => MaybePromise<
    MessageInitShape<typeof EffectiveSubjectSearchResponseSchema>
  >;
  searchActions: (
    request: ActionSearchRequest,
  ) => MaybePromise<MessageInitShape<typeof ActionSearchResponseSchema>>;
  expand?: (
    request: ExpandRequest,
  ) => MaybePromise<MessageInitShape<typeof ExpandResponseSchema>>;
  getMetadata: () => MaybePromise<
    MessageInitShape<typeof AuthorizationMetadataSchema>
  >;
  readRelationships: (
    request: ReadRelationshipsRequest,
  ) => MaybePromise<MessageInitShape<typeof ReadRelationshipsResponseSchema>>;
  writeRelationships: (
    request: WriteRelationshipsRequest,
  ) => MaybePromise<void>;
  getActiveModel: () => MaybePromise<
    MessageInitShape<typeof GetActiveModelResponseSchema>
  >;
  listModels: (
    request: ListModelsRequest,
  ) => MaybePromise<MessageInitShape<typeof ListModelsResponseSchema>>;
  writeModel: (
    request: WriteModelRequest,
  ) => MaybePromise<MessageInitShape<typeof AuthorizationModelRefSchema>>;
}

export class AuthorizationProvider extends ProviderBase {
  readonly kind = "authorization" as const;

  private readonly options: AuthorizationProviderOptions;

  constructor(options: AuthorizationProviderOptions) {
    super(options);
    this.options = options;
  }

  async evaluate(request: AccessEvaluationRequest) {
    return await this.options.evaluate(request);
  }

  async evaluateMany(request: AccessEvaluationsRequest) {
    return await this.options.evaluateMany(request);
  }

  async searchResources(request: ResourceSearchRequest) {
    return await this.options.searchResources(request);
  }

  async searchSubjects(request: SubjectSearchRequest) {
    return await this.options.searchSubjects(request);
  }

  supportsEffectiveSearch(): boolean {
    return (
      this.options.effectiveSearchResources !== undefined &&
      this.options.effectiveSearchSubjects !== undefined
    );
  }

  async effectiveSearchResources(request: ResourceSearchRequest) {
    return await this.options.effectiveSearchResources?.(request);
  }

  async effectiveSearchSubjects(request: EffectiveSubjectSearchRequest) {
    return await this.options.effectiveSearchSubjects?.(request);
  }

  async searchActions(request: ActionSearchRequest) {
    return await this.options.searchActions(request);
  }

  supportsExpand(): boolean {
    return this.options.expand !== undefined;
  }

  async expand(request: ExpandRequest) {
    return await this.options.expand?.(request);
  }

  async getMetadata() {
    return await this.options.getMetadata();
  }

  async readRelationships(request: ReadRelationshipsRequest) {
    return await this.options.readRelationships(request);
  }

  async writeRelationships(request: WriteRelationshipsRequest): Promise<void> {
    await this.options.writeRelationships(request);
  }

  async getActiveModel() {
    return await this.options.getActiveModel();
  }

  async listModels(request: ListModelsRequest) {
    return await this.options.listModels(request);
  }

  async writeModel(request: WriteModelRequest) {
    return await this.options.writeModel(request);
  }
}

export function defineAuthorizationProvider(
  options: AuthorizationProviderOptions,
): AuthorizationProvider {
  return new AuthorizationProvider(options);
}

export function isAuthorizationProvider(
  value: unknown,
): value is AuthorizationProvider {
  return (
    value instanceof AuthorizationProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      String((value as { kind?: unknown }).kind ?? "") === "authorization" &&
      "evaluate" in value &&
      "evaluateMany" in value &&
      "searchResources" in value &&
      "searchSubjects" in value &&
      "searchActions" in value &&
      "getMetadata" in value &&
      "readRelationships" in value &&
      "writeRelationships" in value &&
      "getActiveModel" in value &&
      "listModels" in value &&
      "writeModel" in value)
  );
}

export function createAuthorizationProviderService(
  provider: AuthorizationProvider,
): Partial<ServiceImpl<typeof AuthorizationProviderService>> {
  return {
    async evaluate(request) {
      return create(
        AccessDecisionSchema,
        requiredAuthorizationResponse(
          await provider.evaluate(request),
          "evaluate",
        ),
      );
    },
    async evaluateMany(request) {
      return create(
        AccessEvaluationsResponseSchema,
        requiredAuthorizationResponse(
          await provider.evaluateMany(request),
          "evaluate many",
        ),
      );
    },
    async searchResources(request) {
      return create(
        ResourceSearchResponseSchema,
        requiredAuthorizationResponse(
          await provider.searchResources(request),
          "search resources",
        ),
      );
    },
    async searchSubjects(request) {
      return create(
        SubjectSearchResponseSchema,
        requiredAuthorizationResponse(
          await provider.searchSubjects(request),
          "search subjects",
        ),
      );
    },
    async effectiveSearchResources(request) {
      if (!provider.supportsEffectiveSearch()) {
        throw new ConnectError(
          "authorization provider does not support effective search",
          Code.Unimplemented,
        );
      }
      return create(
        ResourceSearchResponseSchema,
        requiredAuthorizationResponse(
          await provider.effectiveSearchResources(request),
          "effective search resources",
        ),
      );
    },
    async effectiveSearchSubjects(request) {
      if (!provider.supportsEffectiveSearch()) {
        throw new ConnectError(
          "authorization provider does not support effective search",
          Code.Unimplemented,
        );
      }
      return create(
        EffectiveSubjectSearchResponseSchema,
        requiredAuthorizationResponse(
          await provider.effectiveSearchSubjects(request),
          "effective search subjects",
        ),
      );
    },
    async searchActions(request) {
      return create(
        ActionSearchResponseSchema,
        requiredAuthorizationResponse(
          await provider.searchActions(request),
          "search actions",
        ),
      );
    },
    async expand(request) {
      if (!provider.supportsExpand()) {
        throw new ConnectError(
          "authorization provider does not support expansion",
          Code.Unimplemented,
        );
      }
      return create(
        ExpandResponseSchema,
        requiredAuthorizationResponse(await provider.expand(request), "expand"),
      );
    },
    async getMetadata() {
      const metadata = create(
        AuthorizationMetadataSchema,
        requiredAuthorizationResponse(await provider.getMetadata(), "metadata"),
      );
      if (provider.supportsEffectiveSearch()) {
        pushCapability(metadata.capabilities, "effective_search_resources");
        pushCapability(metadata.capabilities, "effective_search_subjects");
      }
      if (provider.supportsExpand()) {
        pushCapability(metadata.capabilities, "expand");
      }
      return metadata;
    },
    async readRelationships(request) {
      return create(
        ReadRelationshipsResponseSchema,
        requiredAuthorizationResponse(
          await provider.readRelationships(request),
          "read relationships",
        ),
      );
    },
    async writeRelationships(request) {
      await provider.writeRelationships(request);
      return create(EmptySchema, {});
    },
    async getActiveModel() {
      return create(
        GetActiveModelResponseSchema,
        requiredAuthorizationResponse(
          await provider.getActiveModel(),
          "get active model",
        ),
      );
    },
    async listModels(request) {
      return create(
        ListModelsResponseSchema,
        requiredAuthorizationResponse(
          await provider.listModels(request),
          "list models",
        ),
      );
    },
    async writeModel(request) {
      return create(
        AuthorizationModelRefSchema,
        requiredAuthorizationResponse(
          await provider.writeModel(request),
          "write model",
        ),
      );
    },
  };
}

function requiredAuthorizationResponse<T>(
  value: T | null | undefined,
  label: string,
): T {
  if (value === null || value === undefined) {
    throw new ConnectError(
      `authorization provider returned nil ${label} response`,
      Code.Internal,
    );
  }
  return value;
}

/**
 * Returns a shared host authorization client for authored providers.
 */
export function Authorization(): AuthorizationClient {
  const target = resolveAuthorizationSocketTarget();
  const token = process.env[ENV_AUTHORIZATION_SOCKET_TOKEN]?.trim() ?? "";
  if (
    sharedAuthorizationTransport.client &&
    sharedAuthorizationTransport.target === target &&
    sharedAuthorizationTransport.token === token
  ) {
    return sharedAuthorizationTransport.client;
  }

  const client = new AuthorizationClient(target, token);
  sharedAuthorizationTransport.target = target;
  sharedAuthorizationTransport.token = token;
  sharedAuthorizationTransport.client = client;
  return client;
}

/** Creates an authorization subject reference. */
export function authorizationSubject(
  type: string,
  id: string,
  properties?: JsonObject,
): AuthorizationSubject {
  return properties === undefined ? { type, id } : { type, id, properties };
}

/** Creates an authorization resource reference. */
export function authorizationResource(
  type: string,
  id: string,
  properties?: JsonObject,
): AuthorizationResource {
  return properties === undefined ? { type, id } : { type, id, properties };
}

/** Creates an authorization subject-set reference. */
export function authorizationSubjectSet(
  resource: AuthorizationResource,
  relation: string,
): AuthorizationSubjectSet {
  return { resource, relation };
}

/** Creates a relationship target from a subject. */
export function authorizationSubjectTarget(
  subject: AuthorizationSubject,
): AuthorizationRelationshipTarget {
  return { kind: { case: "subject", value: subject } };
}

/** Creates a relationship target from a resource. */
export function authorizationResourceTarget(
  resource: AuthorizationResource,
): AuthorizationRelationshipTarget {
  return { kind: { case: "resource", value: resource } };
}

/** Creates a relationship target from a subject set. */
export function authorizationSubjectSetTarget(
  resource: AuthorizationResource,
  relation: string,
): AuthorizationRelationshipTarget {
  return {
    kind: {
      case: "subjectSet",
      value: authorizationSubjectSet(resource, relation),
    },
  };
}

/** Creates the managed authorization resource for an agent session. */
export function agentSessionAuthorizationResource(
  sessionId: string,
): AuthorizationResource {
  return authorizationResource(AGENT_SESSION_RESOURCE_TYPE, sessionId);
}

/** Creates an authorization action reference. */
export function authorizationAction(
  name: string,
  properties?: JsonObject,
): AuthorizationAction {
  return properties === undefined ? { name } : { name, properties };
}

/** Creates a relationship tuple for authorization writes. */
export function authorizationRelationship(
  subject: AuthorizationSubject,
  relation: string,
  resource: AuthorizationResource,
  properties?: JsonObject,
): AuthorizationRelationship {
  return properties === undefined
    ? { subject, relation, resource }
    : { subject, relation, resource, properties };
}

/** Creates a generalized relationship tuple for authorization writes. */
export function authorizationRelationshipWithTarget(
  target: AuthorizationRelationshipTarget,
  relation: string,
  resource: AuthorizationResource,
  properties?: JsonObject,
): AuthorizationRelationship {
  return properties === undefined
    ? { target, relation, resource }
    : { target, relation, resource, properties };
}

/**
 * Creates the relationship that shares an agent session with a canonical
 * Gestalt subject id such as `user:123`.
 *
 * The returned relationship mirrors the subject into both the legacy `subject`
 * field and the generalized `target.subject` field so it remains compatible
 * with mixed host/provider versions.
 */
export function agentSessionEditorRelationship(
  subjectId: string,
  sessionId: string,
): AuthorizationRelationship {
  const subject = authorizationSubject(
    AUTHORIZATION_SUBJECT_TYPE_SUBJECT,
    subjectId,
  );
  return {
    subject,
    target: authorizationSubjectTarget(subject),
    relation: AGENT_SESSION_RELATION_EDITOR,
    resource: agentSessionAuthorizationResource(sessionId),
  };
}

/** Creates a relationship-write request that shares an agent session. */
export function agentSessionEditorWriteRequest(
  subjectId: string,
  sessionId: string,
): AuthorizationWriteRelationshipsInput {
  return {
    writes: [agentSessionEditorRelationship(subjectId, sessionId)],
  };
}

/** Creates a relationship key for authorization deletes. */
export function authorizationRelationshipKey(
  subject: AuthorizationSubject,
  relation: string,
  resource: AuthorizationResource,
): AuthorizationRelationshipKey {
  return { subject, relation, resource };
}

/** Creates a generalized relationship key for authorization deletes. */
export function authorizationRelationshipKeyWithTarget(
  target: AuthorizationRelationshipTarget,
  relation: string,
  resource: AuthorizationResource,
): AuthorizationRelationshipKey {
  return { target, relation, resource };
}

function resolveAuthorizationSocketTarget(
  socketPath = process.env[ENV_AUTHORIZATION_SOCKET],
): string {
  const trimmed = socketPath?.trim() ?? "";
  if (!trimmed) {
    throw new Error(`authorization: ${ENV_AUTHORIZATION_SOCKET} is not set`);
  }
  return trimmed;
}

function authorizationTransportOptions(rawTarget: string): {
  baseUrl: string;
  nodeOptions?: { path: string };
} {
  const target = rawTarget.trim();
  if (!target) {
    throw new Error("authorization: transport target is required");
  }
  if (target.startsWith("tcp://")) {
    const address = target.slice("tcp://".length).trim();
    if (!address) {
      throw new Error(
        `authorization: tcp target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `http://${address}` };
  }
  if (target.startsWith("tls://")) {
    const address = target.slice("tls://".length).trim();
    if (!address) {
      throw new Error(
        `authorization: tls target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `https://${address}` };
  }
  if (target.startsWith("unix://")) {
    const socketPath = target.slice("unix://".length).trim();
    if (!socketPath) {
      throw new Error(
        `authorization: unix target ${JSON.stringify(rawTarget)} is missing a socket path`,
      );
    }
    return { baseUrl: "http://localhost", nodeOptions: { path: socketPath } };
  }
  if (target.includes("://")) {
    const parsed = new URL(target);
    throw new Error(
      `authorization: unsupported target scheme ${JSON.stringify(parsed.protocol.replace(/:$/, ""))}`,
    );
  }
  return { baseUrl: "http://localhost", nodeOptions: { path: target } };
}

function authorizationRelayTokenInterceptor(token: string): Interceptor {
  return (next) => async (req) => {
    req.header.set(AUTHORIZATION_RELAY_TOKEN_HEADER, token);
    return next(req);
  };
}

function pushCapability(capabilities: string[], capability: string): void {
  if (!capabilities.includes(capability)) {
    capabilities.push(capability);
  }
}
