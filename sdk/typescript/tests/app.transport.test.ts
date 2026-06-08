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
  RequestContextSchema,
  SubjectContextSchema,
} from "../src/internal/gen/v1/app_pb.ts";
import {
  App,
  ENV_HOST_SERVICE_SOCKET,
  ENV_HOST_SERVICE_TOKEN,
  request,
} from "../src/index.ts";
import { structFromObject } from "../src/protocol.ts";
import { removeTempDir } from "./helpers.ts";

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
            return create(OperationResultSchema, {
              status: 207,
              headers: {
                Location: {
                  values: ["https://example.test/created"],
                },
              },
              body: JSON.stringify({
                app: input.app,
                operation: input.operation,
                subjectId: input.context?.subject?.id ?? "",
                idempotencyKey: input.idempotencyKey,
              }),
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
              body: JSON.stringify({
                app: input.app,
                document: input.document,
                subjectId: input.context?.subject?.id ?? "",
                idempotencyKey: input.idempotencyKey,
              }),
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
    const context = create(RequestContextSchema, {
      subject: create(SubjectContextSchema, {
        id: "user:user-123",
      }),
      workflow: structFromObject({
        provider: "local",
        runId: "run-123",
      }),
    });
    const app = new App(
      request("", {}, {}, {}, {}, {}, "request-key", {}, {}, [], false, context),
    );

    const first = await app.invokeRaw(
      "github",
      "get_issue",
      { issue_number: 42 },
      {
        connection: "work",
        instance: "secondary",
        idempotencyKey: " issue-42-create ",
      },
    );
    expect(first.status).toBe(207);
    expect(first.headers).toEqual({
      Location: ["https://example.test/created"],
    });
    expect(JSON.parse(first.body)).toEqual({
      app: "github",
      operation: "get_issue",
      subjectId: "user:user-123",
      idempotencyKey: "issue-42-create",
    });

    const graphql = await app.invokeGraphQLRaw(
      "linear",
      "query Viewer { viewer { id } }",
      {
        idempotencyKey: " graphql-call-42 ",
      },
    );
    expect(graphql.status).toBe(208);
    expect(JSON.parse(graphql.body)).toEqual({
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

test("App still requires host service socket configuration", () => {
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];

  try {
    delete process.env[ENV_HOST_SERVICE_SOCKET];
    expect(() => new App(request())).toThrow("app: GESTALT_HOST_SERVICE_SOCKET is not set");
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
            body: JSON.stringify({
              app: input.app,
              operation: input.operation,
              subjectId: input.context?.subject?.id ?? "",
            }),
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

    const app = new App(request("", {}, {}, {}, {}, {}, "", {}, {}, [], false, create(RequestContextSchema, {
      subject: create(SubjectContextSchema, {
        id: "user:user-123",
      }),
    })));
    const response = await app.invokeRaw("github", "get_issue");

    expect(response.status).toBe(204);
    expect(JSON.parse(response.body)).toEqual({
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
