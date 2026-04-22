import { connect } from "node:net";

import type { MessageInitShape } from "@bufbuild/protobuf";
import { createClient, type Client } from "@connectrpc/connect";
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
 * Environment variable containing the Unix socket path for the read-only host
 * authorization client exposed to plugins.
 */
export const ENV_AUTHORIZATION_SOCKET = "GESTALT_AUTHORIZATION_SOCKET";

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

/**
 * Read-only client for the host-configured authorization provider.
 */
export class AuthorizationClient {
  private readonly client: Client<typeof AuthorizationProviderService>;

  constructor() {
    const socketPath = process.env[ENV_AUTHORIZATION_SOCKET];
    if (!socketPath) {
      throw new Error(
        `authorization: ${ENV_AUTHORIZATION_SOCKET} is not set`,
      );
    }

    const transport = createGrpcTransport({
      baseUrl: "http://localhost",
      nodeOptions: {
        createConnection: () => connect(socketPath),
      },
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
  return new AuthorizationClient();
}
