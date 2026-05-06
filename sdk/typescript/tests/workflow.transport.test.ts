import { createServer } from "node:http2";
import { createServer as createNetServer } from "node:net";

import { create } from "@bufbuild/protobuf";
import { type ServiceImpl } from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";
import { expect, test } from "bun:test";

import {
  InvokeWorkflowOperationRequestSchema,
  InvokeWorkflowOperationResponseSchema,
  WorkflowHost as WorkflowHostService,
} from "../src/internal/gen/v1/workflow_pb.ts";
import {
  ENV_WORKFLOW_HOST_SOCKET,
  ENV_WORKFLOW_HOST_SOCKET_TOKEN,
  WorkflowHost,
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

test("WorkflowHost honors tcp target env and relay token env", async () => {
  const previousSocket = process.env[ENV_WORKFLOW_HOST_SOCKET];
  const previousToken = process.env[ENV_WORKFLOW_HOST_SOCKET_TOKEN];
  const seenTokens: string[] = [];
  const address = await reserveTCPAddress();

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(WorkflowHostService, {
        async invokeOperation(input) {
          return create(InvokeWorkflowOperationResponseSchema, {
            status: 202,
            body: input.runId,
          });
        },
      } satisfies Partial<ServiceImpl<typeof WorkflowHostService>>);
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

    process.env[ENV_WORKFLOW_HOST_SOCKET] = `tcp://${address}`;
    process.env[ENV_WORKFLOW_HOST_SOCKET_TOKEN] = "relay-token-typescript";

    const host = new WorkflowHost();
    const response = await host.invokeOperation(
      create(InvokeWorkflowOperationRequestSchema, {
        runId: "run-123",
      }),
    );

    expect(response.status).toBe(202);
    expect(response.body).toBe("run-123");
    expect(seenTokens).toEqual(["relay-token-typescript"]);
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_WORKFLOW_HOST_SOCKET];
    } else {
      process.env[ENV_WORKFLOW_HOST_SOCKET] = previousSocket;
    }
    if (previousToken === undefined) {
      delete process.env[ENV_WORKFLOW_HOST_SOCKET_TOKEN];
    } else {
      process.env[ENV_WORKFLOW_HOST_SOCKET_TOKEN] = previousToken;
    }
    if (server.listening) {
      await new Promise<void>((resolve) => {
        server.close(() => resolve());
      });
    }
  }
});
