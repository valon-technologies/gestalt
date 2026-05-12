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
  AuthorizationProvider as AuthorizationProviderService,
  AccessEvaluationRequestSchema,
  AccessDecisionSchema,
  AccessEvaluationsRequestSchema,
  AccessEvaluationsResponseSchema,
  ActionSearchRequestSchema,
  ActionSearchResponseSchema,
  ActionSchema,
  AuthorizationMetadataSchema,
  AuthorizationModelActionSchema,
  AuthorizationModelAllowedTargetSchema,
  AuthorizationModelComputedUsersetSchema,
  AuthorizationModelResourceTypeSchema,
  AuthorizationModelRelationSchema,
  AuthorizationModelRewriteSchema,
  AuthorizationModelRewriteUnionSchema,
  AuthorizationModelSchema,
  AuthorizationModelRefSchema,
  AuthorizationModelSubjectSetTargetSchema,
  AuthorizationModelTupleToUsersetSchema,
  EffectiveSubjectSearchRequestSchema,
  EffectiveSubjectSearchResponseSchema,
  ExpandNodeSchema,
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
import { dateFromTimestamp, timestampFromDate } from "./protocol.ts";

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

export interface AuthorizationSubject {
  type?: string | undefined;
  id?: string | undefined;
  properties?: JsonObject | undefined;
}

export interface AuthorizationResource {
  type?: string | undefined;
  id?: string | undefined;
  properties?: JsonObject | undefined;
}

export interface AuthorizationSubjectSet {
  resource?: AuthorizationResource | undefined;
  relation?: string | undefined;
}

export interface AuthorizationRelationshipTarget {
  subject?: AuthorizationSubject | undefined;
  resource?: AuthorizationResource | undefined;
  subjectSet?: AuthorizationSubjectSet | undefined;
}

export interface AuthorizationAction {
  name?: string | undefined;
  properties?: JsonObject | undefined;
}

export interface AuthorizationEvaluateInput {
  subject?: AuthorizationSubject | undefined;
  action?: AuthorizationAction | undefined;
  resource?: AuthorizationResource | undefined;
  context?: JsonObject | undefined;
}

export interface AuthorizationDecision {
  allowed?: boolean | undefined;
  context?: JsonObject | undefined;
  modelId?: string | undefined;
}

export interface AuthorizationEvaluateManyInput {
  requests?: AuthorizationEvaluateInput[] | undefined;
}

export interface AuthorizationEvaluationsResponse {
  decisions?: AuthorizationDecision[] | undefined;
}

export interface AuthorizationSearchResourcesInput {
  subject?: AuthorizationSubject | undefined;
  action?: AuthorizationAction | undefined;
  resourceType?: string | undefined;
  context?: JsonObject | undefined;
  pageSize?: number | undefined;
  pageToken?: string | undefined;
}

export interface AuthorizationResourceSearchResponse {
  resources?: AuthorizationResource[] | undefined;
  nextPageToken?: string | undefined;
  modelId?: string | undefined;
}

export interface AuthorizationSearchSubjectsInput {
  resource?: AuthorizationResource | undefined;
  action?: AuthorizationAction | undefined;
  subjectType?: string | undefined;
  context?: JsonObject | undefined;
  pageSize?: number | undefined;
  pageToken?: string | undefined;
}

export interface AuthorizationSubjectSearchResponse {
  subjects?: AuthorizationSubject[] | undefined;
  nextPageToken?: string | undefined;
  modelId?: string | undefined;
}

export interface AuthorizationEffectiveSearchSubjectsInput {
  resource?: AuthorizationResource | undefined;
  action?: AuthorizationAction | undefined;
  context?: JsonObject | undefined;
  pageSize?: number | undefined;
  pageToken?: string | undefined;
}

export interface AuthorizationEffectiveSubjectSearchResponse {
  targets?: AuthorizationRelationshipTarget[] | undefined;
  nextPageToken?: string | undefined;
  modelId?: string | undefined;
  truncated?: boolean | undefined;
}

export interface AuthorizationSearchActionsInput {
  subject?: AuthorizationSubject | undefined;
  resource?: AuthorizationResource | undefined;
  context?: JsonObject | undefined;
  pageSize?: number | undefined;
  pageToken?: string | undefined;
}

export interface AuthorizationActionSearchResponse {
  actions?: AuthorizationAction[] | undefined;
  nextPageToken?: string | undefined;
  modelId?: string | undefined;
}

export interface AuthorizationMetadata {
  capabilities?: string[] | undefined;
  activeModelId?: string | undefined;
}

export interface AuthorizationRelationship {
  subject?: AuthorizationSubject | undefined;
  relation?: string | undefined;
  resource?: AuthorizationResource | undefined;
  properties?: JsonObject | undefined;
  target?: AuthorizationRelationshipTarget | undefined;
}

export interface AuthorizationRelationshipKey {
  subject?: AuthorizationSubject | undefined;
  relation?: string | undefined;
  resource?: AuthorizationResource | undefined;
  target?: AuthorizationRelationshipTarget | undefined;
}

export interface AuthorizationReadRelationshipsInput {
  subject?: AuthorizationSubject | undefined;
  relation?: string | undefined;
  resource?: AuthorizationResource | undefined;
  pageSize?: number | undefined;
  pageToken?: string | undefined;
  modelId?: string | undefined;
  target?: AuthorizationRelationshipTarget | undefined;
}

export interface AuthorizationReadRelationshipsResponse {
  relationships?: AuthorizationRelationship[] | undefined;
  nextPageToken?: string | undefined;
  modelId?: string | undefined;
}

export interface AuthorizationWriteRelationshipsInput {
  writes?: AuthorizationRelationship[] | undefined;
  deletes?: AuthorizationRelationshipKey[] | undefined;
  modelId?: string | undefined;
}

export interface AuthorizationModel {
  version?: number | undefined;
  resourceTypes?: AuthorizationModelResourceType[] | undefined;
}

export interface AuthorizationModelResourceType {
  name?: string | undefined;
  relations?: AuthorizationModelRelation[] | undefined;
  actions?: AuthorizationModelAction[] | undefined;
}

export interface AuthorizationModelRelation {
  name?: string | undefined;
  subjectTypes?: string[] | undefined;
  allowedTargets?: AuthorizationModelAllowedTarget[] | undefined;
  rewrite?: AuthorizationModelRewrite | undefined;
}

export interface AuthorizationModelAction {
  name?: string | undefined;
  relations?: string[] | undefined;
  rewrite?: AuthorizationModelRewrite | undefined;
}

export interface AuthorizationModelAllowedTarget {
  subjectType?: string | undefined;
  resourceType?: string | undefined;
  subjectSet?: AuthorizationModelSubjectSetTarget | undefined;
}

export interface AuthorizationModelSubjectSetTarget {
  resourceType?: string | undefined;
  relation?: string | undefined;
}

export interface AuthorizationModelRewrite {
  this?: Record<string, never> | undefined;
  computedUserset?: AuthorizationModelComputedUserset | undefined;
  tupleToUserset?: AuthorizationModelTupleToUserset | undefined;
  union?: AuthorizationModelRewriteUnion | undefined;
}

export interface AuthorizationModelComputedUserset {
  relation?: string | undefined;
}

export interface AuthorizationModelTupleToUserset {
  tuplesetRelation?: string | undefined;
  computedRelation?: string | undefined;
}

export interface AuthorizationModelRewriteUnion {
  children?: AuthorizationModelRewrite[] | undefined;
}

export interface AuthorizationModelRef {
  id?: string | undefined;
  version?: string | undefined;
  createdAt?: Date | undefined;
}

export interface AuthorizationGetActiveModelResponse {
  model?: AuthorizationModelRef | undefined;
}

export interface AuthorizationListModelsInput {
  pageSize?: number | undefined;
  pageToken?: string | undefined;
}

export interface AuthorizationListModelsResponse {
  models?: AuthorizationModelRef[] | undefined;
  nextPageToken?: string | undefined;
}

export interface AuthorizationWriteModelInput {
  model?: AuthorizationModel | undefined;
}

export interface AuthorizationExpandInput {
  resource?: AuthorizationResource | undefined;
  relation?: string | undefined;
  context?: JsonObject | undefined;
  maxDepth?: number | undefined;
  modelId?: string | undefined;
}

export interface AuthorizationExpandNode {
  target?: AuthorizationRelationshipTarget | undefined;
  relation?: string | undefined;
  children?: AuthorizationExpandNode[] | undefined;
}

export interface AuthorizationExpandResponse {
  root?: AuthorizationExpandNode | undefined;
  truncated?: boolean | undefined;
  cycleDetected?: boolean | undefined;
  maxDepthReached?: boolean | undefined;
  modelId?: string | undefined;
}

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
      baseUrl: transportOptions.baseUrl,
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
  ): Promise<AuthorizationDecision> {
    return authorizationDecisionFromTransport(
      await this.client.evaluate(authorizationEvaluateToTransport(request)),
    );
  }

  async evaluateMany(
    request: AuthorizationEvaluateManyInput,
  ): Promise<AuthorizationEvaluationsResponse> {
    return authorizationEvaluationsResponseFromTransport(
      await this.client.evaluateMany(authorizationEvaluateManyToTransport(request)),
    );
  }

  async searchResources(
    request: AuthorizationSearchResourcesInput,
  ): Promise<AuthorizationResourceSearchResponse> {
    return authorizationResourceSearchResponseFromTransport(
      await this.client.searchResources(authorizationSearchResourcesToTransport(request)),
    );
  }

  async searchSubjects(
    request: AuthorizationSearchSubjectsInput,
  ): Promise<AuthorizationSubjectSearchResponse> {
    return authorizationSubjectSearchResponseFromTransport(
      await this.client.searchSubjects(authorizationSearchSubjectsToTransport(request)),
    );
  }

  async effectiveSearchResources(
    request: AuthorizationSearchResourcesInput,
  ): Promise<AuthorizationResourceSearchResponse> {
    return authorizationResourceSearchResponseFromTransport(
      await this.client.effectiveSearchResources(
        authorizationSearchResourcesToTransport(request),
      ),
    );
  }

  async effectiveSearchSubjects(
    request: AuthorizationEffectiveSearchSubjectsInput,
  ): Promise<AuthorizationEffectiveSubjectSearchResponse> {
    return authorizationEffectiveSubjectSearchResponseFromTransport(
      await this.client.effectiveSearchSubjects(
        authorizationEffectiveSearchSubjectsToTransport(request),
      ),
    );
  }

  async searchActions(
    request: AuthorizationSearchActionsInput,
  ): Promise<AuthorizationActionSearchResponse> {
    return authorizationActionSearchResponseFromTransport(
      await this.client.searchActions(authorizationSearchActionsToTransport(request)),
    );
  }

  async expand(
    request: AuthorizationExpandInput,
  ): Promise<AuthorizationExpandResponse> {
    return authorizationExpandResponseFromTransport(
      await this.client.expand(authorizationExpandToTransport(request)),
    );
  }

  async readRelationships(
    request: AuthorizationReadRelationshipsInput,
  ): Promise<AuthorizationReadRelationshipsResponse> {
    return authorizationReadRelationshipsResponseFromTransport(
      await this.client.readRelationships(
        authorizationReadRelationshipsToTransport(request),
      ),
    );
  }

  /** Writes and deletes authorization relationships. */
  async writeRelationships(
    request: AuthorizationWriteRelationshipsInput,
  ): Promise<void> {
    await this.client.writeRelationships(authorizationWriteRelationshipsToTransport(request));
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

  async getMetadata(): Promise<AuthorizationMetadata> {
    return authorizationMetadataFromTransport(await this.client.getMetadata({}));
  }

  async getActiveModel(): Promise<AuthorizationGetActiveModelResponse> {
    return authorizationGetActiveModelFromTransport(
      await this.client.getActiveModel({}),
    );
  }

  async listModels(
    request: AuthorizationListModelsInput = {},
  ): Promise<AuthorizationListModelsResponse> {
    return authorizationListModelsResponseFromTransport(
      await this.client.listModels(authorizationListModelsToTransport(request)),
    );
  }

  async writeModel(
    request: AuthorizationWriteModelInput,
  ): Promise<AuthorizationModelRef> {
    return authorizationModelRefFromTransport(
      await this.client.writeModel(authorizationWriteModelToTransport(request)),
    );
  }
}

export interface AuthorizationProviderOptions extends ProviderBaseOptions {
  evaluate: (
    request: AuthorizationEvaluateInput,
  ) => MaybePromise<AuthorizationDecision>;
  evaluateMany: (
    request: AuthorizationEvaluateManyInput,
  ) => MaybePromise<AuthorizationEvaluationsResponse>;
  searchResources: (
    request: AuthorizationSearchResourcesInput,
  ) => MaybePromise<AuthorizationResourceSearchResponse>;
  searchSubjects: (
    request: AuthorizationSearchSubjectsInput,
  ) => MaybePromise<AuthorizationSubjectSearchResponse>;
  effectiveSearchResources?: (
    request: AuthorizationSearchResourcesInput,
  ) => MaybePromise<AuthorizationResourceSearchResponse>;
  effectiveSearchSubjects?: (
    request: AuthorizationEffectiveSearchSubjectsInput,
  ) => MaybePromise<AuthorizationEffectiveSubjectSearchResponse>;
  searchActions: (
    request: AuthorizationSearchActionsInput,
  ) => MaybePromise<AuthorizationActionSearchResponse>;
  expand?: (request: AuthorizationExpandInput) => MaybePromise<AuthorizationExpandResponse> | undefined;
  getMetadata: () => MaybePromise<AuthorizationMetadata>;
  readRelationships: (
    request: AuthorizationReadRelationshipsInput,
  ) => MaybePromise<AuthorizationReadRelationshipsResponse>;
  writeRelationships: (
    request: AuthorizationWriteRelationshipsInput,
  ) => MaybePromise<void>;
  getActiveModel: () => MaybePromise<AuthorizationGetActiveModelResponse>;
  listModels: (
    request: AuthorizationListModelsInput,
  ) => MaybePromise<AuthorizationListModelsResponse>;
  writeModel: (
    request: AuthorizationWriteModelInput,
  ) => MaybePromise<AuthorizationModelRef>;
}

export class AuthorizationProvider extends ProviderBase {
  readonly kind = "authorization" as const;

  private readonly options: AuthorizationProviderOptions;

  constructor(options: AuthorizationProviderOptions) {
    super(options);
    this.options = options;
  }

  async evaluate(request: AuthorizationEvaluateInput) {
    return await this.options.evaluate(request);
  }

  async evaluateMany(request: AuthorizationEvaluateManyInput) {
    return await this.options.evaluateMany(request);
  }

  async searchResources(request: AuthorizationSearchResourcesInput) {
    return await this.options.searchResources(request);
  }

  async searchSubjects(request: AuthorizationSearchSubjectsInput) {
    return await this.options.searchSubjects(request);
  }

  supportsEffectiveSearch(): boolean {
    return (
      this.options.effectiveSearchResources !== undefined &&
      this.options.effectiveSearchSubjects !== undefined
    );
  }

  async effectiveSearchResources(request: AuthorizationSearchResourcesInput) {
    return await this.options.effectiveSearchResources?.(request);
  }

  async effectiveSearchSubjects(request: AuthorizationEffectiveSearchSubjectsInput) {
    return await this.options.effectiveSearchSubjects?.(request);
  }

  async searchActions(request: AuthorizationSearchActionsInput) {
    return await this.options.searchActions(request);
  }

  supportsExpand(): boolean {
    return this.options.expand !== undefined;
  }

  async expand(request: AuthorizationExpandInput) {
    return await this.options.expand?.(request);
  }

  async getMetadata() {
    return await this.options.getMetadata();
  }

  async readRelationships(request: AuthorizationReadRelationshipsInput) {
    return await this.options.readRelationships(request);
  }

  async writeRelationships(request: AuthorizationWriteRelationshipsInput): Promise<void> {
    await this.options.writeRelationships(request);
  }

  async getActiveModel() {
    return await this.options.getActiveModel();
  }

  async listModels(request: AuthorizationListModelsInput) {
    return await this.options.listModels(request);
  }

  async writeModel(request: AuthorizationWriteModelInput) {
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
        authorizationDecisionToTransport(
          requiredAuthorizationResponse(
            await provider.evaluate(authorizationEvaluateFromTransport(request)),
            "evaluate",
          ),
        ),
      );
    },
    async evaluateMany(request) {
      return create(
        AccessEvaluationsResponseSchema,
        authorizationEvaluationsResponseToTransport(
          requiredAuthorizationResponse(
            await provider.evaluateMany(
              authorizationEvaluateManyFromTransport(request),
            ),
            "evaluate many",
          ),
        ),
      );
    },
    async searchResources(request) {
      return create(
        ResourceSearchResponseSchema,
        authorizationResourceSearchResponseToTransport(
          requiredAuthorizationResponse(
            await provider.searchResources(
              authorizationSearchResourcesFromTransport(request),
            ),
            "search resources",
          ),
        ),
      );
    },
    async searchSubjects(request) {
      return create(
        SubjectSearchResponseSchema,
        authorizationSubjectSearchResponseToTransport(
          requiredAuthorizationResponse(
            await provider.searchSubjects(
              authorizationSearchSubjectsFromTransport(request),
            ),
            "search subjects",
          ),
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
        authorizationResourceSearchResponseToTransport(
          requiredAuthorizationResponse(
            await provider.effectiveSearchResources(
              authorizationSearchResourcesFromTransport(request),
            ),
            "effective search resources",
          ),
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
        authorizationEffectiveSubjectSearchResponseToTransport(
          requiredAuthorizationResponse(
            await provider.effectiveSearchSubjects(
              authorizationEffectiveSearchSubjectsFromTransport(request),
            ),
            "effective search subjects",
          ),
        ),
      );
    },
    async searchActions(request) {
      return create(
        ActionSearchResponseSchema,
        authorizationActionSearchResponseToTransport(
          requiredAuthorizationResponse(
            await provider.searchActions(
              authorizationSearchActionsFromTransport(request),
            ),
            "search actions",
          ),
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
        authorizationExpandResponseToTransport(
          requiredAuthorizationResponse(
            await provider.expand(authorizationExpandFromTransport(request)),
            "expand",
          ),
        ),
      );
    },
    async getMetadata() {
      const metadata = create(
        AuthorizationMetadataSchema,
        authorizationMetadataToTransport(
          requiredAuthorizationResponse(await provider.getMetadata(), "metadata"),
        ),
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
        authorizationReadRelationshipsResponseToTransport(
          requiredAuthorizationResponse(
            await provider.readRelationships(
              authorizationReadRelationshipsFromTransport(request),
            ),
            "read relationships",
          ),
        ),
      );
    },
    async writeRelationships(request) {
      await provider.writeRelationships(
        authorizationWriteRelationshipsFromTransport(request),
      );
      return create(EmptySchema, {});
    },
    async getActiveModel() {
      return create(
        GetActiveModelResponseSchema,
        authorizationGetActiveModelToTransport(
          requiredAuthorizationResponse(
            await provider.getActiveModel(),
            "get active model",
          ),
        ),
      );
    },
    async listModels(request) {
      return create(
        ListModelsResponseSchema,
        authorizationListModelsResponseToTransport(
          requiredAuthorizationResponse(
            await provider.listModels(authorizationListModelsFromTransport(request)),
            "list models",
          ),
        ),
      );
    },
    async writeModel(request) {
      return create(
        AuthorizationModelRefSchema,
        authorizationModelRefToTransport(
          requiredAuthorizationResponse(
            await provider.writeModel(authorizationWriteModelFromTransport(request)),
            "write model",
          ),
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

type SubjectTransport = MessageInitShape<typeof SubjectSchema>;
type ResourceTransport = MessageInitShape<typeof ResourceSchema>;
type SubjectSetTransport = MessageInitShape<typeof SubjectSetSchema>;
type RelationshipTargetTransport = MessageInitShape<typeof RelationshipTargetSchema>;
type ActionTransport = MessageInitShape<typeof ActionSchema>;
type RelationshipTransport = MessageInitShape<typeof RelationshipSchema>;
type RelationshipKeyTransport = MessageInitShape<typeof RelationshipKeySchema>;
type AuthorizationModelTransport = MessageInitShape<typeof AuthorizationModelSchema>;
type AuthorizationModelResourceTypeTransport = MessageInitShape<
  typeof AuthorizationModelResourceTypeSchema
>;
type AuthorizationModelRelationTransport = MessageInitShape<
  typeof AuthorizationModelRelationSchema
>;
type AuthorizationModelActionTransport = MessageInitShape<
  typeof AuthorizationModelActionSchema
>;
type AuthorizationModelAllowedTargetTransport = MessageInitShape<
  typeof AuthorizationModelAllowedTargetSchema
>;
type AuthorizationModelSubjectSetTargetTransport = MessageInitShape<
  typeof AuthorizationModelSubjectSetTargetSchema
>;
type AuthorizationModelRewriteTransport = MessageInitShape<
  typeof AuthorizationModelRewriteSchema
>;
type AuthorizationModelComputedUsersetTransport = MessageInitShape<
  typeof AuthorizationModelComputedUsersetSchema
>;
type AuthorizationModelTupleToUsersetTransport = MessageInitShape<
  typeof AuthorizationModelTupleToUsersetSchema
>;
type AuthorizationModelRewriteUnionTransport = MessageInitShape<
  typeof AuthorizationModelRewriteUnionSchema
>;
type AuthorizationModelRefTransport = MessageInitShape<
  typeof AuthorizationModelRefSchema
>;
type ExpandNodeTransport = MessageInitShape<typeof ExpandNodeSchema>;

function authorizationSubjectToTransport(
  value?: AuthorizationSubject,
): SubjectTransport | undefined {
  if (value === undefined) {
    return undefined;
  }
  return {
    type: value.type ?? "",
    id: value.id ?? "",
    properties: value.properties,
  };
}

function authorizationSubjectFromTransport(
  value?: SubjectTransport,
): AuthorizationSubject | undefined {
  if (value === undefined) {
    return undefined;
  }
  return {
    type: value.type ?? "",
    id: value.id ?? "",
    properties: value.properties,
  };
}

function authorizationResourceToTransport(
  value?: AuthorizationResource,
): ResourceTransport | undefined {
  if (value === undefined) {
    return undefined;
  }
  return {
    type: value.type ?? "",
    id: value.id ?? "",
    properties: value.properties,
  };
}

function authorizationResourceFromTransport(
  value?: ResourceTransport,
): AuthorizationResource | undefined {
  if (value === undefined) {
    return undefined;
  }
  return {
    type: value.type ?? "",
    id: value.id ?? "",
    properties: value.properties,
  };
}

function authorizationSubjectSetToTransport(
  value?: AuthorizationSubjectSet,
): SubjectSetTransport | undefined {
  if (value === undefined) {
    return undefined;
  }
  return {
    resource: authorizationResourceToTransport(value.resource),
    relation: value.relation ?? "",
  };
}

function authorizationSubjectSetFromTransport(
  value?: SubjectSetTransport,
): AuthorizationSubjectSet | undefined {
  if (value === undefined) {
    return undefined;
  }
  return {
    resource: authorizationResourceFromTransport(value.resource),
    relation: value.relation ?? "",
  };
}

function authorizationRelationshipTargetToTransport(
  value?: AuthorizationRelationshipTarget,
): RelationshipTargetTransport | undefined {
  if (value === undefined) {
    return undefined;
  }
  if (value.subject !== undefined) {
    return {
      kind: {
        case: "subject",
        value: authorizationSubjectToTransport(value.subject) ?? {},
      },
    };
  }
  if (value.resource !== undefined) {
    return {
      kind: {
        case: "resource",
        value: authorizationResourceToTransport(value.resource) ?? {},
      },
    };
  }
  if (value.subjectSet !== undefined) {
    return {
      kind: {
        case: "subjectSet",
        value: authorizationSubjectSetToTransport(value.subjectSet) ?? {},
      },
    };
  }
  return undefined;
}

function authorizationRelationshipTargetFromTransport(
  value?: RelationshipTargetTransport,
): AuthorizationRelationshipTarget | undefined {
  if (value === undefined) {
    return undefined;
  }
  switch (value.kind?.case) {
    case "subject":
      return { subject: authorizationSubjectFromTransport(value.kind.value) };
    case "resource":
      return { resource: authorizationResourceFromTransport(value.kind.value) };
    case "subjectSet":
      return {
        subjectSet: authorizationSubjectSetFromTransport(value.kind.value),
      };
    default:
      return {};
  }
}

function authorizationActionToTransport(
  value?: AuthorizationAction,
): ActionTransport | undefined {
  if (value === undefined) {
    return undefined;
  }
  return {
    name: value.name ?? "",
    properties: value.properties,
  };
}

function authorizationActionFromTransport(
  value?: ActionTransport,
): AuthorizationAction | undefined {
  if (value === undefined) {
    return undefined;
  }
  return {
    name: value.name ?? "",
    properties: value.properties,
  };
}

function authorizationEvaluateToTransport(
  value: AuthorizationEvaluateInput,
): MessageInitShape<typeof AccessEvaluationRequestSchema> {
  return {
    subject: authorizationSubjectToTransport(value.subject),
    action: authorizationActionToTransport(value.action),
    resource: authorizationResourceToTransport(value.resource),
    context: value.context,
  };
}

function authorizationEvaluateFromTransport(
  value: MessageInitShape<typeof AccessEvaluationRequestSchema>,
): AuthorizationEvaluateInput {
  return {
    subject: authorizationSubjectFromTransport(value.subject),
    action: authorizationActionFromTransport(value.action),
    resource: authorizationResourceFromTransport(value.resource),
    context: value.context,
  };
}

function authorizationDecisionToTransport(
  value: AuthorizationDecision,
): MessageInitShape<typeof AccessDecisionSchema> {
  return {
    allowed: value.allowed ?? false,
    context: value.context,
    modelId: value.modelId ?? "",
  };
}

function authorizationDecisionFromTransport(
  value: MessageInitShape<typeof AccessDecisionSchema>,
): AuthorizationDecision {
  return {
    allowed: value.allowed ?? false,
    context: value.context,
    modelId: value.modelId ?? "",
  };
}

function authorizationEvaluateManyToTransport(
  value: AuthorizationEvaluateManyInput,
): MessageInitShape<typeof AccessEvaluationsRequestSchema> {
  return {
    requests: value.requests?.map(authorizationEvaluateToTransport) ?? [],
  };
}

function authorizationEvaluateManyFromTransport(
  value: MessageInitShape<typeof AccessEvaluationsRequestSchema>,
): AuthorizationEvaluateManyInput {
  return {
    requests: value.requests?.map(authorizationEvaluateFromTransport) ?? [],
  };
}

function authorizationEvaluationsResponseToTransport(
  value: AuthorizationEvaluationsResponse,
): MessageInitShape<typeof AccessEvaluationsResponseSchema> {
  return {
    decisions: value.decisions?.map(authorizationDecisionToTransport) ?? [],
  };
}

function authorizationEvaluationsResponseFromTransport(
  value: MessageInitShape<typeof AccessEvaluationsResponseSchema>,
): AuthorizationEvaluationsResponse {
  return {
    decisions: value.decisions?.map(authorizationDecisionFromTransport) ?? [],
  };
}

function authorizationSearchResourcesToTransport(
  value: AuthorizationSearchResourcesInput,
): MessageInitShape<typeof ResourceSearchRequestSchema> {
  return {
    subject: authorizationSubjectToTransport(value.subject),
    action: authorizationActionToTransport(value.action),
    resourceType: value.resourceType ?? "",
    context: value.context,
    pageSize: value.pageSize ?? 0,
    pageToken: value.pageToken ?? "",
  };
}

function authorizationSearchResourcesFromTransport(
  value: MessageInitShape<typeof ResourceSearchRequestSchema>,
): AuthorizationSearchResourcesInput {
  return {
    subject: authorizationSubjectFromTransport(value.subject),
    action: authorizationActionFromTransport(value.action),
    resourceType: value.resourceType ?? "",
    context: value.context,
    pageSize: value.pageSize ?? 0,
    pageToken: value.pageToken ?? "",
  };
}

function authorizationResourceSearchResponseToTransport(
  value: AuthorizationResourceSearchResponse,
): MessageInitShape<typeof ResourceSearchResponseSchema> {
  return {
    resources: value.resources?.map((entry) => authorizationResourceToTransport(entry)!) ?? [],
    nextPageToken: value.nextPageToken ?? "",
    modelId: value.modelId ?? "",
  };
}

function authorizationResourceSearchResponseFromTransport(
  value: MessageInitShape<typeof ResourceSearchResponseSchema>,
): AuthorizationResourceSearchResponse {
  return {
    resources: value.resources?.map((entry) => authorizationResourceFromTransport(entry)!) ?? [],
    nextPageToken: value.nextPageToken ?? "",
    modelId: value.modelId ?? "",
  };
}

function authorizationSearchSubjectsToTransport(
  value: AuthorizationSearchSubjectsInput,
): MessageInitShape<typeof SubjectSearchRequestSchema> {
  return {
    resource: authorizationResourceToTransport(value.resource),
    action: authorizationActionToTransport(value.action),
    subjectType: value.subjectType ?? "",
    context: value.context,
    pageSize: value.pageSize ?? 0,
    pageToken: value.pageToken ?? "",
  };
}

function authorizationSearchSubjectsFromTransport(
  value: MessageInitShape<typeof SubjectSearchRequestSchema>,
): AuthorizationSearchSubjectsInput {
  return {
    resource: authorizationResourceFromTransport(value.resource),
    action: authorizationActionFromTransport(value.action),
    subjectType: value.subjectType ?? "",
    context: value.context,
    pageSize: value.pageSize ?? 0,
    pageToken: value.pageToken ?? "",
  };
}

function authorizationSubjectSearchResponseToTransport(
  value: AuthorizationSubjectSearchResponse,
): MessageInitShape<typeof SubjectSearchResponseSchema> {
  return {
    subjects: value.subjects?.map((entry) => authorizationSubjectToTransport(entry)!) ?? [],
    nextPageToken: value.nextPageToken ?? "",
    modelId: value.modelId ?? "",
  };
}

function authorizationSubjectSearchResponseFromTransport(
  value: MessageInitShape<typeof SubjectSearchResponseSchema>,
): AuthorizationSubjectSearchResponse {
  return {
    subjects: value.subjects?.map((entry) => authorizationSubjectFromTransport(entry)!) ?? [],
    nextPageToken: value.nextPageToken ?? "",
    modelId: value.modelId ?? "",
  };
}

function authorizationEffectiveSearchSubjectsToTransport(
  value: AuthorizationEffectiveSearchSubjectsInput,
): MessageInitShape<typeof EffectiveSubjectSearchRequestSchema> {
  return {
    resource: authorizationResourceToTransport(value.resource),
    action: authorizationActionToTransport(value.action),
    context: value.context,
    pageSize: value.pageSize ?? 0,
    pageToken: value.pageToken ?? "",
  };
}

function authorizationEffectiveSearchSubjectsFromTransport(
  value: MessageInitShape<typeof EffectiveSubjectSearchRequestSchema>,
): AuthorizationEffectiveSearchSubjectsInput {
  return {
    resource: authorizationResourceFromTransport(value.resource),
    action: authorizationActionFromTransport(value.action),
    context: value.context,
    pageSize: value.pageSize ?? 0,
    pageToken: value.pageToken ?? "",
  };
}

function authorizationEffectiveSubjectSearchResponseToTransport(
  value: AuthorizationEffectiveSubjectSearchResponse,
): MessageInitShape<typeof EffectiveSubjectSearchResponseSchema> {
  return {
    targets: value.targets?.map((entry) => authorizationRelationshipTargetToTransport(entry)!) ?? [],
    nextPageToken: value.nextPageToken ?? "",
    modelId: value.modelId ?? "",
    truncated: value.truncated ?? false,
  };
}

function authorizationEffectiveSubjectSearchResponseFromTransport(
  value: MessageInitShape<typeof EffectiveSubjectSearchResponseSchema>,
): AuthorizationEffectiveSubjectSearchResponse {
  return {
    targets: value.targets?.map((entry) => authorizationRelationshipTargetFromTransport(entry)!) ?? [],
    nextPageToken: value.nextPageToken ?? "",
    modelId: value.modelId ?? "",
    truncated: value.truncated ?? false,
  };
}

function authorizationSearchActionsToTransport(
  value: AuthorizationSearchActionsInput,
): MessageInitShape<typeof ActionSearchRequestSchema> {
  return {
    subject: authorizationSubjectToTransport(value.subject),
    resource: authorizationResourceToTransport(value.resource),
    context: value.context,
    pageSize: value.pageSize ?? 0,
    pageToken: value.pageToken ?? "",
  };
}

function authorizationSearchActionsFromTransport(
  value: MessageInitShape<typeof ActionSearchRequestSchema>,
): AuthorizationSearchActionsInput {
  return {
    subject: authorizationSubjectFromTransport(value.subject),
    resource: authorizationResourceFromTransport(value.resource),
    context: value.context,
    pageSize: value.pageSize ?? 0,
    pageToken: value.pageToken ?? "",
  };
}

function authorizationActionSearchResponseToTransport(
  value: AuthorizationActionSearchResponse,
): MessageInitShape<typeof ActionSearchResponseSchema> {
  return {
    actions: value.actions?.map((entry) => authorizationActionToTransport(entry)!) ?? [],
    nextPageToken: value.nextPageToken ?? "",
    modelId: value.modelId ?? "",
  };
}

function authorizationActionSearchResponseFromTransport(
  value: MessageInitShape<typeof ActionSearchResponseSchema>,
): AuthorizationActionSearchResponse {
  return {
    actions: value.actions?.map((entry) => authorizationActionFromTransport(entry)!) ?? [],
    nextPageToken: value.nextPageToken ?? "",
    modelId: value.modelId ?? "",
  };
}

function authorizationMetadataToTransport(
  value: AuthorizationMetadata,
): MessageInitShape<typeof AuthorizationMetadataSchema> {
  return {
    capabilities: value.capabilities ?? [],
    activeModelId: value.activeModelId ?? "",
  };
}

function authorizationMetadataFromTransport(
  value: MessageInitShape<typeof AuthorizationMetadataSchema>,
): AuthorizationMetadata {
  return {
    capabilities: value.capabilities ?? [],
    activeModelId: value.activeModelId ?? "",
  };
}

function authorizationRelationshipToTransport(
  value: AuthorizationRelationship,
): RelationshipTransport {
  return {
    subject: authorizationSubjectToTransport(value.subject),
    relation: value.relation ?? "",
    resource: authorizationResourceToTransport(value.resource),
    properties: value.properties,
    target: authorizationRelationshipTargetToTransport(value.target),
  };
}

function authorizationRelationshipFromTransport(
  value: RelationshipTransport,
): AuthorizationRelationship {
  return {
    subject: authorizationSubjectFromTransport(value.subject),
    relation: value.relation ?? "",
    resource: authorizationResourceFromTransport(value.resource),
    properties: value.properties,
    target: authorizationRelationshipTargetFromTransport(value.target),
  };
}

function authorizationRelationshipKeyToTransport(
  value: AuthorizationRelationshipKey,
): RelationshipKeyTransport {
  return {
    subject: authorizationSubjectToTransport(value.subject),
    relation: value.relation ?? "",
    resource: authorizationResourceToTransport(value.resource),
    target: authorizationRelationshipTargetToTransport(value.target),
  };
}

function authorizationRelationshipKeyFromTransport(
  value: RelationshipKeyTransport,
): AuthorizationRelationshipKey {
  return {
    subject: authorizationSubjectFromTransport(value.subject),
    relation: value.relation ?? "",
    resource: authorizationResourceFromTransport(value.resource),
    target: authorizationRelationshipTargetFromTransport(value.target),
  };
}

function authorizationReadRelationshipsToTransport(
  value: AuthorizationReadRelationshipsInput,
): MessageInitShape<typeof ReadRelationshipsRequestSchema> {
  return {
    subject: authorizationSubjectToTransport(value.subject),
    relation: value.relation ?? "",
    resource: authorizationResourceToTransport(value.resource),
    pageSize: value.pageSize ?? 0,
    pageToken: value.pageToken ?? "",
    modelId: value.modelId ?? "",
    target: authorizationRelationshipTargetToTransport(value.target),
  };
}

function authorizationReadRelationshipsFromTransport(
  value: MessageInitShape<typeof ReadRelationshipsRequestSchema>,
): AuthorizationReadRelationshipsInput {
  return {
    subject: authorizationSubjectFromTransport(value.subject),
    relation: value.relation ?? "",
    resource: authorizationResourceFromTransport(value.resource),
    pageSize: value.pageSize ?? 0,
    pageToken: value.pageToken ?? "",
    modelId: value.modelId ?? "",
    target: authorizationRelationshipTargetFromTransport(value.target),
  };
}

function authorizationReadRelationshipsResponseToTransport(
  value: AuthorizationReadRelationshipsResponse,
): MessageInitShape<typeof ReadRelationshipsResponseSchema> {
  return {
    relationships:
      value.relationships?.map(authorizationRelationshipToTransport) ?? [],
    nextPageToken: value.nextPageToken ?? "",
    modelId: value.modelId ?? "",
  };
}

function authorizationReadRelationshipsResponseFromTransport(
  value: MessageInitShape<typeof ReadRelationshipsResponseSchema>,
): AuthorizationReadRelationshipsResponse {
  return {
    relationships:
      value.relationships?.map(authorizationRelationshipFromTransport) ?? [],
    nextPageToken: value.nextPageToken ?? "",
    modelId: value.modelId ?? "",
  };
}

function authorizationWriteRelationshipsToTransport(
  value: AuthorizationWriteRelationshipsInput,
): MessageInitShape<typeof WriteRelationshipsRequestSchema> {
  return {
    writes: value.writes?.map(authorizationRelationshipToTransport) ?? [],
    deletes: value.deletes?.map(authorizationRelationshipKeyToTransport) ?? [],
    modelId: value.modelId ?? "",
  };
}

function authorizationWriteRelationshipsFromTransport(
  value: MessageInitShape<typeof WriteRelationshipsRequestSchema>,
): AuthorizationWriteRelationshipsInput {
  return {
    writes: value.writes?.map(authorizationRelationshipFromTransport) ?? [],
    deletes: value.deletes?.map(authorizationRelationshipKeyFromTransport) ?? [],
    modelId: value.modelId ?? "",
  };
}

function authorizationModelToTransport(
  value?: AuthorizationModel,
): AuthorizationModelTransport | undefined {
  if (value === undefined) {
    return undefined;
  }
  return {
    version: value.version ?? 0,
    resourceTypes:
      value.resourceTypes?.map(authorizationModelResourceTypeToTransport) ?? [],
  };
}

function authorizationModelFromTransport(
  value?: AuthorizationModelTransport,
): AuthorizationModel | undefined {
  if (value === undefined) {
    return undefined;
  }
  return {
    version: value.version ?? 0,
    resourceTypes:
      value.resourceTypes?.map(authorizationModelResourceTypeFromTransport) ?? [],
  };
}

function authorizationModelResourceTypeToTransport(
  value: AuthorizationModelResourceType,
): AuthorizationModelResourceTypeTransport {
  return {
    name: value.name ?? "",
    relations: value.relations?.map(authorizationModelRelationToTransport) ?? [],
    actions: value.actions?.map(authorizationModelActionToTransport) ?? [],
  };
}

function authorizationModelResourceTypeFromTransport(
  value: AuthorizationModelResourceTypeTransport,
): AuthorizationModelResourceType {
  return {
    name: value.name ?? "",
    relations: value.relations?.map(authorizationModelRelationFromTransport) ?? [],
    actions: value.actions?.map(authorizationModelActionFromTransport) ?? [],
  };
}

function authorizationModelRelationToTransport(
  value: AuthorizationModelRelation,
): AuthorizationModelRelationTransport {
  return {
    name: value.name ?? "",
    subjectTypes: value.subjectTypes ?? [],
    allowedTargets:
      value.allowedTargets?.map(authorizationModelAllowedTargetToTransport) ?? [],
    rewrite: authorizationModelRewriteToTransport(value.rewrite),
  };
}

function authorizationModelRelationFromTransport(
  value: AuthorizationModelRelationTransport,
): AuthorizationModelRelation {
  return {
    name: value.name ?? "",
    subjectTypes: value.subjectTypes ?? [],
    allowedTargets:
      value.allowedTargets?.map(authorizationModelAllowedTargetFromTransport) ?? [],
    rewrite: authorizationModelRewriteFromTransport(value.rewrite),
  };
}

function authorizationModelActionToTransport(
  value: AuthorizationModelAction,
): AuthorizationModelActionTransport {
  return {
    name: value.name ?? "",
    relations: value.relations ?? [],
    rewrite: authorizationModelRewriteToTransport(value.rewrite),
  };
}

function authorizationModelActionFromTransport(
  value: AuthorizationModelActionTransport,
): AuthorizationModelAction {
  return {
    name: value.name ?? "",
    relations: value.relations ?? [],
    rewrite: authorizationModelRewriteFromTransport(value.rewrite),
  };
}

function authorizationModelAllowedTargetToTransport(
  value: AuthorizationModelAllowedTarget,
): AuthorizationModelAllowedTargetTransport {
  if (value.subjectType !== undefined) {
    return { kind: { case: "subjectType", value: value.subjectType } };
  }
  if (value.resourceType !== undefined) {
    return { kind: { case: "resourceType", value: value.resourceType } };
  }
  if (value.subjectSet !== undefined) {
    return {
      kind: {
        case: "subjectSet",
        value: authorizationModelSubjectSetTargetToTransport(value.subjectSet),
      },
    };
  }
  return {};
}

function authorizationModelAllowedTargetFromTransport(
  value: AuthorizationModelAllowedTargetTransport,
): AuthorizationModelAllowedTarget {
  switch (value.kind?.case) {
    case "subjectType":
      return { subjectType: value.kind.value };
    case "resourceType":
      return { resourceType: value.kind.value };
    case "subjectSet":
      return {
        subjectSet: authorizationModelSubjectSetTargetFromTransport(
          value.kind.value,
        ),
      };
    default:
      return {};
  }
}

function authorizationModelSubjectSetTargetToTransport(
  value: AuthorizationModelSubjectSetTarget,
): AuthorizationModelSubjectSetTargetTransport {
  return {
    resourceType: value.resourceType ?? "",
    relation: value.relation ?? "",
  };
}

function authorizationModelSubjectSetTargetFromTransport(
  value: AuthorizationModelSubjectSetTargetTransport,
): AuthorizationModelSubjectSetTarget {
  return {
    resourceType: value.resourceType ?? "",
    relation: value.relation ?? "",
  };
}

function authorizationModelRewriteToTransport(
  value?: AuthorizationModelRewrite,
): AuthorizationModelRewriteTransport | undefined {
  if (value === undefined) {
    return undefined;
  }
  if (value.this !== undefined) {
    return { kind: { case: "this", value: {} } };
  }
  if (value.computedUserset !== undefined) {
    return {
      kind: {
        case: "computedUserset",
        value: authorizationModelComputedUsersetToTransport(
          value.computedUserset,
        ),
      },
    };
  }
  if (value.tupleToUserset !== undefined) {
    return {
      kind: {
        case: "tupleToUserset",
        value: authorizationModelTupleToUsersetToTransport(
          value.tupleToUserset,
        ),
      },
    };
  }
  if (value.union !== undefined) {
    return {
      kind: {
        case: "union",
        value: authorizationModelRewriteUnionToTransport(value.union),
      },
    };
  }
  return {};
}

function authorizationModelRewriteFromTransport(
  value?: AuthorizationModelRewriteTransport,
): AuthorizationModelRewrite | undefined {
  if (value === undefined) {
    return undefined;
  }
  switch (value.kind?.case) {
    case "this":
      return { this: {} };
    case "computedUserset":
      return {
        computedUserset: authorizationModelComputedUsersetFromTransport(
          value.kind.value,
        ),
      };
    case "tupleToUserset":
      return {
        tupleToUserset: authorizationModelTupleToUsersetFromTransport(
          value.kind.value,
        ),
      };
    case "union":
      return {
        union: authorizationModelRewriteUnionFromTransport(value.kind.value),
      };
    default:
      return {};
  }
}

function authorizationModelComputedUsersetToTransport(
  value: AuthorizationModelComputedUserset,
): AuthorizationModelComputedUsersetTransport {
  return { relation: value.relation ?? "" };
}

function authorizationModelComputedUsersetFromTransport(
  value: AuthorizationModelComputedUsersetTransport,
): AuthorizationModelComputedUserset {
  return { relation: value.relation ?? "" };
}

function authorizationModelTupleToUsersetToTransport(
  value: AuthorizationModelTupleToUserset,
): AuthorizationModelTupleToUsersetTransport {
  return {
    tuplesetRelation: value.tuplesetRelation ?? "",
    computedRelation: value.computedRelation ?? "",
  };
}

function authorizationModelTupleToUsersetFromTransport(
  value: AuthorizationModelTupleToUsersetTransport,
): AuthorizationModelTupleToUserset {
  return {
    tuplesetRelation: value.tuplesetRelation ?? "",
    computedRelation: value.computedRelation ?? "",
  };
}

function authorizationModelRewriteUnionToTransport(
  value: AuthorizationModelRewriteUnion,
): AuthorizationModelRewriteUnionTransport {
  return {
    children: value.children?.map((entry) => authorizationModelRewriteToTransport(entry)!) ?? [],
  };
}

function authorizationModelRewriteUnionFromTransport(
  value: AuthorizationModelRewriteUnionTransport,
): AuthorizationModelRewriteUnion {
  return {
    children: value.children?.map((entry) => authorizationModelRewriteFromTransport(entry)!) ?? [],
  };
}

function authorizationModelRefToTransport(
  value: AuthorizationModelRef,
): AuthorizationModelRefTransport {
  return {
    id: value.id ?? "",
    version: value.version ?? "",
    createdAt:
      value.createdAt === undefined
        ? undefined
        : timestampFromDate(value.createdAt),
  };
}

function authorizationModelRefFromTransport(
  value: AuthorizationModelRefTransport,
): AuthorizationModelRef {
  return {
    id: value.id ?? "",
    version: value.version ?? "",
    createdAt:
      value.createdAt === undefined
        ? undefined
        : dateFromTimestamp(
            value.createdAt as Parameters<typeof dateFromTimestamp>[0],
          ),
  };
}

function authorizationGetActiveModelToTransport(
  value: AuthorizationGetActiveModelResponse,
): MessageInitShape<typeof GetActiveModelResponseSchema> {
  return {
    model:
      value.model === undefined
        ? undefined
        : authorizationModelRefToTransport(value.model),
  };
}

function authorizationGetActiveModelFromTransport(
  value: MessageInitShape<typeof GetActiveModelResponseSchema>,
): AuthorizationGetActiveModelResponse {
  return {
    model:
      value.model === undefined
        ? undefined
        : authorizationModelRefFromTransport(value.model),
  };
}

function authorizationListModelsToTransport(
  value: AuthorizationListModelsInput,
): MessageInitShape<typeof ListModelsRequestSchema> {
  return {
    pageSize: value.pageSize ?? 0,
    pageToken: value.pageToken ?? "",
  };
}

function authorizationListModelsFromTransport(
  value: MessageInitShape<typeof ListModelsRequestSchema>,
): AuthorizationListModelsInput {
  return {
    pageSize: value.pageSize ?? 0,
    pageToken: value.pageToken ?? "",
  };
}

function authorizationListModelsResponseToTransport(
  value: AuthorizationListModelsResponse,
): MessageInitShape<typeof ListModelsResponseSchema> {
  return {
    models: value.models?.map(authorizationModelRefToTransport) ?? [],
    nextPageToken: value.nextPageToken ?? "",
  };
}

function authorizationListModelsResponseFromTransport(
  value: MessageInitShape<typeof ListModelsResponseSchema>,
): AuthorizationListModelsResponse {
  return {
    models: value.models?.map(authorizationModelRefFromTransport) ?? [],
    nextPageToken: value.nextPageToken ?? "",
  };
}

function authorizationWriteModelToTransport(
  value: AuthorizationWriteModelInput,
): MessageInitShape<typeof WriteModelRequestSchema> {
  return { model: authorizationModelToTransport(value.model) };
}

function authorizationWriteModelFromTransport(
  value: MessageInitShape<typeof WriteModelRequestSchema>,
): AuthorizationWriteModelInput {
  return { model: authorizationModelFromTransport(value.model) };
}

function authorizationExpandToTransport(
  value: AuthorizationExpandInput,
): MessageInitShape<typeof ExpandRequestSchema> {
  return {
    resource: authorizationResourceToTransport(value.resource),
    relation: value.relation ?? "",
    context: value.context,
    maxDepth: value.maxDepth ?? 0,
    modelId: value.modelId ?? "",
  };
}

function authorizationExpandFromTransport(
  value: MessageInitShape<typeof ExpandRequestSchema>,
): AuthorizationExpandInput {
  return {
    resource: authorizationResourceFromTransport(value.resource),
    relation: value.relation ?? "",
    context: value.context,
    maxDepth: value.maxDepth ?? 0,
    modelId: value.modelId ?? "",
  };
}

function authorizationExpandNodeToTransport(
  value?: AuthorizationExpandNode,
): ExpandNodeTransport | undefined {
  if (value === undefined) {
    return undefined;
  }
  return {
    target: authorizationRelationshipTargetToTransport(value.target),
    relation: value.relation ?? "",
    children: value.children?.map((entry) => authorizationExpandNodeToTransport(entry)!) ?? [],
  };
}

function authorizationExpandNodeFromTransport(
  value?: ExpandNodeTransport,
): AuthorizationExpandNode | undefined {
  if (value === undefined) {
    return undefined;
  }
  return {
    target: authorizationRelationshipTargetFromTransport(value.target),
    relation: value.relation ?? "",
    children: value.children?.map((entry) => authorizationExpandNodeFromTransport(entry)!) ?? [],
  };
}

function authorizationExpandResponseToTransport(
  value: AuthorizationExpandResponse,
): MessageInitShape<typeof ExpandResponseSchema> {
  return {
    root: authorizationExpandNodeToTransport(value.root),
    truncated: value.truncated ?? false,
    cycleDetected: value.cycleDetected ?? false,
    maxDepthReached: value.maxDepthReached ?? false,
    modelId: value.modelId ?? "",
  };
}

function authorizationExpandResponseFromTransport(
  value: MessageInitShape<typeof ExpandResponseSchema>,
): AuthorizationExpandResponse {
  return {
    root: authorizationExpandNodeFromTransport(value.root),
    truncated: value.truncated ?? false,
    cycleDetected: value.cycleDetected ?? false,
    maxDepthReached: value.maxDepthReached ?? false,
    modelId: value.modelId ?? "",
  };
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
  return { subject };
}

/** Creates a relationship target from a resource. */
export function authorizationResourceTarget(
  resource: AuthorizationResource,
): AuthorizationRelationshipTarget {
  return { resource };
}

/** Creates a relationship target from a subject set. */
export function authorizationSubjectSetTarget(
  resource: AuthorizationResource,
  relation: string,
): AuthorizationRelationshipTarget {
  return { subjectSet: authorizationSubjectSet(resource, relation) };
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
  nodeOptions?: { path: string } | undefined;
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
