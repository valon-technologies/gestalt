/**
 * Shared public Gestalt client types.
 */

import type { AuthProvider } from "./auth.ts";
import type {
  PublicAgentClient,
  PublicAppClient,
  PublicAuthorizationClient,
  PublicExternalCredentialsClient,
  PublicIdentityClient,
  PublicIndexedDBClient,
  PublicWorkflowClient,
} from "./generated/grpc_clients.ts";
import type {
  PublicAgentClient as PublicAgentRestClient,
  PublicAppClient as PublicAppRestClient,
  PublicAuthorizationClient as PublicAuthorizationRestClient,
  PublicIdentityClient as PublicIdentityRestClient,
  PublicWorkflowClient as PublicWorkflowRestClient,
} from "./generated/rest_clients.ts";

export interface RestTransport {
  readonly kind: "rest";
}

export interface GrpcTransport {
  readonly kind: "grpc";
}

export type GestaltTransport = RestTransport | GrpcTransport;

export interface GestaltClientOptionsBase {
  address?: string | URL;
  auth: AuthProvider;
}

export interface GestaltRestClientOptions extends GestaltClientOptionsBase {
  transport: RestTransport;
  fetch?: typeof fetch;
  credentials?: RequestCredentials;
}

export interface GestaltGrpcClientOptions extends GestaltClientOptionsBase {
  transport: GrpcTransport;
  address: string | URL;
}

export type GestaltClientOptions =
  | GestaltRestClientOptions
  | GestaltGrpcClientOptions;

export interface GestaltRestClient {
  app: PublicAppRestClient;
  agent: PublicAgentRestClient;
  workflow: PublicWorkflowRestClient;
  identity: PublicIdentityRestClient;
  authorization: PublicAuthorizationRestClient;
}

export interface GestaltGrpcClient {
  app: PublicAppClient;
  agent: PublicAgentClient;
  workflow: PublicWorkflowClient;
  identity: PublicIdentityClient;
  authorization: PublicAuthorizationClient;
  indexedDB: PublicIndexedDBClient;
  externalCredentials: PublicExternalCredentialsClient;
}

export type GestaltClient = GestaltRestClient | GestaltGrpcClient;
