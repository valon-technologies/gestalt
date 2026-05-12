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
  AuthorizationMetadataSchema,
  AuthorizationProvider as AuthorizationProviderService,
  EffectiveSubjectSearchResponseSchema,
  ExpandResponseSchema,
  RelationshipTargetSchema,
  ResourceSchema,
  ResourceSearchResponseSchema,
  SubjectSchema,
  SubjectSetSchema,
  SubjectSearchResponseSchema,
} from "../src/internal/gen/v1/authorization_pb.ts";
import {
  Authorization,
  AuthorizationClient,
  ENV_AUTHORIZATION_SOCKET,
  ENV_AUTHORIZATION_SOCKET_TOKEN,
  agentSessionEditorRelationship,
  authorizationAction,
  authorizationRelationshipWithTarget,
  authorizationResource,
  authorizationSubject,
  authorizationSubjectSetTarget,
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

test("Authorization() forwards authorization requests to the host socket", async () => {
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
  const effectiveResourceCalls: Array<{
    subjectId: string;
    actionName: string;
    resourceType: string;
  }> = [];
  const effectiveSubjectCalls: Array<{
    resourceId: string;
    actionName: string;
  }> = [];
  const expandCalls: Array<{
    resourceId: string;
    relation: string;
  }> = [];
  const writeCalls: Array<{
    subjectId: string;
    targetSubjectId: string;
    targetResourceType: string;
    targetRelation: string;
    relation: string;
    resourceType: string;
    resourceId: string;
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
          async effectiveSearchResources(input) {
            effectiveResourceCalls.push({
              subjectId: input.subject?.id ?? "",
              actionName: input.action?.name ?? "",
              resourceType: input.resourceType,
            });
            return create(ResourceSearchResponseSchema, {
              resources: [
                create(ResourceSchema, {
                  type: "agent_session",
                  id: "session-123",
                }),
              ],
              modelId: "authz-model-1",
            });
          },
          async effectiveSearchSubjects(input) {
            effectiveSubjectCalls.push({
              resourceId: input.resource?.id ?? "",
              actionName: input.action?.name ?? "",
            });
            return create(EffectiveSubjectSearchResponseSchema, {
              targets: [
                create(RelationshipTargetSchema, {
                  kind: {
                    case: "subjectSet",
                    value: create(SubjectSetSchema, {
                      resource: create(ResourceSchema, {
                        type: "slack_channel",
                        id: "C123",
                      }),
                      relation: "member",
                    }),
                  },
                }),
              ],
              modelId: "authz-model-1",
              truncated: true,
            });
          },
          async expand(input) {
            expandCalls.push({
              resourceId: input.resource?.id ?? "",
              relation: input.relation,
            });
            return create(ExpandResponseSchema, {
              root: {
                target: create(RelationshipTargetSchema, {
                  kind: {
                    case: "resource",
                    value:
                      input.resource ??
                      create(ResourceSchema, {
                        type: "agent_session",
                        id: "",
                      }),
                  },
                }),
                relation: input.relation,
              },
              modelId: "authz-model-1",
              maxDepthReached: true,
            });
          },
          async getMetadata() {
            return create(AuthorizationMetadataSchema, {
              capabilities: ["search_subjects", "read_relationships"],
              activeModelId: "authz-model-1",
            });
          },
          async writeRelationships(input) {
            for (const write of input.writes) {
              writeCalls.push({
                subjectId: write.subject?.id ?? "",
                targetSubjectId:
                  write.target?.kind.case === "subject"
                    ? write.target.kind.value.id
                    : "",
                targetResourceType:
                  write.target?.kind.case === "subjectSet"
                    ? write.target.kind.value.resource?.type ?? ""
                    : "",
                targetRelation:
                  write.target?.kind.case === "subjectSet"
                    ? write.target.kind.value.relation
                    : "",
                relation: write.relation,
                resourceType: write.resource?.type ?? "",
                resourceId: write.resource?.id ?? "",
              });
            }
            return {};
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
      resource: authorizationResource(
        "slack_identity",
        "team:T123:user:U456",
      ),
      action: authorizationAction("assume"),
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
    const resourceResponse = await Authorization().effectiveSearchResources({
      subject: authorizationSubject("subject", "user:user-123"),
      action: authorizationAction("edit"),
      resourceType: "agent_session",
    });
    expect(resourceResponse.resources[0]?.id).toBe("session-123");
    expect(effectiveResourceCalls).toEqual([
      {
        subjectId: "user:user-123",
        actionName: "edit",
        resourceType: "agent_session",
      },
    ]);

    const targetResponse = await Authorization().effectiveSearchSubjects({
      resource: authorizationResource("agent_session", "session-123"),
      action: authorizationAction("edit"),
    });
    expect(targetResponse.truncated).toBe(true);
    expect(targetResponse.targets[0]?.kind.case).toBe("subjectSet");
    expect(effectiveSubjectCalls).toEqual([
      {
        resourceId: "session-123",
        actionName: "edit",
      },
    ]);

    const expandResponse = await Authorization().expand({
      resource: authorizationResource("agent_session", "session-123"),
      relation: "editor",
      maxDepth: 1,
    });
    expect(expandResponse.maxDepthReached).toBe(true);
    expect(expandResponse.root?.target?.kind.case).toBe("resource");
    expect(expandCalls).toEqual([
      {
        resourceId: "session-123",
        relation: "editor",
      },
    ]);

    await Authorization().writeRelationships({
      writes: [
        authorizationRelationshipWithTarget(
          authorizationSubjectSetTarget(
            authorizationResource("slack_channel", "C123"),
            "member",
          ),
          "editor",
          authorizationResource("agent_session", "session-123"),
        ),
      ],
    });
    await Authorization().grantAgentSessionEditor("user:user-123", "session-123");
    expect(
      agentSessionEditorRelationship("user:user-123", "session-123"),
    ).toEqual({
      subject: { type: "subject", id: "user:user-123" },
      target: {
        kind: {
          case: "subject",
          value: { type: "subject", id: "user:user-123" },
        },
      },
      relation: "editor",
      resource: { type: "agent_session", id: "session-123" },
    });
    expect(writeCalls).toEqual([
      {
        subjectId: "",
        targetSubjectId: "",
        targetResourceType: "slack_channel",
        targetRelation: "member",
        relation: "editor",
        resourceType: "agent_session",
        resourceId: "session-123",
      },
      {
        subjectId: "user:user-123",
        targetSubjectId: "user:user-123",
        targetResourceType: "",
        targetRelation: "",
        relation: "editor",
        resourceType: "agent_session",
        resourceId: "session-123",
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
      resource: authorizationResource(
        "slack_identity",
        "team:T123:user:U456",
      ),
      action: authorizationAction("assume"),
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
