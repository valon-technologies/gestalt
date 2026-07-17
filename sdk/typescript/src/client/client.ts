/**
 * Server-side external Gestalt client options and factory.
 *
 * @module client/client
 */

import { normalizeAddress } from "./address.ts";
import { authToProvider, type Auth } from "./auth.ts";
import { AppClient } from "./generated/app_client.ts";
import { createRestUnaryTransport } from "./rest_transport.ts";
import type { UnaryTransport } from "./generated/unary_transport.ts";

export {
  bearer,
  unauthenticated,
  type Auth,
  type BearerAuth,
  type Unauthenticated,
} from "./auth.ts";

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
  /** Optional fetch override for testing or custom runtimes. */
  fetch?: typeof fetch;
}

export interface GestaltClient {
  readonly app: AppClient;
  close(): Promise<void>;
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
  const baseUrl = normalizeAddress(options.address);
  const auth = authToProvider(options.auth);
  let transport: UnaryTransport;
  let close: () => Promise<void> = async () => {};

  if (options.transport.kind === "rest") {
    transport = createRestUnaryTransport({
      baseUrl,
      auth,
      ...(options.fetch !== undefined ? { fetch: options.fetch } : {}),
    });
  } else if (options.transport.kind === "grpc") {
    const { createGrpcUnaryTransport } = await import("./grpc_transport.ts");
    const grpcTransport = await createGrpcUnaryTransport({
      baseUrl,
      auth,
    });
    transport = grpcTransport;
    close = () => grpcTransport.close();
  } else {
    const unknownTransport: never = options.transport;
    throw new Error(`unsupported transport: ${JSON.stringify(unknownTransport)}`);
  }

  return {
    app: new AppClient(transport),
    close,
  };
}
