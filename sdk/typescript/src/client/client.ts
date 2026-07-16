/**
 * Server-side external Gestalt client options and factory.
 *
 * @module client/client
 */

import { AppClient } from "./generated/app_client.ts";
import type { UnaryTransport } from "./generated/unary_transport.ts";

export interface BearerAuth {
  readonly kind: "bearer";
  readonly token: () => string | Promise<string>;
}

export interface Unauthenticated {
  readonly kind: "unauthenticated";
}

export type Auth = BearerAuth | Unauthenticated;

export interface RestTransport {
  readonly kind: "rest";
}

export interface GrpcTransport {
  readonly kind: "grpc";
}

export interface ClientOptions {
  address: string | URL;
  transport: RestTransport | GrpcTransport;
  auth: Auth;
}

export interface GestaltClient {
  readonly app: AppClient;
}

export function bearer(token: () => string | Promise<string>): BearerAuth {
  return { kind: "bearer", token };
}

export function unauthenticated(): Unauthenticated {
  return { kind: "unauthenticated" };
}

export function rest(): RestTransport {
  return { kind: "rest" };
}

export function grpc(): GrpcTransport {
  return { kind: "grpc" };
}

export async function createGestaltClient(
  options: ClientOptions,
): Promise<GestaltClient> {
  return { app: new AppClient(await createServerTransport(options)) };
}

async function createServerTransport(
  options: ClientOptions,
): Promise<UnaryTransport> {
  void options;
  throw new Error(
    "createGestaltClient transport wiring is not implemented yet; use SDK-5",
  );
}
