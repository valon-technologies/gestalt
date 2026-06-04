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
  GetWorkflowProviderRunEventsResponseSchema,
  GetWorkflowProviderRunOutputResponseSchema,
  ListWorkflowProviderDefinitionsResponseSchema,
  ListWorkflowProviderRunsResponseSchema,
  SignalWorkflowRunResponseSchema,
  WorkflowDefinitionSchema,
  WorkflowEventSchema,
  WorkflowProvider as WorkflowProviderService,
  WorkflowRunEventSchema,
  WorkflowRunSchema,
} from "../src/internal/gen/v1/workflow_pb.ts";
import { valueFromJson } from "../src/protocol.ts";
import {
  ENV_HOST_SERVICE_SOCKET,
  ENV_HOST_SERVICE_TOKEN,
  Workflow,
  WorkflowRunStatus,
  request,
} from "../src/index.ts";
import { removeTempDir } from "./helpers.ts";

function workflowAppStepTarget(name: string, operation: string) {
  return {
    steps: [{ id: operation, app: { name, operation } }],
  };
}

test("Workflow forwards invocation tokens from strings and Request objects", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-workflow-"));
  const socketPath = join(tempDir, "workflow.sock");
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];
  const calls: Array<{
    method: string;
    invocationToken: string;
    idempotencyKey?: string;
    providerName?: string;
    runId?: string;
    definitionId?: string;
    activationId?: string;
    signalName?: string | undefined;
    workflowKey?: string;
    eventType?: string;
    paused?: boolean;
  }> = [];

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(WorkflowProviderService, {
        async applyDefinition(input) {
          calls.push({
            method: "apply-definition",
            invocationToken: input.invocationToken,
            idempotencyKey: input.idempotencyKey,
            providerName: input.providerName,
            ...(input.spec?.id ? { definitionId: input.spec.id } : {}),
          });
          return create(WorkflowDefinitionSchema, {
            providerName: input.providerName || "basic",
            id: input.spec?.id || "def-1",
            generation: 3n,
            target: input.spec?.target,
            activations: input.spec?.activations ?? [],
          });
        },
        async getDefinition(input) {
          calls.push({
            method: "get-definition",
            invocationToken: input.invocationToken,
            definitionId: input.definitionId,
          });
          return create(WorkflowDefinitionSchema, {
            providerName: "basic",
            id: input.definitionId,
            generation: 3n,
          });
        },
        async listDefinitions(input) {
          calls.push({
            method: "list-definitions",
            invocationToken: input.invocationToken,
          });
          return create(ListWorkflowProviderDefinitionsResponseSchema, {
            definitions: [create(WorkflowDefinitionSchema, { id: "def-1", generation: 3n })],
          });
        },
        async setDefinitionPaused(input) {
          calls.push({
            method: "set-definition-paused",
            invocationToken: input.invocationToken,
            definitionId: input.definitionId,
            paused: input.paused,
          });
          return create(WorkflowDefinitionSchema, {
            id: input.definitionId,
            generation: 3n,
            paused: input.paused,
          });
        },
        async setActivationPaused(input) {
          calls.push({
            method: "set-activation-paused",
            invocationToken: input.invocationToken,
            definitionId: input.definitionId,
            activationId: input.activationId,
            paused: input.paused,
          });
          return create(WorkflowDefinitionSchema, {
            id: input.definitionId,
            generation: 3n,
            activations: [{ id: input.activationId, paused: input.paused }],
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
        async startRun(input) {
          calls.push({
            method: "start-run",
            invocationToken: input.invocationToken,
            idempotencyKey: input.idempotencyKey,
            providerName: input.providerName,
            workflowKey: input.workflowKey,
            definitionId: input.definitionId,
          });
          return create(WorkflowRunSchema, {
            providerName: input.providerName || "basic",
            id: "run-1",
            status: WorkflowRunStatus.PENDING,
            definitionId: input.definitionId,
            workflowKey: input.workflowKey,
            input: input.input,
          });
        },
        async getRun(input) {
          calls.push({
            method: "get-run",
            invocationToken: input.invocationToken,
            runId: input.runId,
          });
          return create(WorkflowRunSchema, {
            providerName: "basic",
            id: input.runId,
            status: WorkflowRunStatus.RUNNING,
          });
        },
        async listRuns(input) {
          calls.push({
            method: "list-runs",
            invocationToken: input.invocationToken,
          });
          return create(ListWorkflowProviderRunsResponseSchema, {
            runs: [create(WorkflowRunSchema, { id: "run-1", status: WorkflowRunStatus.RUNNING })],
            nextPageToken: "next",
          });
        },
        async getRunEvents(input) {
          calls.push({
            method: "get-run-events",
            invocationToken: input.invocationToken,
            runId: input.runId,
          });
          return create(GetWorkflowProviderRunEventsResponseSchema, {
            events: [create(WorkflowRunEventSchema, {
              id: "evt-1",
              runId: input.runId,
              stepId: "sync",
              type: "run.started",
            })],
          });
        },
        async getRunOutput(input) {
          calls.push({
            method: "get-run-output",
            invocationToken: input.invocationToken,
            runId: input.runId,
          });
          return create(GetWorkflowProviderRunOutputResponseSchema, {
            output: valueFromJson({ ok: true, runId: input.runId }),
          });
        },
        async cancelRun(input) {
          calls.push({
            method: "cancel-run",
            invocationToken: input.invocationToken,
            runId: input.runId,
          });
          return create(WorkflowRunSchema, {
            id: input.runId,
            status: WorkflowRunStatus.CANCELED,
            statusMessage: input.reason,
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
            run: create(WorkflowRunSchema, {
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
            definitionId: input.definitionId,
          });
          return create(SignalWorkflowRunResponseSchema, {
            run: create(WorkflowRunSchema, {
              id: "run-2",
              providerName: input.providerName || "basic",
              definitionId: input.definitionId,
              workflowKey: input.workflowKey,
              input: input.input,
            }),
            signal: input.signal,
            startedRun: true,
            workflowKey: input.workflowKey,
          });
        },
        async deliverEvent(input) {
          calls.push({
            method: "deliver-event",
            invocationToken: input.invocationToken,
            providerName: input.providerName,
            ...(input.event?.type ? { eventType: input.event.type } : {}),
          });
          return create(WorkflowEventSchema, {
            id: input.event?.id || "evt-1",
            type: input.event?.type || "dummy.event",
            source: input.event?.source || input.appName || "tests",
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

    const fromHandle = new Workflow("invocation-token-123");
    const applied = await fromHandle.applyDefinition({
      providerName: "basic",
      spec: {
        id: "def-1",
        target: workflowAppStepTarget("roadmap", "sync"),
        activations: [{
          id: "nightly",
          schedule: { cron: "*/5 * * * *", timezone: "UTC" },
        }],
      },
      idempotencyKey: "workflow-definition-key-ts",
    });
    expect(applied.id).toBe("def-1");

    const fromRequest = new Workflow(
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
      definitionId: "def-1",
      input: { itemId: "item-1" },
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
      definitionId: "def-1",
      input: { itemId: "item-1" },
      signal: {
        name: "roadmap.item.updated",
      },
    });
    const fetchedDefinition = await fromRequest.getDefinition({
      definitionId: "def-1",
    });
    const definitions = await fromRequest.listDefinitions();
    const pausedDefinition = await fromRequest.setDefinitionPaused({
      definitionId: "def-1",
      paused: true,
    });
    const pausedActivation = await fromRequest.setActivationPaused({
      definitionId: "def-1",
      activationId: "nightly",
      paused: true,
    });
    await fromRequest.deleteDefinition({ definitionId: "def-1" });
    const fetchedRun = await fromRequest.getRun({ runId: "run-1" });
    const listedRuns = await fromRequest.listRuns();
    const events = await fromRequest.getRunEvents({ runId: "run-1" });
    const output = await fromRequest.getRunOutput({ runId: "run-1" });
    const canceledRun = await fromRequest.cancelRun({ runId: "run-1", reason: "done" });
    const deliveredEvent = await fromRequest.deliverEvent({
      providerName: "secondary",
      event: {
        type: "roadmap.item.updated",
        source: "roadmap",
      },
    });

    expect(startedRun.id).toBe("run-1");
    expect(startedRun.input).toEqual({ itemId: "item-1" });
    expect(signaledRun.signal?.name).toBe("roadmap.item.updated");
    expect(signaledOrStartedRun.startedRun).toBe(true);
    expect(fetchedDefinition.id).toBe("def-1");
    expect(definitions[0]?.id).toBe("def-1");
    expect(pausedDefinition.paused).toBe(true);
    expect(pausedActivation.activations?.[0]?.paused).toBe(true);
    expect(fetchedRun.status).toBe(WorkflowRunStatus.RUNNING);
    expect(listedRuns.nextPageToken).toBe("next");
    expect(events[0]?.type).toBe("run.started");
    expect(output.output).toEqual({ ok: true, runId: "run-1" });
    expect(canceledRun.status).toBe(WorkflowRunStatus.CANCELED);
    expect(deliveredEvent.type).toBe("roadmap.item.updated");
    expect(calls).toEqual([
      {
        method: "apply-definition",
        invocationToken: "invocation-token-123",
        idempotencyKey: "workflow-definition-key-ts",
        providerName: "basic",
        definitionId: "def-1",
      },
      {
        method: "start-run",
        invocationToken: "invocation-token-456",
        idempotencyKey: "workflow-request-key-ts",
        providerName: "basic",
        workflowKey: "roadmap-summary:item-1",
        definitionId: "def-1",
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
        definitionId: "def-1",
      },
      {
        method: "get-definition",
        invocationToken: "invocation-token-456",
        definitionId: "def-1",
      },
      {
        method: "list-definitions",
        invocationToken: "invocation-token-456",
      },
      {
        method: "set-definition-paused",
        invocationToken: "invocation-token-456",
        definitionId: "def-1",
        paused: true,
      },
      {
        method: "set-activation-paused",
        invocationToken: "invocation-token-456",
        definitionId: "def-1",
        activationId: "nightly",
        paused: true,
      },
      {
        method: "delete-definition",
        invocationToken: "invocation-token-456",
        definitionId: "def-1",
      },
      {
        method: "get-run",
        invocationToken: "invocation-token-456",
        runId: "run-1",
      },
      {
        method: "list-runs",
        invocationToken: "invocation-token-456",
      },
      {
        method: "get-run-events",
        invocationToken: "invocation-token-456",
        runId: "run-1",
      },
      {
        method: "get-run-output",
        invocationToken: "invocation-token-456",
        runId: "run-1",
      },
      {
        method: "cancel-run",
        invocationToken: "invocation-token-456",
        runId: "run-1",
      },
      {
        method: "deliver-event",
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

test("Workflow prioritizes invocation-token validation over socket configuration", () => {
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];

  try {
    delete process.env[ENV_HOST_SERVICE_SOCKET];
    expect(() => new Workflow("   ")).toThrow(
      "workflow: invocation token is not available",
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

test("Workflow honors tcp target env and relay token env", async () => {
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
        async applyDefinition(input) {
          return create(WorkflowDefinitionSchema, {
            providerName: input.providerName || "basic",
            id: input.spec?.id || "def-1",
            generation: 1n,
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

    const manager = new Workflow("invoke-token");
    const applied = await manager.applyDefinition({
      providerName: "basic",
      spec: { id: "def-1" },
    });

    expect(applied.providerName).toBe("basic");
    expect(applied.id).toBe("def-1");
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
