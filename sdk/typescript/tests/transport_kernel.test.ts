import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import type { JsonValue } from "@bufbuild/protobuf";
import { describe, expect, it } from "bun:test";

import {
  OperationResultSchema,
  type OperationResult,
} from "../src/internal/gen/v1/app_pb.ts";
import { PUBLIC_METHODS, type PublicMethod } from "../src/client/generated/methods.ts";
import { parseGatewayError } from "../src/client/generated/gateway_error.ts";
import {
  buildRestBody,
  buildRestPath,
  buildRestQuery,
} from "../src/client/generated/rest_request_mapping.ts";
import {
  decodeRestResponse,
  type RawRestResponse,
} from "../src/client/generated/transport_kernel.ts";
import { GestaltError, GestaltErrorCode } from "../src/rpc_support.ts";

const fixturePath = join(
  dirname(fileURLToPath(import.meta.url)),
  "../../testdata/public_conformance/transport_kernel_cases.json",
);

type FixtureCase = {
  id: string;
  request?: Record<string, JsonValue>;
  overrideQueryFields?: Array<{ name: string; jsonName: string }>;
  overrideHttpBody?: string;
  expectPrepare?: {
    verb: string;
    path: string;
    query: [string, string][];
    body: unknown;
  };
  expectDecode?: {
    status: number;
    bodyBase64: string;
    headerKeys?: string[];
    headerValueCounts?: Record<string, number>;
  };
  expectGatewayError?: { code: number; message?: string };
  expectGestaltError?: { code: number };
  expectPrepareError?: { code: number };
  rawResponse?: {
    status: number;
    bodyText?: string;
    bodyBase64?: string;
    headers?: [string, string][];
  };
};

const cases = JSON.parse(readFileSync(fixturePath, "utf8")) as FixtureCase[];

function rawBody(raw: NonNullable<FixtureCase["rawResponse"]>): Uint8Array {
  if (raw.bodyText !== undefined) {
    return new TextEncoder().encode(raw.bodyText);
  }
  if (raw.bodyBase64 !== undefined) {
    return Uint8Array.from(atob(raw.bodyBase64), (ch) => ch.charCodeAt(0));
  }
  return new Uint8Array();
}

function rawResponse(raw: NonNullable<FixtureCase["rawResponse"]>): RawRestResponse {
  return {
    status: raw.status,
    headers: raw.headers ?? [],
    body: rawBody(raw),
  };
}

function methodForPrepareCase(tc: FixtureCase): PublicMethod {
  const method = structuredClone(PUBLIC_METHODS.app.invoke) as PublicMethod;
  if (tc.overrideQueryFields?.length) {
    const http = method.http!;
    method.http = {
      ...http,
      queryFields: [
        ...http.queryFields,
        ...tc.overrideQueryFields.map((field) => ({
          name: field.name,
          jsonName: field.jsonName,
        })),
      ],
    };
  }
  if (tc.overrideHttpBody !== undefined) {
    const http = method.http!;
    method.http = {
      ...http,
      body: tc.overrideHttpBody,
    };
  }
  return method;
}

describe("transport kernel fixture", () => {
  it("covers every fixture case", () => {
    for (const tc of cases) {
      const covered =
        tc.expectPrepare !== undefined ||
        tc.expectDecode !== undefined ||
        tc.expectGatewayError !== undefined ||
        tc.expectGestaltError !== undefined ||
        tc.expectPrepareError !== undefined;
      expect(covered).toBe(true);
    }
  });

  for (const tc of cases) {
    if (tc.expectPrepare && tc.request) {
      it(`prepare: ${tc.id}`, () => {
        const method = methodForPrepareCase(tc);
        const http = method.http!;
        const request = tc.request as Record<string, JsonValue>;
        const path = buildRestPath(http, request);
        const query = [...buildRestQuery(http, request).entries()];
        const body = buildRestBody(method, request);
        expect(http.verb).toBe(tc.expectPrepare!.verb as typeof http.verb);
        expect(path).toBe(tc.expectPrepare!.path);
        expect(query).toEqual(tc.expectPrepare!.query);
        if (tc.expectPrepare!.body === null) {
          expect(body).toBeUndefined();
        } else {
          expect(body).toEqual(tc.expectPrepare!.body as JsonValue);
        }
      });
    }

    if (tc.expectPrepareError && tc.request) {
      it(`prepare error: ${tc.id}`, () => {
        const method = methodForPrepareCase(tc);
        const http = method.http!;
        const request = tc.request as Record<string, JsonValue>;
        expect(() => buildRestQuery(http, request)).toThrow(GestaltError);
        try {
          buildRestQuery(http, request);
        } catch (error) {
          expect(error).toBeInstanceOf(GestaltError);
          expect((error as GestaltError).code).toBe(tc.expectPrepareError!.code);
        }
      });
    }

    if (tc.expectDecode && tc.rawResponse) {
      it(`decode: ${tc.id}`, () => {
        const response = decodeRestResponse(
          PUBLIC_METHODS.app.invoke,
          OperationResultSchema,
          rawResponse(tc.rawResponse!),
        ) as OperationResult;
        expect(response.status).toBe(tc.expectDecode!.status);
        expect(response.body).toEqual(
          Uint8Array.from(atob(tc.expectDecode!.bodyBase64), (ch) => ch.charCodeAt(0)),
        );
        for (const key of tc.expectDecode!.headerKeys ?? []) {
          expect(response.headers[key]).toBeDefined();
        }
        for (const [key, count] of Object.entries(
          tc.expectDecode!.headerValueCounts ?? {},
        )) {
          expect(response.headers[key]?.values.length).toBe(count);
        }
      });
    }

    if (tc.expectGatewayError && tc.rawResponse) {
      it(`gateway error: ${tc.id}`, () => {
        const err = parseGatewayError(
          tc.rawResponse!.status,
          new TextDecoder().decode(rawBody(tc.rawResponse!)),
        );
        expect(err.code).toBe(tc.expectGatewayError!.code);
        if (tc.expectGatewayError!.message) {
          expect(err.message).toBe(tc.expectGatewayError!.message);
        }
      });
    }

    if (tc.expectGestaltError && tc.rawResponse) {
      it(`gestalt error: ${tc.id}`, () => {
        expect(() =>
          decodeRestResponse(
            PUBLIC_METHODS.app.invoke,
            OperationResultSchema,
            rawResponse(tc.rawResponse!),
          ),
        ).toThrow(expect.objectContaining({ code: tc.expectGestaltError!.code }));
      });
    }
  }
});
