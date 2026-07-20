import { create } from "@bufbuild/protobuf";
import { expect, test } from "bun:test";

import {
  AppInvokeRequestSchema,
  InvokeFrameSchema,
  OperationResultSchema,
  type OperationResult,
} from "../src/internal/gen/v1/app_pb.ts";
import { authToProvider, bearer, unauthenticated } from "../src/client/auth.ts";
import { createGestaltClient, rest } from "../src/client/client.ts";
import { GestaltErrorCode } from "../src/client/errors.ts";
import { PUBLIC_METHODS } from "../src/client/generated/methods.ts";
import { buildRestPath } from "../src/client/generated/rest_request_mapping.ts";
import { createRestTransport } from "../src/client/rest_transport.ts";

test("REST transport maps protobuf JSON requests and gateway errors", async () => {
  const calls: Array<{
    method: string;
    url: string;
    body: string;
    authorization?: string;
    credentials?: RequestCredentials;
  }> = [];

  const transport = createRestTransport({
    baseUrl: "https://gestalt.test/",
    auth: authToProvider(bearer(() => "token-123")),
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

  const gatewayTransport = createRestTransport({
    baseUrl: "https://gestalt.test/",
    auth: authToProvider(bearer(() => "token")),
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

test("bearer rotation is evaluated per invocation", async () => {
  let token = "first";
  const authorizations: string[] = [];
  const transport = createRestTransport({
    baseUrl: "https://gestalt.test/",
    auth: authToProvider(bearer(async () => token)),
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

test("unauthenticated auth omits Authorization header", async () => {
  const seen: Array<string | null> = [];
  const transport = createRestTransport({
    baseUrl: "https://gestalt.test/",
    auth: authToProvider(unauthenticated()),
    fetch: (async (_input: RequestInfo | URL, init?: RequestInit) => {
      seen.push(
        init?.headers instanceof Headers
          ? init.headers.get("Authorization")
          : null,
      );
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
  expect(seen[0]).toBeNull();
});

test("createGestaltClient requires and validates address", async () => {
  await expect(
    createGestaltClient({
      address: "",
      transport: rest(),
      auth: unauthenticated(),
    }),
  ).rejects.toThrow(/address is required/);

  await expect(
    createGestaltClient({
      address: "not-a-url",
      transport: rest(),
      auth: unauthenticated(),
    }),
  ).rejects.toThrow(/absolute URL/);

  await expect(
    createGestaltClient({
      address: new URL("ftp://gestalt.test"),
      transport: rest(),
      auth: unauthenticated(),
    }),
  ).rejects.toThrow(/http or https/);
});

test("REST path templates use sdkgen path field metadata", () => {
  const path = buildRestPath(PUBLIC_METHODS.app.invoke.http!, {
    app: "example",
    operation: "sync",
  });
  expect(path).toBe("/api/v2/app/example/operations/sync");
});

test("POST requests append sdkgen query fields", async () => {
  const seenUrls: string[] = [];
  const transport = createRestTransport({
    baseUrl: "https://gestalt.test/",
    auth: authToProvider(bearer(() => "token")),
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

test("REST transport maps malformed success bodies to GestaltError", async () => {
  const transport = createRestTransport({
    baseUrl: "https://gestalt.test/",
    auth: authToProvider(bearer(() => "token")),
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
  ).rejects.toMatchObject({
    name: "GestaltError",
    code: GestaltErrorCode.Internal,
    message: "response body is not valid JSON",
  });
});

test("REST transport propagates cancellation and deadlines", async () => {
  const controller = new AbortController();
  controller.abort();

  const transport = createRestTransport({
    baseUrl: "https://gestalt.test/",
    auth: authToProvider(bearer(() => "token")),
    fetch: (async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.signal?.aborted) {
        const error = new Error("request canceled");
        error.name = "AbortError";
        throw error;
      }
      return new Response(JSON.stringify({ status: 200, body: "", headers: {} }), {
        status: 200,
      });
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

  const slowTransport = createRestTransport({
    baseUrl: "https://gestalt.test/",
    auth: authToProvider(bearer(() => "token")),
    fetch: (async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.signal?.aborted) {
        const error = new Error("The operation was aborted");
        error.name = "AbortError";
        throw error;
      }
      await new Promise<void>((resolve, reject) => {
        const timer = setTimeout(resolve, 50);
        init?.signal?.addEventListener(
          "abort",
          () => {
            clearTimeout(timer);
            const error = new Error("The operation was aborted");
            error.name = "AbortError";
            reject(error);
          },
          { once: true },
        );
      });
      return new Response(JSON.stringify({ status: 200, body: "", headers: {} }), {
        status: 200,
      });
    }) as unknown as typeof fetch,
  });

  await expect(
    slowTransport.unary(
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

test("timeout interrupts a hanging bearer token provider", async () => {
  let tokenCalls = 0;
  const transport = createRestTransport({
    baseUrl: "https://gestalt.test/",
    auth: authToProvider(
      bearer(async () => {
        tokenCalls += 1;
        await new Promise(() => {});
        return "token";
      }),
    ),
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

test("bearer provider is not awaited after cancellation", async () => {
  let tokenCalls = 0;
  const controller = new AbortController();
  controller.abort();

  const transport = createRestTransport({
    baseUrl: "https://gestalt.test/",
    auth: authToProvider(
      bearer(async () => {
        tokenCalls += 1;
        return "token";
      }),
    ),
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

test("arbitrary abort reasons map to Canceled", async () => {
  const controller = new AbortController();
  controller.abort(new Error("stop"));

  const transport = createRestTransport({
    baseUrl: "https://gestalt.test/",
    auth: authToProvider(unauthenticated()),
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

test("REST transport maps body-read aborts to GestaltError", async () => {
  const transport = createRestTransport({
    baseUrl: "https://gestalt.test/",
    auth: authToProvider(bearer(() => "token")),
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

test("REST transport serverStream posts to Invoke path and yields metadata + data frames", async () => {
  const calls: Array<{
    method: string;
    url: string;
    body: string;
    authorization?: string;
  }> = [];

  const ndjsonBody = `{"event":"start"}\n{"event":"end"}\n`;
  const encoder = new TextEncoder();

  const transport = createRestTransport({
    baseUrl: "https://gestalt.test/",
    auth: authToProvider(bearer(() => "token-stream")),
    fetch: (async (input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({
        method: init?.method ?? "GET",
        url: String(input),
        body: typeof init?.body === "string" ? init.body : "",
        ...(init?.headers instanceof Headers &&
        init.headers.get("Authorization")
          ? { authorization: init.headers.get("Authorization")! }
          : {}),
      });
      return new Response(ndjsonBody, {
        status: 200,
        headers: { "Content-Type": "application/x-ndjson" },
      });
    }) as unknown as typeof fetch,
  });

  const request = create(AppInvokeRequestSchema, {
    app: "stream-app",
    operation: "events.watch",
    params: {},
    connection: "",
    instance: "",
    idempotencyKey: "",
    credentialMode: "",
  });

  const frames: Array<{ case: string; data?: Uint8Array | undefined; mediaType?: string | undefined }> =
    [];
  const stream = transport.serverStream!(
    PUBLIC_METHODS.app.invokeStream,
    request,
    AppInvokeRequestSchema,
    InvokeFrameSchema,
  );
  for await (const frame of stream) {
    const wire = frame as {
      value?: { case: string; value: unknown };
    };
    const c = wire.value?.case ?? "unknown";
    if (c === "metadata") {
      const meta = wire.value!.value as { mediaType?: string };
      frames.push({ case: "metadata", mediaType: meta.mediaType });
    } else if (c === "data") {
      frames.push({ case: "data", data: wire.value!.value as Uint8Array });
    } else {
      frames.push({ case: c });
    }
  }

  expect(calls).toHaveLength(1);
  expect(calls[0]?.method).toBe("POST");
  expect(calls[0]?.url).toBe(
    "https://gestalt.test/api/v2/app/stream-app/operations/events.watch",
  );
  expect(calls[0]?.authorization).toBe("Bearer token-stream");

  expect(frames[0]?.case).toBe("metadata");
  expect(frames[0]?.mediaType).toBe("application/x-ndjson");

  const dataFrames = frames.filter((f) => f.case === "data");
  expect(dataFrames.length).toBeGreaterThan(0);
  const reassembled = dataFrames
    .map((f) => new TextDecoder().decode(f.data))
    .join("");
  expect(reassembled).toBe(ndjsonBody);
});

test("REST transport serverStream throws GestaltError on non-2xx", async () => {
  const transport = createRestTransport({
    baseUrl: "https://gestalt.test/",
    auth: authToProvider(bearer(() => "token-err")),
    fetch: (async (_input: RequestInfo | URL, _init?: RequestInit) => {
      return new Response(
        JSON.stringify({ error: "not found", code: "NotFound" }),
        { status: 404, headers: { "Content-Type": "application/json" } },
      );
    }) as unknown as typeof fetch,
  });

  const request = create(AppInvokeRequestSchema, {
    app: "err-app",
    operation: "missing",
    params: {},
    connection: "",
    instance: "",
    idempotencyKey: "",
    credentialMode: "",
  });

  let thrown = false;
  try {
    const stream = transport.serverStream!(
      PUBLIC_METHODS.app.invokeStream,
      request,
      AppInvokeRequestSchema,
      InvokeFrameSchema,
    );
    for await (const _frame of stream) {
    }
  } catch {
    thrown = true;
  }
  expect(thrown).toBe(true);
});
