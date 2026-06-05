import { create } from "@bufbuild/protobuf";
import {
  Code,
  ConnectError,
  createClient,
  type Client,
  type ServiceImpl,
} from "@connectrpc/connect";
import { EmptySchema } from "@bufbuild/protobuf/wkt";

import {
  ActionSchema,
  AddRelationshipRequestSchema,
  AddRelationshipResponseSchema,
  AuthorizationModelSchema,
  AuthorizationModelResourceTypeFilterSchema,
  AuthorizationModelRefSchema,
  AuthorizationModelResourceTypeSchema,
  CheckAccessManyRequestSchema,
  CheckAccessManyResponseSchema,
  CheckAccessRequestSchema,
  CheckAccessResponseSchema,
  DefaultAccessPolicy as ProtoDefaultAccessPolicy,
  DeleteRelationshipRequestSchema,
  DeleteRelationshipResponseSchema,
  GetActiveModelRefResponseSchema,
  ListActiveModelResourceTypesRequestSchema,
  ListRelationshipsRequestSchema,
  ListActiveModelResourceTypesResponseSchema,
  ListRelationshipsResponseSchema,
  ModelActionSchema,
  ModelAllowedTargetSchema,
  ModelRelationSchema,
  RelationshipSchema,
  RelationshipTargetSchema,
  RelationshipTargetType as ProtoRelationshipTargetType,
  RelationshipTupleSchema,
  ResourceSchema,
  SetActiveModelRequestSchema,
  SetActiveModelResponseSchema,
  SetAuthorizationStateRequestSchema,
  SetAuthorizationStateResponseSchema,
  SourceLayer as ProtoSourceLayer,
  SubjectSchema,
  SubjectSetSchema,
  SubjectSetTypeSchema,
  AuthorizationProvider as AuthorizationProviderService,
  type AddRelationshipRequest as ProtoAddRelationshipRequest,
  type AddRelationshipResponse as ProtoAddRelationshipResponse,
  type AuthorizationModel as ProtoAuthorizationModel,
  type AuthorizationModelResourceType as ProtoAuthorizationModelResourceType,
  type CheckAccessManyRequest as ProtoCheckAccessManyRequest,
  type CheckAccessManyResponse as ProtoCheckAccessManyResponse,
  type CheckAccessRequest as ProtoCheckAccessRequest,
  type CheckAccessResponse as ProtoCheckAccessResponse,
  type DeleteRelationshipRequest as ProtoDeleteRelationshipRequest,
  type DeleteRelationshipResponse as ProtoDeleteRelationshipResponse,
  type GetActiveModelRefResponse as ProtoGetActiveModelRefResponse,
  type ListActiveModelResourceTypesRequest as ProtoListActiveModelResourceTypesRequest,
  type ListActiveModelResourceTypesResponse as ProtoListActiveModelResourceTypesResponse,
  type ListRelationshipsRequest as ProtoListRelationshipsRequest,
  type ListRelationshipsResponse as ProtoListRelationshipsResponse,
  type ModelAllowedTarget as ProtoModelAllowedTarget,
  type Relationship as ProtoRelationship,
  type RelationshipFilter as ProtoRelationshipFilter,
  type RelationshipTarget as ProtoRelationshipTarget,
  type RelationshipTuple as ProtoRelationshipTuple,
  type SetActiveModelResponse as ProtoSetActiveModelResponse,
  type SetActiveModelRequest as ProtoSetActiveModelRequest,
  type SetAuthorizationStateResponse as ProtoSetAuthorizationStateResponse,
  type SetAuthorizationStateRequest as ProtoSetAuthorizationStateRequest,
  type SubjectSet as ProtoSubjectSet,
} from "./internal/gen/v1/authorization_pb.ts";
import { errorMessage, type MaybePromise, type Request } from "./api.ts";
import { ProviderBase, type ProviderBaseOptions } from "./provider.ts";
import {
  dateFromTimestamp,
  jsonObjectFromStruct,
  structFromObject,
  timestampFromDate,
  type JsonObjectInput,
} from "./protocol.ts";
import {
  createHostServiceGrpcTransport,
  hostServiceMetadataInterceptors,
  parseHostServiceTarget,
  requireHostServiceTarget,
} from "./host-service.ts";
import { hostInvocationContext } from "./invocation-context.ts";

export const RelationshipTargetType = {
  UNSPECIFIED: ProtoRelationshipTargetType.UNSPECIFIED,
  SUBJECT: ProtoRelationshipTargetType.SUBJECT,
  RESOURCE: ProtoRelationshipTargetType.RESOURCE,
  SUBJECT_SET: ProtoRelationshipTargetType.SUBJECT_SET,
} as const;
export type RelationshipTargetType =
  (typeof RelationshipTargetType)[keyof typeof RelationshipTargetType];

export const SourceLayer = {
  UNSPECIFIED: ProtoSourceLayer.UNSPECIFIED,
  STATIC_CONFIG: ProtoSourceLayer.STATIC_CONFIG,
  RUNTIME: ProtoSourceLayer.RUNTIME,
} as const;
export type SourceLayer = (typeof SourceLayer)[keyof typeof SourceLayer];

export const DefaultAccessPolicy = {
  DENY: ProtoDefaultAccessPolicy.DENY,
  ALLOW: ProtoDefaultAccessPolicy.ALLOW,
} as const;
export type DefaultAccessPolicy =
  (typeof DefaultAccessPolicy)[keyof typeof DefaultAccessPolicy];

export interface AuthorizationSubject {
  type?: string | undefined;
  id?: string | undefined;
  properties?: JsonObjectInput | undefined;
}

export interface AuthorizationAction {
  name?: string | undefined;
  properties?: JsonObjectInput | undefined;
}

export interface AuthorizationResource {
  type?: string | undefined;
  id?: string | undefined;
  properties?: JsonObjectInput | undefined;
}

export interface CheckAccessRequest {
  subject?: AuthorizationSubject | undefined;
  action?: AuthorizationAction | undefined;
  resource?: AuthorizationResource | undefined;
}

export interface CheckAccessResponse {
  allowed?: boolean | undefined;
  modelId?: string | undefined;
}

export interface CheckAccessManyRequest {
  requests?: readonly CheckAccessRequest[] | undefined;
}

export interface CheckAccessManyResponse {
  decisions?: readonly CheckAccessResponse[] | undefined;
}

export interface RelationshipFilter {
  target?: RelationshipTarget | undefined;
  relation?: string | undefined;
  resource?: AuthorizationResource | undefined;
  targetType?: RelationshipTargetType | undefined;
  targetEntityType?: string | undefined;
  resourceType?: string | undefined;
  sourceLayer?: SourceLayer | undefined;
}

export interface ListRelationshipsRequest {
  filter?: RelationshipFilter | undefined;
  pageSize?: number | undefined;
  pageToken?: string | undefined;
}

export interface ListRelationshipsResponse {
  relationships?: readonly Relationship[] | undefined;
  nextPageToken?: string | undefined;
}

export interface AddRelationshipRequest {
  relationship?: Relationship | undefined;
}

export interface AddRelationshipResponse {
  relationship?: Relationship | undefined;
}

export interface DeleteRelationshipRequest {
  relationshipTuple?: RelationshipTuple | undefined;
}

export interface DeleteRelationshipResponse {}

export interface SetAuthorizationStateRequest {
  model?: AuthorizationModel | undefined;
  relationships?: readonly Relationship[] | undefined;
}

export interface SetAuthorizationStateResponse {
  activeModel?: AuthorizationModelRef | undefined;
}

export interface Relationship {
  tuple?: RelationshipTuple | undefined;
  properties?: JsonObjectInput | undefined;
  sourceLayer?: SourceLayer | undefined;
}

export interface RelationshipTuple {
  target?: RelationshipTarget | undefined;
  relation?: string | undefined;
  resource?: AuthorizationResource | undefined;
}

export interface RelationshipTarget {
  subject?: AuthorizationSubject | undefined;
  resource?: AuthorizationResource | undefined;
  subjectSet?: SubjectSet | undefined;
}

export interface SubjectSet {
  resource?: AuthorizationResource | undefined;
  relation?: string | undefined;
}

export interface AuthorizationModel {
  id?: string | undefined;
  version?: string | undefined;
  resourceTypes?: readonly AuthorizationModelResourceType[] | undefined;
}

export interface AuthorizationModelResourceType {
  name?: string | undefined;
  relations?: readonly ModelRelation[] | undefined;
  actions?: readonly ModelAction[] | undefined;
  sourceLayer?: SourceLayer | undefined;
  defaultAccessPolicy?: DefaultAccessPolicy | undefined;
}

export interface ModelRelation {
  name?: string | undefined;
  allowedTargets?: readonly ModelAllowedTarget[] | undefined;
}

export interface ModelAction {
  name?: string | undefined;
  relations?: readonly string[] | undefined;
}

export interface ModelAllowedTarget {
  subjectType?: string | undefined;
  resourceType?: string | undefined;
  subjectSetType?: SubjectSetType | undefined;
}

export interface SubjectSetType {
  resourceType?: string | undefined;
  relation?: string | undefined;
}

export interface AuthorizationModelRef {
  id?: string | undefined;
  version?: string | undefined;
  createdAt?: Date | undefined;
}

export interface GetActiveModelRefResponse {
  model?: AuthorizationModelRef | undefined;
}

export interface SetActiveModelRequest {
  model?: AuthorizationModel | undefined;
}

export interface SetActiveModelResponse {
  model?: AuthorizationModelRef | undefined;
}

export interface AuthorizationModelResourceTypeFilter {
  name?: string | undefined;
  sourceLayer?: SourceLayer | undefined;
}

export interface ListActiveModelResourceTypesRequest {
  filter?: AuthorizationModelResourceTypeFilter | undefined;
  pageSize?: number | undefined;
  pageToken?: string | undefined;
}

export interface ListActiveModelResourceTypesResponse {
  resourceTypes?: readonly AuthorizationModelResourceType[] | undefined;
  nextPageToken?: string | undefined;
  modelId?: string | undefined;
}

export interface Authorization {
  checkAccess(request: CheckAccessRequest): Promise<CheckAccessResponse>;
  checkAccessMany(
    request: CheckAccessManyRequest,
  ): Promise<CheckAccessManyResponse>;
  listRelationships(
    request: ListRelationshipsRequest,
  ): Promise<ListRelationshipsResponse>;
  addRelationship(
    request: AddRelationshipRequest,
  ): Promise<AddRelationshipResponse>;
  deleteRelationship(
    request: DeleteRelationshipRequest,
  ): Promise<DeleteRelationshipResponse>;
  setAuthorizationState(
    request: SetAuthorizationStateRequest,
  ): Promise<SetAuthorizationStateResponse>;
  getActiveModelRef(): Promise<GetActiveModelRefResponse>;
  setActiveModel(
    request: SetActiveModelRequest,
  ): Promise<SetActiveModelResponse>;
  listActiveModelResourceTypes(
    request: ListActiveModelResourceTypesRequest,
  ): Promise<ListActiveModelResourceTypesResponse>;
}

class AuthorizationImpl implements Authorization {
  private readonly client: Client<typeof AuthorizationProviderService>;

  constructor(target?: string, relayToken?: string, invocationToken = "") {
    const host = target
      ? { target, token: relayToken?.trim() ?? "" }
      : requireHostServiceTarget("authorization");
    const transport = createHostServiceGrpcTransport(
      parseHostServiceTarget("authorization", host.target),
      hostServiceMetadataInterceptors(host.token, "", invocationToken),
    );
    this.client = createClient(AuthorizationProviderService, transport);
  }

  async checkAccess(request: CheckAccessRequest): Promise<CheckAccessResponse> {
    return checkAccessResponseFromProto(
      await this.client.checkAccess(checkAccessRequestToProto(request)),
    );
  }

  async checkAccessMany(
    request: CheckAccessManyRequest,
  ): Promise<CheckAccessManyResponse> {
    return checkAccessManyResponseFromProto(
      await this.client.checkAccessMany(checkAccessManyRequestToProto(request)),
    );
  }

  async listRelationships(
    request: ListRelationshipsRequest,
  ): Promise<ListRelationshipsResponse> {
    return listRelationshipsResponseFromProto(
      await this.client.listRelationships(listRelationshipsRequestToProto(request)),
    );
  }

  async addRelationship(
    request: AddRelationshipRequest,
  ): Promise<AddRelationshipResponse> {
    return addRelationshipResponseFromProto(
      await this.client.addRelationship(addRelationshipRequestToProto(request)),
    );
  }

  async deleteRelationship(
    request: DeleteRelationshipRequest,
  ): Promise<DeleteRelationshipResponse> {
    return deleteRelationshipResponseFromProto(
      await this.client.deleteRelationship(deleteRelationshipRequestToProto(request)),
    );
  }

  async setAuthorizationState(
    request: SetAuthorizationStateRequest,
  ): Promise<SetAuthorizationStateResponse> {
    return setAuthorizationStateResponseFromProto(
      await this.client.setAuthorizationState(
        setAuthorizationStateRequestToProto(request),
      ),
    );
  }

  async getActiveModelRef(): Promise<GetActiveModelRefResponse> {
    return getActiveModelRefResponseFromProto(
      await this.client.getActiveModelRef(create(EmptySchema)),
    );
  }

  async setActiveModel(
    request: SetActiveModelRequest,
  ): Promise<SetActiveModelResponse> {
    return setActiveModelResponseFromProto(
      await this.client.setActiveModel(setActiveModelRequestToProto(request)),
    );
  }

  async listActiveModelResourceTypes(
    request: ListActiveModelResourceTypesRequest,
  ): Promise<ListActiveModelResourceTypesResponse> {
    return listActiveModelResourceTypesResponseFromProto(
      await this.client.listActiveModelResourceTypes(
        listActiveModelResourceTypesRequestToProto(request),
      ),
    );
  }
}

let sharedAuthorization:
  | { target: string; token: string; client: Authorization }
  | undefined;

export function Authorization(): Authorization;
export function Authorization(request: Request): Authorization;
export function Authorization(invocationToken: string): Authorization;
export function Authorization(requestOrToken?: Request | string): Authorization {
  if (requestOrToken !== undefined) {
    const { invocationToken } = hostInvocationContext(requestOrToken);
    return new AuthorizationImpl(undefined, undefined, invocationToken);
  }
  const { target, token } = requireHostServiceTarget("authorization");
  if (
    sharedAuthorization &&
    sharedAuthorization.target === target &&
    sharedAuthorization.token === token
  ) {
    return sharedAuthorization.client;
  }

  const client = new AuthorizationImpl(target, token);
  sharedAuthorization = { target, token, client };
  return client;
}

export interface AuthorizationProviderOptions extends ProviderBaseOptions {
  checkAccess: (request: CheckAccessRequest) => MaybePromise<CheckAccessResponse>;
  checkAccessMany: (
    request: CheckAccessManyRequest,
  ) => MaybePromise<CheckAccessManyResponse>;
  listRelationships: (
    request: ListRelationshipsRequest,
  ) => MaybePromise<ListRelationshipsResponse>;
  addRelationship: (
    request: AddRelationshipRequest,
  ) => MaybePromise<AddRelationshipResponse>;
  deleteRelationship: (
    request: DeleteRelationshipRequest,
  ) => MaybePromise<DeleteRelationshipResponse | void>;
  setAuthorizationState: (
    request: SetAuthorizationStateRequest,
  ) => MaybePromise<SetAuthorizationStateResponse>;
  getActiveModelRef: () => MaybePromise<GetActiveModelRefResponse>;
  setActiveModel: (
    request: SetActiveModelRequest,
  ) => MaybePromise<SetActiveModelResponse>;
  listActiveModelResourceTypes: (
    request: ListActiveModelResourceTypesRequest,
  ) => MaybePromise<ListActiveModelResourceTypesResponse>;
}

export class AuthorizationProvider extends ProviderBase {
  readonly kind = "authorization" as const;

  private readonly handlers: AuthorizationProviderOptions;

  constructor(options: AuthorizationProviderOptions) {
    super(options);
    this.handlers = options;
  }

  checkAccess(request: CheckAccessRequest): Promise<CheckAccessResponse> {
    return Promise.resolve(this.handlers.checkAccess(request));
  }

  checkAccessMany(
    request: CheckAccessManyRequest,
  ): Promise<CheckAccessManyResponse> {
    return Promise.resolve(this.handlers.checkAccessMany(request));
  }

  listRelationships(
    request: ListRelationshipsRequest,
  ): Promise<ListRelationshipsResponse> {
    return Promise.resolve(this.handlers.listRelationships(request));
  }

  addRelationship(
    request: AddRelationshipRequest,
  ): Promise<AddRelationshipResponse> {
    return Promise.resolve(this.handlers.addRelationship(request));
  }

  deleteRelationship(
    request: DeleteRelationshipRequest,
  ): Promise<DeleteRelationshipResponse | void> {
    return Promise.resolve(this.handlers.deleteRelationship(request));
  }

  setAuthorizationState(
    request: SetAuthorizationStateRequest,
  ): Promise<SetAuthorizationStateResponse> {
    return Promise.resolve(this.handlers.setAuthorizationState(request));
  }

  getActiveModelRef(): Promise<GetActiveModelRefResponse> {
    return Promise.resolve(this.handlers.getActiveModelRef());
  }

  setActiveModel(
    request: SetActiveModelRequest,
  ): Promise<SetActiveModelResponse> {
    return Promise.resolve(this.handlers.setActiveModel(request));
  }

  listActiveModelResourceTypes(
    request: ListActiveModelResourceTypesRequest,
  ): Promise<ListActiveModelResourceTypesResponse> {
    return Promise.resolve(this.handlers.listActiveModelResourceTypes(request));
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
      "checkAccess" in value &&
      "checkAccessMany" in value &&
      "listRelationships" in value &&
      "addRelationship" in value &&
      "deleteRelationship" in value &&
      "setAuthorizationState" in value &&
      "getActiveModelRef" in value &&
      "setActiveModel" in value &&
      "listActiveModelResourceTypes" in value)
  );
}

export function createAuthorizationProviderService(
  provider: AuthorizationProvider,
): Partial<ServiceImpl<typeof AuthorizationProviderService>> {
  return {
    async checkAccess(request) {
      try {
        return checkAccessResponseToProto(
          await provider.checkAccess(checkAccessRequestFromProto(request)),
        );
      } catch (error) {
        throw authorizationRuntimeError("check access", error);
      }
    },
    async checkAccessMany(request) {
      try {
        return checkAccessManyResponseToProto(
          await provider.checkAccessMany(checkAccessManyRequestFromProto(request)),
        );
      } catch (error) {
        throw authorizationRuntimeError("check access many", error);
      }
    },
    async listRelationships(request) {
      try {
        return listRelationshipsResponseToProto(
          await provider.listRelationships(listRelationshipsRequestFromProto(request)),
        );
      } catch (error) {
        throw authorizationRuntimeError("list relationships", error);
      }
    },
    async addRelationship(request) {
      try {
        return addRelationshipResponseToProto(
          await provider.addRelationship(addRelationshipRequestFromProto(request)),
        );
      } catch (error) {
        throw authorizationRuntimeError("add relationship", error);
      }
    },
    async deleteRelationship(request) {
      try {
        await provider.deleteRelationship(deleteRelationshipRequestFromProto(request));
        return create(DeleteRelationshipResponseSchema);
      } catch (error) {
        throw authorizationRuntimeError("delete relationship", error);
      }
    },
    async setAuthorizationState(request) {
      try {
        return setAuthorizationStateResponseToProto(
          await provider.setAuthorizationState(
            setAuthorizationStateRequestFromProto(request),
          ),
        );
      } catch (error) {
        throw authorizationRuntimeError("set authorization state", error);
      }
    },
    async getActiveModelRef() {
      try {
        return getActiveModelRefResponseToProto(await provider.getActiveModelRef());
      } catch (error) {
        throw authorizationRuntimeError("get active model ref", error);
      }
    },
    async setActiveModel(request) {
      try {
        return setActiveModelResponseToProto(
          await provider.setActiveModel(setActiveModelRequestFromProto(request)),
        );
      } catch (error) {
        throw authorizationRuntimeError("set active model", error);
      }
    },
    async listActiveModelResourceTypes(request) {
      try {
        return listActiveModelResourceTypesResponseToProto(
          await provider.listActiveModelResourceTypes(
            listActiveModelResourceTypesRequestFromProto(request),
          ),
        );
      } catch (error) {
        throw authorizationRuntimeError("list active model resource types", error);
      }
    },
  };
}

function checkAccessRequestFromProto(
  value: ProtoCheckAccessRequest,
): CheckAccessRequest {
  return {
    subject: subjectFromProto(value.subject),
    action: value.action
      ? {
          name: value.action.name,
          properties: jsonObjectFromStruct(value.action.properties),
        }
      : undefined,
    resource: resourceFromProto(value.resource),
  };
}

function checkAccessRequestToProto(value: CheckAccessRequest) {
  return create(CheckAccessRequestSchema, {
    subject: subjectToProto(value.subject),
    action: value.action
      ? create(ActionSchema, {
          name: value.action.name ?? "",
          properties: value.action.properties === undefined
            ? undefined
            : structFromObject(value.action.properties),
        })
      : undefined,
    resource: resourceToProto(value.resource),
  });
}

function checkAccessResponseFromProto(
  value: ProtoCheckAccessResponse,
): CheckAccessResponse {
  return {
    allowed: value.allowed,
    modelId: value.modelId,
  };
}

function checkAccessResponseToProto(value: CheckAccessResponse | undefined) {
  if (!value) {
    throw new ConnectError(
      "authorization provider returned nil response",
      Code.Internal,
    );
  }
  return create(CheckAccessResponseSchema, {
    allowed: value.allowed ?? false,
    modelId: value.modelId ?? "",
  });
}

function checkAccessManyRequestFromProto(
  value: ProtoCheckAccessManyRequest,
): CheckAccessManyRequest {
  return {
    requests: value.requests.map(checkAccessRequestFromProto),
  };
}

function checkAccessManyRequestToProto(value: CheckAccessManyRequest) {
  return create(CheckAccessManyRequestSchema, {
    requests: (value.requests ?? []).map(checkAccessRequestToProto),
  });
}

function checkAccessManyResponseFromProto(
  value: ProtoCheckAccessManyResponse,
): CheckAccessManyResponse {
  return {
    decisions: value.decisions.map(checkAccessResponseFromProto),
  };
}

function checkAccessManyResponseToProto(
  value: CheckAccessManyResponse | undefined,
) {
  if (!value) {
    throw new ConnectError(
      "authorization provider returned nil response",
      Code.Internal,
    );
  }
  return create(CheckAccessManyResponseSchema, {
    decisions: (value.decisions ?? []).map(checkAccessResponseToProto),
  });
}

function listRelationshipsRequestFromProto(
  value: ProtoListRelationshipsRequest,
): ListRelationshipsRequest {
  return {
    filter: relationshipFilterFromProto(value.filter),
    pageSize: value.pageSize,
    pageToken: value.pageToken,
  };
}

function listRelationshipsRequestToProto(
  value: ListRelationshipsRequest,
) {
  return create(ListRelationshipsRequestSchema, {
    filter: relationshipFilterToProto(value.filter),
    pageSize: value.pageSize ?? 0,
    pageToken: value.pageToken ?? "",
  });
}

function listRelationshipsResponseFromProto(
  value: ProtoListRelationshipsResponse,
): ListRelationshipsResponse {
  return {
    relationships: value.relationships.map(relationshipFromProtoRequired),
    nextPageToken: value.nextPageToken,
  };
}

function listRelationshipsResponseToProto(
  value: ListRelationshipsResponse | undefined,
) {
  if (!value) {
    throw new ConnectError(
      "authorization provider returned nil response",
      Code.Internal,
    );
  }
  return create(ListRelationshipsResponseSchema, {
    relationships: (value.relationships ?? []).map(relationshipToProtoRequired),
    nextPageToken: value.nextPageToken ?? "",
  });
}

function addRelationshipRequestFromProto(
  value: ProtoAddRelationshipRequest,
): AddRelationshipRequest {
  return {
    relationship: relationshipFromProto(value.relationship),
  };
}

function addRelationshipRequestToProto(value: AddRelationshipRequest) {
  return create(AddRelationshipRequestSchema, {
    relationship: relationshipToProto(value.relationship),
  });
}

function addRelationshipResponseFromProto(
  value: ProtoAddRelationshipResponse,
): AddRelationshipResponse {
  return {
    relationship: relationshipFromProto(value.relationship),
  };
}

function addRelationshipResponseToProto(
  value: AddRelationshipResponse | undefined,
) {
  if (!value) {
    throw new ConnectError(
      "authorization provider returned nil response",
      Code.Internal,
    );
  }
  return create(AddRelationshipResponseSchema, {
    relationship: value.relationship
      ? relationshipToProto(value.relationship)
      : undefined,
  });
}

function deleteRelationshipRequestFromProto(
  value: ProtoDeleteRelationshipRequest,
): DeleteRelationshipRequest {
  return {
    relationshipTuple: relationshipTupleFromProto(value.relationshipTuple),
  };
}

function deleteRelationshipRequestToProto(value: DeleteRelationshipRequest) {
  return create(DeleteRelationshipRequestSchema, {
    relationshipTuple: relationshipTupleToProto(value.relationshipTuple),
  });
}

function deleteRelationshipResponseFromProto(
  _value: ProtoDeleteRelationshipResponse,
): DeleteRelationshipResponse {
  return {};
}

function setAuthorizationStateRequestFromProto(
  value: ProtoSetAuthorizationStateRequest,
): SetAuthorizationStateRequest {
  return {
    model: authorizationModelFromProto(value.model),
    relationships: value.relationships.map(relationshipFromProtoRequired),
  };
}

function setAuthorizationStateRequestToProto(
  value: SetAuthorizationStateRequest,
) {
  return create(SetAuthorizationStateRequestSchema, {
    model: authorizationModelToProto(value.model),
    relationships: (value.relationships ?? []).map(relationshipToProtoRequired),
  });
}

function setAuthorizationStateResponseFromProto(
  value: ProtoSetAuthorizationStateResponse,
): SetAuthorizationStateResponse {
  return {
    activeModel: authorizationModelRefFromProto(value.activeModel),
  };
}

function setAuthorizationStateResponseToProto(
  value: SetAuthorizationStateResponse | undefined,
) {
  if (!value) {
    throw new ConnectError(
      "authorization provider returned nil response",
      Code.Internal,
    );
  }
  return create(SetAuthorizationStateResponseSchema, {
    activeModel: value.activeModel
      ? authorizationModelRefToProto(value.activeModel)
      : undefined,
  });
}

function getActiveModelRefResponseFromProto(
  value: ProtoGetActiveModelRefResponse,
): GetActiveModelRefResponse {
  return {
    model: authorizationModelRefFromProto(value.model),
  };
}

function getActiveModelRefResponseToProto(
  value: GetActiveModelRefResponse | undefined,
) {
  if (!value) {
    throw new ConnectError(
      "authorization provider returned nil response",
      Code.Internal,
    );
  }
  return create(GetActiveModelRefResponseSchema, {
    model: value.model ? authorizationModelRefToProto(value.model) : undefined,
  });
}

function setActiveModelRequestFromProto(
  value: ProtoSetActiveModelRequest,
): SetActiveModelRequest {
  return {
    model: authorizationModelFromProto(value.model),
  };
}

function setActiveModelRequestToProto(value: SetActiveModelRequest) {
  return create(SetActiveModelRequestSchema, {
    model: authorizationModelToProto(value.model),
  });
}

function setActiveModelResponseFromProto(
  value: ProtoSetActiveModelResponse,
): SetActiveModelResponse {
  return {
    model: authorizationModelRefFromProto(value.model),
  };
}

function setActiveModelResponseToProto(value: SetActiveModelResponse | undefined) {
  if (!value) {
    throw new ConnectError(
      "authorization provider returned nil response",
      Code.Internal,
    );
  }
  return create(SetActiveModelResponseSchema, {
    model: value.model ? authorizationModelRefToProto(value.model) : undefined,
  });
}

function listActiveModelResourceTypesRequestFromProto(
  value: ProtoListActiveModelResourceTypesRequest,
): ListActiveModelResourceTypesRequest {
  return {
    filter: value.filter
      ? {
          name: value.filter.name,
          sourceLayer: value.filter.sourceLayer,
        }
      : undefined,
    pageSize: value.pageSize,
    pageToken: value.pageToken,
  };
}

function listActiveModelResourceTypesRequestToProto(
  value: ListActiveModelResourceTypesRequest,
) {
  return create(ListActiveModelResourceTypesRequestSchema, {
    filter: value.filter
      ? create(AuthorizationModelResourceTypeFilterSchema, {
          name: value.filter.name ?? "",
          sourceLayer: value.filter.sourceLayer ?? SourceLayer.UNSPECIFIED,
        })
      : undefined,
    pageSize: value.pageSize ?? 0,
    pageToken: value.pageToken ?? "",
  });
}

function listActiveModelResourceTypesResponseFromProto(
  value: ProtoListActiveModelResourceTypesResponse,
): ListActiveModelResourceTypesResponse {
  return {
    resourceTypes: value.resourceTypes.map(authorizationModelResourceTypeFromProto),
    nextPageToken: value.nextPageToken,
    modelId: value.modelId,
  };
}

function listActiveModelResourceTypesResponseToProto(
  value: ListActiveModelResourceTypesResponse | undefined,
) {
  if (!value) {
    throw new ConnectError(
      "authorization provider returned nil response",
      Code.Internal,
    );
  }
  return create(ListActiveModelResourceTypesResponseSchema, {
    resourceTypes: (value.resourceTypes ?? []).map(
      authorizationModelResourceTypeToProto,
    ),
    nextPageToken: value.nextPageToken ?? "",
    modelId: value.modelId ?? "",
  });
}

function subjectFromProto(value: ProtoCheckAccessRequest["subject"]): AuthorizationSubject | undefined {
  if (!value) {
    return undefined;
  }
  return {
    type: value.type,
    id: value.id,
    properties: jsonObjectFromStruct(value.properties),
  };
}

function subjectToProto(value: AuthorizationSubject | undefined) {
  if (!value) {
    return undefined;
  }
  return create(SubjectSchema, {
    type: value.type ?? "",
    id: value.id ?? "",
    properties: value.properties === undefined
      ? undefined
      : structFromObject(value.properties),
  });
}

function resourceFromProto(value: ProtoRelationshipFilter["resource"]): AuthorizationResource | undefined {
  if (!value) {
    return undefined;
  }
  return {
    type: value.type,
    id: value.id,
    properties: jsonObjectFromStruct(value.properties),
  };
}

function resourceToProto(value: AuthorizationResource | undefined) {
  if (!value) {
    return undefined;
  }
  return create(ResourceSchema, {
    type: value.type ?? "",
    id: value.id ?? "",
    properties: value.properties === undefined
      ? undefined
      : structFromObject(value.properties),
  });
}

function relationshipFilterFromProto(
  value: ProtoRelationshipFilter | undefined,
): RelationshipFilter | undefined {
  if (!value) {
    return undefined;
  }
  return {
    target: relationshipTargetFromProto(value.target),
    relation: value.relation,
    resource: resourceFromProto(value.resource),
    targetType: value.targetType,
    targetEntityType: value.targetEntityType,
    resourceType: value.resourceType,
    sourceLayer: value.sourceLayer,
  };
}

function relationshipFilterToProto(value: RelationshipFilter | undefined) {
  if (!value) {
    return undefined;
  }
  return {
    target: relationshipTargetToProto(value.target),
    relation: value.relation ?? "",
    resource: resourceToProto(value.resource),
    targetType: value.targetType ?? RelationshipTargetType.UNSPECIFIED,
    targetEntityType: value.targetEntityType ?? "",
    resourceType: value.resourceType ?? "",
    sourceLayer: value.sourceLayer ?? SourceLayer.UNSPECIFIED,
  };
}

function relationshipFromProto(
  value: ProtoRelationship | undefined,
): Relationship | undefined {
  if (!value) {
    return undefined;
  }
  return {
    tuple: relationshipTupleFromProto(value.tuple),
    properties: jsonObjectFromStruct(value.properties),
    sourceLayer: value.sourceLayer,
  };
}

function relationshipFromProtoRequired(value: ProtoRelationship): Relationship {
  return relationshipFromProto(value)!;
}

function relationshipToProto(value: Relationship | undefined) {
  if (!value) {
    return undefined;
  }
  return create(RelationshipSchema, {
    tuple: relationshipTupleToProto(value.tuple),
    properties: value.properties === undefined
      ? undefined
      : structFromObject(value.properties),
    sourceLayer: value.sourceLayer ?? SourceLayer.UNSPECIFIED,
  });
}

function relationshipToProtoRequired(value: Relationship) {
  return relationshipToProto(value)!;
}

function relationshipTupleFromProto(
  value: ProtoRelationshipTuple | undefined,
): RelationshipTuple | undefined {
  if (!value) {
    return undefined;
  }
  return {
    target: relationshipTargetFromProto(value.target),
    relation: value.relation,
    resource: resourceFromProto(value.resource),
  };
}

function relationshipTupleToProto(value: RelationshipTuple | undefined) {
  if (!value) {
    return undefined;
  }
  return create(RelationshipTupleSchema, {
    target: relationshipTargetToProto(value.target),
    relation: value.relation ?? "",
    resource: resourceToProto(value.resource),
  });
}

function relationshipTargetFromProto(
  value: ProtoRelationshipTarget | undefined,
): RelationshipTarget | undefined {
  if (!value) {
    return undefined;
  }
  switch (value.kind.case) {
    case "subject":
      return { subject: subjectFromProto(value.kind.value) };
    case "resource":
      return { resource: resourceFromProto(value.kind.value) };
    case "subjectSet":
      return { subjectSet: subjectSetFromProto(value.kind.value) };
    default:
      return {};
  }
}

function relationshipTargetToProto(value: RelationshipTarget | undefined) {
  if (!value) {
    return undefined;
  }
  if (value.subject) {
    return create(RelationshipTargetSchema, {
      kind: { case: "subject", value: subjectToProto(value.subject)! },
    });
  }
  if (value.resource) {
    return create(RelationshipTargetSchema, {
      kind: { case: "resource", value: resourceToProto(value.resource)! },
    });
  }
  if (value.subjectSet) {
    return create(RelationshipTargetSchema, {
      kind: { case: "subjectSet", value: subjectSetToProto(value.subjectSet) },
    });
  }
  return create(RelationshipTargetSchema);
}

function subjectSetFromProto(value: ProtoSubjectSet | undefined): SubjectSet | undefined {
  if (!value) {
    return undefined;
  }
  return {
    resource: resourceFromProto(value.resource),
    relation: value.relation,
  };
}

function subjectSetToProto(value: SubjectSet) {
  return create(SubjectSetSchema, {
    resource: resourceToProto(value.resource),
    relation: value.relation ?? "",
  });
}

function authorizationModelFromProto(
  value: ProtoAuthorizationModel | undefined,
): AuthorizationModel | undefined {
  if (!value) {
    return undefined;
  }
  return {
    id: value.id,
    version: value.version,
    resourceTypes: value.resourceTypes.map(authorizationModelResourceTypeFromProto),
  };
}

function authorizationModelToProto(value: AuthorizationModel | undefined) {
  if (!value) {
    return undefined;
  }
  return create(AuthorizationModelSchema, {
    id: value.id ?? "",
    version: value.version ?? "",
    resourceTypes: (value.resourceTypes ?? []).map(
      authorizationModelResourceTypeToProto,
    ),
  });
}

function authorizationModelResourceTypeFromProto(
  value: ProtoAuthorizationModelResourceType,
): AuthorizationModelResourceType {
  return {
    name: value.name,
    relations: value.relations.map((relation) => ({
      name: relation.name,
      allowedTargets: relation.allowedTargets.map(modelAllowedTargetFromProto),
    })),
    actions: value.actions.map((action) => ({
      name: action.name,
      relations: [...action.relations],
    })),
    sourceLayer: value.sourceLayer,
    defaultAccessPolicy: value.defaultAccessPolicy,
  };
}

function authorizationModelResourceTypeToProto(
  value: AuthorizationModelResourceType,
) {
  return create(AuthorizationModelResourceTypeSchema, {
    name: value.name ?? "",
    relations: (value.relations ?? []).map((relation) =>
      create(ModelRelationSchema, {
        name: relation.name ?? "",
        allowedTargets: (relation.allowedTargets ?? []).map(
          modelAllowedTargetToProto,
        ),
      })
    ),
    actions: (value.actions ?? []).map((action) =>
      create(ModelActionSchema, {
        name: action.name ?? "",
        relations: [...(action.relations ?? [])],
      })
    ),
    sourceLayer: value.sourceLayer ?? SourceLayer.UNSPECIFIED,
    defaultAccessPolicy: value.defaultAccessPolicy ?? DefaultAccessPolicy.DENY,
  });
}

function modelAllowedTargetFromProto(
  value: ProtoModelAllowedTarget,
): ModelAllowedTarget {
  switch (value.kind.case) {
    case "subjectType":
      return { subjectType: value.kind.value };
    case "resourceType":
      return { resourceType: value.kind.value };
    case "subjectSetType":
      return {
        subjectSetType: {
          resourceType: value.kind.value.resourceType,
          relation: value.kind.value.relation,
        },
      };
    default:
      return {};
  }
}

function modelAllowedTargetToProto(value: ModelAllowedTarget) {
  if (value.subjectType !== undefined) {
    return create(ModelAllowedTargetSchema, {
      kind: { case: "subjectType", value: value.subjectType },
    });
  }
  if (value.resourceType !== undefined) {
    return create(ModelAllowedTargetSchema, {
      kind: { case: "resourceType", value: value.resourceType },
    });
  }
  if (value.subjectSetType !== undefined) {
    return create(ModelAllowedTargetSchema, {
      kind: {
        case: "subjectSetType",
        value: create(SubjectSetTypeSchema, {
          resourceType: value.subjectSetType.resourceType ?? "",
          relation: value.subjectSetType.relation ?? "",
        }),
      },
    });
  }
  return create(ModelAllowedTargetSchema);
}

function authorizationModelRefFromProto(
  value: ProtoGetActiveModelRefResponse["model"] | undefined,
): AuthorizationModelRef | undefined {
  if (!value) {
    return undefined;
  }
  return {
    id: value.id,
    version: value.version,
    createdAt: value.createdAt ? dateFromTimestamp(value.createdAt) : undefined,
  };
}

function authorizationModelRefToProto(value: AuthorizationModelRef) {
  return create(AuthorizationModelRefSchema, {
    id: value.id ?? "",
    version: value.version ?? "",
    createdAt: value.createdAt ? timestampFromDate(value.createdAt) : undefined,
  });
}

function authorizationRuntimeError(label: string, error: unknown): ConnectError {
  if (error instanceof ConnectError) {
    return error;
  }
  return new ConnectError(`${label}: ${errorMessage(error)}`, Code.Unknown);
}
