import { mkdtempSync } from "node:fs";
import { createServer } from "node:http2";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { create } from "@bufbuild/protobuf";
import { type ServiceImpl } from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";
import { expect, test } from "bun:test";

import {
  AgentHost as AgentHostService,
  ExecuteAgentToolRequestSchema,
  ExecuteAgentToolResponseSchema,
} from "../gen/v1/agent_pb.ts";
import { AgentHost, ENV_AGENT_HOST_SOCKET } from "../src/index.ts";
import { removeTempDir } from "./helpers.ts";

test("AgentHost executes tools through the configured unix socket", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-agent-host-"));
  const socketPath = join(tempDir, "agent-host.sock");
  const previousSocket = process.env[ENV_AGENT_HOST_SOCKET];
  const calls: Array<{ runId: string; toolCallId: string; toolId: string }> = [];

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(AgentHostService, {
        async executeTool(input) {
          calls.push({
            runId: input.runId,
            toolCallId: input.toolCallId,
            toolId: input.toolId,
          });
          return create(ExecuteAgentToolResponseSchema, {
            status: 207,
            body: JSON.stringify({
              arguments: input.arguments,
              toolId: input.toolId,
            }),
          });
        },
      } satisfies Partial<ServiceImpl<typeof AgentHostService>>);
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

    process.env[ENV_AGENT_HOST_SOCKET] = socketPath;

    const host = new AgentHost();
    const response = await host.executeTool(
      create(ExecuteAgentToolRequestSchema, {
        runId: "run-123",
        toolCallId: "call-123",
        toolId: "lookup-status",
        arguments: {
          deployment: "blue",
        },
      }),
    );

    expect(response.status).toBe(207);
    expect(JSON.parse(response.body)).toEqual({
      arguments: {
        deployment: "blue",
      },
      toolId: "lookup-status",
    });
    expect(calls).toEqual([
      {
        runId: "run-123",
        toolCallId: "call-123",
        toolId: "lookup-status",
      },
    ]);
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_AGENT_HOST_SOCKET];
    } else {
      process.env[ENV_AGENT_HOST_SOCKET] = previousSocket;
    }
    if (server.listening) {
      await new Promise<void>((resolve) => {
        server.close(() => resolve());
      });
    }
    removeTempDir(tempDir);
  }
});
