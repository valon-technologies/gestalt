import {
  Code,
  ConnectError,
} from "@connectrpc/connect";
import {
  credentials,
  Metadata,
  status as GrpcStatus,
  type ChannelCredentials,
  type ClientUnaryCall,
  type ServiceError,
} from "@grpc/grpc-js";

import {
  AuthorizationProviderClient,
  DefaultAccessPolicy as AuthorizationDefaultAccessPolicy,
  RelationshipTargetType as AuthorizationRelationshipTargetType,
  SourceLayer as AuthorizationSourceLayer,
} from "./internal/ts-proto/v1/authorization.ts";
import {
  ENV_HOST_SERVICE_TOKEN,
  HOST_SERVICE_RELAY_TOKEN_HEADER,
  requireHostServiceTarget,
} from "./host-service.ts";

export interface AuthorizationClient {
  checkAccess(
    request: Authorization.CheckAccessRequest,
  ): Promise<Authorization.CheckAccessResponse>;
  checkAccessMany(
    request: Authorization.CheckAccessManyRequest,
  ): Promise<Authorization.CheckAccessManyResponse>;
  listRelationships(
    request: Authorization.ListRelationshipsRequest,
  ): Promise<Authorization.ListRelationshipsResponse>;
  addRelationship(
    request: Authorization.AddRelationshipRequest,
  ): Promise<Authorization.AddRelationshipResponse>;
  deleteRelationship(
    request: Authorization.DeleteRelationshipRequest,
  ): Promise<Authorization.DeleteRelationshipResponse>;
  setAuthorizationState(
    request: Authorization.SetAuthorizationStateRequest,
  ): Promise<Authorization.SetAuthorizationStateResponse>;
  getActiveModelRef(): Promise<Authorization.GetActiveModelRefResponse>;
  setActiveModel(
    request: Authorization.SetActiveModelRequest,
  ): Promise<Authorization.SetActiveModelResponse>;
  listActiveModelResourceTypes(
    request: Authorization.ListActiveModelResourceTypesRequest,
  ): Promise<Authorization.ListActiveModelResourceTypesResponse>;
}
class AuthorizationImpl implements AuthorizationClient {
  private readonly client: AuthorizationProviderClient;
  private readonly metadata: Metadata;

  constructor(target?: string, relayToken?: string) {
    const host = target
      ? { target, token: relayToken?.trim() ?? "" }
      : requireHostServiceTarget("authorization");
    const endpoint = parseAuthorizationTarget(host.target);
    this.client = new AuthorizationProviderClient(
      endpoint.target,
      endpoint.credentials,
      {
        "grpc.enable_http_proxy": 0,
        "grpc.max_receive_message_length": 64 * 1024 * 1024,
        "grpc.max_send_message_length": 64 * 1024 * 1024,
      },
    );
    this.metadata = authorizationMetadata(host.token);
  }

  async checkAccess(
    request: Authorization.CheckAccessRequest,
  ): Promise<Authorization.CheckAccessResponse> {
    return await this.unary(
      "check access",
      (callback) =>
        this.client.checkAccess(request, this.metadata, callback),
    );
  }

  async checkAccessMany(
    request: Authorization.CheckAccessManyRequest,
  ): Promise<Authorization.CheckAccessManyResponse> {
    return await this.unary(
      "check access many",
      (callback) =>
        this.client.checkAccessMany(request, this.metadata, callback),
    );
  }

  async listRelationships(
    request: Authorization.ListRelationshipsRequest,
  ): Promise<Authorization.ListRelationshipsResponse> {
    return await this.unary(
      "list relationships",
      (callback) =>
        this.client.listRelationships(request, this.metadata, callback),
    );
  }

  async addRelationship(
    request: Authorization.AddRelationshipRequest,
  ): Promise<Authorization.AddRelationshipResponse> {
    return await this.unary(
      "add relationship",
      (callback) =>
        this.client.addRelationship(request, this.metadata, callback),
    );
  }

  async deleteRelationship(
    request: Authorization.DeleteRelationshipRequest,
  ): Promise<Authorization.DeleteRelationshipResponse> {
    return await this.unary(
      "delete relationship",
      (callback) =>
        this.client.deleteRelationship(request, this.metadata, callback),
    );
  }

  async setAuthorizationState(
    request: Authorization.SetAuthorizationStateRequest,
  ): Promise<Authorization.SetAuthorizationStateResponse> {
    return await this.unary(
      "set authorization state",
      (callback) =>
        this.client.setAuthorizationState(request, this.metadata, callback),
    );
  }

  async getActiveModelRef(): Promise<Authorization.GetActiveModelRefResponse> {
    return await this.unary(
      "get active model ref",
      (callback) =>
        this.client.getActiveModelRef({}, this.metadata, callback),
    );
  }

  async setActiveModel(
    request: Authorization.SetActiveModelRequest,
  ): Promise<Authorization.SetActiveModelResponse> {
    return await this.unary(
      "set active model",
      (callback) =>
        this.client.setActiveModel(request, this.metadata, callback),
    );
  }

  async listActiveModelResourceTypes(
    request: Authorization.ListActiveModelResourceTypesRequest,
  ): Promise<Authorization.ListActiveModelResourceTypesResponse> {
    return await this.unary(
      "list active model resource types",
      (callback) =>
        this.client.listActiveModelResourceTypes(request, this.metadata, callback),
    );
  }

  private async unary<Response>(
    label: string,
    call: (
      callback: (error: ServiceError | null, response: Response) => void,
    ) => ClientUnaryCall,
  ): Promise<Response> {
    return await new Promise<Response>((resolve, reject) => {
      call((error, response) => {
        if (error) {
          reject(authorizationGrpcError(label, error));
          return;
        }
        resolve(response);
      });
    });
  }
}

let sharedAuthorization:
  | { target: string; token: string; client: AuthorizationClient }
  | undefined;

export function Authorization(options: Authorization.Options = {}): AuthorizationClient {
  if (options.transport !== undefined && options.transport !== "grpc") {
    throw new Error(`authorization: unsupported transport ${JSON.stringify(options.transport)}`);
  }
  const envRelayToken = process.env[ENV_HOST_SERVICE_TOKEN]?.trim() ?? "";
  const host = options.target
    ? { target: options.target, token: options.relayToken?.trim() ?? envRelayToken }
    : requireHostServiceTarget("authorization");
  const target = host.target;
  const token = options.relayToken?.trim() ?? host.token;
  if (
    (options.transport === undefined || options.transport === "grpc") &&
    options.target === undefined &&
    options.relayToken === undefined &&
    sharedAuthorization &&
    sharedAuthorization.target === target &&
    sharedAuthorization.token === token
  ) {
    return sharedAuthorization.client;
  }

  const client = new AuthorizationImpl(target, token);
  if (options.target === undefined && options.relayToken === undefined) {
    sharedAuthorization = { target, token, client };
  }
  return client;
}

export namespace Authorization {
  export const RelationshipTargetType = AuthorizationRelationshipTargetType;
  export type RelationshipTargetType = AuthorizationRelationshipTargetType;

  export const SourceLayer = AuthorizationSourceLayer;
  export type SourceLayer = AuthorizationSourceLayer;

  export const DefaultAccessPolicy = AuthorizationDefaultAccessPolicy;
  export type DefaultAccessPolicy = AuthorizationDefaultAccessPolicy;

  export type Subject =
    import("./internal/ts-proto/v1/authorization.ts").Subject;
  export type Action =
    import("./internal/ts-proto/v1/authorization.ts").Action;
  export type Resource =
    import("./internal/ts-proto/v1/authorization.ts").Resource;
  export type CheckAccessRequest =
    import("./internal/ts-proto/v1/authorization.ts").CheckAccessRequest;
  export type CheckAccessResponse =
    import("./internal/ts-proto/v1/authorization.ts").CheckAccessResponse;
  export type CheckAccessManyRequest =
    import("./internal/ts-proto/v1/authorization.ts").CheckAccessManyRequest;
  export type CheckAccessManyResponse =
    import("./internal/ts-proto/v1/authorization.ts").CheckAccessManyResponse;
  export type RelationshipFilter =
    import("./internal/ts-proto/v1/authorization.ts").RelationshipFilter;
  export type ListRelationshipsRequest =
    import("./internal/ts-proto/v1/authorization.ts").ListRelationshipsRequest;
  export type ListRelationshipsResponse =
    import("./internal/ts-proto/v1/authorization.ts").ListRelationshipsResponse;
  export type AddRelationshipRequest =
    import("./internal/ts-proto/v1/authorization.ts").AddRelationshipRequest;
  export type AddRelationshipResponse =
    import("./internal/ts-proto/v1/authorization.ts").AddRelationshipResponse;
  export type DeleteRelationshipRequest =
    import("./internal/ts-proto/v1/authorization.ts").DeleteRelationshipRequest;
  export type DeleteRelationshipResponse =
    import("./internal/ts-proto/v1/authorization.ts").DeleteRelationshipResponse;
  export type SetAuthorizationStateRequest =
    import("./internal/ts-proto/v1/authorization.ts").SetAuthorizationStateRequest;
  export type SetAuthorizationStateResponse =
    import("./internal/ts-proto/v1/authorization.ts").SetAuthorizationStateResponse;
  export type Relationship =
    import("./internal/ts-proto/v1/authorization.ts").Relationship;
  export type RelationshipTuple =
    import("./internal/ts-proto/v1/authorization.ts").RelationshipTuple;
  export type RelationshipTarget =
    import("./internal/ts-proto/v1/authorization.ts").RelationshipTarget;
  export type SubjectSet =
    import("./internal/ts-proto/v1/authorization.ts").SubjectSet;
  export type AuthorizationModel =
    import("./internal/ts-proto/v1/authorization.ts").AuthorizationModel;
  export type AuthorizationModelResourceType =
    import("./internal/ts-proto/v1/authorization.ts").AuthorizationModelResourceType;
  export type ModelRelation =
    import("./internal/ts-proto/v1/authorization.ts").ModelRelation;
  export type ModelAction =
    import("./internal/ts-proto/v1/authorization.ts").ModelAction;
  export type ModelAllowedTarget =
    import("./internal/ts-proto/v1/authorization.ts").ModelAllowedTarget;
  export type SubjectSetType =
    import("./internal/ts-proto/v1/authorization.ts").SubjectSetType;
  export type AuthorizationModelRef =
    import("./internal/ts-proto/v1/authorization.ts").AuthorizationModelRef;
  export type GetActiveModelRefResponse =
    import("./internal/ts-proto/v1/authorization.ts").GetActiveModelRefResponse;
  export type SetActiveModelRequest =
    import("./internal/ts-proto/v1/authorization.ts").SetActiveModelRequest;
  export type SetActiveModelResponse =
    import("./internal/ts-proto/v1/authorization.ts").SetActiveModelResponse;
  export type AuthorizationModelResourceTypeFilter =
    import("./internal/ts-proto/v1/authorization.ts").AuthorizationModelResourceTypeFilter;
  export type ListActiveModelResourceTypesRequest =
    import("./internal/ts-proto/v1/authorization.ts").ListActiveModelResourceTypesRequest;
  export type ListActiveModelResourceTypesResponse =
    import("./internal/ts-proto/v1/authorization.ts").ListActiveModelResourceTypesResponse;

  export interface Options {
    transport?: "grpc" | undefined;
    target?: string | undefined;
    relayToken?: string | undefined;
  }
}

type AuthorizationEndpoint = {
  target: string;
  credentials: ChannelCredentials;
};

function parseAuthorizationTarget(rawTarget: string): AuthorizationEndpoint {
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
    return { target: address, credentials: credentials.createInsecure() };
  }
  if (target.startsWith("tls://")) {
    const address = target.slice("tls://".length).trim();
    if (!address) {
      throw new Error(
        `authorization: tls target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { target: address, credentials: credentials.createSsl() };
  }
  if (target.startsWith("unix://")) {
    const socketPath = target.slice("unix://".length).trim();
    if (!socketPath) {
      throw new Error(
        `authorization: unix target ${JSON.stringify(rawTarget)} is missing a socket path`,
      );
    }
    return {
      target: `unix:${socketPath}`,
      credentials: credentials.createInsecure(),
    };
  }
  if (target.includes("://")) {
    const parsed = new URL(target);
    throw new Error(
      `authorization: unsupported target scheme ${JSON.stringify(parsed.protocol.replace(/:$/, ""))}`,
    );
  }
  return {
    target: `unix:${target}`,
    credentials: credentials.createInsecure(),
  };
}

function authorizationMetadata(token: string): Metadata {
  const metadata = new Metadata();
  const normalizedToken = token.trim();
  if (normalizedToken) {
    metadata.set(HOST_SERVICE_RELAY_TOKEN_HEADER, normalizedToken);
  }
  return metadata;
}

function authorizationGrpcError(label: string, error: ServiceError): ConnectError {
  return new ConnectError(
    `${label}: ${error.details || error.message}`,
    grpcStatusToConnectCode(error.code),
  );
}

function grpcStatusToConnectCode(code: GrpcStatus): Code {
  switch (code) {
    case GrpcStatus.CANCELLED:
      return Code.Canceled;
    case GrpcStatus.UNKNOWN:
      return Code.Unknown;
    case GrpcStatus.INVALID_ARGUMENT:
      return Code.InvalidArgument;
    case GrpcStatus.DEADLINE_EXCEEDED:
      return Code.DeadlineExceeded;
    case GrpcStatus.NOT_FOUND:
      return Code.NotFound;
    case GrpcStatus.ALREADY_EXISTS:
      return Code.AlreadyExists;
    case GrpcStatus.PERMISSION_DENIED:
      return Code.PermissionDenied;
    case GrpcStatus.RESOURCE_EXHAUSTED:
      return Code.ResourceExhausted;
    case GrpcStatus.FAILED_PRECONDITION:
      return Code.FailedPrecondition;
    case GrpcStatus.ABORTED:
      return Code.Aborted;
    case GrpcStatus.OUT_OF_RANGE:
      return Code.OutOfRange;
    case GrpcStatus.UNIMPLEMENTED:
      return Code.Unimplemented;
    case GrpcStatus.INTERNAL:
      return Code.Internal;
    case GrpcStatus.UNAVAILABLE:
      return Code.Unavailable;
    case GrpcStatus.DATA_LOSS:
      return Code.DataLoss;
    case GrpcStatus.UNAUTHENTICATED:
      return Code.Unauthenticated;
    default:
      return Code.Unknown;
  }
}
