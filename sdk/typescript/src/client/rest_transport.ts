/**
 * Fetch-based REST transport for the public Gestalt API (/api/v2).
 */

import type { DescMessage, Message } from "@bufbuild/protobuf";

import type { AuthProvider } from "./auth.ts";
import type { PublicMethod } from "./generated/methods.ts";
import {
  decodeRestResponse,
  prepareRestRequest,
} from "./generated/transport_kernel.ts";
import {
  raceWithAbort,
  resolveEffectiveAbortSignal,
  throwIfAborted,
  toTransportGestaltError,
} from "./generated/transport_support.ts";
import type {
  PublicUnaryCallOptions,
  UnaryTransport,
} from "./generated/unary_transport.ts";

export interface RestUnaryTransportOptions {
  baseUrl: string;
  auth: AuthProvider;
  fetch?: typeof fetch;
}

function responseHeaderPairs(
  response: Response,
): readonly (readonly [string, string])[] {
  const pairs: [string, string][] = [];
  const setCookies =
    typeof response.headers.getSetCookie === "function"
      ? response.headers.getSetCookie()
      : [];
  response.headers.forEach((value, key) => {
    if (key.toLowerCase() === "set-cookie" && setCookies.length > 0) {
      return;
    }
    pairs.push([key, value]);
  });
  for (const cookie of setCookies) {
    pairs.push(["set-cookie", cookie]);
  }
  return pairs;
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
      const signal = resolveEffectiveAbortSignal(callOptions);

      try {
        const prepared = prepareRestRequest(method, requestSchema, request);
        const url = new URL(prepared.path, baseUrl);
        for (const [key, value] of prepared.query) {
          url.searchParams.append(key, value);
        }

        throwIfAborted(signal);

        const headers = new Headers({
          Accept: "application/json",
          "Content-Type": "application/json",
        });
        const authorization = await raceWithAbort(
          options.auth.getAuthorization(),
          signal,
          { removeListener: callOptions?.timeoutMs === undefined },
        );
        if (authorization) {
          headers.set("Authorization", authorization);
        }

        const init: RequestInit = {
          method: prepared.verb,
          headers,
          credentials: "omit",
          ...(signal !== undefined ? { signal } : {}),
        };
        if (prepared.body !== undefined) {
          init.body = JSON.stringify(prepared.body);
        }

        const response = await fetchImpl(url, init);
        const body = new Uint8Array(await response.arrayBuffer());

        return decodeRestResponse<Output>(method, responseSchema, {
          status: response.status,
          headers: responseHeaderPairs(response),
          body,
        });
      } catch (error) {
        throw toTransportGestaltError(callOptions, error, signal);
      }
    },
  };
}
