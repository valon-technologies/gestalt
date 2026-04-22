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
  ActionSchema,
  AuthorizationMetadataSchema,
  AuthorizationProvider as AuthorizationProviderService,
  ResourceSchema,
  SubjectSchema,
  SubjectSearchResponseSchema,
} from "../gen/v1/authorization_pb.ts";
import {
  Authorization,
  AuthorizationClient,
  ENV_AUTHORIZATION_SOCKET,
  ENV_AUTHORIZATION_SOCKET_TOKEN,
} from "../src/index.ts";
import { removeTempDir } from "./helpers.ts";

test("Authorization() and AuthorizationClient fail fast when the host socket is unset", () => {
  const previousSocket = process.env[ENV_AUTHORIZATION_SOCKET];
  delete process.env[ENV_AUTHORIZATION_SOCKET];

  try {
    expect(() => Authorization()).toThrow(ENV_AUTHORIZATION_SOCKET);
    expect(() => new AuthorizationClient()).toThrow(ENV_AUTHORIZATION_SOCKET);
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_AUTHORIZATION_SOCKET];
    } else {
      process.env[ENV_AUTHORIZATION_SOCKET] = previousSocket;
    }
  }
});

test("Authorization() forwards read-only authorization requests to the host socket", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-authorization-"));
  const socketPath = join(tempDir, "authorization.sock");
  const previousSocket = process.env[ENV_AUTHORIZATION_SOCKET];
  const searchCalls: Array<{
    resourceType: string;
    resourceId: string;
    actionName: string;
    subjectType: string;
    pageSize: number;
  }> = [];
  let sessionCount = 0;

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(
        AuthorizationProviderService,
        {
          async searchSubjects(input) {
            searchCalls.push({
              resourceType: input.resource?.type ?? "",
              resourceId: input.resource?.id ?? "",
              actionName: input.action?.name ?? "",
              subjectType: input.subjectType,
              pageSize: input.pageSize,
            });
            return create(SubjectSearchResponseSchema, {
              subjects: [
                create(SubjectSchema, {
                  type: "user",
                  id: "user:user-123",
                  properties: {
                    display_name: "Slack User",
                  },
                }),
              ],
              modelId: "authz-model-1",
            });
          },
          async getMetadata() {
            return create(AuthorizationMetadataSchema, {
              capabilities: ["search_subjects", "read_relationships"],
              activeModelId: "authz-model-1",
            });
          },
        } satisfies Partial<ServiceImpl<typeof AuthorizationProviderService>>,
      );
    },
  });
  const server = createServer(handler);
  server.on("session", () => {
    sessionCount += 1;
  });

  try {
    await new Promise<void>((resolve, reject) => {
      server.once("error", reject);
      server.listen(socketPath, () => {
        server.off("error", reject);
        resolve();
      });
    });

    process.env[ENV_AUTHORIZATION_SOCKET] = socketPath;

    const metadata = await Authorization().getMetadata();
    expect(metadata.capabilities).toEqual([
      "search_subjects",
      "read_relationships",
    ]);
    expect(metadata.activeModelId).toBe("authz-model-1");

    const response = await Authorization().searchSubjects({
      resource: create(ResourceSchema, {
        type: "slack_identity",
        id: "team:T123:user:U456",
      }),
      action: create(ActionSchema, {
        name: "assume",
      }),
      subjectType: "user",
      pageSize: 1,
    });
    expect(response.modelId).toBe("authz-model-1");
    expect(response.subjects).toHaveLength(1);
    expect(response.subjects[0]).toMatchObject({
      type: "user",
      id: "user:user-123",
    });
    expect(searchCalls).toEqual([
      {
        resourceType: "slack_identity",
        resourceId: "team:T123:user:U456",
        actionName: "assume",
        subjectType: "user",
        pageSize: 1,
      },
    ]);
    expect(sessionCount).toBe(1);
  } finally {
    server.close();
    if (previousSocket === undefined) {
      delete process.env[ENV_AUTHORIZATION_SOCKET];
    } else {
      process.env[ENV_AUTHORIZATION_SOCKET] = previousSocket;
    }
    removeTempDir(tempDir);
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

test("Authorization honors tcp target env and relay token env", async () => {
  const previousSocket = process.env[ENV_AUTHORIZATION_SOCKET];
  const previousToken = process.env[ENV_AUTHORIZATION_SOCKET_TOKEN];
  const seenTokens: string[] = [];
  const address = await reserveTCPAddress();

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(
        AuthorizationProviderService,
        {
          async searchSubjects(input) {
            return create(SubjectSearchResponseSchema, {
              subjects: [
                create(SubjectSchema, {
                  type: input.subjectType || "user",
                  id: "user:user-123",
                }),
              ],
              modelId: "authz-model-1",
            });
          },
        } satisfies Partial<ServiceImpl<typeof AuthorizationProviderService>>,
      );
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

    process.env[ENV_AUTHORIZATION_SOCKET] = `tcp://${address}`;
    process.env[ENV_AUTHORIZATION_SOCKET_TOKEN] = "relay-token-typescript";

    const response = await Authorization().searchSubjects({
      resource: create(ResourceSchema, {
        type: "slack_identity",
        id: "team:T123:user:U456",
      }),
      action: create(ActionSchema, { name: "assume" }),
      subjectType: "user",
      pageSize: 1,
    });

    expect(response.modelId).toBe("authz-model-1");
    expect(response.subjects).toHaveLength(1);
    expect(response.subjects[0]?.id).toBe("user:user-123");
    expect(seenTokens).toEqual(["relay-token-typescript"]);
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_AUTHORIZATION_SOCKET];
    } else {
      process.env[ENV_AUTHORIZATION_SOCKET] = previousSocket;
    }
    if (previousToken === undefined) {
      delete process.env[ENV_AUTHORIZATION_SOCKET_TOKEN];
    } else {
      process.env[ENV_AUTHORIZATION_SOCKET_TOKEN] = previousToken;
    }
    if (server.listening) {
      await new Promise<void>((resolve) => {
        server.close(() => resolve());
      });
    }
  }
});
