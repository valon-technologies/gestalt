/**
 * Browser fetch-based REST transport for the public Gestalt API (/api/v2).
 */

import type { DescMessage, Message } from "@bufbuild/protobuf";

import type { Auth } from "./client.ts";
import type { PublicMethod } from "./client/generated/methods.ts";
import {
  decodeRestResponse,
  prepareRestRequest,
} from "./client/generated/transport_kernel.ts";
import {
  raceWithAbort,
  resolveEffectiveAbortSignal,
  throwIfAborted,
  toTransportGestaltError,
} from "./client/generated/transport_support.ts";
import type {
  PublicUnaryCallOptions,
  UnaryTransport,
} from "./client/generated/unary_transport.ts";

export interface RestUnaryTransportOptions {
  baseUrl: string;
  auth: Auth;
  fetch?: typeof fetch;
}

export function createRestUnaryTransport(
  options: RestUnaryTransportOptions,
): UnaryTransport {
  const baseUrl = options.baseUrl;
  const fetchImpl = options.fetch ?? fetch;

  return {
    async unary<Output extends Message>(
      method: PublicMethod,
      request: Message,
      requestSchema: DescMessage,
      responseSchema: DescMessage,
      callOptions?: PublicUnaryCallOptions,
    ): Promise<Output> {
      const prepared = prepareRestRequest(method, requestSchema, request);
      const url = new URL(prepared.path, baseUrl);
      for (const [key, value] of prepared.query) {
        url.searchParams.append(key, value);
      }

      const signal = resolveEffectiveAbortSignal(callOptions);

      try {
        throwIfAborted(signal);

        const headers = new Headers({
          Accept: "application/json",
          "Content-Type": "application/json",
        });
        const authorization = await raceWithAbort(
          resolveAuthorization(options.auth),
          signal,
          { removeListener: callOptions?.timeoutMs === undefined },
        );
        if (authorization) {
          headers.set("Authorization", authorization);
        }

        const init: RequestInit = {
          method: prepared.verb,
          headers,
          credentials: credentialsForAuth(options.auth),
          ...(signal !== undefined ? { signal } : {}),
        };
        if (prepared.body !== undefined) {
          init.body = JSON.stringify(prepared.body);
        }

        const response = await fetchImpl(url, init);
        const body = new Uint8Array(await response.arrayBuffer());
        const headerEntries: Array<[string, string]> = [];
        response.headers.forEach((value, key) => {
          headerEntries.push([key, value]);
        });

        return decodeRestResponse<Output>(method, responseSchema, {
          status: response.status,
          headers: headerEntries,
          body,
        });
      } catch (error) {
        throw toTransportGestaltError(callOptions, error, signal);
      }
    },
  };
}

async function resolveAuthorization(auth: Auth): Promise<string | undefined> {
  if (auth.kind === "bearer") {
    const value = (await auth.token()).trim();
    return value ? `Bearer ${value}` : undefined;
  }
  return undefined;
}

function credentialsForAuth(auth: Auth): RequestCredentials {
  if (auth.kind === "session") {
    return "include";
  }
  return "omit";
}
