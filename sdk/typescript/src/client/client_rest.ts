/**
 * REST public Gestalt client factory.
 */

import { normalizeClientAddress } from "./address.ts";
import type { GestaltRestClient, GestaltRestClientOptions } from "./types.ts";
import { createRestTransport } from "./rest_transport.ts";
import type { PublicTransport } from "./transport.ts";
import {
  PublicAgentClient as PublicAgentRestClient,
  PublicAppClient as PublicAppRestClient,
  PublicAuthorizationClient as PublicAuthorizationRestClient,
  PublicIdentityClient as PublicIdentityRestClient,
  PublicWorkflowClient as PublicWorkflowRestClient,
} from "./generated/rest_clients.ts";

export async function createGestaltClient(
  options: GestaltRestClientOptions,
): Promise<GestaltRestClient> {
  const transport = createRestTransport({
    baseUrl: normalizeClientAddress(options.address),
    auth: options.auth,
    ...(options.fetch !== undefined ? { fetch: options.fetch } : {}),
    ...(options.credentials !== undefined
      ? { credentials: options.credentials }
      : {}),
  });
  return createRestClients(transport);
}

function createRestClients(transport: PublicTransport): GestaltRestClient {
  return {
    app: new PublicAppRestClient(transport),
    agent: new PublicAgentRestClient(transport),
    workflow: new PublicWorkflowRestClient(transport),
    identity: new PublicIdentityRestClient(transport),
    authorization: new PublicAuthorizationRestClient(transport),
  };
}

export type { GestaltRestClient, GestaltRestClientOptions } from "./types.ts";
