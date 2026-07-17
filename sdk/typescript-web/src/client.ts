/**
 * Browser REST Gestalt client options and factory.
 *
 * @module client
 */

import { AppClient } from "./client/generated/app_client.ts";
import type { UnaryTransport } from "./client/generated/unary_transport.ts";

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
}

export interface GestaltClient {
  readonly address: string;
  readonly app: AppClient;
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

export async function createGestaltClient(
  options: ClientOptions,
): Promise<GestaltClient> {
  const address = resolveAddress(options.address);
  return {
    address,
    app: new AppClient(await createWebTransport(options)),
  };
}

function resolveAddress(address: string | URL | undefined): string {
  if (address === undefined) {
    if (typeof globalThis.location === "undefined") {
      throw new Error("createGestaltClient requires an explicit address");
    }
    return globalThis.location.origin;
  }
  return typeof address === "string" ? address : address.toString();
}

async function createWebTransport(
  _options: ClientOptions,
): Promise<UnaryTransport> {
  throw new Error(
    "createGestaltClient REST transport is not implemented yet; use SDK-4",
  );
}
