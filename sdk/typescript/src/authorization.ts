import { connect } from "node:net";

import type { MessageInitShape } from "@bufbuild/protobuf";
import {
  createClient,
  type Client,
  type Interceptor,
} from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  type AccessDecision,
  type ActionSearchResponse,
  type AuthorizationMetadata,
  AuthorizationProvider as AuthorizationProviderService,
  type ReadRelationshipsResponse,
  type ResourceSearchResponse,
  type SubjectSearchResponse,
  AccessEvaluationRequestSchema,
  ActionSearchRequestSchema,
  ReadRelationshipsRequestSchema,
  ResourceSearchRequestSchema,
  SubjectSearchRequestSchema,
} from "../gen/v1/authorization_pb.ts";

/**
 * Environment variable containing the Unix socket path or relay target for the
 * read-only host authorization client exposed to plugins.
 */
export const ENV_AUTHORIZATION_SOCKET = "GESTALT_AUTHORIZATION_SOCKET";
export const ENV_AUTHORIZATION_SOCKET_TOKEN =
  `${ENV_AUTHORIZATION_SOCKET}_TOKEN`;
const AUTHORIZATION_RELAY_TOKEN_HEADER =
  "x-gestalt-host-service-relay-token";

export type AuthorizationEvaluateInput = MessageInitShape<
  typeof AccessEvaluationRequestSchema
>;
export type AuthorizationSearchResourcesInput = MessageInitShape<
  typeof ResourceSearchRequestSchema
>;
export type AuthorizationSearchSubjectsInput = MessageInitShape<
  typeof SubjectSearchRequestSchema
>;
export type AuthorizationSearchActionsInput = MessageInitShape<
  typeof ActionSearchRequestSchema
>;
export type AuthorizationReadRelationshipsInput = MessageInitShape<
  typeof ReadRelationshipsRequestSchema
>;

export type AuthorizationDecisionMessage = AccessDecision;
export type AuthorizationMetadataMessage = AuthorizationMetadata;
export type AuthorizationResourceSearchMessage = ResourceSearchResponse;
export type AuthorizationSubjectSearchMessage = SubjectSearchResponse;
export type AuthorizationActionSearchMessage = ActionSearchResponse;
export type AuthorizationReadRelationshipsMessage = ReadRelationshipsResponse;

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
 * Read-only client for the host-configured authorization provider.
 */
export class AuthorizationClient {
  private readonly client: Client<typeof AuthorizationProviderService>;

  constructor(socketTarget?: string, relayToken = process.env[ENV_AUTHORIZATION_SOCKET_TOKEN]?.trim() ?? "") {
    const resolvedTarget = resolveAuthorizationSocketTarget(socketTarget);
    const transportOptions = authorizationTransportOptions(resolvedTarget);
    const transport = createGrpcTransport({
      ...transportOptions,
      ...(transportOptions.nodeOptions
        ? {
            nodeOptions: {
              createConnection: () => connect(transportOptions.nodeOptions!.path),
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

  async searchActions(
    request: AuthorizationSearchActionsInput,
  ): Promise<AuthorizationActionSearchMessage> {
    return await this.client.searchActions(request);
  }

  async readRelationships(
    request: AuthorizationReadRelationshipsInput,
  ): Promise<AuthorizationReadRelationshipsMessage> {
    return await this.client.readRelationships(request);
  }

  async getMetadata(): Promise<AuthorizationMetadataMessage> {
    return await this.client.getMetadata({});
  }
}

/**
 * Mirrors the Go SDK helper for obtaining the read-only host authorization
 * client inside authored providers.
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

function resolveAuthorizationSocketTarget(socketPath = process.env[ENV_AUTHORIZATION_SOCKET]): string {
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
