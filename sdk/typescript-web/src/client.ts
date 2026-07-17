/**
 * Browser REST Gestalt client options and factory.
 *
 * @module client
 */

import { normalizeAddress } from "./address.ts";
import {
  AgentClient,
  AppClient,
  AuthorizationClient,
  IdentityClient,
  WorkflowClient,
} from "./client/generated/app_client.ts";
import { createRestUnaryTransport } from "./rest_transport.ts";

export interface SessionAuth {
  readonly kind: "session";
}

export interface BearerAuth {
  readonly kind: "bearer";
  readonly token: () => string | Promise<string>;
}

export interface Unauthenticated {
  readonly kind: "unauthenticated";
}

export type Auth = SessionAuth | BearerAuth | Unauthenticated;

export interface ClientOptions {
  address?: string | URL;
  auth: Auth;
  /** Optional fetch override for testing. */
  fetch?: typeof fetch;
}

export interface GestaltClient {
  readonly address: string;
  readonly app: AppClient;
  readonly agent: AgentClient;
  readonly authorization: AuthorizationClient;
  readonly identity: IdentityClient;
  readonly workflow: WorkflowClient;
}

export function session(): SessionAuth {
  return { kind: "session" };
}

export function bearer(token: () => string | Promise<string>): BearerAuth {
  return { kind: "bearer", token };
}

export function unauthenticated(): Unauthenticated {
  return { kind: "unauthenticated" };
}

export function createGestaltClient(options: ClientOptions): GestaltClient {
  const address = resolveAddress(options.address);
  const transport = createRestUnaryTransport({
    baseUrl: address,
    auth: options.auth,
    ...(options.fetch !== undefined ? { fetch: options.fetch } : {}),
  });
  return {
    address,
    app: new AppClient(transport),
    agent: new AgentClient(transport),
    authorization: new AuthorizationClient(transport),
    identity: new IdentityClient(transport),
    workflow: new WorkflowClient(transport),
  };
}

function resolveAddress(address: string | URL | undefined): string {
  if (address === undefined) {
    if (typeof globalThis.location === "undefined") {
      throw new Error("createGestaltClient requires an explicit address");
    }
    return globalThis.location.origin;
  }
  return normalizeAddress(address);
}
