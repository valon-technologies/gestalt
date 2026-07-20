/**
 * Fetch-based REST transport for the public Gestalt API (/api/v2).
 */

import { create, toJson } from "@bufbuild/protobuf";
import type { DescMessage, JsonValue, Message } from "@bufbuild/protobuf";

import type { AuthProvider } from "./auth.ts";
import { parseGatewayError } from "./generated/gateway_error.ts";
import { PUBLIC_METHODS, type PublicMethod } from "./generated/methods.ts";
import {
  buildRestBody,
  buildRestPath,
  buildRestQuery,
} from "./generated/rest_request_mapping.ts";
import {
  parseSuccessJson,
  parseSuccessMessage,
  raceWithAbort,
  readResponseBodyText,
  resolveEffectiveAbortSignal,
  throwIfAborted,
  toTransportGestaltError,
} from "./generated/transport_support.ts";
import type {
  PublicUnaryCallOptions,
  Transport,
} from "./generated/transport.ts";

export interface RestTransportOptions {
  baseUrl: string;
  auth: AuthProvider;
  fetch?: typeof fetch;
}

export function createRestTransport(
  options: RestTransportOptions,
): Transport {
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
          options.auth.getAuthorization(),
          signal,
          { removeListener: callOptions?.timeoutMs === undefined },
        );
        if (authorization) {
          headers.set("Authorization", authorization);
        }

        const init: RequestInit = {
          method: http.verb,
          headers,
          credentials: "omit",
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

    serverStream<Output extends Message>(
      method: PublicMethod,
      request: Message,
      requestSchema: DescMessage,
      responseSchema: DescMessage,
      callOptions?: PublicUnaryCallOptions,
    ): AsyncIterable<Output> {
      const http = method.http ?? resolveInvokeHTTPBinding(method);
      if (!http) {
        throw new Error(`method ${method.grpcPath} has no HTTP binding`);
      }
      const requestJson = toJson(requestSchema, request) as Record<string, JsonValue>;
      const path = buildRestPath(http, requestJson);
      const url = new URL(path, baseUrl);
      buildRestQuery(http, requestJson).forEach((value, key) => {
        url.searchParams.append(key, value);
      });

      const signal = resolveEffectiveAbortSignal(callOptions);

      return (async function* serverStreamFrames(): AsyncIterable<Output> {
        let authorization: string | undefined;
        try {
          throwIfAborted(signal);
          authorization = await raceWithAbort(
            options.auth.getAuthorization(),
            signal,
            { removeListener: callOptions?.timeoutMs === undefined },
          );
        } catch (error) {
          throw toTransportGestaltError(callOptions, error, signal);
        }

        const headers = new Headers({
          Accept: "application/json",
          "Content-Type": "application/json",
        });
        if (authorization) {
          headers.set("Authorization", authorization);
        }

        const init: RequestInit = {
          method: http.verb,
          headers,
          credentials: "omit",
          ...(signal !== undefined ? { signal } : {}),
        };
        const body = buildRestBody(
          { ...method, http } as PublicMethod,
          requestJson,
        );
        if (body !== undefined) {
          init.body = JSON.stringify(body);
        }

        let response: Response;
        try {
          response = await fetchImpl(url, init);
        } catch (error) {
          throw toTransportGestaltError(callOptions, error, signal);
        }

        if (!response.ok) {
          const bodyText = await readResponseBodyText(response);
          try {
            throw parseGatewayError(response.status, bodyText);
          } catch (error) {
            throw toTransportGestaltError(callOptions, error, signal);
          }
        }

        const mediaType =
          response.headers.get("Content-Type") ?? "application/octet-stream";
        const responseHeaders: Record<string, { values: string[] }> = {};
        response.headers.forEach((value, key) => {
          responseHeaders[key] = { values: [value] };
        });
        yield create(responseSchema, {
          value: {
            case: "metadata",
            value: { status: response.status, mediaType, headers: responseHeaders },
          },
        }) as unknown as Output;

        if (response.body === null) {
          return;
        }
        const reader = response.body.getReader();
        try {
          while (true) {
            throwIfAborted(signal);
            const { done, value: chunk } = await reader.read();
            if (done) {
              break;
            }
            if (chunk !== undefined && chunk.length > 0) {
              yield create(responseSchema, {
                value: { case: "data", value: chunk },
              }) as unknown as Output;
            }
          }
        } catch (error) {
          if (isAbortLike(error, signal)) {
            throw toTransportGestaltError(callOptions, error, signal);
          }
          throw error;
        } finally {
          try {
            reader.releaseLock();
          } catch {
          }
        }
      })();
    },
  };
}

function resolveInvokeHTTPBinding(method: PublicMethod): PublicMethod["http"] {
  if (method.service === "App" && method.method === "InvokeStream") {
    return PUBLIC_METHODS.app.invoke.http;
  }
  return undefined;
}

function isAbortLike(error: unknown, signal: AbortSignal | undefined): boolean {
  if (signal !== undefined && signal.aborted && error instanceof Error) {
    return true;
  }
  return error instanceof DOMException && error.name === "AbortError";
}
