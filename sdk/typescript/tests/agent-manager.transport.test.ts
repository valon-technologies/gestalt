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
  AgentManagerHost as AgentManagerHostService,
  AgentManagerListInteractionsResponseSchema,
  AgentManagerListSessionsResponseSchema,
  AgentManagerListTurnEventsResponseSchema,
  AgentManagerListTurnsResponseSchema,
  AgentSessionSchema,
  AgentSessionState,
  AgentTurnEventSchema,
  AgentTurnSchema,
} from "../gen/v1/agent_pb.ts";
import {
  AgentManager,
  ENV_AGENT_MANAGER_SOCKET,
  ENV_AGENT_MANAGER_SOCKET_TOKEN,
  request,
} from "../src/index.ts";
import { removeTempDir } from "./helpers.ts";

test("AgentManager forwards invocation tokens across session, turn, and interaction calls", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-agent-manager-"));
  const socketPath = join(tempDir, "agent-manager.sock");
  const previousSocket = process.env[ENV_AGENT_MANAGER_SOCKET];
  const calls: Array<{
    method: string;
    invocationToken: string;
    providerName?: string;
    sessionId?: string;
    turnId?: string;
    interactionId?: string;
    reason?: string;
  }> = [];

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(AgentManagerHostService, {
        async createSession(input) {
          calls.push({
            method: "createSession",
            invocationToken: input.invocationToken,
            providerName: input.providerName,
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
          return create(AgentManagerListSessionsResponseSchema, {
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
          return create(AgentManagerListTurnsResponseSchema, {
            turns: [
              create(AgentTurnSchema, {
                id: "turn-1",
                sessionId: input.sessionId,
                providerName: "basic",
                model: "gpt-test",
                status: AgentExecutionStatus.WAITING_FOR_INPUT,
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
          return create(AgentManagerListTurnEventsResponseSchema, {
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
          return create(AgentManagerListInteractionsResponseSchema, {
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
      } satisfies Partial<ServiceImpl<typeof AgentManagerHostService>>);
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

    process.env[ENV_AGENT_MANAGER_SOCKET] = socketPath;

    const fromHandle = new AgentManager("invocation-token-123");
    const session = await fromHandle.createSession({
      providerName: "basic",
      model: "gpt-test",
      clientRef: "cli-session-1",
      metadata: {
        source: "transport-test",
      },
      idempotencyKey: "session-req-1",
    });

    expect(session.id).toBe("session-1");
    expect(session.state).toBe(AgentSessionState.ACTIVE);

    const fromRequest = new AgentManager(
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
          plugin: "statuspage",
          operation: "lookup",
        },
      ],
      idempotencyKey: "turn-req-1",
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
        providerName: "basic",
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
    ]);
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_AGENT_MANAGER_SOCKET];
    } else {
      process.env[ENV_AGENT_MANAGER_SOCKET] = previousSocket;
    }
    if (server.listening) {
      await new Promise<void>((resolve) => {
        server.close(() => resolve());
      });
    }
    removeTempDir(tempDir);
  }
});

test("AgentManager prioritizes invocation-token validation over socket configuration", () => {
  const previousSocket = process.env[ENV_AGENT_MANAGER_SOCKET];

  try {
    delete process.env[ENV_AGENT_MANAGER_SOCKET];
    expect(() => new AgentManager("   ")).toThrow(
      "agent manager: invocation token is not available",
    );
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_AGENT_MANAGER_SOCKET];
    } else {
      process.env[ENV_AGENT_MANAGER_SOCKET] = previousSocket;
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

test("AgentManager honors tcp target env and relay token env", async () => {
  const previousSocket = process.env[ENV_AGENT_MANAGER_SOCKET];
  const previousToken = process.env[ENV_AGENT_MANAGER_SOCKET_TOKEN];
  const seenTokens: string[] = [];
  const address = await reserveTCPAddress();

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(AgentManagerHostService, {
        async createSession(input) {
          return create(AgentSessionSchema, {
            id: "session-1",
            providerName: input.providerName || "basic",
            model: input.model,
            state: AgentSessionState.ACTIVE,
          });
        },
      } satisfies Partial<ServiceImpl<typeof AgentManagerHostService>>);
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

    process.env[ENV_AGENT_MANAGER_SOCKET] = `tcp://${address}`;
    process.env[ENV_AGENT_MANAGER_SOCKET_TOKEN] = "relay-token-typescript";

    const manager = new AgentManager("invoke-token");
    const session = await manager.createSession({
      providerName: "basic",
      model: "gpt-test",
    });

    expect(session.providerName).toBe("basic");
    expect(session.id).toBe("session-1");
    expect(seenTokens).toEqual(["relay-token-typescript"]);
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_AGENT_MANAGER_SOCKET];
    } else {
      process.env[ENV_AGENT_MANAGER_SOCKET] = previousSocket;
    }
    if (previousToken === undefined) {
      delete process.env[ENV_AGENT_MANAGER_SOCKET_TOKEN];
    } else {
      process.env[ENV_AGENT_MANAGER_SOCKET_TOKEN] = previousToken;
    }
    if (server.listening) {
      await new Promise<void>((resolve) => {
        server.close(() => resolve());
      });
    }
  }
});
