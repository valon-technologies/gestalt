import { readFileSync } from "node:fs";
import { join } from "node:path";

import { create } from "@bufbuild/protobuf";
import type { JsonObject } from "@bufbuild/protobuf";
import { expect, test } from "bun:test";

import { bearer, createGestaltClient } from "../src/client.ts";
import { GestaltErrorCode } from "../src/client/runtime/rpc_support.ts";
import type { PublicAppInvokeRequest } from "../src/client/generated/types.ts";
import { createRestTransport } from "../src/rest_transport.ts";
import {
  AppInvokeRequestSchema,
  OperationResultSchema,
} from "../src/client/runtime/internal/gen/v1/app_pb.ts";
import { PUBLIC_METHODS } from "../src/client/generated/methods.ts";

const fixtureRoot = join(import.meta.dir, "..", "..", "testdata", "public_conformance");
const fixtureCases = JSON.parse(
  readFileSync(join(fixtureRoot, "client_cases.json"), "utf8"),
) as Array<{
  id: string;
  publicRequest: PublicAppInvokeRequest;
  response: {
    operationResult?: { bodyBase64: string };
    gestaltError?: { code: number; message: string };
  };
  expect: {
    result?: unknown;
    gestaltError?: { code: number; message: string };
  };
}>;

const invokeSuccessBody =
  fixtureCases.find((entry) => entry.id === "invoke_success")?.response
    .operationResult?.bodyBase64 ?? "";

test("REST transport runs shared public conformance cases", async () => {
  const success = fixtureCases.find((entry) => entry.id === "invoke_success");
  if (!success) throw new Error("missing invoke_success case");

  let calls = 0;
  const client = createGestaltClient({
    address: "https://gestalt.test",
    auth: bearer(() => "token"),
    fetch: (async () => {
      calls += 1;
      return new Response(
        JSON.stringify({
          status: 200,
          body: invokeSuccessBody,
          headers: {},
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }) as unknown as typeof fetch,
  });

  await expect(client.app.invoke(success.publicRequest)).resolves.toEqual(
    success.expect.result,
  );
  expect(calls).toBe(1);
});

test("REST transport surfaces platform errors from gateway responses", async () => {
  const platformCase = fixtureCases.find((entry) => entry.id === "platform_error");
  if (!platformCase) throw new Error("missing platform_error case");
  const gestaltError = platformCase.response.gestaltError;
  if (!gestaltError) throw new Error("missing gestalt error fixture");

  const transport = createRestTransport({
    baseUrl: "https://gestalt.test/",
    auth: bearer(() => "token"),
    fetch: (async () =>
      new Response(
        JSON.stringify({
          code: gestaltError.code,
          message: gestaltError.message,
        }),
        { status: 401, headers: { "Content-Type": "application/json" } },
      )) as unknown as typeof fetch,
  });

  const request = create(AppInvokeRequestSchema, {
    app: platformCase.publicRequest.app ?? "",
    operation: platformCase.publicRequest.operation ?? "",
    params: (platformCase.publicRequest.params ?? {}) as unknown as JsonObject,
    connection: "",
    instance: "",
    idempotencyKey: "",
    credentialMode: "",
  });

  await expect(
    transport.unary(
      PUBLIC_METHODS.app.invoke,
      request,
      AppInvokeRequestSchema,
      OperationResultSchema,
    ),
  ).rejects.toMatchObject({
    name: "GestaltError",
    code: GestaltErrorCode.Unauthenticated,
    message: platformCase.expect.gestaltError?.message,
  });
});
