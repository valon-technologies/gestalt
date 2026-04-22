import { mkdtempSync } from "node:fs";
import { createServer } from "node:http2";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { create } from "@bufbuild/protobuf";
import { type ServiceImpl } from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";
import { expect, test } from "bun:test";

import {
  ExchangeInvocationTokenResponseSchema,
  OperationResultSchema,
  PluginInvoker as PluginInvokerService,
} from "../gen/v1/plugin_pb.ts";
import {
  ENV_PLUGIN_INVOKER_SOCKET,
  PluginInvoker,
  request,
} from "../src/index.ts";
import { removeTempDir } from "./helpers.ts";

test("PluginInvoker forwards invocation tokens from strings and Request objects", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-plugin-invoker-"));
  const socketPath = join(tempDir, "plugin-invoker.sock");
  const previousSocket = process.env[ENV_PLUGIN_INVOKER_SOCKET];
  const calls: Array<{
    invocationToken: string;
    plugin: string;
    operation: string;
    params: Record<string, unknown>;
    connection: string;
    instance: string;
  }> = [];
  const graphqlCalls: Array<{
    invocationToken: string;
    plugin: string;
    document: string;
    variables: Record<string, unknown>;
    connection: string;
    instance: string;
  }> = [];
  const exchanges: Array<{
    parentInvocationToken: string;
    grants: Array<{
      plugin: string;
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
        PluginInvokerService,
        {
          async exchangeInvocationToken(input) {
            exchanges.push({
              parentInvocationToken: input.parentInvocationToken,
              grants: input.grants.map((grant) => ({
                plugin: grant.plugin,
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
              plugin: input.plugin,
              operation: input.operation,
              params,
              connection: input.connection,
              instance: input.instance,
            });
            return create(OperationResultSchema, {
              status: 207,
              body: JSON.stringify({
                invocationToken: input.invocationToken,
                plugin: input.plugin,
                operation: input.operation,
                params,
                connection: input.connection,
                instance: input.instance,
              }),
            });
          },
          async invokeGraphQL(input) {
            const variables = Object.fromEntries(Object.entries(input.variables ?? {}));
            graphqlCalls.push({
              invocationToken: input.invocationToken,
              plugin: input.plugin,
              document: input.document,
              variables,
              connection: input.connection,
              instance: input.instance,
            });
            return create(OperationResultSchema, {
              status: 208,
              body: JSON.stringify({
                invocationToken: input.invocationToken,
                plugin: input.plugin,
                document: input.document,
                variables,
                connection: input.connection,
                instance: input.instance,
              }),
            });
          },
        } satisfies Partial<ServiceImpl<typeof PluginInvokerService>>,
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

    process.env[ENV_PLUGIN_INVOKER_SOCKET] = socketPath;

    const fromHandle = new PluginInvoker("invocation-token-123");
    const childToken = await fromHandle.exchangeInvocationToken({
      grants: [
        {
          plugin: "github",
          operations: ["get_issue"],
        },
        {
          plugin: "linear",
          surfaces: ["graphql"],
        },
        {
          plugin: "google_sheets",
          allOperations: true,
        },
      ],
      ttlSeconds: 45,
    });
    expect(childToken).toBe("invocation-token-123:child");

    const first = await fromHandle.invoke(
      "github",
      "get_issue",
      {
        issue_number: 42,
      },
      {
        connection: "work",
        instance: "secondary",
      },
    );

    expect(first.status).toBe(207);
    expect(JSON.parse(first.body)).toEqual({
      invocationToken: "invocation-token-123",
      plugin: "github",
      operation: "get_issue",
      params: {
        issue_number: 42,
      },
      connection: "work",
      instance: "secondary",
    });

    const fromRequest = new PluginInvoker(
      request("tok", {}, {}, {}, {}, {}, "invocation-token-456"),
    );
    const second = await fromRequest.invoke("slack", "post_message", {
      channel: "eng",
      text: "hello",
    });

    expect(second.status).toBe(207);
    expect(JSON.parse(second.body)).toEqual({
      invocationToken: "invocation-token-456",
      plugin: "slack",
      operation: "post_message",
      params: {
        channel: "eng",
        text: "hello",
      },
      connection: "",
      instance: "",
    });

    const graphql = await fromRequest.invokeGraphQL(
      "linear",
      "query Viewer($team: String!) { viewer(team: $team) { id } }",
      {
        variables: { team: "eng" },
        connection: "workspace",
      },
    );

    expect(graphql.status).toBe(208);
    expect(JSON.parse(graphql.body)).toEqual({
      invocationToken: "invocation-token-456",
      plugin: "linear",
      document: "query Viewer($team: String!) { viewer(team: $team) { id } }",
      variables: {
        team: "eng",
      },
      connection: "workspace",
      instance: "",
    });

    expect(exchanges).toEqual([
      {
        parentInvocationToken: "invocation-token-123",
        grants: [
          {
            plugin: "github",
            operations: ["get_issue"],
            surfaces: [],
            allOperations: false,
          },
          {
            plugin: "linear",
            operations: [],
            surfaces: ["graphql"],
            allOperations: false,
          },
          {
            plugin: "google_sheets",
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
        plugin: "github",
        operation: "get_issue",
        params: {
          issue_number: 42,
        },
        connection: "work",
        instance: "secondary",
      },
      {
        invocationToken: "invocation-token-456",
        plugin: "slack",
        operation: "post_message",
        params: {
          channel: "eng",
          text: "hello",
        },
        connection: "",
        instance: "",
      },
    ]);

    expect(graphqlCalls).toEqual([
      {
        invocationToken: "invocation-token-456",
        plugin: "linear",
        document: "query Viewer($team: String!) { viewer(team: $team) { id } }",
        variables: {
          team: "eng",
        },
        connection: "workspace",
        instance: "",
      },
    ]);
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_PLUGIN_INVOKER_SOCKET];
    } else {
      process.env[ENV_PLUGIN_INVOKER_SOCKET] = previousSocket;
    }
    if (server.listening) {
      await new Promise<void>((resolve) => {
        server.close(() => resolve());
      });
    }
    removeTempDir(tempDir);
  }
});

test("PluginInvoker prioritizes invocation-token validation over socket configuration", () => {
  const previousSocket = process.env[ENV_PLUGIN_INVOKER_SOCKET];

  try {
    delete process.env[ENV_PLUGIN_INVOKER_SOCKET];
    expect(() => new PluginInvoker("   ")).toThrow(
      "plugin invoker: invocation token is not available",
    );
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_PLUGIN_INVOKER_SOCKET];
    } else {
      process.env[ENV_PLUGIN_INVOKER_SOCKET] = previousSocket;
    }
  }
});
