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
  AgentProvider as AgentProviderService,
  AgentSessionSchema,
  AgentSessionState,
  AgentTurnSchema,
  ListAgentProviderSessionsResponseSchema,
} from "../src/internal/gen/v1/agent_pb.ts";
import {
  RequestContextSchema,
  SubjectContextSchema,
} from "../src/internal/gen/v1/app_pb.ts";
import {
  Agent,
  ENV_HOST_SERVICE_SOCKET,
  ENV_HOST_SERVICE_TOKEN,
  request,
} from "../src/index.ts";
import { structFromObject } from "../src/protocol.ts";
import { removeTempDir } from "./helpers.ts";

test("Agent forwards request context across session and turn calls", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-agent-"));
  const socketPath = join(tempDir, "agent.sock");
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];
  const calls: Array<{
    method: string;
    subjectId: string;
    providerName?: string;
    sessionId?: string;
    workflowRunId?: string;
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
            subjectId: input.context?.subject?.id ?? "",
            providerName: input.providerName,
            ...(typeof input.context?.workflow?.runId === "string"
              ? { workflowRunId: input.context.workflow.runId }
              : {}),
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
            subjectId: input.context?.subject?.id ?? "",
            sessionId: input.sessionId,
          });
          return create(AgentSessionSchema, {
            id: input.sessionId,
            providerName: "basic",
            model: "gpt-test",
            clientRef: "cli-session-1",
            state: AgentSessionState.ACTIVE,
          });
        },
        async listSessions(input) {
          calls.push({
            method: "listSessions",
            subjectId: input.context?.subject?.id ?? "",
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
        async createTurn(input) {
          calls.push({
            method: "createTurn",
            subjectId: input.context?.subject?.id ?? "",
            sessionId: input.sessionId,
          });
          return create(AgentTurnSchema, {
            id: "turn-1",
            sessionId: input.sessionId,
            providerName: "basic",
            model: input.model,
            status: AgentExecutionStatus.WAITING_FOR_INPUT,
            output: { case: "text", value: { text: "pending" } },
            statusMessage: "waiting for input",
            executionRef: "exec-turn-1",
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
    const context = create(RequestContextSchema, {
      subject: create(SubjectContextSchema, {
        id: "user:user-123",
      }),
      workflow: structFromObject({
        provider: "local",
        runId: "run-123",
      }),
    });
    const agent = new Agent(
      request("", {}, {}, {}, {}, {}, "", {}, {}, [], false, context),
    );

    const session = await agent.createSession({
      providerName: "workflow-agent",
      model: "gpt-test",
      clientRef: "workflow-session",
    });
    const fetchedSession = await agent.getSession({ sessionId: "session-1" });
    const listedSessions = await agent.listSessions({ providerName: "basic" });
    const turn = await agent.createTurn({
      sessionId: "session-1",
      model: "gpt-test",
      messages: [{ role: "user", text: "Summarize incidents" }],
      output: { text: {} },
      timeoutSeconds: 120,
    });

    expect(session.id).toBe("session-1");
    expect(fetchedSession.id).toBe("session-1");
    expect(listedSessions.map((entry) => entry.id)).toEqual(["session-1"]);
    expect(turn.status).toBe(AgentExecutionStatus.WAITING_FOR_INPUT);
    expect(calls).toEqual([
      {
        method: "createSession",
        subjectId: "user:user-123",
        providerName: "workflow-agent",
        workflowRunId: "run-123",
      },
      {
        method: "getSession",
        subjectId: "user:user-123",
        sessionId: "session-1",
      },
      {
        method: "listSessions",
        subjectId: "user:user-123",
        providerName: "basic",
      },
      {
        method: "createTurn",
        subjectId: "user:user-123",
        sessionId: "session-1",
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
    expect(() => new Agent(request())).toThrow("agent: GESTALT_HOST_SERVICE_SOCKET is not set");
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
        async getSession(input) {
          return create(AgentSessionSchema, {
            id: input.sessionId,
            providerName: "basic",
            model: "gpt-test",
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

    const agent = new Agent(request());
    const response = await agent.getSession({ sessionId: "session-1" });

    expect(response.id).toBe("session-1");
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
