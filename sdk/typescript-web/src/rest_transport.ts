/**
 * Browser fetch-based REST transport for the public Gestalt API (/api/v2).
 */

import { toJson } from "@bufbuild/protobuf";
import type { DescMessage, JsonValue, Message } from "@bufbuild/protobuf";

import type { Auth } from "./client.ts";
import { parseGatewayError } from "./client/generated/gateway_error.ts";
import type { PublicMethod } from "./client/generated/methods.ts";
import {
  buildRestBody,
  buildRestPath,
  buildRestQuery,
} from "./client/generated/rest_request_mapping.ts";
import {
  parseSuccessJson,
  parseSuccessMessage,
  raceWithAbort,
  readResponseBodyText,
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
      if (!method.http) {
        throw new Error(`method ${method.grpcPath} has no HTTP binding`);
      }
      const http = method.http;
      const requestJson = toJson(requestSchema, request) as Record<string, JsonValue>;
      const path = buildRestPath(http, requestJson);
      const url = new URL(path, baseUrl);
      buildRestQuery(http, requestJson).forEach((value, key) => {
        url.searchParams.append(key, value);
      });

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
          method: http.verb,
          headers,
          credentials: credentialsForAuth(options.auth),
          ...(signal !== undefined ? { signal } : {}),
        };
        const body = buildRestBody(method, requestJson);
        if (body !== undefined) {
          init.body = JSON.stringify(body);
        }

        const response = await fetchImpl(url, init);
        const bodyText = await readResponseBodyText(response);

        if (response.ok) {
          return parseSuccessMessage<Output>(
            responseSchema,
            parseSuccessJson(bodyText),
          );
        }

        throw parseGatewayError(response.status, bodyText);
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
