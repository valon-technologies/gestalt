import { createServer } from "node:http2";
import { createServer as createNetServer } from "node:net";

import { create } from "@bufbuild/protobuf";
import { createClient } from "@connectrpc/connect";
import { connectNodeAdapter, createGrpcTransport } from "@connectrpc/connect-node";
import { expect, test } from "bun:test";

import {
  ApplyWorkflowProviderDefinitionRequestSchema,
  DeliverWorkflowProviderEventRequestSchema,
  GetWorkflowProviderRunEventsRequestSchema,
  GetWorkflowProviderRunOutputRequestSchema,
  StartWorkflowProviderRunRequestSchema,
  Workflow as WorkflowProviderService,
} from "../src/internal/gen/v1/workflow_pb.ts";
import {
  RequestContextSchema,
  SubjectContextSchema,
} from "../src/internal/gen/v1/app_pb.ts";
import { jsonObjectFromStruct, structFromObject } from "../src/protocol.ts";
import {
  WorkflowRunStatus,
  WorkflowStepStatus,
  createWorkflowProviderService,
  defineWorkflowProvider,
  workflowDefinition,
  workflowEvent,
  workflowRun,
  workflowRunEvent,
} from "../src/index.ts";

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

test("WorkflowProvider service converts transport messages to native callbacks", async () => {
  const address = await reserveTCPAddress();
  const calls: Array<{ method: string; detail: string }> = [];
  const provider = defineWorkflowProvider({
    displayName: "Workflow transport fixture",
    async applyDefinition(request) {
      const subjectId = request.context?.subject?.id ?? "";
      calls.push({ method: "apply-definition", detail: subjectId });
      return workflowDefinition({
        id: request.spec?.id,
        generation: 7n,
        target: request.spec?.target,
        activations: request.spec?.activations,
      });
    },
    async getDefinition(request) {
      return workflowDefinition({ id: request.definitionId, generation: 7n });
    },
    async listDefinitions() {
      return [workflowDefinition({ id: "definition-native-ts", generation: 7n })];
    },
    async setDefinitionPaused(request) {
      return workflowDefinition({ id: request.definitionId, paused: request.paused });
    },
    async setActivationPaused(request) {
      return workflowDefinition({
        id: request.definitionId,
        activations: [{ id: request.activationId, paused: request.paused }],
      });
    },
    async deleteDefinition() {},
    async startRun(request) {
      const input = request.input as { operation?: unknown } | undefined;
      calls.push({ method: "start-run", detail: typeof input?.operation === "string" ? input.operation : "" });
      return workflowRun({
        id: request.idempotencyKey,
        status: WorkflowRunStatus.PENDING,
        definitionId: request.definitionId,
        definitionGeneration: request.expectedDefinitionGeneration,
        workflowKey: request.workflowKey,
        input: request.input,
        currentStepId: "sync",
        steps: [{
          stepId: "sync",
          status: WorkflowStepStatus.PENDING,
          input: request.input,
        }],
      });
    },
    async getRun(request) {
      return workflowRun({ id: request.runId, status: WorkflowRunStatus.RUNNING });
    },
    async listRuns() {
      return [];
    },
    async getRunEvents(request) {
      return [workflowRunEvent({
        id: `${request.runId}:started`,
        runId: request.runId,
        stepId: "sync",
        type: "run.started",
      })];
    },
    async getRunOutput(request) {
      return { output: { runId: request.runId, ok: true } };
    },
    async cancelRun(request) {
      return workflowRun({
        id: request.runId,
        status: WorkflowRunStatus.CANCELED,
        statusMessage: request.reason,
      });
    },
    async signalRun(request) {
      return {
        run: workflowRun({ id: request.runId, status: WorkflowRunStatus.RUNNING }),
        signal: request.signal,
        startedRun: false,
        workflowKey: "",
      };
    },
    async signalOrStartRun(request) {
      return {
        run: workflowRun({
          id: request.workflowKey,
          status: WorkflowRunStatus.PENDING,
          definitionId: request.definitionId,
          workflowKey: request.workflowKey,
          input: request.input,
        }),
        signal: request.signal,
        startedRun: true,
        workflowKey: request.workflowKey,
      };
    },
    async deliverEvent(request) {
      calls.push({ method: "deliver-event", detail: request.event?.type ?? "" });
      return workflowEvent({ id: "delivered-ts", type: request.event?.type ?? "", source: request.event?.source ?? "" });
    },
  });

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(WorkflowProviderService, createWorkflowProviderService(provider));
    },
  });
  const server = createServer((req, res) => handler(req, res));

  try {
    await new Promise<void>((resolve, reject) => {
      server.once("error", reject);
      server.listen(Number(address.split(":").at(-1)), "127.0.0.1", () => {
        server.off("error", reject);
        resolve();
      });
    });

    const client = createClient(
      WorkflowProviderService,
      createGrpcTransport({ baseUrl: `http://${address}` }),
    );
    const definition = await client.applyDefinition(create(ApplyWorkflowProviderDefinitionRequestSchema, {
      provider: "local",
      idempotencyKey: "definition-native-ts",
      context: create(RequestContextSchema, {
        subject: create(SubjectContextSchema, { id: "user:ada" }),
      }),
      spec: {
        id: "definition-native-ts",
        target: {
          steps: [{
            id: "sync",
            action: {
              case: "app",
              value: { name: "demo", operation: "sync" },
            },
          }],
        },
      },
    }));
    expect(definition.id).toBe("definition-native-ts");

    const run = await client.startRun(create(StartWorkflowProviderRunRequestSchema, {
      provider: "local",
      idempotencyKey: "run-native-ts",
      definitionId: "definition-native-ts",
      expectedDefinitionGeneration: 7n,
      workflowKey: "demo:1",
      input: structFromObject({ operation: "sync" }),
    }));
    expect(run.id).toBe("run-native-ts");
    expect(run.status).toBe(WorkflowRunStatus.PENDING);
    expect(jsonObjectFromStruct(run.input).operation).toBe("sync");
    expect(run.steps[0]?.stepId).toBe("sync");

    const events = await client.getRunEvents(create(GetWorkflowProviderRunEventsRequestSchema, {
      provider: "local",
      runId: "run-native-ts",
    }));
    expect(events.events[0]?.type).toBe("run.started");

    const output = await client.getRunOutput(create(GetWorkflowProviderRunOutputRequestSchema, {
      provider: "local",
      runId: "run-native-ts",
    }));
    expect(output.output?.kind.case).toBe("structValue");

    const delivered = await client.deliverEvent(create(DeliverWorkflowProviderEventRequestSchema, {
      provider: "local",
      event: { type: "demo.synced", source: "demo" },
    }));
    expect(delivered.id).toBe("delivered-ts");
    expect(calls).toEqual([
      { method: "apply-definition", detail: "user:ada" },
      { method: "start-run", detail: "sync" },
      { method: "deliver-event", detail: "demo.synced" },
    ]);
  } finally {
    if (server.listening) {
      await new Promise<void>((resolve) => {
        server.close(() => resolve());
      });
    }
  }
});
