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
  ListWorkflowProviderDefinitionsResponseSchema,
  SignalWorkflowRunResponseSchema,
  WorkflowDefinitionSchema,
  Workflow as WorkflowProviderService,
  WorkflowRunSchema,
} from "../src/internal/gen/v1/workflow_pb.ts";
import {
  ENV_HOST_SERVICE_SOCKET,
  ENV_HOST_SERVICE_TOKEN,
  Workflow,
  WorkflowRunStatus,
  type RequestContext,
  type SubjectContext,
} from "../src/index.ts";
import { removeTempDir } from "./helpers.ts";

function subjectContext(id: string): SubjectContext {
  return {
    id,
    credentialSubjectId: "",
    email: "",
    displayName: "",
    scopes: [],
    permissions: [],
  };
}

test("Workflow forwards request context to provider calls", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-workflow-"));
  const socketPath = join(tempDir, "workflow.sock");
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];
  const calls: Array<{
    method: string;
    subjectId: string;
    idempotencyKey?: string;
    providerName?: string;
    runId?: string;
    definitionId?: string;
    signalName?: string | undefined;
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
            subjectId: input.context?.subject?.id ?? "",
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
        async listDefinitions(input) {
          calls.push({
            method: "list-definitions",
            subjectId: input.context?.subject?.id ?? "",
          });
          return create(ListWorkflowProviderDefinitionsResponseSchema, {
            definitions: [create(WorkflowDefinitionSchema, { id: "def-1", generation: 3n })],
          });
        },
        async startRun(input) {
          calls.push({
            method: "start-run",
            subjectId: input.context?.subject?.id ?? "",
            idempotencyKey: input.idempotencyKey,
            providerName: input.providerName,
            definitionId: input.definitionId,
          });
          return create(WorkflowRunSchema, {
            providerName: input.providerName || "basic",
            id: "run-1",
            status: WorkflowRunStatus.PENDING,
            definitionId: input.definitionId,
            input: input.input,
          });
        },
        async signalRun(input) {
          calls.push({
            method: "signal-run",
            subjectId: input.context?.subject?.id ?? "",
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
        async deleteDefinition(input) {
          calls.push({
            method: "delete-definition",
            subjectId: input.context?.subject?.id ?? "",
            definitionId: input.definitionId,
          });
          return create(EmptySchema, {});
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
    const context: RequestContext = {
      subject: subjectContext("user:user-123"),
      workflow: {
        provider: "local",
        runId: "run-123",
      },
      toolRefs: [],
      toolRefsSet: false,
    };
    const workflow = Workflow.connect({ context });

    const applied = await workflow.applyDefinition(
      "basic",
      "workflow-definition-key-ts",
      {
        id: "def-1",
        target: {
          steps: [{
            id: "sync",
            inputs: {},
            timeoutSeconds: 0,
            action: {
              case: "app",
              value: {
                name: "roadmap",
                operation: "sync",
                connection: "",
                instance: "",
                credentialMode: "",
              },
            },
          }],
        },
        activations: [],
        paused: false,
      },
    );
    const definitions = await workflow.listDefinitions({});
    const startedRun = await workflow.startRun(
      "workflow-request-key-ts",
      "roadmap-summary:item-1",
      "basic",
      "def-1",
      0n,
      undefined,
      { itemId: "item-1" },
    );
    const signaledRun = await workflow.signalRun("run-1", {
      id: "",
      name: "roadmap.item.updated",
      createdBySubjectId: "",
      idempotencyKey: "",
      sequence: 0n,
    });
    await workflow.deleteDefinition("def-1");

    expect(applied.id).toBe("def-1");
    expect(definitions[0]?.id).toBe("def-1");
    expect(startedRun.id).toBe("run-1");
    expect(signaledRun.signal?.name).toBe("roadmap.item.updated");
    expect(calls).toEqual([
      {
        method: "apply-definition",
        subjectId: "user:user-123",
        idempotencyKey: "workflow-definition-key-ts",
        providerName: "basic",
        definitionId: "def-1",
      },
      {
        method: "list-definitions",
        subjectId: "user:user-123",
      },
      {
        method: "start-run",
        subjectId: "user:user-123",
        idempotencyKey: "workflow-request-key-ts",
        providerName: "basic",
        definitionId: "def-1",
      },
      {
        method: "signal-run",
        subjectId: "user:user-123",
        runId: "run-1",
        signalName: "roadmap.item.updated",
      },
      {
        method: "delete-definition",
        subjectId: "user:user-123",
        definitionId: "def-1",
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

test("Workflow still requires host service socket configuration", () => {
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];

  try {
    delete process.env[ENV_HOST_SERVICE_SOCKET];
    expect(() => Workflow.connect()).toThrow("workflow: GESTALT_HOST_SERVICE_SOCKET is not set");
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
        async listDefinitions() {
          return create(ListWorkflowProviderDefinitionsResponseSchema, {
            definitions: [create(WorkflowDefinitionSchema, { id: "def-1", generation: 1n })],
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

    const workflow = Workflow.connect();
    const definitions = await workflow.listDefinitions({});

    expect(definitions[0]?.id).toBe("def-1");
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
