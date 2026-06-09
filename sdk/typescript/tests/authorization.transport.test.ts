import { createServer } from "node:http2";
import { createServer as createNetServer } from "node:net";

import { create } from "@bufbuild/protobuf";
import { type ServiceImpl } from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";
import { expect, test } from "bun:test";

import {
  ListRelationshipsRequestSchema,
  ListRelationshipsResponseSchema,
  RelationshipTargetSchema,
  SubjectSchema,
  CheckAccessResponseSchema,
  CheckAccessRequestSchema,
  AuthorizationProvider as AuthorizationProviderService,
} from "../src/internal/gen/v1/authorization_pb.ts";
import {
  Authorization,
  ENV_HOST_SERVICE_SOCKET,
  ENV_HOST_SERVICE_TOKEN,
  createAuthorizationProviderService,
  defineAuthorizationProvider,
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

test("Authorization custom target uses relay token env", async () => {
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];
  const previousToken = process.env[ENV_HOST_SERVICE_TOKEN];
  const seenTokens: string[] = [];
  const calls: Array<{
    subject: string;
    action: string;
    resource: string;
  }> = [];
  const address = await reserveTCPAddress();

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(AuthorizationProviderService, {
        async checkAccess(input) {
          calls.push({
            subject: `${input.subject?.type}:${input.subject?.id}`,
            action: input.action?.name ?? "",
            resource: `${input.resource?.type}:${input.resource?.id}`,
          });
          return create(CheckAccessResponseSchema, {
            allowed: true,
            modelId: "model-1",
          });
        },
      } satisfies Partial<ServiceImpl<typeof AuthorizationProviderService>>);
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

    delete process.env[ENV_HOST_SERVICE_SOCKET];
    process.env[ENV_HOST_SERVICE_TOKEN] = "relay-token-typescript-authz";

    const authorization = Authorization({ target: `tcp://${address}` });
    const response = await authorization.checkAccess({
      subject: { type: "user", id: "user-1" },
      action: { name: "read" },
      resource: { type: "document", id: "doc-1" },
    });

    expect(response).toEqual({
      allowed: true,
      modelId: "model-1",
    });
    expect(calls).toEqual([
      {
        subject: "user:user-1",
        action: "read",
        resource: "document:doc-1",
      },
    ]);
    expect(seenTokens).toEqual(["relay-token-typescript-authz"]);
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

test("Authorization close evicts the shared cached client", async () => {
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];
  const previousToken = process.env[ENV_HOST_SERVICE_TOKEN];
  const calls: string[] = [];
  const address = await reserveTCPAddress();

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(AuthorizationProviderService, {
        async checkAccess(input) {
          calls.push(input.subject?.id ?? "");
          return create(CheckAccessResponseSchema, {
            allowed: true,
            modelId: `model-${calls.length}`,
          });
        },
      } satisfies Partial<ServiceImpl<typeof AuthorizationProviderService>>);
    },
  });
  const server = createServer((req, res) => {
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

    delete process.env[ENV_HOST_SERVICE_SOCKET];
    process.env[ENV_HOST_SERVICE_TOKEN] = "relay-token-typescript-authz-close";

    const first = Authorization({ target: `tcp://${address}` });
    expect(await first.checkAccess({ subject: { id: "first" } })).toEqual({
      allowed: true,
      modelId: "model-1",
    });

    first.close();

    const second = Authorization({ target: `tcp://${address}` });
    expect(second).not.toBe(first);
    expect(await second.checkAccess({ subject: { id: "second" } })).toEqual({
      allowed: true,
      modelId: "model-2",
    });
    expect(calls).toEqual(["first", "second"]);
    second.close();
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

test("Authorization provider service preserves absent properties and unset relationship targets", async () => {
  const seen: Array<{
    subjectProperties: Authorization.SubjectInput["properties"];
    targetKind: Authorization.RelationshipTarget["kind"] | undefined;
  }> = [];
  const provider = defineAuthorizationProvider({
    displayName: "authz-test",
    checkAccess(request) {
      seen.push({
        subjectProperties: request.subject?.properties,
        targetKind: undefined,
      });
      return { allowed: true };
    },
    checkAccessMany() {
      return {};
    },
    listRelationships(request) {
      seen.push({
        subjectProperties: undefined,
        targetKind: request.filter?.target?.kind,
      });
      return {};
    },
    addRelationship() {
      return {};
    },
    deleteRelationship() {
      return {};
    },
    setAuthorizationState() {
      return {};
    },
    getActiveModelRef() {
      return {};
    },
    setActiveModel() {
      return {};
    },
    listActiveModelResourceTypes() {
      return {};
    },
  });
  const service = createAuthorizationProviderService(provider);
  const context = {} as never;

  await service.checkAccess?.(create(CheckAccessRequestSchema, {
    subject: create(SubjectSchema, {
      type: "user",
      id: "u1",
    }),
  }), context);
  await service.listRelationships?.(create(ListRelationshipsRequestSchema, {
    filter: {
      target: create(RelationshipTargetSchema),
    },
  }), context);

  expect(seen).toEqual([
    {
      subjectProperties: undefined,
      targetKind: undefined,
    },
    {
      subjectProperties: undefined,
      targetKind: "unset",
    },
  ]);

  const response = await service.listRelationships?.(
    create(ListRelationshipsRequestSchema),
    context,
  );
  expect(response).toEqual(create(ListRelationshipsResponseSchema));
});
