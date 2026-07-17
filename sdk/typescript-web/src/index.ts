/**
 * Browser REST client, session auth, and mount helpers for Gestalt web apps.
 *
 * @example
 * ```ts
 * import {
 *   createGestaltClient,
 *   session,
 *   bearer,
 *   unauthenticated,
 * } from "@valon-technologies/gestalt-web";
 * ```
 *
 * @packageDocumentation
 */

export {
  bearer,
  createGestaltClient,
  session,
  unauthenticated,
  type Auth,
  type BearerAuth,
  type ClientOptions,
  type GestaltClient,
  type SessionAuth,
  type Unauthenticated,
} from "./client.ts";

export { AppClient } from "./client/generated/app_client.ts";
export { PUBLIC_METHODS } from "./client/generated/methods.ts";
export type { PublicMethod, PublicMethodHttp } from "./client/generated/methods.ts";
export type {
  PublicAppInvokeGraphQLRequest,
  PublicAppInvokeRequest,
} from "./client/generated/types.ts";
export type { UnaryTransport } from "./client/generated/unary_transport.ts";
