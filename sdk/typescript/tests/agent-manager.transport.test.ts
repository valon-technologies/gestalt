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
  AgentManagerHost as AgentManagerHostService,
  AgentManagerListRunsResponseSchema,
  AgentRunStatus,
  BoundAgentRunSchema,
  ManagedAgentRunSchema,
} from "../gen/v1/agent_pb.ts";
import {
  AgentManager,
  ENV_AGENT_MANAGER_SOCKET,
  ENV_AGENT_MANAGER_SOCKET_TOKEN,
  request,
} from "../src/index.ts";
import { removeTempDir } from "./helpers.ts";

test("AgentManager forwards invocation tokens from strings and Request objects", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-agent-manager-"));
  const socketPath = join(tempDir, "agent-manager.sock");
  const previousSocket = process.env[ENV_AGENT_MANAGER_SOCKET];
  const calls: Array<{
    method: string;
    invocationToken: string;
    runId?: string;
  }> = [];

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(AgentManagerHostService, {
        async run(input) {
          calls.push({
            method: "run",
            invocationToken: input.invocationToken,
          });
          return create(ManagedAgentRunSchema, {
            providerName: input.providerName || "basic",
            run: create(BoundAgentRunSchema, {
              id: "run-1",
              providerName: input.providerName || "basic",
              model: input.model,
              status: AgentRunStatus.PENDING,
              messages: input.messages,
              sessionRef: input.sessionRef,
            }),
          });
        },
        async getRun(input) {
          calls.push({
            method: "get",
            invocationToken: input.invocationToken,
            runId: input.runId,
          });
          return create(ManagedAgentRunSchema, {
            providerName: "basic",
            run: create(BoundAgentRunSchema, {
              id: input.runId,
              providerName: "basic",
              status: AgentRunStatus.RUNNING,
            }),
          });
        },
        async listRuns(input) {
          calls.push({
            method: "list",
            invocationToken: input.invocationToken,
          });
          return create(AgentManagerListRunsResponseSchema, {
            runs: [
              create(ManagedAgentRunSchema, {
                providerName: "basic",
                run: create(BoundAgentRunSchema, {
                  id: "run-1",
                  providerName: "basic",
                  status: AgentRunStatus.RUNNING,
                }),
              }),
            ],
          });
        },
        async cancelRun(input) {
          calls.push({
            method: "cancel",
            invocationToken: input.invocationToken,
            runId: input.runId,
          });
          return create(ManagedAgentRunSchema, {
            providerName: "basic",
            run: create(BoundAgentRunSchema, {
              id: input.runId,
              providerName: "basic",
              status: AgentRunStatus.CANCELED,
              statusMessage: input.reason,
            }),
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
    const started = await fromHandle.run({
      providerName: "basic",
      model: "gpt-test",
      messages: [
        {
          role: "user",
          text: "Summarize incidents",
        },
      ],
      toolRefs: [
        {
          pluginName: "statuspage",
          operation: "lookup",
        },
      ],
      sessionRef: "session-123",
    });

    expect(started.providerName).toBe("basic");
    expect(started.run?.id).toBe("run-1");

    const fromRequest = new AgentManager(
      request("tok", {}, {}, {}, {}, {}, "invocation-token-456"),
    );
    const fetched = await fromRequest.getRun({ runId: "run-1" });
    const listed = await fromRequest.listRuns();
    const canceled = await fromRequest.cancelRun({
      runId: "run-1",
      reason: "user requested cancellation",
    });

    expect(fetched.run?.id).toBe("run-1");
    expect(listed.map((entry) => entry.run?.id)).toEqual(["run-1"]);
    expect(canceled.run?.status).toBe(AgentRunStatus.CANCELED);
    expect(canceled.run?.statusMessage).toBe("user requested cancellation");
    expect(calls).toEqual([
      {
        method: "run",
        invocationToken: "invocation-token-123",
      },
      {
        method: "get",
        invocationToken: "invocation-token-456",
        runId: "run-1",
      },
      {
        method: "list",
        invocationToken: "invocation-token-456",
      },
      {
        method: "cancel",
        invocationToken: "invocation-token-456",
        runId: "run-1",
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
        async run(input) {
          return create(ManagedAgentRunSchema, {
            providerName: input.providerName || "basic",
            run: create(BoundAgentRunSchema, {
              id: "run-1",
              providerName: input.providerName || "basic",
              model: input.model,
              status: AgentRunStatus.PENDING,
            }),
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
    const started = await manager.run({
      providerName: "basic",
      model: "gpt-test",
      messages: [],
      toolRefs: [],
    });

    expect(started.providerName).toBe("basic");
    expect(started.run?.id).toBe("run-1");
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
