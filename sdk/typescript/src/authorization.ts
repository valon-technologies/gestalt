import { create } from "@bufbuild/protobuf";
import {
  Code,
  ConnectError,
  createClient,
  type Client,
  type ServiceImpl,
} from "@connectrpc/connect";

import {
  ActionSchema,
  AddRelationshipRequestSchema,
  AddRelationshipResponseSchema,
  AuthorizationModelActionSchema,
  AuthorizationModelAllowedTargetSchema,
  AuthorizationModelRefSchema,
  AuthorizationModelRelationSchema,
  AuthorizationModelResourceTypeFilterSchema,
  AuthorizationModelResourceTypeSchema,
  AuthorizationModelSchema,
  AuthorizationProvider as AuthorizationProviderService,
  CheckAccessManyRequestSchema,
  CheckAccessRequestSchema,
  ListActiveModelResourceTypesRequestSchema,
  ListRelationshipsRequestSchema,
  RelationshipFilterSchema,
  RelationshipSchema,
  RelationshipTargetSchema,
  RelationshipTupleSchema,
  ResourceSchema,
  SetActiveModelRequestSchema,
  SetRelationshipsRequestSchema,
  SubjectSchema,
  SubjectSetSchema,
  type Action as ProtoAction,
  type AuthorizationModel as ProtoAuthorizationModel,
  type AuthorizationModelAction as ProtoAuthorizationModelAction,
  type AuthorizationModelAllowedTarget as ProtoAuthorizationModelAllowedTarget,
  type AuthorizationModelRef as ProtoAuthorizationModelRef,
  type AuthorizationModelRelation as ProtoAuthorizationModelRelation,
  type AuthorizationModelResourceType as ProtoAuthorizationModelResourceType,
  type AuthorizationModelResourceTypeFilter as ProtoAuthorizationModelResourceTypeFilter,
  type CheckAccessRequest as ProtoCheckAccessRequest,
  type CheckAccessResponse as ProtoCheckAccessResponse,
  type Relationship as ProtoRelationship,
  type RelationshipFilter as ProtoRelationshipFilter,
  type RelationshipTarget as ProtoRelationshipTarget,
  type RelationshipTuple as ProtoRelationshipTuple,
  type Resource as ProtoResource,
  type Subject as ProtoSubject,
  type SubjectSet as ProtoSubjectSet,
  RelationshipTargetType,
  SourceLayer,
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

export const AUTHORIZATION_SUBJECT_TYPE_SUBJECT = "subject";

export { RelationshipTargetType, SourceLayer };

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

export interface AuthorizationCheckAccessInput {
  subject?: AuthorizationSubject | undefined;
  action?: AuthorizationAction | undefined;
  resource?: AuthorizationResource | undefined;
}

export interface AuthorizationDecision {
  allowed?: boolean | undefined;
  modelId?: string | undefined;
}

export interface AuthorizationCheckAccessManyInput {
  requests: readonly AuthorizationCheckAccessInput[];
}

export interface AuthorizationCheckAccessManyResponse {
  decisions: readonly AuthorizationDecision[];
}

export interface AuthorizationRelationshipTuple {
  target?: AuthorizationRelationshipTarget | undefined;
  relation: string;
  resource?: AuthorizationResource | undefined;
}

export interface AuthorizationRelationship {
  tuple?: AuthorizationRelationshipTuple | undefined;
  properties?: JsonObjectInput | undefined;
  sourceLayer?: SourceLayer | undefined;
}

export interface AuthorizationRelationshipFilter {
  target?: AuthorizationRelationshipTarget | undefined;
  relation?: string | undefined;
  resource?: AuthorizationResource | undefined;
  targetType?: RelationshipTargetType | undefined;
  targetEntityType?: string | undefined;
  resourceType?: string | undefined;
  sourceLayer?: SourceLayer | undefined;
}

export interface AuthorizationListRelationshipsInput {
  filter?: AuthorizationRelationshipFilter | undefined;
  pageSize?: number | undefined;
  pageToken?: string | undefined;
}

export interface AuthorizationListRelationships {
  relationships: readonly AuthorizationRelationship[];
  nextPageToken?: string | undefined;
}

export interface AuthorizationAddRelationshipInput {
  relationship?: AuthorizationRelationship | undefined;
}

export interface AuthorizationAddRelationshipResponse {
  relationship?: AuthorizationRelationship | undefined;
}

export interface AuthorizationDeleteRelationshipInput {
  relationshipTuple?: AuthorizationRelationshipTuple | undefined;
}

export interface AuthorizationSetRelationshipsInput {
  relationships: readonly AuthorizationRelationship[];
}

export interface AuthorizationSetRelationshipsResponse {
  relationships: readonly AuthorizationRelationship[];
}

export interface AuthorizationModel {
  id?: string | undefined;
  version?: string | undefined;
  resourceTypes?: readonly AuthorizationModelResourceType[] | undefined;
}

export interface AuthorizationModelResourceType {
  name: string;
  relations?: readonly AuthorizationModelRelation[] | undefined;
  actions?: readonly AuthorizationModelAction[] | undefined;
  sourceLayer?: SourceLayer | undefined;
}

export interface AuthorizationModelRelation {
  name: string;
  allowedTargets?: readonly AuthorizationModelAllowedTarget[] | undefined;
}

export interface AuthorizationModelAction {
  name: string;
  relations?: readonly string[] | undefined;
}

export type AuthorizationModelAllowedTargetKind =
  | { case: "subjectType"; value: string }
  | { case: "resourceType"; value: string }
  | { case: "subjectSetType"; value: AuthorizationSubjectSetType }
  | { case: undefined; value?: undefined };

export interface AuthorizationModelAllowedTarget {
  kind: AuthorizationModelAllowedTargetKind;
}

export interface AuthorizationSubjectSetType {
  resourceType: string;
  relation: string;
}

export interface AuthorizationModelRef {
  id: string;
  version: string;
  createdAt?: Date | undefined;
}

export interface AuthorizationGetActiveModelRefResponse {
  model?: AuthorizationModelRef | undefined;
}

export interface AuthorizationSetActiveModelInput {
  model?: AuthorizationModel | undefined;
}

export interface AuthorizationSetActiveModelResponse {
  model?: AuthorizationModelRef | undefined;
}

export interface AuthorizationModelResourceTypeFilter {
  name?: string | undefined;
  sourceLayer?: SourceLayer | undefined;
}

export interface AuthorizationListActiveModelResourceTypesInput {
  modelId?: string | undefined;
  filter?: AuthorizationModelResourceTypeFilter | undefined;
}

export interface AuthorizationListActiveModelResourceTypesResponse {
  resourceTypes: readonly AuthorizationModelResourceType[];
}

export type AuthorizationEvaluateInput = AuthorizationCheckAccessInput;
export type AuthorizationEvaluateManyInput = AuthorizationCheckAccessManyInput;
export type AuthorizationEvaluationsResponse = AuthorizationCheckAccessManyResponse;
export type AuthorizationReadRelationshipsInput = AuthorizationListRelationshipsInput;
export type AuthorizationReadRelationships = AuthorizationListRelationships;
export type AuthorizationWriteRelationshipsInput = AuthorizationSetRelationshipsInput;
export type AuthorizationGetActiveModel = AuthorizationGetActiveModelRefResponse;
export type AuthorizationWriteModelInput = AuthorizationSetActiveModelInput;
export type AuthorizationModelSubjectSetTarget = AuthorizationSubjectSetType;
export type AuthorizationRelationshipKey = AuthorizationRelationshipTuple;
export interface AuthorizationMetadata {
  capabilities?: readonly string[] | undefined;
  activeModelId?: string | undefined;
}
export interface AuthorizationResourceSearch {
  resources: readonly AuthorizationResource[];
  nextPageToken?: string | undefined;
  modelId?: string | undefined;
}
export interface AuthorizationSubjectSearch {
  subjects: readonly AuthorizationSubject[];
  nextPageToken?: string | undefined;
  modelId?: string | undefined;
}
export interface AuthorizationActionSearch {
  actions: readonly AuthorizationAction[];
  nextPageToken?: string | undefined;
  modelId?: string | undefined;
}
export type AuthorizationSearchResourcesInput = AuthorizationListRelationshipsInput;
export type AuthorizationSearchSubjectsInput = AuthorizationListRelationshipsInput;
export type AuthorizationSearchActionsInput = AuthorizationListRelationshipsInput;
export type AuthorizationEffectiveSearchSubjectsInput = AuthorizationListRelationshipsInput;
export interface AuthorizationEffectiveSubjectSearch {
  targets: readonly AuthorizationRelationshipTarget[];
  nextPageToken?: string | undefined;
  modelId?: string | undefined;
  truncated?: boolean | undefined;
}
export interface AuthorizationExpandInput {
  resource?: AuthorizationResource | undefined;
  relation?: string | undefined;
}
export interface AuthorizationExpandNode {
  target?: AuthorizationRelationshipTarget | undefined;
  relation?: string | undefined;
  children?: readonly AuthorizationExpandNode[] | undefined;
}
export interface AuthorizationExpand {
  root?: AuthorizationExpandNode | undefined;
}
export interface AuthorizationListModelsInput {
  pageSize?: number | undefined;
  pageToken?: string | undefined;
}
export interface AuthorizationListModels {
  models?: readonly AuthorizationModelRef[] | undefined;
  nextPageToken?: string | undefined;
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
export type AuthorizationModelRewriteKind =
  | { case: "this"; value: AuthorizationModelRewriteThis }
  | { case: "computedUserset"; value: AuthorizationModelComputedUserset }
  | { case: "tupleToUserset"; value: AuthorizationModelTupleToUserset }
  | { case: "union"; value: AuthorizationModelRewriteUnion }
  | { case: undefined; value?: undefined };
export interface AuthorizationModelRewrite {
  kind: AuthorizationModelRewriteKind;
}

export interface Authorization {
  checkAccess(request: AuthorizationCheckAccessInput): Promise<AuthorizationDecision>;
  checkAccessMany(request: AuthorizationCheckAccessManyInput): Promise<AuthorizationCheckAccessManyResponse>;
  listRelationships(request?: AuthorizationListRelationshipsInput): Promise<AuthorizationListRelationships>;
  addRelationship(request: AuthorizationAddRelationshipInput): Promise<AuthorizationAddRelationshipResponse>;
  deleteRelationship(request: AuthorizationDeleteRelationshipInput): Promise<void>;
  setRelationships(request: AuthorizationSetRelationshipsInput): Promise<AuthorizationSetRelationshipsResponse>;
  getActiveModelRef(): Promise<AuthorizationGetActiveModelRefResponse>;
  setActiveModel(request: AuthorizationSetActiveModelInput): Promise<AuthorizationSetActiveModelResponse>;
  listActiveModelResourceTypes(request?: AuthorizationListActiveModelResourceTypesInput): Promise<AuthorizationListActiveModelResourceTypesResponse>;
}

class AuthorizationImpl implements Authorization {
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

  async checkAccess(request: AuthorizationCheckAccessInput) {
    return authorizationDecisionFromProto(
      await this.client.checkAccess(authorizationCheckAccessInputToProto(request)),
    );
  }

  async checkAccessMany(request: AuthorizationCheckAccessManyInput) {
    return authorizationCheckAccessManyResponseFromProto(
      await this.client.checkAccessMany(authorizationCheckAccessManyInputToProto(request)),
    );
  }

  async listRelationships(request: AuthorizationListRelationshipsInput = {}) {
    return authorizationListRelationshipsFromProto(
      await this.client.listRelationships(authorizationListRelationshipsInputToProto(request)),
    );
  }

  async addRelationship(request: AuthorizationAddRelationshipInput) {
    return {
      relationship: authorizationRelationshipFromProto(
        (await this.client.addRelationship(create(AddRelationshipRequestSchema, {
          relationship: request.relationship === undefined ? undefined : authorizationRelationshipToProto(request.relationship),
        }))).relationship,
      ),
    };
  }

  async deleteRelationship(request: AuthorizationDeleteRelationshipInput) {
    await this.client.deleteRelationship({
      relationshipTuple: request.relationshipTuple === undefined ? undefined : authorizationRelationshipTupleToProto(request.relationshipTuple),
    });
  }

  async setRelationships(request: AuthorizationSetRelationshipsInput) {
    return authorizationSetRelationshipsResponseFromProto(
      await this.client.setRelationships(create(SetRelationshipsRequestSchema, {
        relationships: request.relationships.map(authorizationRelationshipToProto),
      })),
    );
  }

  async getActiveModelRef() {
    return {
      model: authorizationModelRefFromProto((await this.client.getActiveModelRef({})).model),
    };
  }

  async setActiveModel(request: AuthorizationSetActiveModelInput) {
    return {
      model: authorizationModelRefFromProto(
        (await this.client.setActiveModel(create(SetActiveModelRequestSchema, {
          model: request.model === undefined ? undefined : authorizationModelToProto(request.model),
        }))).model,
      ),
    };
  }

  async listActiveModelResourceTypes(request: AuthorizationListActiveModelResourceTypesInput = {}) {
    return {
      resourceTypes: (await this.client.listActiveModelResourceTypes(
        authorizationListActiveModelResourceTypesInputToProto(request),
      )).resourceTypes.map(authorizationModelResourceTypeFromProto),
    };
  }
}

export interface AuthorizationProviderOptions extends ProviderBaseOptions {
  checkAccess: (request: AuthorizationCheckAccessInput) => MaybePromise<AuthorizationDecision>;
  checkAccessMany: (request: AuthorizationCheckAccessManyInput) => MaybePromise<AuthorizationCheckAccessManyResponse>;
  listRelationships: (request: AuthorizationListRelationshipsInput) => MaybePromise<AuthorizationListRelationships>;
  addRelationship: (request: AuthorizationAddRelationshipInput) => MaybePromise<AuthorizationAddRelationshipResponse>;
  deleteRelationship: (request: AuthorizationDeleteRelationshipInput) => MaybePromise<void>;
  setRelationships: (request: AuthorizationSetRelationshipsInput) => MaybePromise<AuthorizationSetRelationshipsResponse>;
  getActiveModelRef: () => MaybePromise<AuthorizationGetActiveModelRefResponse>;
  setActiveModel: (request: AuthorizationSetActiveModelInput) => MaybePromise<AuthorizationSetActiveModelResponse>;
  listActiveModelResourceTypes: (request: AuthorizationListActiveModelResourceTypesInput) => MaybePromise<AuthorizationListActiveModelResourceTypesResponse>;
}

export class AuthorizationProvider extends ProviderBase implements Authorization {
  readonly kind = "authorization" as const;

  constructor(private readonly options: AuthorizationProviderOptions) {
    super(options);
  }

  async checkAccess(request: AuthorizationCheckAccessInput) {
    return await this.options.checkAccess(request);
  }

  async checkAccessMany(request: AuthorizationCheckAccessManyInput) {
    return await this.options.checkAccessMany(request);
  }

  async listRelationships(request: AuthorizationListRelationshipsInput = {}) {
    return await this.options.listRelationships(request);
  }

  async addRelationship(request: AuthorizationAddRelationshipInput) {
    return await this.options.addRelationship(request);
  }

  async deleteRelationship(request: AuthorizationDeleteRelationshipInput) {
    await this.options.deleteRelationship(request);
  }

  async setRelationships(request: AuthorizationSetRelationshipsInput) {
    return await this.options.setRelationships(request);
  }

  async getActiveModelRef() {
    return await this.options.getActiveModelRef();
  }

  async setActiveModel(request: AuthorizationSetActiveModelInput) {
    return await this.options.setActiveModel(request);
  }

  async listActiveModelResourceTypes(request: AuthorizationListActiveModelResourceTypesInput = {}) {
    return await this.options.listActiveModelResourceTypes(request);
  }
}

export function defineAuthorizationProvider(options: AuthorizationProviderOptions): AuthorizationProvider {
  return new AuthorizationProvider(options);
}

export function isAuthorizationProvider(value: unknown): value is AuthorizationProvider {
  return (
    value instanceof AuthorizationProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      String((value as { kind?: unknown }).kind ?? "") === "authorization" &&
      "checkAccess" in value &&
      "checkAccessMany" in value &&
      "listRelationships" in value &&
      "addRelationship" in value &&
      "deleteRelationship" in value &&
      "setRelationships" in value &&
      "getActiveModelRef" in value &&
      "setActiveModel" in value &&
      "listActiveModelResourceTypes" in value)
  );
}

export function createAuthorizationProviderService(provider: AuthorizationProvider): AuthorizationProviderServiceImpl {
  return {
    async checkAccess(request) {
      return authorizationDecisionToProto(
        requiredAuthorizationResponse(await provider.checkAccess(authorizationCheckAccessInputFromProto(request)), "check access"),
      );
    },
    async checkAccessMany(request) {
      return authorizationCheckAccessManyResponseToProto(
        requiredAuthorizationResponse(await provider.checkAccessMany({
          requests: request.requests.map(authorizationCheckAccessInputFromProto),
        }), "check access many"),
      );
    },
    async listRelationships(request) {
      return authorizationListRelationshipsToProto(
        requiredAuthorizationResponse(await provider.listRelationships(authorizationListRelationshipsInputFromProto(request)), "list relationships"),
      );
    },
    async addRelationship(request) {
      return create(AddRelationshipResponseSchema, {
        relationship: authorizationRelationshipToProtoRequired(
          requiredAuthorizationResponse(await provider.addRelationship({
            relationship: authorizationRelationshipFromProto(request.relationship),
          }), "add relationship").relationship,
        ),
      });
    },
    async deleteRelationship(request) {
      await provider.deleteRelationship({
        relationshipTuple: authorizationRelationshipTupleFromProto(request.relationshipTuple),
      });
      return {};
    },
    async setRelationships(request) {
      return authorizationSetRelationshipsResponseToProto(
        requiredAuthorizationResponse(await provider.setRelationships({
          relationships: request.relationships.map(authorizationRelationshipFromProtoRequired),
        }), "set relationships"),
      );
    },
    async getActiveModelRef() {
      return {
        model: authorizationModelRefToProto(requiredAuthorizationResponse(await provider.getActiveModelRef(), "active model ref").model),
      };
    },
    async setActiveModel(request) {
      return {
        model: authorizationModelRefToProto(requiredAuthorizationResponse(await provider.setActiveModel({
          model: authorizationModelFromProto(request.model),
        }), "set active model").model),
      };
    },
    async listActiveModelResourceTypes(request) {
      return {
        resourceTypes: requiredAuthorizationResponse(await provider.listActiveModelResourceTypes(
          authorizationListActiveModelResourceTypesInputFromProto(request),
        ), "list active model resource types").resourceTypes.map(authorizationModelResourceTypeToProto),
      };
    },
  };
}

function requiredAuthorizationResponse<T>(value: T | null | undefined, label: string): T {
  if (value === null || value === undefined) {
    throw new ConnectError(`authorization provider returned nil ${label} response`, Code.Internal);
  }
  return value;
}

const sharedAuthorizationTransport: {
  target: string;
  token: string;
  client: AuthorizationImpl | undefined;
} = { target: "", token: "", client: undefined };

export function Authorization(): Authorization {
  const target = resolveAuthorizationSocketTarget();
  const token = process.env[ENV_HOST_SERVICE_TOKEN]?.trim() ?? "";
  if (sharedAuthorizationTransport.client && sharedAuthorizationTransport.target === target && sharedAuthorizationTransport.token === token) {
    return sharedAuthorizationTransport.client;
  }
  const client = new AuthorizationImpl(target, token);
  sharedAuthorizationTransport.target = target;
  sharedAuthorizationTransport.token = token;
  sharedAuthorizationTransport.client = client;
  return client;
}

export function authorizationSubject(type: string, id: string, properties?: JsonObjectInput): AuthorizationSubject {
  return properties === undefined ? { type, id } : { type, id, properties };
}

export function authorizationResource(type: string, id: string, properties?: JsonObjectInput): AuthorizationResource {
  return properties === undefined ? { type, id } : { type, id, properties };
}

export function authorizationSubjectSet(resource: AuthorizationResource, relation: string): AuthorizationSubjectSet {
  return { resource, relation };
}

export function authorizationSubjectTarget(subject: AuthorizationSubject): AuthorizationRelationshipTarget {
  return { kind: { case: "subject", value: subject } };
}

export function authorizationResourceTarget(resource: AuthorizationResource): AuthorizationRelationshipTarget {
  return { kind: { case: "resource", value: resource } };
}

export function authorizationSubjectSetTarget(resource: AuthorizationResource, relation: string): AuthorizationRelationshipTarget {
  return { kind: { case: "subjectSet", value: authorizationSubjectSet(resource, relation) } };
}

export function authorizationAction(name: string, properties?: JsonObjectInput): AuthorizationAction {
  return properties === undefined ? { name } : { name, properties };
}

export function authorizationRelationship(subject: AuthorizationSubject, relation: string, resource: AuthorizationResource, properties?: JsonObjectInput): AuthorizationRelationship {
  return authorizationRelationshipWithTarget(authorizationSubjectTarget(subject), relation, resource, properties);
}

export function authorizationRelationshipWithTarget(target: AuthorizationRelationshipTarget, relation: string, resource: AuthorizationResource, properties?: JsonObjectInput): AuthorizationRelationship {
  const tuple = { target, relation, resource };
  return properties === undefined ? { tuple } : { tuple, properties };
}

export function authorizationRelationshipTuple(target: AuthorizationRelationshipTarget, relation: string, resource: AuthorizationResource): AuthorizationRelationshipTuple {
  return { target, relation, resource };
}

export const authorizationRelationshipKey = authorizationRelationshipTuple;
export const authorizationRelationshipKeyWithTarget = authorizationRelationshipTuple;

function authorizationCheckAccessInputToProto(input: AuthorizationCheckAccessInput) {
  return create(CheckAccessRequestSchema, {
    subject: input.subject === undefined ? undefined : authorizationSubjectToProto(input.subject),
    action: input.action === undefined ? undefined : authorizationActionToProto(input.action),
    resource: input.resource === undefined ? undefined : authorizationResourceToProto(input.resource),
  });
}

function authorizationCheckAccessInputFromProto(input: ProtoCheckAccessRequest): AuthorizationCheckAccessInput {
  return {
    subject: authorizationSubjectFromProto(input.subject),
    action: authorizationActionFromProto(input.action),
    resource: authorizationResourceFromProto(input.resource),
  };
}

function authorizationCheckAccessManyInputToProto(input: AuthorizationCheckAccessManyInput) {
  return create(CheckAccessManyRequestSchema, {
    requests: input.requests.map(authorizationCheckAccessInputToProto),
  });
}

function authorizationDecisionToProto(input: AuthorizationDecision) {
  return { allowed: input.allowed ?? false, modelId: input.modelId ?? "" };
}

function authorizationDecisionFromProto(input: ProtoCheckAccessResponse): AuthorizationDecision {
  return { allowed: input.allowed, modelId: input.modelId };
}

function authorizationCheckAccessManyResponseToProto(input: AuthorizationCheckAccessManyResponse) {
  return { decisions: input.decisions.map(authorizationDecisionToProto) };
}

function authorizationCheckAccessManyResponseFromProto(input: { decisions: ProtoCheckAccessResponse[] }): AuthorizationCheckAccessManyResponse {
  return { decisions: input.decisions.map(authorizationDecisionFromProto) };
}

function authorizationListRelationshipsInputToProto(input: AuthorizationListRelationshipsInput) {
  return create(ListRelationshipsRequestSchema, {
    filter: input.filter === undefined ? undefined : authorizationRelationshipFilterToProto(input.filter),
    pageSize: input.pageSize ?? 0,
    pageToken: input.pageToken ?? "",
  });
}

function authorizationListRelationshipsInputFromProto(input: { filter?: ProtoRelationshipFilter | undefined; pageSize: number; pageToken: string }): AuthorizationListRelationshipsInput {
  return {
    filter: authorizationRelationshipFilterFromProto(input.filter),
    pageSize: input.pageSize,
    pageToken: input.pageToken,
  };
}

function authorizationListRelationshipsToProto(input: AuthorizationListRelationships) {
  return {
    relationships: input.relationships.map(authorizationRelationshipToProto),
    nextPageToken: input.nextPageToken ?? "",
  };
}

function authorizationListRelationshipsFromProto(input: { relationships: ProtoRelationship[]; nextPageToken: string }): AuthorizationListRelationships {
  return {
    relationships: input.relationships.map(authorizationRelationshipFromProtoRequired),
    nextPageToken: input.nextPageToken,
  };
}

function authorizationSetRelationshipsResponseToProto(input: AuthorizationSetRelationshipsResponse) {
  return { relationships: input.relationships.map(authorizationRelationshipToProto) };
}

function authorizationSetRelationshipsResponseFromProto(input: { relationships: ProtoRelationship[] }): AuthorizationSetRelationshipsResponse {
  return { relationships: input.relationships.map(authorizationRelationshipFromProtoRequired) };
}

function authorizationRelationshipFilterToProto(input: AuthorizationRelationshipFilter) {
  return create(RelationshipFilterSchema, {
    target: input.target === undefined ? undefined : authorizationRelationshipTargetToProto(input.target),
    relation: input.relation ?? "",
    resource: input.resource === undefined ? undefined : authorizationResourceToProto(input.resource),
    targetType: input.targetType ?? RelationshipTargetType.UNSPECIFIED,
    targetEntityType: input.targetEntityType ?? "",
    resourceType: input.resourceType ?? "",
    sourceLayer: input.sourceLayer ?? SourceLayer.UNSPECIFIED,
  });
}

function authorizationRelationshipFilterFromProto(input?: ProtoRelationshipFilter): AuthorizationRelationshipFilter | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    target: authorizationRelationshipTargetFromProto(input.target),
    relation: input.relation,
    resource: authorizationResourceFromProto(input.resource),
    targetType: input.targetType,
    targetEntityType: input.targetEntityType,
    resourceType: input.resourceType,
    sourceLayer: input.sourceLayer,
  };
}

function authorizationRelationshipToProto(input: AuthorizationRelationship): ProtoRelationship {
  return create(RelationshipSchema, {
    tuple: input.tuple === undefined ? undefined : authorizationRelationshipTupleToProto(input.tuple),
    properties: optionalStruct(input.properties),
    sourceLayer: input.sourceLayer ?? SourceLayer.UNSPECIFIED,
  });
}

function authorizationRelationshipToProtoRequired(input?: AuthorizationRelationship): ProtoRelationship {
  return authorizationRelationshipToProto(requiredAuthorizationResponse(input, "relationship"));
}

function authorizationRelationshipFromProto(input?: ProtoRelationship): AuthorizationRelationship | undefined {
  return input === undefined ? undefined : authorizationRelationshipFromProtoRequired(input);
}

function authorizationRelationshipFromProtoRequired(input: ProtoRelationship): AuthorizationRelationship {
  return {
    tuple: authorizationRelationshipTupleFromProto(input.tuple),
    properties: optionalObjectFromStruct(input.properties),
    sourceLayer: input.sourceLayer,
  };
}

function authorizationRelationshipTupleToProto(input: AuthorizationRelationshipTuple): ProtoRelationshipTuple {
  return create(RelationshipTupleSchema, {
    target: input.target === undefined ? undefined : authorizationRelationshipTargetToProto(input.target),
    relation: input.relation,
    resource: input.resource === undefined ? undefined : authorizationResourceToProto(input.resource),
  });
}

function authorizationRelationshipTupleFromProto(input?: ProtoRelationshipTuple): AuthorizationRelationshipTuple | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    target: authorizationRelationshipTargetFromProto(input.target),
    relation: input.relation,
    resource: authorizationResourceFromProto(input.resource),
  };
}

function authorizationSubjectToProto(input: AuthorizationSubject): ProtoSubject {
  return create(SubjectSchema, {
    type: input.type,
    id: input.id,
    properties: optionalStruct(input.properties),
  });
}

function authorizationSubjectFromProto(input?: ProtoSubject): AuthorizationSubject | undefined {
  return input === undefined ? undefined : {
    type: input.type,
    id: input.id,
    properties: optionalObjectFromStruct(input.properties),
  };
}

function authorizationResourceToProto(input: AuthorizationResource): ProtoResource {
  return create(ResourceSchema, {
    type: input.type,
    id: input.id,
    properties: optionalStruct(input.properties),
  });
}

function authorizationResourceFromProto(input?: ProtoResource): AuthorizationResource | undefined {
  return input === undefined ? undefined : {
    type: input.type,
    id: input.id,
    properties: optionalObjectFromStruct(input.properties),
  };
}

function authorizationSubjectSetToProto(input: AuthorizationSubjectSet): ProtoSubjectSet {
  return create(SubjectSetSchema, {
    resource: input.resource === undefined ? undefined : authorizationResourceToProto(input.resource),
    relation: input.relation,
  });
}

function authorizationSubjectSetFromProto(input?: ProtoSubjectSet): AuthorizationSubjectSet | undefined {
  return input === undefined ? undefined : {
    resource: authorizationResourceFromProto(input.resource),
    relation: input.relation,
  };
}

function authorizationRelationshipTargetToProto(input: AuthorizationRelationshipTarget): ProtoRelationshipTarget {
  return create(RelationshipTargetSchema, {
    kind: input.kind.case === "subject"
      ? { case: "subject", value: authorizationSubjectToProto(input.kind.value) }
      : input.kind.case === "resource"
        ? { case: "resource", value: authorizationResourceToProto(input.kind.value) }
        : input.kind.case === "subjectSet"
          ? { case: "subjectSet", value: authorizationSubjectSetToProto(input.kind.value) }
          : { case: undefined },
  });
}

function authorizationRelationshipTargetFromProto(input?: ProtoRelationshipTarget): AuthorizationRelationshipTarget | undefined {
  if (input === undefined) {
    return undefined;
  }
  switch (input.kind.case) {
    case "subject":
      return { kind: { case: "subject", value: authorizationSubjectFromProto(input.kind.value)! } };
    case "resource":
      return { kind: { case: "resource", value: authorizationResourceFromProto(input.kind.value)! } };
    case "subjectSet":
      return { kind: { case: "subjectSet", value: authorizationSubjectSetFromProto(input.kind.value)! } };
    default:
      return { kind: { case: undefined } };
  }
}

function authorizationActionToProto(input: AuthorizationAction): ProtoAction {
  return create(ActionSchema, {
    name: input.name,
    properties: optionalStruct(input.properties),
  });
}

function authorizationActionFromProto(input?: ProtoAction): AuthorizationAction | undefined {
  return input === undefined ? undefined : {
    name: input.name,
    properties: optionalObjectFromStruct(input.properties),
  };
}

function authorizationModelToProto(input: AuthorizationModel): ProtoAuthorizationModel {
  return create(AuthorizationModelSchema, {
    id: input.id ?? "",
    version: input.version ?? "",
    resourceTypes: input.resourceTypes?.map(authorizationModelResourceTypeToProto) ?? [],
  });
}

function authorizationModelFromProto(input?: ProtoAuthorizationModel): AuthorizationModel | undefined {
  return input === undefined ? undefined : {
    id: input.id,
    version: input.version,
    resourceTypes: input.resourceTypes.map(authorizationModelResourceTypeFromProto),
  };
}

function authorizationModelResourceTypeToProto(input: AuthorizationModelResourceType): ProtoAuthorizationModelResourceType {
  return create(AuthorizationModelResourceTypeSchema, {
    name: input.name,
    relations: input.relations?.map(authorizationModelRelationToProto) ?? [],
    actions: input.actions?.map(authorizationModelActionToProto) ?? [],
    sourceLayer: input.sourceLayer ?? SourceLayer.UNSPECIFIED,
  });
}

function authorizationModelResourceTypeFromProto(input: ProtoAuthorizationModelResourceType): AuthorizationModelResourceType {
  return {
    name: input.name,
    relations: input.relations.map(authorizationModelRelationFromProto),
    actions: input.actions.map(authorizationModelActionFromProto),
    sourceLayer: input.sourceLayer,
  };
}

function authorizationModelRelationToProto(input: AuthorizationModelRelation): ProtoAuthorizationModelRelation {
  return create(AuthorizationModelRelationSchema, {
    name: input.name,
    allowedTargets: input.allowedTargets?.map(authorizationModelAllowedTargetToProto) ?? [],
  });
}

function authorizationModelRelationFromProto(input: ProtoAuthorizationModelRelation): AuthorizationModelRelation {
  return {
    name: input.name,
    allowedTargets: input.allowedTargets.map(authorizationModelAllowedTargetFromProto),
  };
}

function authorizationModelActionToProto(input: AuthorizationModelAction): ProtoAuthorizationModelAction {
  return create(AuthorizationModelActionSchema, {
    name: input.name,
    relations: input.relations === undefined ? [] : [...input.relations],
  });
}

function authorizationModelActionFromProto(input: ProtoAuthorizationModelAction): AuthorizationModelAction {
  return { name: input.name, relations: [...input.relations] };
}

function authorizationModelAllowedTargetToProto(input: AuthorizationModelAllowedTarget): ProtoAuthorizationModelAllowedTarget {
  return create(AuthorizationModelAllowedTargetSchema, {
    kind: input.kind.case === "subjectType"
      ? { case: "subjectType", value: input.kind.value }
      : input.kind.case === "resourceType"
        ? { case: "resourceType", value: input.kind.value }
        : input.kind.case === "subjectSetType"
          ? { case: "subjectSetType", value: input.kind.value }
          : { case: undefined },
  });
}

function authorizationModelAllowedTargetFromProto(input: ProtoAuthorizationModelAllowedTarget): AuthorizationModelAllowedTarget {
  if (input.kind.case === "subjectType" || input.kind.case === "resourceType" || input.kind.case === "subjectSetType") {
    return { kind: input.kind };
  }
  return { kind: { case: undefined } };
}

function authorizationModelRefToProto(input?: AuthorizationModelRef): ProtoAuthorizationModelRef | undefined {
  return input === undefined ? undefined : create(AuthorizationModelRefSchema, {
    id: input.id,
    version: input.version,
    createdAt: input.createdAt === undefined ? undefined : timestampFromDate(input.createdAt),
  });
}

function authorizationModelRefFromProto(input?: ProtoAuthorizationModelRef): AuthorizationModelRef | undefined {
  return input === undefined ? undefined : {
    id: input.id,
    version: input.version,
    createdAt: input.createdAt === undefined ? undefined : dateFromTimestamp(input.createdAt),
  };
}

function authorizationListActiveModelResourceTypesInputToProto(input: AuthorizationListActiveModelResourceTypesInput) {
  return create(ListActiveModelResourceTypesRequestSchema, {
    modelId: input.modelId ?? "",
    filter: input.filter === undefined ? undefined : authorizationModelResourceTypeFilterToProto(input.filter),
  });
}

function authorizationListActiveModelResourceTypesInputFromProto(input: { modelId: string; filter?: ProtoAuthorizationModelResourceTypeFilter | undefined }): AuthorizationListActiveModelResourceTypesInput {
  return {
    modelId: input.modelId,
    filter: authorizationModelResourceTypeFilterFromProto(input.filter),
  };
}

function authorizationModelResourceTypeFilterToProto(input: AuthorizationModelResourceTypeFilter): ProtoAuthorizationModelResourceTypeFilter {
  return create(AuthorizationModelResourceTypeFilterSchema, {
    name: input.name ?? "",
    sourceLayer: input.sourceLayer ?? SourceLayer.UNSPECIFIED,
  });
}

function authorizationModelResourceTypeFilterFromProto(input?: ProtoAuthorizationModelResourceTypeFilter): AuthorizationModelResourceTypeFilter | undefined {
  return input === undefined ? undefined : {
    name: input.name,
    sourceLayer: input.sourceLayer,
  };
}

function resolveAuthorizationSocketTarget(socketTarget?: string): string {
  return socketTarget?.trim() || process.env[ENV_HOST_SERVICE_SOCKET]?.trim() || "unix:///tmp/gestalt-authorization.sock";
}
