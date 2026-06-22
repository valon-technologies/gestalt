import { mkdtempSync } from "node:fs";
import { createServer } from "node:http2";
import { createServer as createNetServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { create } from "@bufbuild/protobuf";
import { type ServiceImpl } from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";
import { expect, test } from "bun:test";

import {
  App as AppService,
  OperationResultSchema,
} from "../src/internal/gen/v1/app_pb.ts";
import {
  Identity as IdentityService,
  UserInfoResponseSchema,
} from "../src/internal/gen/v1/identity_pb.ts";
import {
  App,
  CALLER_BEARER_TOKEN_METADATA_KEY,
  ENV_HOST_SERVICE_SOCKET,
  ENV_HOST_SERVICE_TOKEN,
  GestaltError,
  GestaltErrorCode,
  InvokeError,
  request,
  type RequestContext,
  type SubjectContext,
} from "../src/index.ts";
import { removeTempDir } from "./helpers.ts";

function subjectContext(id: string): SubjectContext {
  return {
    id,
    email: "",
    displayName: "",
    scopes: [],
    permissions: [],
  };
}

function text(result: { body: Uint8Array }): string {
  return new TextDecoder().decode(result.body);
}

function bytes(text: string): Uint8Array {
  return new TextEncoder().encode(text);
}

test("App forwards request context to operation and GraphQL calls", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-plugin-app-"));
  const socketPath = join(tempDir, "plugin-app.sock");
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];
  const calls: Array<{
    app: string;
    operation: string;
    subjectId: string;
    workflowRunId?: string;
    idempotencyKey: string;
  }> = [];
  const graphqlCalls: Array<{
    app: string;
    document: string;
    subjectId: string;
    idempotencyKey: string;
  }> = [];

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(
        AppService,
        {
          async invoke(input) {
            calls.push({
              app: input.app,
              operation: input.operation,
              subjectId: input.context?.subject?.id ?? "",
              ...(typeof input.context?.workflow?.runId === "string"
                ? { workflowRunId: input.context.workflow.runId }
                : {}),
              idempotencyKey: input.idempotencyKey,
            });
            if (input.operation === "fail_envelope") {
              return create(OperationResultSchema, {
                status: 200,
                body: bytes(JSON.stringify({
                  status: "error",
                  error: { message: "missing credential", code: "missing_credential" },
                })),
              });
            }
            return create(OperationResultSchema, {
              status: 207,
              headers: {
                Location: {
                  values: ["https://example.test/created"],
                },
              },
              body: bytes(JSON.stringify({
                app: input.app,
                operation: input.operation,
                subjectId: input.context?.subject?.id ?? "",
                idempotencyKey: input.idempotencyKey,
              })),
            });
          },
          async invokeGraphQL(input) {
            graphqlCalls.push({
              app: input.app,
              document: input.document,
              subjectId: input.context?.subject?.id ?? "",
              idempotencyKey: input.idempotencyKey,
            });
            return create(OperationResultSchema, {
              status: 208,
              body: bytes(JSON.stringify({
                app: input.app,
                document: input.document,
                subjectId: input.context?.subject?.id ?? "",
                idempotencyKey: input.idempotencyKey,
              })),
            });
          },
        } satisfies Partial<ServiceImpl<typeof AppService>>,
      );
    },
  });
  const server = createServer(handler);

  try {
    await new Promise<void>((resolve, reject) => {
      server.once("error", reject);
      server.listen(socketPath, () => {
        server.off("error", reject);
        resolve();
      });
    });

    process.env[ENV_HOST_SERVICE_SOCKET] = socketPath;
    const context: RequestContext = {
      subject: subjectContext("user:user-123"),
      workflow: {
        provider: "local",
        runId: "run-123",
      },
      toolRefs: [],
      toolRefsSet: false,
    };
    const app = App.connect({ context });

    const first = await app.invokeRaw({
      app: "github",
      operation: "get_issue",
      connection: "work",
      instance: "secondary",
      idempotencyKey: "issue-42-create",
      credentialMode: "",
      params: { issue_number: 42 },
    });
    expect(first.status).toBe(207);
    expect(first.headers).toEqual({
      Location: { values: ["https://example.test/created"] },
    });
    expect(text(first)).toBe(JSON.stringify({
      app: "github",
      operation: "get_issue",
      subjectId: "user:user-123",
      idempotencyKey: "issue-42-create",
    }));
    expect(JSON.parse(text(first))).toEqual({
      app: "github",
      operation: "get_issue",
      subjectId: "user:user-123",
      idempotencyKey: "issue-42-create",
    });

    const decoded = await app.invoke(
      "github",
      "get_issue",
      { issue_number: 42 },
      {
        connection: "work",
        instance: "secondary",
        idempotencyKey: "issue-42-decode",
      },
    );
    expect(decoded).toEqual({
      app: "github",
      operation: "get_issue",
      subjectId: "user:user-123",
      idempotencyKey: "issue-42-decode",
    });

    // A direct try/catch keeps bun's http2 request write deterministic; an
    // expect().rejects wrapper perturbs the stream and the call dies in the
    // transport before the envelope decode runs.
    try {
      await app.invoke("github", "fail_envelope", undefined, {
        connection: "work",
        instance: "secondary",
        idempotencyKey: "fail-1",
      });
      throw new Error("expected InvokeError");
    } catch (error) {
      expect(error).toBeInstanceOf(InvokeError);
      const invokeError = error as InvokeError;
      expect(invokeError.message).toBe("missing credential");
      expect(invokeError.code).toBe("missing_credential");
      expect(invokeError.app).toBe("github");
      expect(invokeError.operation).toBe("fail_envelope");
    }

    const graphql = await app.invokeGraphQL(
      "linear",
      "query Viewer { viewer { id } }",
      { idempotencyKey: "graphql-call-42" },
    );
    expect(graphql.status).toBe(208);
    expect(JSON.parse(text(graphql))).toEqual({
      app: "linear",
      document: "query Viewer { viewer { id } }",
      subjectId: "user:user-123",
      idempotencyKey: "graphql-call-42",
    });

    expect(calls).toEqual([
      {
        app: "github",
        operation: "get_issue",
        subjectId: "user:user-123",
        workflowRunId: "run-123",
        idempotencyKey: "issue-42-create",
      },
      {
        app: "github",
        operation: "get_issue",
        subjectId: "user:user-123",
        workflowRunId: "run-123",
        idempotencyKey: "issue-42-decode",
      },
      {
        app: "github",
        operation: "fail_envelope",
        subjectId: "user:user-123",
        workflowRunId: "run-123",
        idempotencyKey: "fail-1",
      },
    ]);
    expect(graphqlCalls).toEqual([
      {
        app: "linear",
        document: "query Viewer { viewer { id } }",
        subjectId: "user:user-123",
        idempotencyKey: "graphql-call-42",
      },
    ]);
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_HOST_SERVICE_SOCKET];
    } else {
      process.env[ENV_HOST_SERVICE_SOCKET] = previousSocket;
    }
    if (server.listening) {
      await new Promise<void>((resolve) => {
        server.close(() => resolve());
      });
    }
    removeTempDir(tempDir);
  }
});

test("App client-level timeoutMs applies a per-call deadline", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-plugin-app-timeout-"));
  const socketPath = join(tempDir, "plugin-app.sock");
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(AppService, {
        async invoke(input) {
          await new Promise((resolve) => setTimeout(resolve, 2_000));
          return create(OperationResultSchema, {
            status: 200,
            body: bytes(JSON.stringify({ operation: input.operation })),
          });
        },
      } satisfies Partial<ServiceImpl<typeof AppService>>);
    },
  });
  const server = createServer(handler);

  try {
    await new Promise<void>((resolve, reject) => {
      server.once("error", reject);
      server.listen(socketPath, () => {
        server.off("error", reject);
        resolve();
      });
    });

    process.env[ENV_HOST_SERVICE_SOCKET] = socketPath;
    const app = App.connect({ timeoutMs: 50 });

    // A direct try/catch keeps bun's http2 request write deterministic; see
    // the envelope-failure case above.
    try {
      await app.invoke("github", "slow_operation");
      throw new Error("expected GestaltError");
    } catch (error) {
      expect(error).toBeInstanceOf(GestaltError);
      expect((error as GestaltError).code).toBe(GestaltErrorCode.DeadlineExceeded);
    }
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_HOST_SERVICE_SOCKET];
    } else {
      process.env[ENV_HOST_SERVICE_SOCKET] = previousSocket;
    }
    if (server.listening) {
      await new Promise<void>((resolve) => {
        server.close(() => resolve());
      });
    }
    removeTempDir(tempDir);
  }
});

test("App still requires host service socket configuration", () => {
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];

  try {
    delete process.env[ENV_HOST_SERVICE_SOCKET];
    expect(() => App.connect()).toThrow("app: GESTALT_HOST_SERVICE_SOCKET is not set");
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_HOST_SERVICE_SOCKET];
    } else {
      process.env[ENV_HOST_SERVICE_SOCKET] = previousSocket;
    }
  }
});

async function reserveTCPAddress(): Promise<string> {
  return await new Promise((resolve, reject) => {
    const server = createNetServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      if (!address || typeof address === "string") {
        server.close();
        reject(new Error("failed to reserve tcp address"));
        return;
      }
      const result = `${address.address}:${address.port}`;
      server.close((err) => {
        if (err) {
          reject(err);
          return;
        }
        resolve(result);
      });
    });
  });
}

test("App honors tcp target env and relay token env", async () => {
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];
  const previousToken = process.env[ENV_HOST_SERVICE_TOKEN];
  const seenTokens: string[] = [];
  const address = await reserveTCPAddress();

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(AppService, {
        async invoke(input) {
          return create(OperationResultSchema, {
            status: 204,
            body: bytes(JSON.stringify({
              app: input.app,
              operation: input.operation,
              subjectId: input.context?.subject?.id ?? "",
            })),
          });
        },
      } satisfies Partial<ServiceImpl<typeof AppService>>);
    },
  });
  const server = createServer((req, res) => {
    const tokenHeader = req.headers["x-gestalt-host-service-relay-token"];
    if (typeof tokenHeader === "string") {
      seenTokens.push(tokenHeader);
    }
    handler(req, res);
  });

  try {
    await new Promise<void>((resolve, reject) => {
      server.once("error", reject);
      server.listen(Number(address.split(":").at(-1)), "127.0.0.1", () => {
        server.off("error", reject);
        resolve();
      });
    });

    process.env[ENV_HOST_SERVICE_SOCKET] = `tcp://${address}`;
    process.env[ENV_HOST_SERVICE_TOKEN] = "relay-token-typescript";

    const app = App.connect({
      context: {
        subject: subjectContext("user:user-123"),
        toolRefs: [],
        toolRefsSet: false,
      },
    });
    const response = await app.invokeRaw({
      app: "github",
      operation: "get_issue",
      connection: "",
      instance: "",
      idempotencyKey: "",
      credentialMode: "",
    });

    expect(response.status).toBe(204);
    expect(JSON.parse(text(response))).toEqual({
      app: "github",
      operation: "get_issue",
      subjectId: "user:user-123",
    });
    expect(seenTokens).toEqual(["relay-token-typescript"]);
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_HOST_SERVICE_SOCKET];
    } else {
      process.env[ENV_HOST_SERVICE_SOCKET] = previousSocket;
    }
    if (previousToken === undefined) {
      delete process.env[ENV_HOST_SERVICE_TOKEN];
    } else {
      process.env[ENV_HOST_SERVICE_TOKEN] = previousToken;
    }
    if (server.listening) {
      await new Promise<void>((resolve) => {
        server.close(() => resolve());
      });
    }
  }
});

test("Request.app creates an app client", async () => {
  const req = request();
  await expect(req.app()).rejects.toThrow("app: GESTALT_HOST_SERVICE_SOCKET is not set");
});

test("Request.authentication forwards the caller bearer token", async () => {
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];
  const previousToken = process.env[ENV_HOST_SERVICE_TOKEN];
  const seenHeaders: Array<{ relayToken: string; callerToken: string }> = [];
  const address = await reserveTCPAddress();

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(IdentityService, {
        async userInfo() {
          return create(UserInfoResponseSchema, {
            subjectId: "user:hugh@valon.com",
            email: "hugh@valon.com",
            name: "Hugh Han",
          });
        },
      } satisfies Partial<ServiceImpl<typeof IdentityService>>);
    },
  });
  const server = createServer((req, res) => {
    const relayToken = req.headers["x-gestalt-host-service-relay-token"];
    const callerToken = req.headers[CALLER_BEARER_TOKEN_METADATA_KEY];
    seenHeaders.push({
      relayToken: typeof relayToken === "string" ? relayToken : "",
      callerToken: typeof callerToken === "string" ? callerToken : "",
    });
    handler(req, res);
  });

  try {
    await new Promise<void>((resolve, reject) => {
      server.once("error", reject);
      server.listen(Number(address.split(":").at(-1)), "127.0.0.1", () => {
        server.off("error", reject);
        resolve();
      });
    });

    process.env[ENV_HOST_SERVICE_SOCKET] = `tcp://${address}`;
    process.env[ENV_HOST_SERVICE_TOKEN] = "relay-token-typescript";

    const auth = await request("caller-access-token").identity();
    const response = await auth.userInfo({});

    expect(response).toEqual({
      subjectId: "user:hugh@valon.com",
      email: "hugh@valon.com",
      name: "Hugh Han",
    });
    expect(seenHeaders).toEqual([{
      relayToken: "relay-token-typescript",
      callerToken: "caller-access-token",
    }]);
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_HOST_SERVICE_SOCKET];
    } else {
      process.env[ENV_HOST_SERVICE_SOCKET] = previousSocket;
    }
    if (previousToken === undefined) {
      delete process.env[ENV_HOST_SERVICE_TOKEN];
    } else {
      process.env[ENV_HOST_SERVICE_TOKEN] = previousToken;
    }
    if (server.listening) {
      await new Promise<void>((resolve) => {
        server.close(() => resolve());
      });
    }
  }
});
