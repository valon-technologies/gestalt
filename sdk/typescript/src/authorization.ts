import { create } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import {
  Code,
  ConnectError,
  createClient,
  type Client,
  type ServiceImpl,
} from "@connectrpc/connect";

import {
  AccessDecisionSchema,
  AccessEvaluationRequestSchema,
  AccessEvaluationsRequestSchema,
  AccessEvaluationsResponseSchema,
  ActionSearchRequestSchema,
  ActionSearchResponseSchema,
  ActionSchema,
  AuthorizationMetadataSchema,
  AuthorizationModelRefSchema,
  AuthorizationProvider as AuthorizationProviderService,
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
  SubjectSearchRequestSchema,
  SubjectSearchResponseSchema,
  SubjectSetSchema,
  WriteModelRequestSchema,
  WriteRelationshipsRequestSchema,
  type AccessDecision as ProtoAccessDecision,
  type AccessEvaluationRequest as ProtoAccessEvaluationRequest,
  type AccessEvaluationsRequest as ProtoAccessEvaluationsRequest,
  type AccessEvaluationsResponse as ProtoAccessEvaluationsResponse,
  type Action as ProtoAction,
  type ActionSearchRequest as ProtoActionSearchRequest,
  type ActionSearchResponse as ProtoActionSearchResponse,
  type AuthorizationMetadata as ProtoAuthorizationMetadata,
  type AuthorizationModel as ProtoAuthorizationModel,
  type AuthorizationModelAction as ProtoAuthorizationModelAction,
  type AuthorizationModelAllowedTarget as ProtoAuthorizationModelAllowedTarget,
  type AuthorizationModelComputedUserset as ProtoAuthorizationModelComputedUserset,
  type AuthorizationModelRef as ProtoAuthorizationModelRef,
  type AuthorizationModelRelation as ProtoAuthorizationModelRelation,
  type AuthorizationModelResourceType as ProtoAuthorizationModelResourceType,
  type AuthorizationModelRewrite as ProtoAuthorizationModelRewrite,
  type AuthorizationModelRewriteUnion as ProtoAuthorizationModelRewriteUnion,
  type AuthorizationModelSubjectSetTarget as ProtoAuthorizationModelSubjectSetTarget,
  type AuthorizationModelTupleToUserset as ProtoAuthorizationModelTupleToUserset,
  type EffectiveSubjectSearchRequest as ProtoEffectiveSubjectSearchRequest,
  type EffectiveSubjectSearchResponse as ProtoEffectiveSubjectSearchResponse,
  type ExpandNode as ProtoExpandNode,
  type ExpandRequest as ProtoExpandRequest,
  type ExpandResponse as ProtoExpandResponse,
  type GetActiveModelResponse as ProtoGetActiveModelResponse,
  type ListModelsRequest as ProtoListModelsRequest,
  type ListModelsResponse as ProtoListModelsResponse,
  type ReadRelationshipsRequest as ProtoReadRelationshipsRequest,
  type ReadRelationshipsResponse as ProtoReadRelationshipsResponse,
  type Relationship as ProtoRelationship,
  type RelationshipKey as ProtoRelationshipKey,
  type RelationshipTarget as ProtoRelationshipTarget,
  type Resource as ProtoResource,
  type ResourceSearchRequest as ProtoResourceSearchRequest,
  type ResourceSearchResponse as ProtoResourceSearchResponse,
  type Subject as ProtoSubject,
  type SubjectSearchRequest as ProtoSubjectSearchRequest,
  type SubjectSearchResponse as ProtoSubjectSearchResponse,
  type SubjectSet as ProtoSubjectSet,
  type WriteModelRequest as ProtoWriteModelRequest,
  type WriteRelationshipsRequest as ProtoWriteRelationshipsRequest,
} from "./internal/gen/v1/authorization_pb.ts";
import {
  dateFromTimestamp,
  timestampFromDate,
  type JsonObjectInput,
} from "./protocol.ts";
import {
  optionalObjectFromStruct,
  optionalStruct,
} from "./protocol-internal.ts";
import type { MaybePromise } from "./api.ts";
import { ProviderBase, type ProviderBaseOptions } from "./provider.ts";
import {
  createHostServiceGrpcTransport,
  hostServiceMetadataInterceptors,
  parseHostServiceTarget,
  ENV_HOST_SERVICE_SOCKET,
  ENV_HOST_SERVICE_TOKEN,
} from "./host-service.ts";

type AuthorizationProviderServiceImpl = Partial<
  ServiceImpl<typeof AuthorizationProviderService>
>;

/** Subject type used for canonical Gestalt subject ids in managed grants. */
export const AUTHORIZATION_SUBJECT_TYPE_SUBJECT = "subject";

export interface AuthorizationSubject {
  type: string;
  id: string;
  properties?: JsonObjectInput | undefined;
}

export interface AuthorizationResource {
  type: string;
  id: string;
  properties?: JsonObjectInput | undefined;
}

export interface AuthorizationSubjectSet {
  resource?: AuthorizationResource | undefined;
  relation: string;
}

export type AuthorizationRelationshipTargetKind =
  | { case: "subject"; value: AuthorizationSubject }
  | { case: "resource"; value: AuthorizationResource }
  | { case: "subjectSet"; value: AuthorizationSubjectSet }
  | { case: undefined; value?: undefined };

export interface AuthorizationRelationshipTarget {
  kind: AuthorizationRelationshipTargetKind;
}

export interface AuthorizationAction {
  name: string;
  properties?: JsonObjectInput | undefined;
}

export interface AuthorizationEvaluateInput {
  subject?: AuthorizationSubject | undefined;
  action?: AuthorizationAction | undefined;
  resource?: AuthorizationResource | undefined;
  context?: JsonObjectInput | undefined;
}

export interface AuthorizationDecision {
  allowed?: boolean | undefined;
  context?: JsonObjectInput | undefined;
  modelId?: string | undefined;
}

export interface AuthorizationEvaluateManyInput {
  requests: readonly AuthorizationEvaluateInput[];
}

export interface AuthorizationEvaluationsResponse {
  decisions: readonly AuthorizationDecision[];
}

export interface AuthorizationSearchResourcesInput {
  subject?: AuthorizationSubject | undefined;
  action?: AuthorizationAction | undefined;
  resourceType?: string | undefined;
  context?: JsonObjectInput | undefined;
  pageSize?: number | undefined;
  pageToken?: string | undefined;
}

export interface AuthorizationResourceSearch {
  resources: readonly AuthorizationResource[];
  nextPageToken?: string | undefined;
  modelId?: string | undefined;
}

export interface AuthorizationSearchSubjectsInput {
  resource?: AuthorizationResource | undefined;
  action?: AuthorizationAction | undefined;
  subjectType?: string | undefined;
  context?: JsonObjectInput | undefined;
  pageSize?: number | undefined;
  pageToken?: string | undefined;
}

export interface AuthorizationSubjectSearch {
  subjects: readonly AuthorizationSubject[];
  nextPageToken?: string | undefined;
  modelId?: string | undefined;
}

export interface AuthorizationEffectiveSearchSubjectsInput {
  resource?: AuthorizationResource | undefined;
  action?: AuthorizationAction | undefined;
  context?: JsonObjectInput | undefined;
  pageSize?: number | undefined;
  pageToken?: string | undefined;
}

export interface AuthorizationEffectiveSubjectSearch {
  targets: readonly AuthorizationRelationshipTarget[];
  nextPageToken?: string | undefined;
  modelId?: string | undefined;
  truncated?: boolean | undefined;
}

export interface AuthorizationSearchActionsInput {
  subject?: AuthorizationSubject | undefined;
  resource?: AuthorizationResource | undefined;
  context?: JsonObjectInput | undefined;
  pageSize?: number | undefined;
  pageToken?: string | undefined;
}

export interface AuthorizationActionSearch {
  actions: readonly AuthorizationAction[];
  nextPageToken?: string | undefined;
  modelId?: string | undefined;
}

export interface AuthorizationMetadata {
  capabilities?: readonly string[] | undefined;
  activeModelId?: string | undefined;
}

export interface AuthorizationRelationship {
  subject?: AuthorizationSubject | undefined;
  relation: string;
  resource?: AuthorizationResource | undefined;
  properties?: JsonObjectInput | undefined;
  target?: AuthorizationRelationshipTarget | undefined;
}

export interface AuthorizationRelationshipKey {
  subject?: AuthorizationSubject | undefined;
  relation: string;
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

export interface AuthorizationReadRelationships {
  relationships: readonly AuthorizationRelationship[];
  nextPageToken?: string | undefined;
  modelId?: string | undefined;
}

export interface AuthorizationWriteRelationshipsInput {
  writes?: readonly AuthorizationRelationship[] | undefined;
  deletes?: readonly AuthorizationRelationshipKey[] | undefined;
  modelId?: string | undefined;
}

export interface AuthorizationModel {
  version?: number | undefined;
  resourceTypes?: readonly AuthorizationModelResourceType[] | undefined;
}

export interface AuthorizationModelResourceType {
  name: string;
  relations?: readonly AuthorizationModelRelation[] | undefined;
  actions?: readonly AuthorizationModelAction[] | undefined;
}

export interface AuthorizationModelRelation {
  name: string;
  subjectTypes?: readonly string[] | undefined;
  allowedTargets?: readonly AuthorizationModelAllowedTarget[] | undefined;
  rewrite?: AuthorizationModelRewrite | undefined;
}

export interface AuthorizationModelAction {
  name: string;
  relations?: readonly string[] | undefined;
  rewrite?: AuthorizationModelRewrite | undefined;
}

export type AuthorizationModelAllowedTargetKind =
  | { case: "subjectType"; value: string }
  | { case: "resourceType"; value: string }
  | { case: "subjectSet"; value: AuthorizationModelSubjectSetTarget }
  | { case: undefined; value?: undefined };

export interface AuthorizationModelAllowedTarget {
  kind: AuthorizationModelAllowedTargetKind;
}

export interface AuthorizationModelSubjectSetTarget {
  resourceType: string;
  relation: string;
}

export type AuthorizationModelRewriteKind =
  | { case: "this"; value: AuthorizationModelRewriteThis }
  | { case: "computedUserset"; value: AuthorizationModelComputedUserset }
  | { case: "tupleToUserset"; value: AuthorizationModelTupleToUserset }
  | { case: "union"; value: AuthorizationModelRewriteUnion }
  | { case: undefined; value?: undefined };

export interface AuthorizationModelRewrite {
  kind: AuthorizationModelRewriteKind;
}

export interface AuthorizationModelRewriteThis {}

export interface AuthorizationModelComputedUserset {
  relation: string;
}

export interface AuthorizationModelTupleToUserset {
  tuplesetRelation: string;
  computedRelation: string;
}

export interface AuthorizationModelRewriteUnion {
  children?: readonly AuthorizationModelRewrite[] | undefined;
}

export interface AuthorizationModelRef {
  id: string;
  version: string;
  createdAt?: Date | undefined;
}

export interface AuthorizationExpandInput {
  resource?: AuthorizationResource | undefined;
  relation?: string | undefined;
  context?: JsonObjectInput | undefined;
  maxDepth?: number | undefined;
  modelId?: string | undefined;
}

export interface AuthorizationExpandNode {
  target?: AuthorizationRelationshipTarget | undefined;
  relation?: string | undefined;
  children?: readonly AuthorizationExpandNode[] | undefined;
}

export interface AuthorizationExpand {
  root?: AuthorizationExpandNode | undefined;
  truncated?: boolean | undefined;
  cycleDetected?: boolean | undefined;
  maxDepthReached?: boolean | undefined;
  modelId?: string | undefined;
}

export interface AuthorizationGetActiveModel {
  model?: AuthorizationModelRef | undefined;
}

export interface AuthorizationListModelsInput {
  pageSize?: number | undefined;
  pageToken?: string | undefined;
}

export interface AuthorizationListModels {
  models?: readonly AuthorizationModelRef[] | undefined;
  nextPageToken?: string | undefined;
}

export interface AuthorizationWriteModelInput {
  model?: AuthorizationModel | undefined;
}

const sharedAuthorizationTransport: {
  target: string;
  token: string;
  client: HostAuthorization | undefined;
} = {
  target: "",
  token: "",
  client: undefined,
};

/**
 * Fakeable client contract for host authorization calls.
 */
export interface Authorization {
  evaluate(request: AuthorizationEvaluateInput): Promise<AuthorizationDecision>;
  evaluateMany(
    request: AuthorizationEvaluateManyInput,
  ): Promise<AuthorizationEvaluationsResponse>;
  searchResources(
    request: AuthorizationSearchResourcesInput,
  ): Promise<AuthorizationResourceSearch>;
  searchSubjects(
    request: AuthorizationSearchSubjectsInput,
  ): Promise<AuthorizationSubjectSearch>;
  effectiveSearchResources(
    request: AuthorizationSearchResourcesInput,
  ): Promise<AuthorizationResourceSearch>;
  effectiveSearchSubjects(
    request: AuthorizationEffectiveSearchSubjectsInput,
  ): Promise<AuthorizationEffectiveSubjectSearch>;
  searchActions(
    request: AuthorizationSearchActionsInput,
  ): Promise<AuthorizationActionSearch>;
  expand(request: AuthorizationExpandInput): Promise<AuthorizationExpand>;
  readRelationships(
    request: AuthorizationReadRelationshipsInput,
  ): Promise<AuthorizationReadRelationships>;
  writeRelationships(
    request: AuthorizationWriteRelationshipsInput,
  ): Promise<void>;
  getMetadata(): Promise<AuthorizationMetadata>;
  getActiveModel(): Promise<AuthorizationGetActiveModel>;
  listModels(
    request?: AuthorizationListModelsInput,
  ): Promise<AuthorizationListModels>;
  writeModel(
    request: AuthorizationWriteModelInput,
  ): Promise<AuthorizationModelRef>;
}

/**
 * Client for the host-configured authorization provider.
 *
 * The client accepts plain SDK request objects and keeps transport message
 * construction inside the SDK.
 */
class HostAuthorization implements Authorization {
  private readonly client: Client<typeof AuthorizationProviderService>;

  constructor(
    socketTarget?: string,
    relayToken = process.env[ENV_HOST_SERVICE_TOKEN]?.trim() ?? "",
  ) {
    const resolvedTarget = resolveAuthorizationSocketTarget(socketTarget);
    const transport = createHostServiceGrpcTransport(
      parseHostServiceTarget("authorization", resolvedTarget),
      hostServiceMetadataInterceptors(relayToken, ""),
    );
    this.client = createClient(AuthorizationProviderService, transport);
  }

  async evaluate(
    request: AuthorizationEvaluateInput,
  ): Promise<AuthorizationDecision> {
    return authorizationDecisionFromProto(
      await this.client.evaluate(authorizationEvaluateInputToProto(request)),
    );
  }

  async evaluateMany(
    request: AuthorizationEvaluateManyInput,
  ): Promise<AuthorizationEvaluationsResponse> {
    return authorizationEvaluationsResponseFromProto(
      await this.client.evaluateMany(authorizationEvaluateManyInputToProto(request)),
    );
  }

  async searchResources(
    request: AuthorizationSearchResourcesInput,
  ): Promise<AuthorizationResourceSearch> {
    return authorizationResourceSearchFromProto(
      await this.client.searchResources(authorizationSearchResourcesInputToProto(request)),
    );
  }

  async searchSubjects(
    request: AuthorizationSearchSubjectsInput,
  ): Promise<AuthorizationSubjectSearch> {
    return authorizationSubjectSearchFromProto(
      await this.client.searchSubjects(authorizationSearchSubjectsInputToProto(request)),
    );
  }

  async effectiveSearchResources(
    request: AuthorizationSearchResourcesInput,
  ): Promise<AuthorizationResourceSearch> {
    return authorizationResourceSearchFromProto(
      await this.client.effectiveSearchResources(authorizationSearchResourcesInputToProto(request)),
    );
  }

  async effectiveSearchSubjects(
    request: AuthorizationEffectiveSearchSubjectsInput,
  ): Promise<AuthorizationEffectiveSubjectSearch> {
    return authorizationEffectiveSubjectSearchFromProto(
      await this.client.effectiveSearchSubjects(authorizationEffectiveSearchSubjectsInputToProto(request)),
    );
  }

  async searchActions(
    request: AuthorizationSearchActionsInput,
  ): Promise<AuthorizationActionSearch> {
    return authorizationActionSearchFromProto(
      await this.client.searchActions(authorizationSearchActionsInputToProto(request)),
    );
  }

  async expand(
    request: AuthorizationExpandInput,
  ): Promise<AuthorizationExpand> {
    return authorizationExpandFromProto(
      await this.client.expand(authorizationExpandInputToProto(request)),
    );
  }

  async readRelationships(
    request: AuthorizationReadRelationshipsInput,
  ): Promise<AuthorizationReadRelationships> {
    return authorizationReadRelationshipsFromProto(
      await this.client.readRelationships(authorizationReadRelationshipsInputToProto(request)),
    );
  }

  /** Writes and deletes authorization relationships. */
  async writeRelationships(
    request: AuthorizationWriteRelationshipsInput,
  ): Promise<void> {
    await this.client.writeRelationships(authorizationWriteRelationshipsInputToProto(request));
  }

  async getMetadata(): Promise<AuthorizationMetadata> {
    return authorizationMetadataFromProto(await this.client.getMetadata({}));
  }

  async getActiveModel(): Promise<AuthorizationGetActiveModel> {
    return authorizationGetActiveModelFromProto(await this.client.getActiveModel({}));
  }

  async listModels(
    request: AuthorizationListModelsInput = {},
  ): Promise<AuthorizationListModels> {
    return authorizationListModelsFromProto(
      await this.client.listModels(authorizationListModelsInputToProto(request)),
    );
  }

  async writeModel(
    request: AuthorizationWriteModelInput,
  ): Promise<AuthorizationModelRef> {
    return authorizationModelRefFromProtoRequired(
      await this.client.writeModel(authorizationWriteModelInputToProto(request)),
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
  ) => MaybePromise<AuthorizationResourceSearch>;
  searchSubjects: (
    request: AuthorizationSearchSubjectsInput,
  ) => MaybePromise<AuthorizationSubjectSearch>;
  effectiveSearchResources?: (
    request: AuthorizationSearchResourcesInput,
  ) => MaybePromise<AuthorizationResourceSearch>;
  effectiveSearchSubjects?: (
    request: AuthorizationEffectiveSearchSubjectsInput,
  ) => MaybePromise<AuthorizationEffectiveSubjectSearch>;
  searchActions: (
    request: AuthorizationSearchActionsInput,
  ) => MaybePromise<AuthorizationActionSearch>;
  expand?: (
    request: AuthorizationExpandInput,
  ) => MaybePromise<AuthorizationExpand>;
  getMetadata: () => MaybePromise<AuthorizationMetadata>;
  readRelationships: (
    request: AuthorizationReadRelationshipsInput,
  ) => MaybePromise<AuthorizationReadRelationships>;
  writeRelationships: (
    request: AuthorizationWriteRelationshipsInput,
  ) => MaybePromise<void>;
  getActiveModel: () => MaybePromise<AuthorizationGetActiveModel>;
  listModels: (
    request: AuthorizationListModelsInput,
  ) => MaybePromise<AuthorizationListModels>;
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
): AuthorizationProviderServiceImpl {
  return {
    async evaluate(request) {
      return authorizationDecisionToProto(
        requiredAuthorizationResponse(
          await provider.evaluate(authorizationEvaluateInputFromProto(request)),
          "evaluate",
        ),
      );
    },
    async evaluateMany(request) {
      return authorizationEvaluationsResponseToProto(
        requiredAuthorizationResponse(
          await provider.evaluateMany(authorizationEvaluateManyInputFromProto(request)),
          "evaluate many",
        ),
      );
    },
    async searchResources(request) {
      return authorizationResourceSearchToProto(
        requiredAuthorizationResponse(
          await provider.searchResources(authorizationSearchResourcesInputFromProto(request)),
          "search resources",
        ),
      );
    },
    async searchSubjects(request) {
      return authorizationSubjectSearchToProto(
        requiredAuthorizationResponse(
          await provider.searchSubjects(authorizationSearchSubjectsInputFromProto(request)),
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
      return authorizationResourceSearchToProto(
        requiredAuthorizationResponse(
          await provider.effectiveSearchResources(authorizationSearchResourcesInputFromProto(request)),
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
      return authorizationEffectiveSubjectSearchToProto(
        requiredAuthorizationResponse(
          await provider.effectiveSearchSubjects(authorizationEffectiveSearchSubjectsInputFromProto(request)),
          "effective search subjects",
        ),
      );
    },
    async searchActions(request) {
      return authorizationActionSearchToProto(
        requiredAuthorizationResponse(
          await provider.searchActions(authorizationSearchActionsInputFromProto(request)),
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
      return authorizationExpandToProto(
        requiredAuthorizationResponse(
          await provider.expand(authorizationExpandInputFromProto(request)),
          "expand",
        ),
      );
    },
    async getMetadata() {
      const metadata = authorizationMetadataToProto(
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
      return authorizationReadRelationshipsToProto(
        requiredAuthorizationResponse(
          await provider.readRelationships(authorizationReadRelationshipsInputFromProto(request)),
          "read relationships",
        ),
      );
    },
    async writeRelationships(request) {
      await provider.writeRelationships(authorizationWriteRelationshipsInputFromProto(request));
      return create(EmptySchema, {});
    },
    async getActiveModel() {
      return authorizationGetActiveModelToProto(
        requiredAuthorizationResponse(
          await provider.getActiveModel(),
          "get active model",
        ),
      );
    },
    async listModels(request) {
      return authorizationListModelsToProto(
        requiredAuthorizationResponse(
          await provider.listModels(authorizationListModelsInputFromProto(request)),
          "list models",
        ),
      );
    },
    async writeModel(request) {
      return authorizationModelRefToProto(
        requiredAuthorizationResponse(
          await provider.writeModel(authorizationWriteModelInputFromProto(request)),
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
export function Authorization(): Authorization {
  const target = resolveAuthorizationSocketTarget();
  const token = process.env[ENV_HOST_SERVICE_TOKEN]?.trim() ?? "";
  if (
    sharedAuthorizationTransport.client &&
    sharedAuthorizationTransport.target === target &&
    sharedAuthorizationTransport.token === token
  ) {
    return sharedAuthorizationTransport.client;
  }

  const client = new HostAuthorization(target, token);
  sharedAuthorizationTransport.target = target;
  sharedAuthorizationTransport.token = token;
  sharedAuthorizationTransport.client = client;
  return client;
}

/** Creates an authorization subject reference. */
export function authorizationSubject(
  type: string,
  id: string,
  properties?: JsonObjectInput,
): AuthorizationSubject {
  return properties === undefined ? { type, id } : { type, id, properties };
}

/** Creates an authorization resource reference. */
export function authorizationResource(
  type: string,
  id: string,
  properties?: JsonObjectInput,
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

/** Creates an authorization action reference. */
export function authorizationAction(
  name: string,
  properties?: JsonObjectInput,
): AuthorizationAction {
  return properties === undefined ? { name } : { name, properties };
}

/** Creates a relationship tuple for authorization writes. */
export function authorizationRelationship(
  subject: AuthorizationSubject,
  relation: string,
  resource: AuthorizationResource,
  properties?: JsonObjectInput,
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
  properties?: JsonObjectInput,
): AuthorizationRelationship {
  return properties === undefined
    ? { target, relation, resource }
    : { target, relation, resource, properties };
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

function authorizationEvaluateInputToProto(input: AuthorizationEvaluateInput) {
  return create(AccessEvaluationRequestSchema, {
    subject: input.subject === undefined ? undefined : authorizationSubjectToProto(input.subject),
    action: input.action === undefined ? undefined : authorizationActionToProto(input.action),
    resource: input.resource === undefined ? undefined : authorizationResourceToProto(input.resource),
    context: optionalStruct(input.context),
  });
}

function authorizationEvaluateInputFromProto(
  input: ProtoAccessEvaluationRequest,
): AuthorizationEvaluateInput {
  return {
    subject: authorizationSubjectFromProto(input.subject),
    action: authorizationActionFromProto(input.action),
    resource: authorizationResourceFromProto(input.resource),
    context: optionalObjectFromStruct(input.context),
  };
}

function authorizationEvaluateManyInputToProto(input: AuthorizationEvaluateManyInput) {
  return create(AccessEvaluationsRequestSchema, {
    requests: input.requests?.map(authorizationEvaluateInputToProto) ?? [],
  });
}

function authorizationEvaluateManyInputFromProto(
  input: ProtoAccessEvaluationsRequest,
): AuthorizationEvaluateManyInput {
  return { requests: input.requests.map(authorizationEvaluateInputFromProto) };
}

function authorizationSearchResourcesInputToProto(input: AuthorizationSearchResourcesInput) {
  return create(ResourceSearchRequestSchema, {
    subject: input.subject === undefined ? undefined : authorizationSubjectToProto(input.subject),
    action: input.action === undefined ? undefined : authorizationActionToProto(input.action),
    resourceType: input.resourceType ?? "",
    context: optionalStruct(input.context),
    pageSize: input.pageSize ?? 0,
    pageToken: input.pageToken ?? "",
  });
}

function authorizationSearchResourcesInputFromProto(
  input: ProtoResourceSearchRequest,
): AuthorizationSearchResourcesInput {
  return {
    subject: authorizationSubjectFromProto(input.subject),
    action: authorizationActionFromProto(input.action),
    resourceType: input.resourceType,
    context: optionalObjectFromStruct(input.context),
    pageSize: input.pageSize,
    pageToken: input.pageToken,
  };
}

function authorizationSearchSubjectsInputToProto(input: AuthorizationSearchSubjectsInput) {
  return create(SubjectSearchRequestSchema, {
    resource: input.resource === undefined ? undefined : authorizationResourceToProto(input.resource),
    action: input.action === undefined ? undefined : authorizationActionToProto(input.action),
    subjectType: input.subjectType ?? "",
    context: optionalStruct(input.context),
    pageSize: input.pageSize ?? 0,
    pageToken: input.pageToken ?? "",
  });
}

function authorizationSearchSubjectsInputFromProto(
  input: ProtoSubjectSearchRequest,
): AuthorizationSearchSubjectsInput {
  return {
    resource: authorizationResourceFromProto(input.resource),
    action: authorizationActionFromProto(input.action),
    subjectType: input.subjectType,
    context: optionalObjectFromStruct(input.context),
    pageSize: input.pageSize,
    pageToken: input.pageToken,
  };
}

function authorizationEffectiveSearchSubjectsInputToProto(
  input: AuthorizationEffectiveSearchSubjectsInput,
) {
  return create(EffectiveSubjectSearchRequestSchema, {
    resource: input.resource === undefined ? undefined : authorizationResourceToProto(input.resource),
    action: input.action === undefined ? undefined : authorizationActionToProto(input.action),
    context: optionalStruct(input.context),
    pageSize: input.pageSize ?? 0,
    pageToken: input.pageToken ?? "",
  });
}

function authorizationEffectiveSearchSubjectsInputFromProto(
  input: ProtoEffectiveSubjectSearchRequest,
): AuthorizationEffectiveSearchSubjectsInput {
  return {
    resource: authorizationResourceFromProto(input.resource),
    action: authorizationActionFromProto(input.action),
    context: optionalObjectFromStruct(input.context),
    pageSize: input.pageSize,
    pageToken: input.pageToken,
  };
}

function authorizationSearchActionsInputToProto(input: AuthorizationSearchActionsInput) {
  return create(ActionSearchRequestSchema, {
    subject: input.subject === undefined ? undefined : authorizationSubjectToProto(input.subject),
    resource: input.resource === undefined ? undefined : authorizationResourceToProto(input.resource),
    context: optionalStruct(input.context),
    pageSize: input.pageSize ?? 0,
    pageToken: input.pageToken ?? "",
  });
}

function authorizationSearchActionsInputFromProto(
  input: ProtoActionSearchRequest,
): AuthorizationSearchActionsInput {
  return {
    subject: authorizationSubjectFromProto(input.subject),
    resource: authorizationResourceFromProto(input.resource),
    context: optionalObjectFromStruct(input.context),
    pageSize: input.pageSize,
    pageToken: input.pageToken,
  };
}

function authorizationExpandInputToProto(input: AuthorizationExpandInput) {
  return create(ExpandRequestSchema, {
    resource: input.resource === undefined ? undefined : authorizationResourceToProto(input.resource),
    relation: input.relation ?? "",
    context: optionalStruct(input.context),
    maxDepth: input.maxDepth ?? 0,
    modelId: input.modelId ?? "",
  });
}

function authorizationExpandInputFromProto(input: ProtoExpandRequest): AuthorizationExpandInput {
  return {
    resource: authorizationResourceFromProto(input.resource),
    relation: input.relation,
    context: optionalObjectFromStruct(input.context),
    maxDepth: input.maxDepth,
    modelId: input.modelId,
  };
}

function authorizationReadRelationshipsInputToProto(input: AuthorizationReadRelationshipsInput) {
  return create(ReadRelationshipsRequestSchema, {
    subject: input.subject === undefined ? undefined : authorizationSubjectToProto(input.subject),
    relation: input.relation ?? "",
    resource: input.resource === undefined ? undefined : authorizationResourceToProto(input.resource),
    pageSize: input.pageSize ?? 0,
    pageToken: input.pageToken ?? "",
    modelId: input.modelId ?? "",
    target: input.target === undefined ? undefined : authorizationRelationshipTargetToProto(input.target),
  });
}

function authorizationReadRelationshipsInputFromProto(
  input: ProtoReadRelationshipsRequest,
): AuthorizationReadRelationshipsInput {
  return {
    subject: authorizationSubjectFromProto(input.subject),
    relation: input.relation,
    resource: authorizationResourceFromProto(input.resource),
    pageSize: input.pageSize,
    pageToken: input.pageToken,
    modelId: input.modelId,
    target: authorizationRelationshipTargetFromProto(input.target),
  };
}

function authorizationWriteRelationshipsInputToProto(input: AuthorizationWriteRelationshipsInput) {
  return create(WriteRelationshipsRequestSchema, {
    writes: input.writes?.map(authorizationRelationshipToProto) ?? [],
    deletes: input.deletes?.map(authorizationRelationshipKeyToProto) ?? [],
    modelId: input.modelId ?? "",
  });
}

function authorizationWriteRelationshipsInputFromProto(
  input: ProtoWriteRelationshipsRequest,
): AuthorizationWriteRelationshipsInput {
  return {
    writes: input.writes.map(authorizationRelationshipFromProto),
    deletes: input.deletes.map(authorizationRelationshipKeyFromProto),
    modelId: input.modelId,
  };
}

function authorizationListModelsInputToProto(input: AuthorizationListModelsInput) {
  return create(ListModelsRequestSchema, {
    pageSize: input.pageSize ?? 0,
    pageToken: input.pageToken ?? "",
  });
}

function authorizationListModelsInputFromProto(input: ProtoListModelsRequest): AuthorizationListModelsInput {
  return {
    pageSize: input.pageSize,
    pageToken: input.pageToken,
  };
}

function authorizationWriteModelInputToProto(input: AuthorizationWriteModelInput) {
  return create(WriteModelRequestSchema, {
    model: input.model === undefined ? undefined : authorizationModelToProto(input.model),
  });
}

function authorizationWriteModelInputFromProto(input: ProtoWriteModelRequest): AuthorizationWriteModelInput {
  return {
    model: authorizationModelFromProto(input.model),
  };
}

function authorizationDecisionToProto(input: AuthorizationDecision) {
  return create(AccessDecisionSchema, {
    allowed: input.allowed ?? false,
    context: optionalStruct(input.context),
    modelId: input.modelId ?? "",
  });
}

function authorizationDecisionFromProto(input: ProtoAccessDecision): AuthorizationDecision {
  return {
    allowed: input.allowed,
    context: optionalObjectFromStruct(input.context),
    modelId: input.modelId,
  };
}

function authorizationEvaluationsResponseToProto(input: AuthorizationEvaluationsResponse) {
  return create(AccessEvaluationsResponseSchema, {
    decisions: input.decisions?.map(authorizationDecisionToProto) ?? [],
  });
}

function authorizationEvaluationsResponseFromProto(
  input: ProtoAccessEvaluationsResponse,
): AuthorizationEvaluationsResponse {
  return { decisions: input.decisions.map(authorizationDecisionFromProto) };
}

function authorizationResourceSearchToProto(input: AuthorizationResourceSearch) {
  return create(ResourceSearchResponseSchema, {
    resources: input.resources?.map(authorizationResourceToProto) ?? [],
    nextPageToken: input.nextPageToken ?? "",
    modelId: input.modelId ?? "",
  });
}

function authorizationResourceSearchFromProto(input: ProtoResourceSearchResponse): AuthorizationResourceSearch {
  return {
    resources: input.resources.map(authorizationResourceFromProtoRequired),
    nextPageToken: input.nextPageToken,
    modelId: input.modelId,
  };
}

function authorizationSubjectSearchToProto(input: AuthorizationSubjectSearch) {
  return create(SubjectSearchResponseSchema, {
    subjects: input.subjects?.map(authorizationSubjectToProto) ?? [],
    nextPageToken: input.nextPageToken ?? "",
    modelId: input.modelId ?? "",
  });
}

function authorizationSubjectSearchFromProto(input: ProtoSubjectSearchResponse): AuthorizationSubjectSearch {
  return {
    subjects: input.subjects.map(authorizationSubjectFromProtoRequired),
    nextPageToken: input.nextPageToken,
    modelId: input.modelId,
  };
}

function authorizationEffectiveSubjectSearchToProto(input: AuthorizationEffectiveSubjectSearch) {
  return create(EffectiveSubjectSearchResponseSchema, {
    targets: input.targets?.map(authorizationRelationshipTargetToProto) ?? [],
    nextPageToken: input.nextPageToken ?? "",
    modelId: input.modelId ?? "",
    truncated: input.truncated ?? false,
  });
}

function authorizationEffectiveSubjectSearchFromProto(
  input: ProtoEffectiveSubjectSearchResponse,
): AuthorizationEffectiveSubjectSearch {
  return {
    targets: input.targets.map(authorizationRelationshipTargetFromProtoRequired),
    nextPageToken: input.nextPageToken,
    modelId: input.modelId,
    truncated: input.truncated,
  };
}

function authorizationActionSearchToProto(input: AuthorizationActionSearch) {
  return create(ActionSearchResponseSchema, {
    actions: input.actions?.map(authorizationActionToProto) ?? [],
    nextPageToken: input.nextPageToken ?? "",
    modelId: input.modelId ?? "",
  });
}

function authorizationActionSearchFromProto(input: ProtoActionSearchResponse): AuthorizationActionSearch {
  return {
    actions: input.actions.map(authorizationActionFromProtoRequired),
    nextPageToken: input.nextPageToken,
    modelId: input.modelId,
  };
}

function authorizationMetadataToProto(input: AuthorizationMetadata) {
  return create(AuthorizationMetadataSchema, {
    capabilities: [...(input.capabilities ?? [])],
    activeModelId: input.activeModelId ?? "",
  });
}

function authorizationMetadataFromProto(input: ProtoAuthorizationMetadata): AuthorizationMetadata {
  return {
    capabilities: [...input.capabilities],
    activeModelId: input.activeModelId,
  };
}

function authorizationReadRelationshipsToProto(input: AuthorizationReadRelationships) {
  return create(ReadRelationshipsResponseSchema, {
    relationships: input.relationships?.map(authorizationRelationshipToProto) ?? [],
    nextPageToken: input.nextPageToken ?? "",
    modelId: input.modelId ?? "",
  });
}

function authorizationReadRelationshipsFromProto(
  input: ProtoReadRelationshipsResponse,
): AuthorizationReadRelationships {
  return {
    relationships: input.relationships.map(authorizationRelationshipFromProto),
    nextPageToken: input.nextPageToken,
    modelId: input.modelId,
  };
}

function authorizationGetActiveModelToProto(input: AuthorizationGetActiveModel) {
  return create(GetActiveModelResponseSchema, {
    model: input.model === undefined ? undefined : authorizationModelRefToProto(input.model),
  });
}

function authorizationGetActiveModelFromProto(input: ProtoGetActiveModelResponse): AuthorizationGetActiveModel {
  return {
    model: authorizationModelRefFromProto(input.model),
  };
}

function authorizationListModelsToProto(input: AuthorizationListModels) {
  return create(ListModelsResponseSchema, {
    models: input.models?.map(authorizationModelRefToProto) ?? [],
    nextPageToken: input.nextPageToken ?? "",
  });
}

function authorizationListModelsFromProto(input: ProtoListModelsResponse): AuthorizationListModels {
  return {
    models: input.models.map(authorizationModelRefFromProtoRequired),
    nextPageToken: input.nextPageToken,
  };
}

function authorizationSubjectToProto(input: AuthorizationSubject) {
  return create(SubjectSchema, {
    type: input.type,
    id: input.id,
    properties: optionalStruct(input.properties),
  });
}

function authorizationSubjectFromProto(input?: ProtoSubject | undefined): AuthorizationSubject | undefined {
  return input === undefined ? undefined : authorizationSubjectFromProtoRequired(input);
}

function authorizationSubjectFromProtoRequired(input: ProtoSubject): AuthorizationSubject {
  return {
    type: input.type,
    id: input.id,
    properties: optionalObjectFromStruct(input.properties),
  };
}

function authorizationResourceToProto(input: AuthorizationResource) {
  return create(ResourceSchema, {
    type: input.type,
    id: input.id,
    properties: optionalStruct(input.properties),
  });
}

function authorizationResourceFromProto(input?: ProtoResource | undefined): AuthorizationResource | undefined {
  return input === undefined ? undefined : authorizationResourceFromProtoRequired(input);
}

function authorizationResourceFromProtoRequired(input: ProtoResource): AuthorizationResource {
  return {
    type: input.type,
    id: input.id,
    properties: optionalObjectFromStruct(input.properties),
  };
}

function authorizationSubjectSetToProto(input: AuthorizationSubjectSet) {
  return create(SubjectSetSchema, {
    resource: input.resource === undefined ? undefined : authorizationResourceToProto(input.resource),
    relation: input.relation,
  });
}

function authorizationSubjectSetFromProto(input?: ProtoSubjectSet | undefined): AuthorizationSubjectSet | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    resource: authorizationResourceFromProto(input.resource),
    relation: input.relation,
  };
}

function authorizationRelationshipTargetToProto(input: AuthorizationRelationshipTarget) {
  switch (input.kind.case) {
    case "subject":
      return create(RelationshipTargetSchema, {
        kind: { case: "subject", value: authorizationSubjectToProto(input.kind.value) },
      });
    case "resource":
      return create(RelationshipTargetSchema, {
        kind: { case: "resource", value: authorizationResourceToProto(input.kind.value) },
      });
    case "subjectSet":
      return create(RelationshipTargetSchema, {
        kind: { case: "subjectSet", value: authorizationSubjectSetToProto(input.kind.value) },
      });
    default:
      return create(RelationshipTargetSchema);
  }
}

function authorizationRelationshipTargetFromProto(
  input?: ProtoRelationshipTarget | undefined,
): AuthorizationRelationshipTarget | undefined {
  return input === undefined ? undefined : authorizationRelationshipTargetFromProtoRequired(input);
}

function authorizationRelationshipTargetFromProtoRequired(
  input: ProtoRelationshipTarget,
): AuthorizationRelationshipTarget {
  switch (input.kind.case) {
    case "subject":
      return { kind: { case: "subject", value: authorizationSubjectFromProtoRequired(input.kind.value) } };
    case "resource":
      return { kind: { case: "resource", value: authorizationResourceFromProtoRequired(input.kind.value) } };
    case "subjectSet":
      return { kind: { case: "subjectSet", value: authorizationSubjectSetFromProto(input.kind.value)! } };
    default:
      return { kind: { case: undefined } };
  }
}

function authorizationActionToProto(input: AuthorizationAction) {
  return create(ActionSchema, {
    name: input.name,
    properties: optionalStruct(input.properties),
  });
}

function authorizationActionFromProto(input?: ProtoAction | undefined): AuthorizationAction | undefined {
  return input === undefined ? undefined : authorizationActionFromProtoRequired(input);
}

function authorizationActionFromProtoRequired(input: ProtoAction): AuthorizationAction {
  return {
    name: input.name,
    properties: optionalObjectFromStruct(input.properties),
  };
}

function authorizationRelationshipToProto(input: AuthorizationRelationship) {
  return create(RelationshipSchema, {
    subject: input.subject === undefined ? undefined : authorizationSubjectToProto(input.subject),
    relation: input.relation,
    resource: input.resource === undefined ? undefined : authorizationResourceToProto(input.resource),
    properties: optionalStruct(input.properties),
    target: input.target === undefined ? undefined : authorizationRelationshipTargetToProto(input.target),
  });
}

function authorizationRelationshipFromProto(input: ProtoRelationship): AuthorizationRelationship {
  return {
    subject: authorizationSubjectFromProto(input.subject),
    relation: input.relation,
    resource: authorizationResourceFromProto(input.resource),
    properties: optionalObjectFromStruct(input.properties),
    target: authorizationRelationshipTargetFromProto(input.target),
  };
}

function authorizationRelationshipKeyToProto(input: AuthorizationRelationshipKey) {
  return create(RelationshipKeySchema, {
    subject: input.subject === undefined ? undefined : authorizationSubjectToProto(input.subject),
    relation: input.relation,
    resource: input.resource === undefined ? undefined : authorizationResourceToProto(input.resource),
    target: input.target === undefined ? undefined : authorizationRelationshipTargetToProto(input.target),
  });
}

function authorizationRelationshipKeyFromProto(input: ProtoRelationshipKey): AuthorizationRelationshipKey {
  return {
    subject: authorizationSubjectFromProto(input.subject),
    relation: input.relation,
    resource: authorizationResourceFromProto(input.resource),
    target: authorizationRelationshipTargetFromProto(input.target),
  };
}

function authorizationModelToProto(input: AuthorizationModel) {
  return {
    version: input.version ?? 0,
    resourceTypes: input.resourceTypes?.map(authorizationModelResourceTypeToProto) ?? [],
  };
}

function authorizationModelFromProto(input?: ProtoAuthorizationModel | undefined): AuthorizationModel | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    version: input.version,
    resourceTypes: input.resourceTypes.map(authorizationModelResourceTypeFromProto),
  };
}

function authorizationModelResourceTypeToProto(input: AuthorizationModelResourceType) {
  return {
    name: input.name,
    relations: input.relations?.map(authorizationModelRelationToProto) ?? [],
    actions: input.actions?.map(authorizationModelActionToProto) ?? [],
  };
}

function authorizationModelResourceTypeFromProto(
  input: ProtoAuthorizationModelResourceType,
): AuthorizationModelResourceType {
  return {
    name: input.name,
    relations: input.relations.map(authorizationModelRelationFromProto),
    actions: input.actions.map(authorizationModelActionFromProto),
  };
}

function authorizationModelRelationToProto(input: AuthorizationModelRelation) {
  return {
    name: input.name,
    subjectTypes: [...(input.subjectTypes ?? [])],
    allowedTargets: input.allowedTargets?.map(authorizationModelAllowedTargetToProto) ?? [],
    rewrite: input.rewrite === undefined ? undefined : authorizationModelRewriteToProto(input.rewrite),
  };
}

function authorizationModelRelationFromProto(
  input: ProtoAuthorizationModelRelation,
): AuthorizationModelRelation {
  return {
    name: input.name,
    subjectTypes: [...input.subjectTypes],
    allowedTargets: input.allowedTargets.map(authorizationModelAllowedTargetFromProto),
    rewrite: authorizationModelRewriteFromProto(input.rewrite),
  };
}

function authorizationModelActionToProto(input: AuthorizationModelAction) {
  return {
    name: input.name,
    relations: [...(input.relations ?? [])],
    rewrite: input.rewrite === undefined ? undefined : authorizationModelRewriteToProto(input.rewrite),
  };
}

function authorizationModelActionFromProto(input: ProtoAuthorizationModelAction): AuthorizationModelAction {
  return {
    name: input.name,
    relations: [...input.relations],
    rewrite: authorizationModelRewriteFromProto(input.rewrite),
  };
}

function authorizationModelAllowedTargetToProto(input: AuthorizationModelAllowedTarget) {
  switch (input.kind.case) {
    case "subjectType":
      return { kind: { case: "subjectType" as const, value: input.kind.value } };
    case "resourceType":
      return { kind: { case: "resourceType" as const, value: input.kind.value } };
    case "subjectSet":
      return {
        kind: {
          case: "subjectSet" as const,
          value: {
            resourceType: input.kind.value.resourceType,
            relation: input.kind.value.relation,
          },
        },
      };
    default:
      return { kind: { case: undefined } };
  }
}

function authorizationModelAllowedTargetFromProto(
  input: ProtoAuthorizationModelAllowedTarget,
): AuthorizationModelAllowedTarget {
  switch (input.kind.case) {
    case "subjectType":
      return { kind: { case: "subjectType", value: input.kind.value } };
    case "resourceType":
      return { kind: { case: "resourceType", value: input.kind.value } };
    case "subjectSet":
      return {
        kind: {
          case: "subjectSet",
          value: authorizationModelSubjectSetTargetFromProto(input.kind.value),
        },
      };
    default:
      return { kind: { case: undefined } };
  }
}

function authorizationModelSubjectSetTargetFromProto(
  input: ProtoAuthorizationModelSubjectSetTarget,
): AuthorizationModelSubjectSetTarget {
  return {
    resourceType: input.resourceType,
    relation: input.relation,
  };
}

function authorizationModelRewriteToProto(input: AuthorizationModelRewrite): ProtoAuthorizationModelRewrite {
  switch (input.kind.case) {
    case "this":
      return { kind: { case: "this", value: {} } } as ProtoAuthorizationModelRewrite;
    case "computedUserset":
      return {
        kind: {
          case: "computedUserset",
          value: { relation: input.kind.value.relation },
        },
      } as ProtoAuthorizationModelRewrite;
    case "tupleToUserset":
      return {
        kind: {
          case: "tupleToUserset",
          value: {
            tuplesetRelation: input.kind.value.tuplesetRelation,
            computedRelation: input.kind.value.computedRelation,
          },
        },
      } as ProtoAuthorizationModelRewrite;
    case "union":
      return {
        kind: {
          case: "union",
          value: {
            children: input.kind.value.children?.map(authorizationModelRewriteToProto) ?? [],
          },
        },
      } as ProtoAuthorizationModelRewrite;
    default:
      return { kind: { case: undefined } } as ProtoAuthorizationModelRewrite;
  }
}

function authorizationModelRewriteFromProto(
  input?: ProtoAuthorizationModelRewrite | undefined,
): AuthorizationModelRewrite | undefined {
  if (input === undefined) {
    return undefined;
  }
  switch (input.kind.case) {
    case "this":
      return { kind: { case: "this", value: {} } };
    case "computedUserset":
      return { kind: { case: "computedUserset", value: authorizationComputedUsersetFromProto(input.kind.value) } };
    case "tupleToUserset":
      return { kind: { case: "tupleToUserset", value: authorizationTupleToUsersetFromProto(input.kind.value) } };
    case "union":
      return { kind: { case: "union", value: authorizationRewriteUnionFromProto(input.kind.value) } };
    default:
      return { kind: { case: undefined } };
  }
}

function authorizationComputedUsersetFromProto(
  input: ProtoAuthorizationModelComputedUserset,
): AuthorizationModelComputedUserset {
  return { relation: input.relation };
}

function authorizationTupleToUsersetFromProto(
  input: ProtoAuthorizationModelTupleToUserset,
): AuthorizationModelTupleToUserset {
  return {
    tuplesetRelation: input.tuplesetRelation,
    computedRelation: input.computedRelation,
  };
}

function authorizationRewriteUnionFromProto(
  input: ProtoAuthorizationModelRewriteUnion,
): AuthorizationModelRewriteUnion {
  return { children: input.children.map((child) => authorizationModelRewriteFromProto(child)!) };
}

function authorizationModelRefToProto(input: AuthorizationModelRef) {
  return create(AuthorizationModelRefSchema, {
    id: input.id,
    version: input.version,
    createdAt: input.createdAt === undefined ? undefined : timestampFromDate(input.createdAt),
  });
}

function authorizationModelRefFromProto(input?: ProtoAuthorizationModelRef | undefined): AuthorizationModelRef | undefined {
  return input === undefined ? undefined : authorizationModelRefFromProtoRequired(input);
}

function authorizationModelRefFromProtoRequired(input: ProtoAuthorizationModelRef): AuthorizationModelRef {
  return {
    id: input.id,
    version: input.version,
    createdAt: input.createdAt === undefined ? undefined : dateFromTimestamp(input.createdAt),
  };
}

function authorizationExpandToProto(input: AuthorizationExpand) {
  return create(ExpandResponseSchema, {
    root: input.root === undefined ? undefined : authorizationExpandNodeToProto(input.root),
    truncated: input.truncated ?? false,
    cycleDetected: input.cycleDetected ?? false,
    maxDepthReached: input.maxDepthReached ?? false,
    modelId: input.modelId ?? "",
  });
}

function authorizationExpandFromProto(input: ProtoExpandResponse): AuthorizationExpand {
  return {
    root: authorizationExpandNodeFromProto(input.root),
    truncated: input.truncated,
    cycleDetected: input.cycleDetected,
    maxDepthReached: input.maxDepthReached,
    modelId: input.modelId,
  };
}

function authorizationExpandNodeToProto(input: AuthorizationExpandNode): ProtoExpandNode {
  return create(ExpandNodeSchema, {
    target: input.target === undefined ? undefined : authorizationRelationshipTargetToProto(input.target),
    relation: input.relation ?? "",
    children: input.children?.map(authorizationExpandNodeToProto) ?? [],
  });
}

function authorizationExpandNodeFromProto(input?: ProtoExpandNode | undefined): AuthorizationExpandNode | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    target: authorizationRelationshipTargetFromProto(input.target),
    relation: input.relation,
    children: input.children.map((child) => authorizationExpandNodeFromProto(child)!),
  };
}

function resolveAuthorizationSocketTarget(
  socketPath = process.env[ENV_HOST_SERVICE_SOCKET],
): string {
  const trimmed = socketPath?.trim() ?? "";
  if (!trimmed) {
    throw new Error(`authorization: ${ENV_HOST_SERVICE_SOCKET} is not set`);
  }
  return trimmed;
}

function pushCapability(capabilities: string[], capability: string): void {
  if (!capabilities.includes(capability)) {
    capabilities.push(capability);
  }
}
