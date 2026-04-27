import { mkdtempSync } from "node:fs";
import { createServer } from "node:http2";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { create, toJson } from "@bufbuild/protobuf";
import { type ServiceImpl } from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";
import { expect, test } from "bun:test";

import {
  AgentTurnEventSchema,
  AgentHost as AgentHostService,
  ExecuteAgentToolRequestSchema,
  ExecuteAgentToolResponseSchema,
  ListAgentProviderTurnEventsRequestSchema,
  SearchAgentToolsRequestSchema,
  SearchAgentToolsResponseSchema,
} from "../gen/v1/agent_pb.ts";
import {
  AgentHost,
  createAgentProviderService,
  defineAgentProvider,
  ENV_AGENT_HOST_SOCKET,
} from "../src/index.ts";
import { removeTempDir } from "./helpers.ts";

test("AgentProvider accepts JSON display payloads for turn events", async () => {
  const provider = defineAgentProvider({
    displayName: "Agent transport fixture",
    async listTurnEvents() {
      return [
        {
          id: "event-1",
          turnId: "turn-1",
          seq: 1n,
          type: "tool.started",
          source: "fixture-agent",
          visibility: "public",
          display: {
            kind: "tool",
            phase: "started",
            label: "Search docs",
            ref: "call-1",
            input: {
              query: "docs",
            },
            output: ["hit-1"],
            error: "none",
          },
        },
      ];
    },
  });
  const service = createAgentProviderService(provider);
  const response = await (service.listTurnEvents as any)(
    create(ListAgentProviderTurnEventsRequestSchema, {
      turnId: "turn-1",
      afterSeq: 0n,
      limit: 10,
    }),
  );

  const event = response.events[0]!;
  expect(event?.display?.input?.kind.case).toBe("structValue");
  expect(event?.display?.output?.kind.case).toBe("listValue");
  expect(event?.display?.error?.kind.case).toBe("stringValue");
  expect(toJson(AgentTurnEventSchema, event!)).toEqual({
    id: "event-1",
    turnId: "turn-1",
    seq: "1",
    type: "tool.started",
    source: "fixture-agent",
    visibility: "public",
    display: {
      kind: "tool",
      phase: "started",
      label: "Search docs",
      ref: "call-1",
      input: {
        query: "docs",
      },
      output: ["hit-1"],
      error: "none",
    },
  });
});

test("AgentHost executes tools through the configured unix socket", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-agent-host-"));
  const socketPath = join(tempDir, "agent-host.sock");
  const previousSocket = process.env[ENV_AGENT_HOST_SOCKET];
  const calls: Array<{ turnId: string; toolCallId: string; toolId: string }> = [];
  const searches: Array<{ turnId: string; query: string; maxResults: number }> = [];

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(AgentHostService, {
        async executeTool(input) {
          calls.push({
            turnId: input.turnId,
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
        async searchTools(input) {
          searches.push({
            turnId: input.turnId,
            query: input.query,
            maxResults: input.maxResults,
          });
          return create(SearchAgentToolsResponseSchema, {
            tools: [
              {
                id: "slack.send_message",
                name: "Send Slack message",
                description: "Send a direct message",
                target: {
                  plugin: "slack",
                  operation: "send_message",
                },
              },
            ],
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
        sessionId: "session-123",
        turnId: "turn-123",
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
        turnId: "turn-123",
        toolCallId: "call-123",
        toolId: "lookup-status",
      },
    ]);

    const searchResponse = await host.searchTools(
      create(SearchAgentToolsRequestSchema, {
        sessionId: "session-123",
        turnId: "turn-123",
        query: "send slack dm",
        maxResults: 3,
      }),
    );

    expect(searchResponse.tools).toHaveLength(1);
    expect(searchResponse.tools[0]?.target?.plugin).toBe("slack");
    expect(searchResponse.tools[0]?.target?.operation).toBe("send_message");
    expect(searches).toEqual([
      {
        turnId: "turn-123",
        query: "send slack dm",
        maxResults: 3,
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
