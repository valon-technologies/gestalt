/**
 * Fetch-based REST transport for the public Gestalt API (/api/v2).
 */

import { create, fromJson, toJson } from "@bufbuild/protobuf";
import type { DescMessage, JsonValue, Message } from "@bufbuild/protobuf";

import { StringListSchema, type StringList } from "../internal/gen/v1/app_pb.ts";

import { type AuthProvider, restCredentials } from "./auth.ts";
import { GestaltError, GestaltErrorCode, parseGatewayError } from "./errors.ts";
import type { PublicMethod } from "./generated/methods.ts";
import {
  buildRestBody,
  buildRestPath,
  buildRestQuery,
} from "./rest_mapping.ts";
import type { PublicTransport } from "./transport.ts";

export const GESTALT_RESPONSE_KIND_HEADER = "X-Gestalt-Response-Kind";
export const GESTALT_RESPONSE_KIND_OPERATION_RESULT = "operation-result";

export interface RestTransportOptions {
  baseUrl: string;
  auth: AuthProvider;
  fetch?: typeof fetch;
  credentials?: RequestCredentials;
}

export function createRestTransport(
  options: RestTransportOptions,
): PublicTransport {
  const baseUrl = options.baseUrl;
  const fetchImpl = options.fetch ?? fetch;
  const credentials = options.credentials ?? restCredentials(options.auth);

  return {
    async unary<Res extends Message>(
      method: PublicMethod,
      request: Message,
      requestSchema: DescMessage,
      responseSchema: DescMessage,
    ): Promise<Res> {
      if (!method.http) {
        throw new Error(`method ${method.grpcPath} has no HTTP binding`);
      }
      const http = method.http;
      const requestJson = toJson(requestSchema, request) as Record<string, JsonValue>;
      const { path, pathFields } = buildRestPath(http.path, requestJson);
      const url = new URL(path, baseUrl);
      if (http.verb === "GET" || http.verb === "DELETE") {
        const query = buildRestQuery(requestJson, pathFields);
        query.forEach((value, key) => {
          url.searchParams.append(key, value);
        });
      }

      const headers = new Headers({
        Accept: "application/json",
        "Content-Type": "application/json",
      });
      const authorization = await options.auth.getAuthorization();
      if (authorization) {
        headers.set("Authorization", authorization);
      }

      const init: RequestInit = {
        method: http.verb,
        headers,
        credentials,
      };
      const body = buildRestBody(http, requestJson, pathFields);
      if (body !== undefined) {
        init.body = JSON.stringify(body);
      }

      let response: Response;
      try {
        response = await fetchImpl(url, init);
      } catch (error) {
        throw new GestaltError(
          GestaltErrorCode.Unavailable,
          error instanceof Error ? error.message : String(error),
        );
      }
      const responseKind = response.headers.get(GESTALT_RESPONSE_KIND_HEADER);
      const bodyBytes = new Uint8Array(await response.arrayBuffer());

      if (responseKind === GESTALT_RESPONSE_KIND_OPERATION_RESULT) {
        return createOperationResultMessage(
          responseSchema,
          response.status,
          bodyBytes,
          response.headers,
        ) as Res;
      }

      const bodyText = new TextDecoder().decode(bodyBytes);

      if (!response.ok) {
        throw parseGatewayError(response.status, bodyText);
      }

      const parsed = bodyText.trim() === "" ? {} : (JSON.parse(bodyText) as JsonValue);
      return fromJson(responseSchema, parsed) as Res;
    },
  };
}

function createOperationResultMessage(
  schema: DescMessage,
  status: number,
  body: Uint8Array,
  headers: Headers,
): Message {
  const headerMap: Record<string, StringList> = {};
  headers.forEach((value, key) => {
    const lower = key.toLowerCase();
    if (lower === GESTALT_RESPONSE_KIND_HEADER.toLowerCase()) {
      return;
    }
    const existing = headerMap[key];
    if (existing) {
      existing.values.push(value);
    } else {
      headerMap[key] = create(StringListSchema, { values: [value] });
    }
  });
  return create(schema, {
    status,
    body,
    headers: headerMap,
  });
}
