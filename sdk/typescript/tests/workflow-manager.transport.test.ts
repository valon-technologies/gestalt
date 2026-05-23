import { mkdtempSync } from "node:fs";
import { createServer } from "node:http2";
import { createServer as createNetServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { create } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import { type ServiceImpl } from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";
import { expect, test } from "bun:test";

import {
  BoundWorkflowDefinitionSchema,
  BoundWorkflowEventTriggerSchema,
  BoundWorkflowRunSchema,
  BoundWorkflowScheduleSchema,
  SignalWorkflowRunResponseSchema,
  WorkflowEventSchema,
  WorkflowProvider as WorkflowProviderService,
} from "../src/internal/gen/v1/workflow_pb.ts";
import {
  ENV_HOST_SERVICE_SOCKET,
  ENV_HOST_SERVICE_TOKEN,
  WorkflowManager,
  request,
} from "../src/index.ts";
import { removeTempDir } from "./helpers.ts";

function workflowAppStepTarget(name: string, operation: string) {
  return {
    steps: [{ id: operation, app: { name, operation } }],
  };
}

function workflowAgentStepTarget(provider: string, prompt: string) {
  return {
    steps: [{ id: "agent", agent: { provider, prompt } }],
  };
}

test("WorkflowManager forwards invocation tokens from strings and Request objects", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-workflow-manager-"));
  const socketPath = join(tempDir, "workflow-manager.sock");
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];
  const calls: Array<{
    method: string;
    invocationToken: string;
    idempotencyKey?: string;
    scheduleId?: string;
    triggerId?: string;
    eventType?: string;
    providerName?: string;
    runId?: string;
    definitionId?: string;
    signalName?: string | undefined;
    workflowKey?: string;
  }> = [];

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(WorkflowProviderService, {
        async startRun(input) {
          calls.push({
            method: "start-run",
            invocationToken: input.invocationToken,
            idempotencyKey: input.idempotencyKey,
            providerName: input.providerName,
            workflowKey: input.workflowKey,
          });
          return create(BoundWorkflowRunSchema, {
            providerName: input.providerName || "basic",
            id: "run-1",
            ...(input.target ? { target: input.target } : {}),
          });
        },
        async signalRun(input) {
          calls.push({
            method: "signal-run",
            invocationToken: input.invocationToken,
            runId: input.runId,
            signalName: input.signal?.name,
          });
          return create(SignalWorkflowRunResponseSchema, {
            run: create(BoundWorkflowRunSchema, {
              id: input.runId,
              providerName: "basic",
            }),
            signal: input.signal,
          });
        },
        async signalOrStartRun(input) {
          calls.push({
            method: "signal-or-start-run",
            invocationToken: input.invocationToken,
            idempotencyKey: input.idempotencyKey,
            providerName: input.providerName,
            signalName: input.signal?.name,
            workflowKey: input.workflowKey,
          });
          return create(SignalWorkflowRunResponseSchema, {
            run: create(BoundWorkflowRunSchema, {
              id: "run-2",
              providerName: input.providerName || "basic",
              ...(input.target ? { target: input.target } : {}),
            }),
            signal: input.signal,
            startedRun: true,
            workflowKey: input.workflowKey,
          });
        },
        async createDefinition(input) {
          calls.push({
            method: "create-definition",
            invocationToken: input.invocationToken,
            idempotencyKey: input.idempotencyKey,
            providerName: input.providerName,
          });
          return create(BoundWorkflowDefinitionSchema, {
            providerName: input.providerName || "basic",
            id: "def-1",
            ...(input.target ? { target: input.target } : {}),
          });
        },
        async getDefinition(input) {
          calls.push({
            method: "get-definition",
            invocationToken: input.invocationToken,
            definitionId: input.definitionId,
          });
          return create(BoundWorkflowDefinitionSchema, {
            providerName: "basic",
            id: input.definitionId,
          });
        },
        async updateDefinition(input) {
          calls.push({
            method: "update-definition",
            invocationToken: input.invocationToken,
            definitionId: input.definitionId,
            providerName: input.providerName,
          });
          return create(BoundWorkflowDefinitionSchema, {
            providerName: input.providerName || "basic",
            id: input.definitionId,
            ...(input.target ? { target: input.target } : {}),
          });
        },
        async deleteDefinition(input) {
          calls.push({
            method: "delete-definition",
            invocationToken: input.invocationToken,
            definitionId: input.definitionId,
          });
          return create(EmptySchema, {});
        },
        async upsertSchedule(input) {
          calls.push({
            method: input.scheduleId ? "update" : "create",
            invocationToken: input.invocationToken,
            ...(input.idempotencyKey
              ? { idempotencyKey: input.idempotencyKey }
              : {}),
            ...(input.scheduleId ? { scheduleId: input.scheduleId } : {}),
          });
          return create(BoundWorkflowScheduleSchema, {
            providerName: input.providerName || "basic",
            id: input.scheduleId || "sched-1",
            cron: input.cron,
            timezone: input.timezone,
            paused: input.paused,
            ...(input.target ? { target: input.target } : {}),
          });
        },
        async getSchedule(input) {
          calls.push({
            method: "get",
            invocationToken: input.invocationToken,
            scheduleId: input.scheduleId,
          });
          return create(BoundWorkflowScheduleSchema, {
            providerName: "basic",
            id: input.scheduleId,
          });
        },
        async deleteSchedule(input) {
          calls.push({
            method: "delete",
            invocationToken: input.invocationToken,
            scheduleId: input.scheduleId,
          });
          return create(EmptySchema, {});
        },
        async pauseSchedule(input) {
          calls.push({
            method: "pause",
            invocationToken: input.invocationToken,
            scheduleId: input.scheduleId,
          });
          return create(BoundWorkflowScheduleSchema, {
            providerName: "basic",
            id: input.scheduleId,
            paused: true,
          });
        },
        async resumeSchedule(input) {
          calls.push({
            method: "resume",
            invocationToken: input.invocationToken,
            scheduleId: input.scheduleId,
          });
          return create(BoundWorkflowScheduleSchema, {
            providerName: "basic",
            id: input.scheduleId,
            paused: false,
          });
        },
        async upsertEventTrigger(input) {
          calls.push({
            method: input.triggerId ? "update-trigger" : "create-trigger",
            invocationToken: input.invocationToken,
            ...(input.idempotencyKey
              ? { idempotencyKey: input.idempotencyKey }
              : {}),
            ...(input.triggerId ? { triggerId: input.triggerId } : {}),
          });
          return create(BoundWorkflowEventTriggerSchema, {
            providerName: input.providerName || "basic",
            id: input.triggerId || "trg-1",
            paused: input.paused,
            ...(input.match ? { match: input.match } : {}),
            ...(input.target ? { target: input.target } : {}),
          });
        },
        async getEventTrigger(input) {
          calls.push({
            method: "get-trigger",
            invocationToken: input.invocationToken,
            triggerId: input.triggerId,
          });
          return create(BoundWorkflowEventTriggerSchema, {
            providerName: "basic",
            id: input.triggerId,
          });
        },
        async deleteEventTrigger(input) {
          calls.push({
            method: "delete-trigger",
            invocationToken: input.invocationToken,
            triggerId: input.triggerId,
          });
          return create(EmptySchema, {});
        },
        async pauseEventTrigger(input) {
          calls.push({
            method: "pause-trigger",
            invocationToken: input.invocationToken,
            triggerId: input.triggerId,
          });
          return create(BoundWorkflowEventTriggerSchema, {
            providerName: "basic",
            id: input.triggerId,
            paused: true,
          });
        },
        async resumeEventTrigger(input) {
          calls.push({
            method: "resume-trigger",
            invocationToken: input.invocationToken,
            triggerId: input.triggerId,
          });
          return create(BoundWorkflowEventTriggerSchema, {
            providerName: "basic",
            id: input.triggerId,
            paused: false,
          });
        },
        async publishEvent(input) {
          calls.push({
            method: "publish-event",
            invocationToken: input.invocationToken,
            providerName: input.providerName,
            ...(input.event?.type ? { eventType: input.event.type } : {}),
          });
          return create(WorkflowEventSchema, {
            id: input.event?.id || "evt-1",
            type: input.event?.type || "dummy.event",
            source: input.event?.source || "tests",
            subject: input.event?.subject || "subject",
          });
        },
      } satisfies Partial<ServiceImpl<typeof WorkflowProviderService>>);
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

    const fromHandle = new WorkflowManager("invocation-token-123");
    const created = await fromHandle.createSchedule({
      providerName: "basic",
      cron: "*/5 * * * *",
      timezone: "UTC",
      target: workflowAppStepTarget("roadmap", "sync"),
      paused: false,
      idempotencyKey: "workflow-schedule-key-ts",
    });

    expect(created.providerName).toBe("basic");
    expect(created.schedule?.id).toBe("sched-1");

    const fromRequest = new WorkflowManager(
      request(
        "tok",
        {},
        {},
        {},
        {},
        {},
        "invocation-token-456",
        "workflow-request-key-ts",
      ),
    );
    const startedRun = await fromRequest.startRun({
      providerName: "basic",
      workflowKey: "roadmap-summary:item-1",
      target: workflowAgentStepTarget("simple", "Summarize item 1."),
    });
    const signaledRun = await fromRequest.signalRun({
      runId: "run-1",
      signal: {
        name: "roadmap.item.updated",
      },
    });
    const signaledOrStartedRun = await fromRequest.signalOrStartRun({
      providerName: "basic",
      workflowKey: "roadmap-summary:item-1",
      target: workflowAgentStepTarget("simple", "Summarize item 1."),
      signal: {
        name: "roadmap.item.updated",
      },
    });
    const createdDefinition = await fromRequest.createDefinition({
      providerName: "basic",
      target: workflowAppStepTarget("roadmap", "sync"),
    });
    const fetchedDefinition = await fromRequest.getDefinition({
      definitionId: "def-1",
    });
    const updatedDefinition = await fromRequest.updateDefinition({
      definitionId: "def-1",
      providerName: "secondary",
      target: workflowAppStepTarget("roadmap", "status"),
    });
    await fromRequest.deleteDefinition({ definitionId: "def-1" });
    const fetched = await fromRequest.getSchedule({ scheduleId: "sched-1" });
    const updated = await fromRequest.updateSchedule({
      scheduleId: "sched-1",
      providerName: "secondary",
      cron: "0 * * * *",
      timezone: "America/New_York",
      target: workflowAppStepTarget("roadmap", "status"),
      paused: true,
    });
    const paused = await fromRequest.pauseSchedule({ scheduleId: "sched-1" });
    const resumed = await fromRequest.resumeSchedule({ scheduleId: "sched-1" });
    await fromRequest.deleteSchedule({ scheduleId: "sched-1" });
    const createdTrigger = await fromRequest.createTrigger({
      providerName: "basic",
      match: {
        type: "roadmap.item.updated",
        source: "roadmap",
      },
      target: workflowAppStepTarget("slack", "chat.postMessage"),
      paused: false,
    });
    const fetchedTrigger = await fromRequest.getTrigger({ triggerId: "trg-1" });
    const updatedTrigger = await fromRequest.updateTrigger({
      triggerId: "trg-1",
      providerName: "secondary",
      match: {
        type: "roadmap.item.synced",
      },
      target: workflowAppStepTarget("slack", "chat.postMessage"),
      paused: true,
    });
    const pausedTrigger = await fromRequest.pauseTrigger({
      triggerId: "trg-1",
    });
    const resumedTrigger = await fromRequest.resumeTrigger({
      triggerId: "trg-1",
    });
    await fromRequest.deleteTrigger({ triggerId: "trg-1" });
    const publishedEvent = await fromRequest.publishEvent({
      providerName: "secondary",
      event: {
        type: "roadmap.item.updated",
        source: "roadmap",
      },
    });

    expect(startedRun.run?.id).toBe("run-1");
    expect(signaledRun.signal?.name).toBe("roadmap.item.updated");
    expect(signaledOrStartedRun.startedRun).toBe(true);
    expect(createdDefinition.definition?.id).toBe("def-1");
    expect(fetchedDefinition.definition?.id).toBe("def-1");
    expect(updatedDefinition.providerName).toBe("secondary");
    expect(fetched.schedule?.id).toBe("sched-1");
    expect(updated.providerName).toBe("secondary");
    expect(updated.schedule?.paused).toBe(true);
    expect(paused.schedule?.paused).toBe(true);
    expect(resumed.schedule?.paused).toBe(false);
    expect(createdTrigger.providerName).toBe("basic");
    expect(createdTrigger.trigger?.id).toBe("trg-1");
    expect(fetchedTrigger.trigger?.id).toBe("trg-1");
    expect(updatedTrigger.providerName).toBe("secondary");
    expect(updatedTrigger.trigger?.paused).toBe(true);
    expect(pausedTrigger.trigger?.paused).toBe(true);
    expect(resumedTrigger.trigger?.paused).toBe(false);
    expect(publishedEvent.type).toBe("roadmap.item.updated");
    expect(calls).toEqual([
      {
        method: "create",
        invocationToken: "invocation-token-123",
        idempotencyKey: "workflow-schedule-key-ts",
      },
      {
        method: "start-run",
        invocationToken: "invocation-token-456",
        idempotencyKey: "workflow-request-key-ts",
        providerName: "basic",
        workflowKey: "roadmap-summary:item-1",
      },
      {
        method: "signal-run",
        invocationToken: "invocation-token-456",
        runId: "run-1",
        signalName: "roadmap.item.updated",
      },
      {
        method: "signal-or-start-run",
        invocationToken: "invocation-token-456",
        idempotencyKey: "workflow-request-key-ts",
        providerName: "basic",
        signalName: "roadmap.item.updated",
        workflowKey: "roadmap-summary:item-1",
      },
      {
        method: "create-definition",
        invocationToken: "invocation-token-456",
        idempotencyKey: "workflow-request-key-ts",
        providerName: "basic",
      },
      {
        method: "get-definition",
        invocationToken: "invocation-token-456",
        definitionId: "def-1",
      },
      {
        method: "update-definition",
        invocationToken: "invocation-token-456",
        definitionId: "def-1",
        providerName: "secondary",
      },
      {
        method: "delete-definition",
        invocationToken: "invocation-token-456",
        definitionId: "def-1",
      },
      {
        method: "get",
        invocationToken: "invocation-token-456",
        scheduleId: "sched-1",
      },
      {
        method: "update",
        invocationToken: "invocation-token-456",
        scheduleId: "sched-1",
      },
      {
        method: "pause",
        invocationToken: "invocation-token-456",
        scheduleId: "sched-1",
      },
      {
        method: "resume",
        invocationToken: "invocation-token-456",
        scheduleId: "sched-1",
      },
      {
        method: "delete",
        invocationToken: "invocation-token-456",
        scheduleId: "sched-1",
      },
      {
        method: "create-trigger",
        invocationToken: "invocation-token-456",
        idempotencyKey: "workflow-request-key-ts",
      },
      {
        method: "get-trigger",
        invocationToken: "invocation-token-456",
        triggerId: "trg-1",
      },
      {
        method: "update-trigger",
        invocationToken: "invocation-token-456",
        triggerId: "trg-1",
      },
      {
        method: "pause-trigger",
        invocationToken: "invocation-token-456",
        triggerId: "trg-1",
      },
      {
        method: "resume-trigger",
        invocationToken: "invocation-token-456",
        triggerId: "trg-1",
      },
      {
        method: "delete-trigger",
        invocationToken: "invocation-token-456",
        triggerId: "trg-1",
      },
      {
        method: "publish-event",
        invocationToken: "invocation-token-456",
        providerName: "secondary",
        eventType: "roadmap.item.updated",
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

test("WorkflowManager prioritizes invocation-token validation over socket configuration", () => {
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];

  try {
    delete process.env[ENV_HOST_SERVICE_SOCKET];
    expect(() => new WorkflowManager("   ")).toThrow(
      "workflow manager: invocation token is not available",
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

test("WorkflowManager honors tcp target env and relay token env", async () => {
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];
  const previousToken = process.env[ENV_HOST_SERVICE_TOKEN];
  const seenTokens: string[] = [];
  const address = await reserveTCPAddress();

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(WorkflowProviderService, {
        async upsertSchedule(input) {
          return create(BoundWorkflowScheduleSchema, {
            providerName: input.providerName || "basic",
            id: "sched-1",
            cron: input.cron,
          });
        },
      } satisfies Partial<ServiceImpl<typeof WorkflowProviderService>>);
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

    const manager = new WorkflowManager("invoke-token");
    const created = await manager.createSchedule({
      providerName: "basic",
      cron: "*/5 * * * *",
    });

    expect(created.providerName).toBe("basic");
    expect(created.schedule?.id).toBe("sched-1");
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
