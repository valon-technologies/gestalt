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
  ExchangeInvocationTokenResponseSchema,
  OperationResultSchema,
  AppInvoker as AppInvokerService,
} from "../src/internal/gen/v1/app_pb.ts";
import {
  ENV_HOST_SERVICE_SOCKET,
  ENV_HOST_SERVICE_TOKEN,
  AppInvoker,
  request,
} from "../src/index.ts";
import { removeTempDir } from "./helpers.ts";

interface IssueLookupParams {
  issue_number: number;
}

test("AppInvoker forwards invocation tokens from strings and Request objects", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-plugin-invoker-"));
  const socketPath = join(tempDir, "plugin-invoker.sock");
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];
  const calls: Array<{
    invocationToken: string;
    app: string;
    operation: string;
    params: Record<string, unknown>;
    connection: string;
    instance: string;
    idempotencyKey: string;
  }> = [];
  const graphqlCalls: Array<{
    invocationToken: string;
    app: string;
    document: string;
    variables: Record<string, unknown>;
    connection: string;
    instance: string;
    idempotencyKey: string;
  }> = [];
  const exchanges: Array<{
    parentInvocationToken: string;
    grants: Array<{
      app: string;
      operations: string[];
      surfaces: string[];
      allOperations: boolean;
    }>;
    ttlSeconds: bigint;
  }> = [];

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(
        AppInvokerService,
        {
          async exchangeInvocationToken(input) {
            exchanges.push({
              parentInvocationToken: input.parentInvocationToken,
              grants: input.grants.map((grant) => ({
                app: grant.app,
                operations: [...grant.operations],
                surfaces: [...grant.surfaces],
                allOperations: grant.allOperations,
              })),
              ttlSeconds: input.ttlSeconds,
            });
            return create(ExchangeInvocationTokenResponseSchema, {
              invocationToken: `${input.parentInvocationToken}:child`,
            });
          },
          async invoke(input) {
            const params = Object.fromEntries(Object.entries(input.params ?? {}));
            calls.push({
              invocationToken: input.invocationToken,
              app: input.app,
              operation: input.operation,
              params,
              connection: input.connection,
              instance: input.instance,
              idempotencyKey: input.idempotencyKey,
            });
            return create(OperationResultSchema, {
              status: 207,
              body: JSON.stringify({
                invocationToken: input.invocationToken,
                app: input.app,
                operation: input.operation,
                params,
                connection: input.connection,
                instance: input.instance,
                idempotencyKey: input.idempotencyKey,
              }),
            });
          },
          async invokeGraphQL(input) {
            const variables = Object.fromEntries(Object.entries(input.variables ?? {}));
            graphqlCalls.push({
              invocationToken: input.invocationToken,
              app: input.app,
              document: input.document,
              variables,
              connection: input.connection,
              instance: input.instance,
              idempotencyKey: input.idempotencyKey,
            });
            return create(OperationResultSchema, {
              status: 208,
              body: JSON.stringify({
                invocationToken: input.invocationToken,
                app: input.app,
                document: input.document,
                variables,
                connection: input.connection,
                instance: input.instance,
                idempotencyKey: input.idempotencyKey,
              }),
            });
          },
        } satisfies Partial<ServiceImpl<typeof AppInvokerService>>,
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

    const fromHandle = new AppInvoker("invocation-token-123");
    const childToken = await fromHandle.exchangeInvocationToken({
      grants: [
        {
          app: "github",
          operations: ["get_issue"],
        },
        {
          app: "linear",
          surfaces: ["graphql"],
        },
        {
          app: "google_sheets",
          allOperations: true,
        },
      ],
      ttlSeconds: 45,
    });
    expect(childToken).toBe("invocation-token-123:child");

    const issueLookupParams: IssueLookupParams = {
      issue_number: 42,
    };
    const first = await fromHandle.invoke(
      "github",
      "get_issue",
      issueLookupParams,
      {
        connection: "work",
        instance: "secondary",
        idempotencyKey: " issue-42-create ",
      },
    );

    expect(first.status).toBe(207);
    expect(JSON.parse(first.body)).toEqual({
      invocationToken: "invocation-token-123",
      app: "github",
      operation: "get_issue",
      params: {
        issue_number: 42,
      },
      connection: "work",
      instance: "secondary",
      idempotencyKey: "issue-42-create",
    });

    const fromRequest = new AppInvoker(
      request("tok", {}, {}, {}, {}, {}, "invocation-token-456"),
    );
    const second = await fromRequest.invoke("slack", "post_message", {
      channel: "eng",
      text: "hello",
      nested: Object.freeze({ urgent: true }),
    });

    expect(second.status).toBe(207);
    expect(JSON.parse(second.body)).toEqual({
      invocationToken: "invocation-token-456",
      app: "slack",
      operation: "post_message",
      params: {
        channel: "eng",
        text: "hello",
        nested: { urgent: true },
      },
      connection: "",
      instance: "",
      idempotencyKey: "",
    });

    await expect(
      fromRequest.invoke("slack", "bad", { when: new Date() } as any),
    ).rejects.toThrow(TypeError);

    const graphql = await fromRequest.invokeGraphQL(
      "linear",
      "query Viewer($team: String!) { viewer(team: $team) { id } }",
      {
        variables: { team: "eng" },
        connection: "workspace",
        idempotencyKey: " graphql-call-42 ",
      },
    );

    expect(graphql.status).toBe(208);
    expect(JSON.parse(graphql.body)).toEqual({
      invocationToken: "invocation-token-456",
      app: "linear",
      document: "query Viewer($team: String!) { viewer(team: $team) { id } }",
      variables: {
        team: "eng",
      },
      connection: "workspace",
      instance: "",
      idempotencyKey: "graphql-call-42",
    });

    expect(exchanges).toEqual([
      {
        parentInvocationToken: "invocation-token-123",
        grants: [
          {
            app: "github",
            operations: ["get_issue"],
            surfaces: [],
            allOperations: false,
          },
          {
            app: "linear",
            operations: [],
            surfaces: ["graphql"],
            allOperations: false,
          },
          {
            app: "google_sheets",
            operations: [],
            surfaces: [],
            allOperations: true,
          },
        ],
        ttlSeconds: 45n,
      },
    ]);

    expect(calls).toEqual([
      {
        invocationToken: "invocation-token-123",
        app: "github",
        operation: "get_issue",
        params: {
          issue_number: 42,
        },
        connection: "work",
        instance: "secondary",
        idempotencyKey: "issue-42-create",
      },
      {
        invocationToken: "invocation-token-456",
        app: "slack",
        operation: "post_message",
        params: {
          channel: "eng",
          text: "hello",
          nested: { urgent: true },
        },
        connection: "",
        instance: "",
        idempotencyKey: "",
      },
    ]);

    expect(graphqlCalls).toEqual([
      {
        invocationToken: "invocation-token-456",
        app: "linear",
        document: "query Viewer($team: String!) { viewer(team: $team) { id } }",
        variables: {
          team: "eng",
        },
        connection: "workspace",
        instance: "",
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

test("AppInvoker prioritizes invocation-token validation over socket configuration", () => {
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];

  try {
    delete process.env[ENV_HOST_SERVICE_SOCKET];
    expect(() => new AppInvoker("   ")).toThrow(
      "app invoker: invocation token is not available",
    );
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

test("AppInvoker honors tcp target env and relay token env", async () => {
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];
  const previousToken = process.env[ENV_HOST_SERVICE_TOKEN];
  const seenTokens: string[] = [];
  const address = await reserveTCPAddress();

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(AppInvokerService, {
        async invoke(input) {
          return create(OperationResultSchema, {
            status: 204,
            body: JSON.stringify({
              invocationToken: input.invocationToken,
              app: input.app,
              operation: input.operation,
            }),
          });
        },
      } satisfies Partial<ServiceImpl<typeof AppInvokerService>>);
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

    const invoker = new AppInvoker("invoke-token");
    const response = await invoker.invoke("github", "get_issue");

    expect(response.status).toBe(204);
    expect(JSON.parse(response.body)).toEqual({
      invocationToken: "invoke-token",
      app: "github",
      operation: "get_issue",
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
