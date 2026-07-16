import { create } from "@bufbuild/protobuf";
import { expect, test } from "bun:test";

import {
  AppInvokeRequestSchema,
  OperationResultSchema,
  type OperationResult,
} from "../src/internal/gen/v1/app_pb.ts";
import { GetAgentProviderSessionRequestSchema } from "../src/internal/gen/v1/agent_pb.ts";
import { bearer, session, unauthenticated } from "../src/client/auth.ts";
import { rest } from "../src/client/rest.ts";
import { createGestaltClient } from "../src/client/client_rest.ts";
import {
  GESTALT_RESPONSE_KIND_HEADER,
  GESTALT_RESPONSE_KIND_OPERATION_RESULT,
  createRestTransport,
} from "../src/client/rest_transport.ts";
import { PUBLIC_METHODS } from "../src/client/generated/methods.ts";
import { GestaltErrorCode } from "../src/client/errors.ts";
import { buildRestPath } from "../src/client/rest_mapping.ts";

test("REST transport maps protobuf JSON requests and distinguishes response kinds", async () => {
  const calls: Array<{ method: string; url: string; body: string; authorization?: string; credentials?: RequestCredentials }> = [];

  const transport = createRestTransport({
    baseUrl: "https://gestalt.test/",
    auth: bearer("token-123"),
    fetch: (async (input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({
        method: init?.method ?? "GET",
        url: String(input),
        body: typeof init?.body === "string" ? init.body : "",
        ...(init?.headers instanceof Headers &&
        init.headers.get("Authorization")
          ? { authorization: init.headers.get("Authorization")! }
          : {}),
        ...(init?.credentials !== undefined
          ? { credentials: init.credentials }
          : {}),
      });
      return new Response("teapot", {
        status: 418,
        headers: {
          [GESTALT_RESPONSE_KIND_HEADER]: GESTALT_RESPONSE_KIND_OPERATION_RESULT,
          "X-Example": "rest-v2",
          "Content-Type": "text/plain",
        },
      });
    }) as unknown as typeof fetch,
  });

  const request = create(AppInvokeRequestSchema, {
    app: "example",
    operation: "sync",
    params: { ok: true },
    connection: "",
    instance: "",
    idempotencyKey: "key-1",
    credentialMode: "",
  });

  const response: OperationResult = await transport.unary(
    PUBLIC_METHODS.app.invoke,
    request,
    AppInvokeRequestSchema,
    OperationResultSchema,
  );

  expect(calls).toHaveLength(1);
  expect(calls[0]?.method).toBe("POST");
  expect(calls[0]?.url).toBe(
    "https://gestalt.test/api/v2/app/example/operations/sync",
  );
  expect(calls[0]?.authorization).toBe("Bearer token-123");
  expect(calls[0]?.credentials).toBe("omit");
  expect(JSON.parse(calls[0]?.body ?? "{}")).toEqual({
    params: { ok: true },
    idempotencyKey: "key-1",
  });
  expect(response.status).toBe(418);
  expect(new TextDecoder().decode(response.body)).toBe("teapot");

  const gatewayTransport = createRestTransport({
    baseUrl: "https://gestalt.test/",
    auth: bearer("token"),
    fetch: (async () =>
      new Response(JSON.stringify({ code: 16, message: "unauthorized" }), {
        status: 401,
        headers: { "Content-Type": "application/json" },
      })) as unknown as typeof fetch,
  });

  await expect(
    gatewayTransport.unary(
      PUBLIC_METHODS.app.invoke,
      request,
      AppInvokeRequestSchema,
      OperationResultSchema,
    ),
  ).rejects.toMatchObject({
    name: "GestaltError",
    code: GestaltErrorCode.Unauthenticated,
    message: "unauthorized",
  });
});

test("session and unauthenticated auth control cookie credentials", async () => {
  const seen: RequestCredentials[] = [];
  const recordingFetch = (async (_input: RequestInfo | URL, init?: RequestInit) => {
    seen.push(init?.credentials ?? "same-origin");
    return new Response("{}", { status: 200 });
  }) as unknown as typeof fetch;

  await createGestaltClient({
    address: "https://gestalt.test",
    transport: rest(),
    auth: session(),
    fetch: recordingFetch,
  }).then((client) =>
    client.agent.getSession(
      create(GetAgentProviderSessionRequestSchema, {
        sessionId: "sess-1",
        providerName: "demo",
      }),
    ),
  );
  expect(seen[0]).toBe("include");

  seen.length = 0;
  await createGestaltClient({
    address: "https://gestalt.test",
    transport: rest(),
    auth: unauthenticated(),
    fetch: recordingFetch,
  }).then((client) =>
    client.agent.getSession(
      create(GetAgentProviderSessionRequestSchema, {
        sessionId: "sess-1",
        providerName: "demo",
      }),
    ),
  );
  expect(seen[0]).toBe("omit");
});

test("REST path templates resolve snake_case placeholders from camelCase JSON", () => {
  const { path } = buildRestPath("/api/v2/agent/sessions/{session_id}", {
    sessionId: "sess-42",
    providerName: "demo",
  });
  expect(path).toBe("/api/v2/agent/sessions/sess-42");
});
