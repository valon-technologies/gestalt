import { mkdtempSync } from "node:fs";
import { createServer } from "node:http2";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { create } from "@bufbuild/protobuf";
import { type ServiceImpl } from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";
import { expect, test } from "bun:test";

import {
  OperationResultSchema,
  PluginInvoker as PluginInvokerService,
} from "../gen/v1/plugin_pb.ts";
import {
  ENV_PLUGIN_INVOKER_SOCKET,
  PluginInvoker,
  request,
} from "../src/index.ts";
import { removeTempDir } from "./helpers.ts";

test("PluginInvoker forwards request handles from strings and Request objects", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-plugin-invoker-"));
  const socketPath = join(tempDir, "plugin-invoker.sock");
  const previousSocket = process.env[ENV_PLUGIN_INVOKER_SOCKET];
  const calls: Array<{
    requestHandle: string;
    plugin: string;
    operation: string;
    params: Record<string, unknown>;
    connection: string;
    instance: string;
  }> = [];

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(
        PluginInvokerService,
        {
          async invoke(input) {
            const params = Object.fromEntries(Object.entries(input.params ?? {}));
            calls.push({
              requestHandle: input.requestHandle,
              plugin: input.plugin,
              operation: input.operation,
              params,
              connection: input.connection,
              instance: input.instance,
            });
            return create(OperationResultSchema, {
              status: 207,
              body: JSON.stringify({
                requestHandle: input.requestHandle,
                plugin: input.plugin,
                operation: input.operation,
                params,
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

    const fromHandle = new PluginInvoker("request-handle-123");
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
      requestHandle: "request-handle-123",
      plugin: "github",
      operation: "get_issue",
      params: {
        issue_number: 42,
      },
      connection: "work",
      instance: "secondary",
    });

    const fromRequest = new PluginInvoker(
      request("tok", {}, {}, {}, {}, "request-handle-456"),
    );
    const second = await fromRequest.invoke("slack", "post_message", {
      channel: "eng",
      text: "hello",
    });

    expect(second.status).toBe(207);
    expect(JSON.parse(second.body)).toEqual({
      requestHandle: "request-handle-456",
      plugin: "slack",
      operation: "post_message",
      params: {
        channel: "eng",
        text: "hello",
      },
      connection: "",
      instance: "",
    });

    expect(calls).toEqual([
      {
        requestHandle: "request-handle-123",
        plugin: "github",
        operation: "get_issue",
        params: {
          issue_number: 42,
        },
        connection: "work",
        instance: "secondary",
      },
      {
        requestHandle: "request-handle-456",
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

test("PluginInvoker prioritizes request-handle validation over socket configuration", () => {
  const previousSocket = process.env[ENV_PLUGIN_INVOKER_SOCKET];

  try {
    delete process.env[ENV_PLUGIN_INVOKER_SOCKET];
    expect(() => new PluginInvoker("   ")).toThrow(
      "plugin invoker: request handle is not available",
    );
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_PLUGIN_INVOKER_SOCKET];
    } else {
      process.env[ENV_PLUGIN_INVOKER_SOCKET] = previousSocket;
    }
  }
});
