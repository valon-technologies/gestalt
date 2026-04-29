import { mkdtempSync } from "node:fs";
import { createServer } from "node:http2";
import { createServer as createNetServer } from "node:net";
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
  ENV_AGENT_HOST_SOCKET_TOKEN,
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

test("AgentHost honors tcp target env and relay token env", async () => {
  const previousSocket = process.env[ENV_AGENT_HOST_SOCKET];
  const previousToken = process.env[ENV_AGENT_HOST_SOCKET_TOKEN];
  const seenTokens: string[] = [];
  const address = await reserveTCPAddress();

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(AgentHostService, {
        async executeTool(input) {
          return create(ExecuteAgentToolResponseSchema, {
            status: 200,
            body: input.toolId,
          });
        },
      } satisfies Partial<ServiceImpl<typeof AgentHostService>>);
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

    process.env[ENV_AGENT_HOST_SOCKET] = `tcp://${address}`;
    process.env[ENV_AGENT_HOST_SOCKET_TOKEN] = "relay-token-typescript";

    const host = new AgentHost();
    const response = await host.executeTool(
      create(ExecuteAgentToolRequestSchema, {
        sessionId: "session-123",
        turnId: "turn-123",
        toolCallId: "call-123",
        toolId: "lookup-status",
      }),
    );

    expect(response.status).toBe(200);
    expect(response.body).toBe("lookup-status");
    expect(seenTokens).toEqual(["relay-token-typescript"]);
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_AGENT_HOST_SOCKET];
    } else {
      process.env[ENV_AGENT_HOST_SOCKET] = previousSocket;
    }
    if (previousToken === undefined) {
      delete process.env[ENV_AGENT_HOST_SOCKET_TOKEN];
    } else {
      process.env[ENV_AGENT_HOST_SOCKET_TOKEN] = previousToken;
    }
    if (server.listening) {
      await new Promise<void>((resolve) => {
        server.close(() => resolve());
      });
    }
  }
});

test("AgentHost executes tools through the configured unix socket", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-agent-host-"));
  const socketPath = join(tempDir, "agent-host.sock");
  const previousSocket = process.env[ENV_AGENT_HOST_SOCKET];
  const calls: Array<{ turnId: string; toolCallId: string; toolId: string }> = [];
  const searches: Array<{
    turnId: string;
    query: string;
    maxResults: number;
    candidateLimit: number;
    loadRefs: Array<{
      plugin: string;
      operation: string;
      connection: string;
      instance: string;
    }>;
  }> = [];

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
            candidateLimit: input.candidateLimit,
            loadRefs: input.loadRefs.map((ref) => ({
              plugin: ref.plugin,
              operation: ref.operation,
              connection: ref.connection,
              instance: ref.instance,
            })),
          });
          return create(SearchAgentToolsResponseSchema, {
            tools: [
              {
                id: "slack.send_message",
                name: "Send Slack message",
                description: "Send a direct message",
              },
            ],
            candidates: [
              {
                ref: {
                  plugin: "slack",
                  operation: "search_messages",
                  connection: "workspace",
                  instance: "primary",
                },
                id: "slack/search_messages/workspace/primary",
                name: "Search Slack messages",
                description: "Search messages",
                parameters: ["query", "channel"],
                score: 12.5,
              },
            ],
            hasMore: true,
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
        candidateLimit: 12,
        loadRefs: [
          {
            plugin: "slack",
            operation: "search_messages",
            connection: "workspace",
            instance: "primary",
          },
        ],
      }),
    );

    expect(searchResponse.tools).toHaveLength(1);
    expect(searchResponse.tools[0]?.id).toBe("slack.send_message");
    expect(searchResponse.tools[0]?.name).toBe("Send Slack message");
    expect(searchResponse.candidates).toHaveLength(1);
    expect(searchResponse.candidates[0]?.ref?.operation).toBe("search_messages");
    expect(searchResponse.hasMore).toBe(true);
    expect(searches).toEqual([
      {
        turnId: "turn-123",
        query: "send slack dm",
        maxResults: 3,
        candidateLimit: 12,
        loadRefs: [
          {
            plugin: "slack",
            operation: "search_messages",
            connection: "workspace",
            instance: "primary",
          },
        ],
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
