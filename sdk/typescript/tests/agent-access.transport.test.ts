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
  AgentExecutionStatus,
  AgentInteractionSchema,
  AgentInteractionState,
  AgentInteractionType,
  AgentProvider as AgentProviderService,
  ListAgentProviderInteractionsResponseSchema,
  ListAgentProviderSessionsResponseSchema,
  ListAgentProviderTurnEventsResponseSchema,
  ListAgentProviderTurnsResponseSchema,
  AgentSessionSchema,
  AgentSessionState,
  AgentTurnEventSchema,
  AgentTurnSchema,
} from "../src/internal/gen/v1/agent_pb.ts";
import {
  Agent,
  ENV_HOST_SERVICE_SOCKET,
  ENV_HOST_SERVICE_TOKEN,
  request,
} from "../src/index.ts";
import { removeTempDir } from "./helpers.ts";

test("Agent forwards invocation tokens across session, turn, and interaction calls", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-agent-"));
  const socketPath = join(tempDir, "agent.sock");
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];
  const calls: Array<{
    method: string;
    invocationToken: string;
    providerName?: string;
    sessionId?: string;
    turnId?: string;
    interactionId?: string;
    reason?: string;
    workflowRunId?: string;
    workflowKeys?: string[];
  }> = [];

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(AgentProviderService, {
        async createSession(input) {
          calls.push({
            method: "createSession",
            invocationToken: input.invocationToken,
            providerName: input.providerName,
            ...(typeof input.workflow?.runId === "string" ? { workflowRunId: input.workflow.runId } : {}),
            ...(input.workflow !== undefined ? { workflowKeys: Object.keys(input.workflow).sort() } : {}),
          });
          return create(AgentSessionSchema, {
            id: "session-1",
            providerName: input.providerName || "basic",
            model: input.model,
            clientRef: input.clientRef,
            state: AgentSessionState.ACTIVE,
            metadata: input.metadata ?? {},
          });
        },
        async getSession(input) {
          calls.push({
            method: "getSession",
            invocationToken: input.invocationToken,
            sessionId: input.sessionId,
          });
          return create(AgentSessionSchema, {
            id: input.sessionId,
            providerName: "basic",
            model: "gpt-test",
            clientRef: "cli-session-1",
            state: AgentSessionState.ACTIVE,
            metadata: {
              source: "transport-test",
            },
          });
        },
        async listSessions(input) {
          calls.push({
            method: "listSessions",
            invocationToken: input.invocationToken,
            providerName: input.providerName,
          });
          return create(ListAgentProviderSessionsResponseSchema, {
            sessions: [
              create(AgentSessionSchema, {
                id: "session-1",
                providerName: input.providerName || "basic",
                model: "gpt-test",
                clientRef: "cli-session-1",
                state: AgentSessionState.ACTIVE,
              }),
            ],
          });
        },
        async updateSession(input) {
          calls.push({
            method: "updateSession",
            invocationToken: input.invocationToken,
            sessionId: input.sessionId,
          });
          return create(AgentSessionSchema, {
            id: input.sessionId,
            providerName: "basic",
            model: "gpt-test",
            clientRef: input.clientRef,
            state: input.state,
            metadata: input.metadata ?? {},
          });
        },
        async createTurn(input) {
          calls.push({
            method: "createTurn",
            invocationToken: input.invocationToken,
            sessionId: input.sessionId,
          });
          return create(AgentTurnSchema, {
            id: "turn-1",
            sessionId: input.sessionId,
            providerName: "basic",
            model: input.model,
            status: AgentExecutionStatus.WAITING_FOR_INPUT,
            messages: input.messages,
            output: { case: "text", value: { text: "pending" } },
            statusMessage: "waiting for input",
            executionRef: "exec-turn-1",
          });
        },
        async getTurn(input) {
          calls.push({
            method: "getTurn",
            invocationToken: input.invocationToken,
            turnId: input.turnId,
          });
          return create(AgentTurnSchema, {
            id: input.turnId,
            sessionId: "session-1",
            providerName: "basic",
            model: "gpt-test",
            status: AgentExecutionStatus.WAITING_FOR_INPUT,
            output: { case: "text", value: { text: "pending" } },
            statusMessage: "waiting for input",
            executionRef: "exec-turn-1",
          });
        },
        async listTurns(input) {
          calls.push({
            method: "listTurns",
            invocationToken: input.invocationToken,
            sessionId: input.sessionId,
          });
          return create(ListAgentProviderTurnsResponseSchema, {
            turns: [
              create(AgentTurnSchema, {
                id: "turn-1",
                sessionId: input.sessionId,
                providerName: "basic",
                model: "gpt-test",
                status: AgentExecutionStatus.WAITING_FOR_INPUT,
                output: { case: "text", value: { text: "pending" } },
                statusMessage: "waiting for input",
                executionRef: "exec-turn-1",
              }),
            ],
          });
        },
        async cancelTurn(input) {
          calls.push({
            method: "cancelTurn",
            invocationToken: input.invocationToken,
            turnId: input.turnId,
            reason: input.reason,
          });
          return create(AgentTurnSchema, {
            id: input.turnId,
            sessionId: "session-1",
            providerName: "basic",
            model: "gpt-test",
            status: AgentExecutionStatus.CANCELED,
            statusMessage: input.reason,
            executionRef: "exec-turn-1",
          });
        },
        async listTurnEvents(input) {
          calls.push({
            method: "listTurnEvents",
            invocationToken: input.invocationToken,
            turnId: input.turnId,
          });
          return create(ListAgentProviderTurnEventsResponseSchema, {
            events: [
              create(AgentTurnEventSchema, {
                id: "event-1",
                turnId: input.turnId,
                seq: 1n,
                type: "turn.started",
                source: "basic",
                visibility: "private",
              }),
            ],
          });
        },
        async listInteractions(input) {
          calls.push({
            method: "listInteractions",
            invocationToken: input.invocationToken,
            turnId: input.turnId,
          });
          return create(ListAgentProviderInteractionsResponseSchema, {
            interactions: [
              create(AgentInteractionSchema, {
                id: "interaction-1",
                turnId: input.turnId,
                sessionId: "session-1",
                type: AgentInteractionType.APPROVAL,
                state: AgentInteractionState.PENDING,
                title: "Approve command",
              }),
            ],
          });
        },
        async resolveInteraction(input) {
          calls.push({
            method: "resolveInteraction",
            invocationToken: input.invocationToken,
            turnId: input.turnId,
            interactionId: input.interactionId,
          });
          return create(AgentInteractionSchema, {
            id: input.interactionId,
            turnId: input.turnId,
            sessionId: "session-1",
            type: AgentInteractionType.APPROVAL,
            state: AgentInteractionState.RESOLVED,
            title: "Approve command",
            resolution: input.resolution ?? {},
          });
        },
      } satisfies Partial<ServiceImpl<typeof AgentProviderService>>);
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

    const fromHandle = new Agent("invocation-token-123");
    const session = await fromHandle.createSession({
      model: "gpt-test",
      clientRef: "cli-session-1",
      metadata: {
        source: "transport-test",
      },
      idempotencyKey: "session-req-1",
    });

    expect(session.id).toBe("session-1");
    expect(session.state).toBe(AgentSessionState.ACTIVE);

    const fromRequest = new Agent(
      request("tok", {}, {}, {}, {}, {}, "invocation-token-456"),
    );
    const fetchedSession = await fromRequest.getSession({ sessionId: "session-1" });
    const listedSessions = await fromRequest.listSessions({ providerName: "basic" });
    const updatedSession = await fromRequest.updateSession({
      sessionId: "session-1",
      clientRef: "cli-session-2",
      state: AgentSessionState.ARCHIVED,
      metadata: {
        source: "transport-test-updated",
      },
    });
    const turn = await fromRequest.createTurn({
      sessionId: "session-1",
      model: "gpt-test",
      messages: [
        {
          role: "user",
          text: "Summarize incidents",
        },
      ],
      toolRefs: [
        {
          app: "statuspage",
          operation: "lookup",
        },
      ],
      output: { text: {} },
      idempotencyKey: "turn-req-1",
      timeoutSeconds: 120,
    });
    const fetchedTurn = await fromRequest.getTurn({ turnId: "turn-1" });
    const listedTurns = await fromRequest.listTurns({ sessionId: "session-1" });
    const events = await fromRequest.listTurnEvents({
      turnId: "turn-1",
      afterSeq: 0n,
      limit: 10,
    });
    const interactions = await fromRequest.listInteractions({ turnId: "turn-1" });
    const resolvedInteraction = await fromRequest.resolveInteraction({
      turnId: "turn-1",
      interactionId: "interaction-1",
      resolution: {
        approved: true,
      },
    });
    const canceledTurn = await fromRequest.cancelTurn({
      turnId: "turn-1",
      reason: "user requested cancellation",
    });
    const fromWorkflow = new Agent(
      request("", {}, {}, {}, {}, {
        provider: "local",
        runId: "run-123",
        runAs: { id: "service_account:caller-supplied" },
        trigger: { event: { data: { ignored: true } } },
      }),
    );
    await fromWorkflow.createSession({
      providerName: "workflow-agent",
      model: "gpt-test",
      clientRef: "workflow-session",
    });

    expect(fetchedSession.metadata).toEqual({ source: "transport-test" });
    expect(listedSessions.map((entry) => entry.id)).toEqual(["session-1"]);
    expect(updatedSession.clientRef).toBe("cli-session-2");
    expect(updatedSession.state).toBe(AgentSessionState.ARCHIVED);
    expect(turn.id).toBe("turn-1");
    expect(turn.status).toBe(AgentExecutionStatus.WAITING_FOR_INPUT);
    expect(fetchedTurn.statusMessage).toBe("waiting for input");
    expect(listedTurns.map((entry) => entry.id)).toEqual(["turn-1"]);
    expect(events.map((entry) => entry.type)).toEqual(["turn.started"]);
    expect(interactions.map((entry) => entry.id)).toEqual(["interaction-1"]);
    expect(resolvedInteraction.state).toBe(AgentInteractionState.RESOLVED);
    expect(resolvedInteraction.resolution).toEqual({ approved: true });
    expect(canceledTurn.status).toBe(AgentExecutionStatus.CANCELED);
    expect(canceledTurn.statusMessage).toBe("user requested cancellation");
    expect(calls).toEqual([
      {
        method: "createSession",
        invocationToken: "invocation-token-123",
        providerName: "",
      },
      {
        method: "getSession",
        invocationToken: "invocation-token-456",
        sessionId: "session-1",
      },
      {
        method: "listSessions",
        invocationToken: "invocation-token-456",
        providerName: "basic",
      },
      {
        method: "updateSession",
        invocationToken: "invocation-token-456",
        sessionId: "session-1",
      },
      {
        method: "createTurn",
        invocationToken: "invocation-token-456",
        sessionId: "session-1",
      },
      {
        method: "getTurn",
        invocationToken: "invocation-token-456",
        turnId: "turn-1",
      },
      {
        method: "listTurns",
        invocationToken: "invocation-token-456",
        sessionId: "session-1",
      },
      {
        method: "listTurnEvents",
        invocationToken: "invocation-token-456",
        turnId: "turn-1",
      },
      {
        method: "listInteractions",
        invocationToken: "invocation-token-456",
        turnId: "turn-1",
      },
      {
        method: "resolveInteraction",
        invocationToken: "invocation-token-456",
        turnId: "turn-1",
        interactionId: "interaction-1",
      },
      {
        method: "cancelTurn",
        invocationToken: "invocation-token-456",
        turnId: "turn-1",
        reason: "user requested cancellation",
      },
      {
        method: "createSession",
        invocationToken: "",
        providerName: "workflow-agent",
        workflowRunId: "run-123",
        workflowKeys: ["provider", "providerName", "runId"],
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

test("Agent still requires host service socket configuration", () => {
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];

  try {
    delete process.env[ENV_HOST_SERVICE_SOCKET];
    expect(() => new Agent("   ")).toThrow("agent: GESTALT_HOST_SERVICE_SOCKET is not set");
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

test("Agent honors tcp target env and relay token env", async () => {
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];
  const previousToken = process.env[ENV_HOST_SERVICE_TOKEN];
  const seenTokens: string[] = [];
  const address = await reserveTCPAddress();

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(AgentProviderService, {
        async createSession(input) {
          return create(AgentSessionSchema, {
            id: "session-1",
            providerName: input.providerName || "basic",
            model: input.model,
            state: AgentSessionState.ACTIVE,
          });
        },
      } satisfies Partial<ServiceImpl<typeof AgentProviderService>>);
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

    const manager = new Agent("invoke-token");
    const session = await manager.createSession({
      providerName: "basic",
      model: "gpt-test",
    });

    expect(session.providerName).toBe("basic");
    expect(session.id).toBe("session-1");
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
