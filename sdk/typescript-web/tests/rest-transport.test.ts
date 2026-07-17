import { create } from "@bufbuild/protobuf";
import { expect, test } from "bun:test";

import {
  AppInvokeRequestSchema,
  OperationResultSchema,
  type OperationResult,
} from "../src/client/runtime/internal/gen/v1/app_pb.ts";
import {
  bearer,
  createGestaltClient,
  session,
  unauthenticated,
} from "../src/client.ts";
import { GestaltErrorCode } from "../src/client/runtime/rpc_support.ts";
import { PUBLIC_METHODS } from "../src/client/generated/methods.ts";
import { buildRestPath, buildRestBody } from "../src/client/generated/rest_request_mapping.ts";
import { createRestUnaryTransport } from "../src/rest_transport.ts";

test("REST transport maps protobuf JSON requests and gateway errors", async () => {
  const calls: Array<{
    method: string;
    url: string;
    body: string;
    authorization?: string;
    credentials?: RequestCredentials;
  }> = [];

  const transport = createRestUnaryTransport({
    baseUrl: "https://gestalt.test/",
    auth: bearer(() => "token-123"),
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
      return new Response(
        JSON.stringify({
          status: 418,
          body: btoa("teapot"),
          headers: {
            "X-Example": { values: ["rest-v2"] },
          },
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      );
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

  const gatewayTransport = createRestUnaryTransport({
    baseUrl: "https://gestalt.test/",
    auth: bearer(() => "token"),
    fetch: (async () =>
      new Response(JSON.stringify({ error: "unauthorized", code: "Unauthenticated" }), {
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

test("session auth sends credentials include without Authorization header", async () => {
  const calls: Array<{ authorization: string | null; credentials?: RequestCredentials }> =
    [];
  const transport = createRestUnaryTransport({
    baseUrl: "https://gestalt.test/",
    auth: session(),
    fetch: (async (_input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({
        authorization:
          init?.headers instanceof Headers
            ? init.headers.get("Authorization")
            : null,
        ...(init?.credentials !== undefined
          ? { credentials: init.credentials }
          : {}),
      });
      return new Response(
        JSON.stringify({ status: 200, body: "", headers: {} }),
        { status: 200 },
      );
    }) as unknown as typeof fetch,
  });

  const request = create(AppInvokeRequestSchema, {
    app: "example",
    operation: "sync",
    params: {},
    connection: "",
    instance: "",
    idempotencyKey: "",
    credentialMode: "",
  });

  await transport.unary(
    PUBLIC_METHODS.app.invoke,
    request,
    AppInvokeRequestSchema,
    OperationResultSchema,
  );
  expect(calls[0]?.authorization).toBeNull();
  expect(calls[0]?.credentials).toBe("include");
});

test("bearer token provider is evaluated per invocation", async () => {
  let token = "first";
  const authorizations: string[] = [];
  const transport = createRestUnaryTransport({
    baseUrl: "https://gestalt.test/",
    auth: bearer(async () => token),
    fetch: (async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.headers instanceof Headers) {
        authorizations.push(init.headers.get("Authorization") ?? "");
      }
      return new Response(
        JSON.stringify({ status: 200, body: "", headers: {} }),
        { status: 200 },
      );
    }) as unknown as typeof fetch,
  });

  const request = create(AppInvokeRequestSchema, {
    app: "example",
    operation: "sync",
    params: {},
    connection: "",
    instance: "",
    idempotencyKey: "",
    credentialMode: "",
  });

  await transport.unary(
    PUBLIC_METHODS.app.invoke,
    request,
    AppInvokeRequestSchema,
    OperationResultSchema,
  );
  token = "second";
  await transport.unary(
    PUBLIC_METHODS.app.invoke,
    request,
    AppInvokeRequestSchema,
    OperationResultSchema,
  );

  expect(authorizations).toEqual(["Bearer first", "Bearer second"]);
});

test("unauthenticated auth omits Authorization header and credentials", async () => {
  const seen: Array<{ authorization: string | null; credentials?: RequestCredentials }> =
    [];
  const transport = createRestUnaryTransport({
    baseUrl: "https://gestalt.test/",
    auth: unauthenticated(),
    fetch: (async (_input: RequestInfo | URL, init?: RequestInit) => {
      seen.push({
        authorization:
          init?.headers instanceof Headers
            ? init.headers.get("Authorization")
            : null,
        ...(init?.credentials !== undefined
          ? { credentials: init.credentials }
          : {}),
      });
      return new Response(
        JSON.stringify({ status: 200, body: "", headers: {} }),
        { status: 200 },
      );
    }) as unknown as typeof fetch,
  });

  const request = create(AppInvokeRequestSchema, {
    app: "example",
    operation: "sync",
    params: {},
    connection: "",
    instance: "",
    idempotencyKey: "",
    credentialMode: "",
  });

  await transport.unary(
    PUBLIC_METHODS.app.invoke,
    request,
    AppInvokeRequestSchema,
    OperationResultSchema,
  );
  expect(seen[0]?.authorization).toBeNull();
  expect(seen[0]?.credentials).toBe("omit");
});

test("AbortSignal cancels in-flight requests without retrying", async () => {
  const controller = new AbortController();
  controller.abort(new DOMException("canceled", "AbortError"));
  const transport = createRestUnaryTransport({
    baseUrl: "https://gestalt.test/",
    auth: unauthenticated(),
    fetch: (async () => {
      throw new Error("fetch should not run");
    }) as unknown as typeof fetch,
  });

  const request = create(AppInvokeRequestSchema, {
    app: "example",
    operation: "sync",
    params: {},
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
      { signal: controller.signal },
    ),
  ).rejects.toMatchObject({
    name: "GestaltError",
    code: GestaltErrorCode.Canceled,
  });
});

test("timeout surfaces DeadlineExceeded without retrying", async () => {
  const transport = createRestUnaryTransport({
    baseUrl: "https://gestalt.test/",
    auth: unauthenticated(),
    fetch: (async (_input: RequestInfo | URL, init?: RequestInit) => {
      await new Promise((resolve) => setTimeout(resolve, 50));
      if (init?.signal?.aborted) {
        const error = new Error("The operation was aborted");
        error.name = "AbortError";
        throw error;
      }
      return new Response(
        JSON.stringify({ status: 200, body: "", headers: {} }),
        { status: 200 },
      );
    }) as unknown as typeof fetch,
  });

  const request = create(AppInvokeRequestSchema, {
    app: "example",
    operation: "sync",
    params: {},
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
      { timeoutMs: 1 },
    ),
  ).rejects.toMatchObject({
    name: "GestaltError",
    code: GestaltErrorCode.DeadlineExceeded,
  });
});

test("createGestaltClient wires REST transport", async () => {
  let calls = 0;
  const client = createGestaltClient({
    address: "https://gestalt.test",
    auth: bearer(() => "token"),
    fetch: (async () => {
      calls += 1;
      return new Response(
        JSON.stringify({
          status: 200,
          body: btoa(JSON.stringify({ status: "success", data: { ok: true } })),
          headers: {},
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }) as unknown as typeof fetch,
  });

  await expect(
    client.app.invoke({
      app: "example",
      operation: "sync",
      params: { ok: true },
      connection: "",
      instance: "",
      idempotencyKey: "",
      credentialMode: "",
    }),
  ).resolves.toEqual({ ok: true });
  expect(calls).toBe(1);
});

test("REST path templates use sdkgen path field metadata", () => {
  const path = buildRestPath(PUBLIC_METHODS.app.invoke.http!, {
    app: "example",
    operation: "sync",
  });
  expect(path).toBe("/api/v2/app/example/operations/sync");
});

test("REST body omits fill and reject metadata fields", () => {
  const body = buildRestBody(PUBLIC_METHODS.app.invoke, {
      app: "example",
      operation: "sync",
      params: { ok: true },
      context: { subject: "user" },
      runAs: "admin",
    }) as Record<string, unknown>;
  expect(body).toEqual({ params: { ok: true } });
});

test("bearer provider is not awaited after cancellation", async () => {
  let tokenCalls = 0;
  const controller = new AbortController();
  controller.abort(new DOMException("canceled", "AbortError"));
  const transport = createRestUnaryTransport({
    baseUrl: "https://gestalt.test/",
    auth: bearer(async () => {
      tokenCalls += 1;
      return "token";
    }),
    fetch: (async () => {
      throw new Error("fetch should not run");
    }) as unknown as typeof fetch,
  });

  const request = create(AppInvokeRequestSchema, {
    app: "example",
    operation: "sync",
    params: {},
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
      { signal: controller.signal },
    ),
  ).rejects.toMatchObject({
    name: "GestaltError",
    code: GestaltErrorCode.Canceled,
  });
  expect(tokenCalls).toBe(0);
});

test("timeout interrupts a hanging bearer token provider", async () => {
  let tokenCalls = 0;
  const transport = createRestUnaryTransport({
    baseUrl: "https://gestalt.test/",
    auth: bearer(async () => {
      tokenCalls += 1;
      await new Promise(() => {});
      return "token";
    }),
    fetch: (async () => {
      throw new Error("fetch should not run");
    }) as unknown as typeof fetch,
  });

  const request = create(AppInvokeRequestSchema, {
    app: "example",
    operation: "sync",
    params: {},
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
      { timeoutMs: 1 },
    ),
  ).rejects.toMatchObject({
    name: "GestaltError",
    code: GestaltErrorCode.DeadlineExceeded,
  });
  expect(tokenCalls).toBe(1);
});

test("arbitrary abort reasons map to Canceled", async () => {
  const controller = new AbortController();
  controller.abort("user canceled");
  const transport = createRestUnaryTransport({
    baseUrl: "https://gestalt.test/",
    auth: unauthenticated(),
    fetch: (async () => {
      throw new Error("fetch should not run");
    }) as unknown as typeof fetch,
  });

  const request = create(AppInvokeRequestSchema, {
    app: "example",
    operation: "sync",
    params: {},
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
      { signal: controller.signal },
    ),
  ).rejects.toMatchObject({
    name: "GestaltError",
    code: GestaltErrorCode.Canceled,
  });
});

test("POST requests append sdkgen query fields", async () => {
  const seenUrls: string[] = [];
  const transport = createRestUnaryTransport({
    baseUrl: "https://gestalt.test/",
    auth: bearer(() => "token"),
    fetch: (async (input: RequestInfo | URL) => {
      seenUrls.push(String(input));
      return new Response(
        JSON.stringify({ status: 200, body: "", headers: {} }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }) as unknown as typeof fetch,
  });

  const method = {
    ...PUBLIC_METHODS.app.invoke,
    http: {
      ...PUBLIC_METHODS.app.invoke.http!,
      queryFields: [{ name: "connection", jsonName: "connection" }],
    },
  };

  const request = create(AppInvokeRequestSchema, {
    app: "example",
    operation: "sync",
    params: {},
    connection: "prod",
    instance: "",
    idempotencyKey: "",
    credentialMode: "",
  });

  await transport.unary(
    method,
    request,
    AppInvokeRequestSchema,
    OperationResultSchema,
  );

  expect(seenUrls[0]).toBe(
    "https://gestalt.test/api/v2/app/example/operations/sync?connection=prod",
  );
});

test("REST transport maps body-read aborts to GestaltError", async () => {
  const transport = createRestUnaryTransport({
    baseUrl: "https://gestalt.test/",
    auth: bearer(() => "token"),
    fetch: (async () =>
      ({
        ok: true,
        arrayBuffer: async () => {
          throw new DOMException("The operation was aborted", "AbortError");
        },
      }) as unknown as Response) as unknown as typeof fetch,
  });

  const request = create(AppInvokeRequestSchema, {
    app: "example",
    operation: "sync",
    params: {},
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
    code: GestaltErrorCode.Canceled,
  });
});

test("createGestaltClient validates explicit addresses", () => {
  expect(() =>
    createGestaltClient({
      address: "not-a-url",
      auth: unauthenticated(),
    }),
  ).toThrow(/absolute URL/);

  expect(() =>
    createGestaltClient({
      address: new URL("ftp://gestalt.test"),
      auth: unauthenticated(),
    }),
  ).toThrow(/http or https/);
});

test("REST transport omits undefined optional params from sparse JSON bodies", async () => {
  const bodies: string[] = [];
  const client = createGestaltClient({
    address: "https://gestalt.test/",
    auth: bearer(() => "token"),
    fetch: (async (_input: RequestInfo | URL, init?: RequestInit) => {
      bodies.push(typeof init?.body === "string" ? init.body : "");
      return new Response(
        JSON.stringify({ status: 200, body: "", headers: {} }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }) as unknown as typeof fetch,
  });

  await client.app.invokeRaw({
    app: "example",
    operation: "list",
    params: {
      nested: { keep: true, drop: undefined },
      cursor: undefined,
      page: 1,
    },
  });

  expect(JSON.parse(bodies[0] ?? "{}")).toEqual({
    params: { nested: { keep: true }, page: 1 },
  });
});

test("REST transport maps 403 gateway errors to PermissionDenied", async () => {
  const transport = createRestUnaryTransport({
    baseUrl: "https://gestalt.test/",
    auth: bearer(() => "token"),
    fetch: (async () =>
      new Response(
        JSON.stringify({ error: "forbidden", code: "PermissionDenied" }),
        { status: 403, headers: { "Content-Type": "application/json" } },
      )) as unknown as typeof fetch,
  });

  const request = create(AppInvokeRequestSchema, {
    app: "example",
    operation: "sync",
    params: {},
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
    code: GestaltErrorCode.PermissionDenied,
    message: "forbidden",
  });
});

test("REST transport maps offline fetch failures to Unavailable", async () => {
  const transport = createRestUnaryTransport({
    baseUrl: "https://gestalt.test/",
    auth: bearer(() => "token"),
    fetch: (async () => {
      throw new TypeError("Failed to fetch");
    }) as unknown as typeof fetch,
  });

  const request = create(AppInvokeRequestSchema, {
    app: "example",
    operation: "sync",
    params: {},
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
    code: GestaltErrorCode.Unavailable,
  });
});

test("REST transport rejects malformed success JSON bodies", async () => {
  const transport = createRestUnaryTransport({
    baseUrl: "https://gestalt.test/",
    auth: bearer(() => "token"),
    fetch: (async () =>
      new Response("not-json", {
        status: 200,
        headers: { "Content-Type": "application/json" },
      })) as unknown as typeof fetch,
  });

  const request = create(AppInvokeRequestSchema, {
    app: "example",
    operation: "sync",
    params: {},
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
  ).rejects.toThrow();
});

test("REST transport accepts empty 204 success responses", async () => {
  const transport = createRestUnaryTransport({
    baseUrl: "https://gestalt.test/",
    auth: bearer(() => "token"),
    fetch: (async () => new Response(null, { status: 204 })) as unknown as typeof fetch,
  });

  const request = create(AppInvokeRequestSchema, {
    app: "example",
    operation: "sync",
    params: {},
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
  ).resolves.toMatchObject({ status: 0, body: new Uint8Array() });
});

test("invoke surfaces raw error details through InvokeError", async () => {
  const client = createGestaltClient({
    address: "https://gestalt.test",
    auth: bearer(() => "token"),
    fetch: (async () =>
      new Response(
        JSON.stringify({
          status: 400,
          body: btoa(
            JSON.stringify({
              status: "error",
              error: { code: "validation_failed", message: "bad field" },
            }),
          ),
          headers: {},
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      )) as unknown as typeof fetch,
  });

  await expect(
    client.app.invoke({
      app: "example",
      operation: "sync",
      params: {},
    }),
  ).rejects.toMatchObject({
    name: "InvokeError",
    reason: "validation_failed",
    status: 400,
  });
});
