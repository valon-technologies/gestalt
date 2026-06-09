import {
  Metadata,
  Server,
  ServerCredentials,
  status,
  type sendUnaryData,
  type ServerUnaryCall,
  type ServiceError,
} from "@grpc/grpc-js";
import { expect, test } from "bun:test";

import {
  Authorization,
  ENV_HOST_SERVICE_SOCKET,
  ENV_HOST_SERVICE_TOKEN,
  HOST_SERVICE_RELAY_TOKEN_HEADER,
} from "../src/index.ts";
import * as AuthorizationMessages from "../src/internal/ts-proto/v1/authorization.ts";

test("Authorization uses the generated ts-proto grpc-js client", async () => {
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];
  const previousToken = process.env[ENV_HOST_SERVICE_TOKEN];
  const calls: {
    checkAccess: Array<{ subjectProperties: unknown; resourceType: string }>;
    checkAccessMany: number[];
    addRelationshipTargets: unknown[];
    deleteRelationshipRelations: string[];
    setAuthorizationStateAllowedTargets: unknown[];
    setActiveModelIds: string[];
    listActiveModelResourceTypesFilters: unknown[];
  } = {
    checkAccess: [],
    checkAccessMany: [],
    addRelationshipTargets: [],
    deleteRelationshipRelations: [],
    setAuthorizationStateAllowedTargets: [],
    setActiveModelIds: [],
    listActiveModelResourceTypesFilters: [],
  };
  const createdAt = new Date("2026-01-02T03:04:05.006Z");
  const server = new Server();
  server.addService(AuthorizationMessages.AuthorizationProviderService, {
    checkAccess(call, callback) {
      calls.checkAccess.push({
        subjectProperties: call.request.subject?.properties,
        resourceType: call.request.resource?.type ?? "",
      });
      callback(null, { allowed: true, modelId: "model-1" });
    },
    checkAccessMany(call, callback) {
      const requests = call.request.requests ?? [];
      calls.checkAccessMany.push(requests.length);
      callback(null, {
        decisions: requests.map((entry, index) => {
          const properties = entry.subject?.properties as
            | { tenant?: unknown }
            | undefined;
          return {
            allowed: properties?.tenant === "acme",
            modelId: `model-${index + 1}`,
          };
        }),
      });
    },
    listRelationships(_call, callback) {
      callback(null, {
        relationships: [
          {
            tuple: {
              target: {
                subject: {
                  type: "user",
                  id: "user-123",
                  properties: { tenant: "acme" },
                },
              },
              relation: "viewer",
              resource: { type: "document", id: "doc-123" },
            },
            properties: { grantedBy: "test" },
            sourceLayer: Authorization.SourceLayer.SOURCE_LAYER_RUNTIME,
          },
        ],
      });
    },
    addRelationship(call, callback) {
      calls.addRelationshipTargets.push(call.request.relationship?.tuple?.target);
      callback(null, { relationship: call.request.relationship });
    },
    deleteRelationship(call, callback) {
      calls.deleteRelationshipRelations.push(
        call.request.relationshipTuple?.relation ?? "",
      );
      callback(null, {});
    },
    setAuthorizationState(call, callback) {
      calls.setAuthorizationStateAllowedTargets.push(
        call.request.model?.resourceTypes?.[0]?.relations?.[0]?.allowedTargets?.[0],
      );
      callback(null, {
        activeModel: {
          id: call.request.model?.id ?? "model-1",
          version: call.request.model?.version ?? "v1",
          createdAt,
        },
      });
    },
    getActiveModelRef(_call, callback) {
      callback(null, { model: { id: "model-1", version: "v1", createdAt } });
    },
    setActiveModel(call, callback) {
      calls.setActiveModelIds.push(call.request.model?.id ?? "");
      callback(null, {
        model: {
          id: call.request.model?.id ?? "model-1",
          version: call.request.model?.version ?? "v1",
          createdAt,
        },
      });
    },
    listActiveModelResourceTypes(call, callback) {
      calls.listActiveModelResourceTypesFilters.push(call.request.filter);
      callback(null, {
        modelId: "model-1",
        resourceTypes: [
          {
            name: "document",
            sourceLayer: Authorization.SourceLayer.SOURCE_LAYER_RUNTIME,
            defaultAccessPolicy: Authorization.DefaultAccessPolicy.DEFAULT_ACCESS_POLICY_ALLOW,
          },
        ],
      });
    },
  } satisfies AuthorizationMessages.AuthorizationProviderServer);

  try {
    const port = await bindGrpcServer(server);
    process.env[ENV_HOST_SERVICE_SOCKET] = `tcp://127.0.0.1:${port}`;
    process.env[ENV_HOST_SERVICE_TOKEN] = "relay-token-typescript";

    const authorization = Authorization();
    const decision = await authorization.checkAccess({
      subject: {
        type: "user",
        id: "user-123",
        properties: { tenant: "acme", flags: ["beta"] },
      },
      action: { name: "read" },
      resource: { type: "document", id: "doc-123" },
    });
    const modelRef = await authorization.getActiveModelRef();
    const relationships = await authorization.listRelationships({});
    const many = await authorization.checkAccessMany({
      requests: [
        {
          subject: { type: "user", id: "user-123", properties: { tenant: "acme" } },
          action: { name: "read", properties: { urgency: "normal" } },
          resource: { type: "document", id: "doc-123" },
        },
        {
          subject: { type: "user", id: "user-456", properties: { tenant: "other" } },
          action: { name: "read" },
          resource: { type: "document", id: "doc-456" },
        },
      ],
    });
    const added = await authorization.addRelationship({
      relationship: {
        tuple: {
          target: {
            subject: { type: "user", id: "user-123" },
          },
          relation: "viewer",
          resource: { type: "document", id: "doc-123" },
        },
        properties: { grantedBy: "test" },
        sourceLayer: Authorization.SourceLayer.SOURCE_LAYER_RUNTIME,
      },
    });
    const deleted = await authorization.deleteRelationship({
      relationshipTuple: {
        target: {
          subjectSet: {
            resource: { type: "group", id: "group-123" },
            relation: "member",
          },
        },
        relation: "viewer",
        resource: { type: "document", id: "doc-123" },
      },
    });
    const state = await authorization.setAuthorizationState({
      model: {
        id: "model-state",
        version: "v2",
        resourceTypes: [
          {
            name: "document",
            relations: [
              {
                name: "viewer",
                allowedTargets: [{ subjectType: "user" }],
              },
            ],
            actions: [{ name: "read", relations: ["viewer"] }],
            sourceLayer: Authorization.SourceLayer.SOURCE_LAYER_RUNTIME,
            defaultAccessPolicy: Authorization.DefaultAccessPolicy.DEFAULT_ACCESS_POLICY_DENY,
          },
        ],
      },
      relationships: relationships.relationships ?? [],
    });
    const activeModel = await authorization.setActiveModel({
      model: {
        id: "model-active",
        version: "v3",
        resourceTypes: [],
      },
    });
    const resourceTypes = await authorization.listActiveModelResourceTypes({
      filter: { name: "document", sourceLayer: Authorization.SourceLayer.SOURCE_LAYER_RUNTIME },
      pageSize: 10,
      pageToken: "page-1",
    });

    expect(decision).toEqual({ allowed: true, modelId: "model-1" });
    expect(modelRef.model?.createdAt).toEqual(createdAt);
    expect(relationships.relationships?.[0]?.properties).toEqual({ grantedBy: "test" });
    expect(relationships.relationships?.[0]?.sourceLayer).toBe(
      Authorization.SourceLayer.SOURCE_LAYER_RUNTIME,
    );
    expect(many.decisions).toEqual([
      { allowed: true, modelId: "model-1" },
      { allowed: false, modelId: "model-2" },
    ]);
    expect(added.relationship?.tuple?.target?.subject).toEqual({
      type: "user",
      id: "user-123",
      properties: undefined,
    });
    expect(deleted).toEqual({});
    expect(state.activeModel).toEqual({
      id: "model-state",
      version: "v2",
      createdAt,
    });
    expect(activeModel.model?.id).toBe("model-active");
    expect(resourceTypes).toEqual({
      modelId: "model-1",
      nextPageToken: "",
      resourceTypes: [
        {
          name: "document",
          relations: [],
          actions: [],
          sourceLayer: Authorization.SourceLayer.SOURCE_LAYER_RUNTIME,
          defaultAccessPolicy: Authorization.DefaultAccessPolicy.DEFAULT_ACCESS_POLICY_ALLOW,
        },
      ],
    });
    expect(calls.checkAccess).toEqual([
      {
        subjectProperties: { tenant: "acme", flags: ["beta"] },
        resourceType: "document",
      },
    ]);
    expect(calls.checkAccessMany).toEqual([2]);
    expect(calls.addRelationshipTargets).toEqual([
      {
        subject: { type: "user", id: "user-123", properties: undefined },
        resource: undefined,
        subjectSet: undefined,
      },
    ]);
    expect(calls.deleteRelationshipRelations).toEqual(["viewer"]);
    expect(calls.setAuthorizationStateAllowedTargets).toEqual([
      { subjectType: "user" },
    ]);
    expect(calls.setActiveModelIds).toEqual(["model-active"]);
    expect(calls.listActiveModelResourceTypesFilters).toEqual([
      { name: "document", sourceLayer: Authorization.SourceLayer.SOURCE_LAYER_RUNTIME },
    ]);
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
    await shutdownGrpcServer(server);
  }
});

test("Authorization sends relay metadata over a tcp grpc-js transport", async () => {
  const previousToken = process.env[ENV_HOST_SERVICE_TOKEN];
  const seenTokens: unknown[] = [];
  const server = new Server();
  server.addService(AuthorizationMessages.AuthorizationProviderService, {
    checkAccess(call, callback) {
      seenTokens.push(call.metadata.get(HOST_SERVICE_RELAY_TOKEN_HEADER)[0]);
      callback(null, {
        allowed: true,
        modelId: String(call.request.subject?.properties?.tenant ?? ""),
      });
    },
    checkAccessMany: unimplemented,
    listRelationships: unimplemented,
    addRelationship: unimplemented,
    deleteRelationship: unimplemented,
    setAuthorizationState: unimplemented,
    getActiveModelRef: unimplemented,
    setActiveModel: unimplemented,
    listActiveModelResourceTypes: unimplemented,
  } satisfies AuthorizationMessages.AuthorizationProviderServer);

  try {
    const port = await bindGrpcServer(server);
    const authorization = Authorization({
      target: `tcp://127.0.0.1:${port}`,
      relayToken: "relay-token-typescript",
    });

    const response = await authorization.checkAccess({
      subject: {
        type: "user",
        id: "user-123",
        properties: { tenant: "acme" },
      },
    });

    expect(response).toEqual({ allowed: true, modelId: "acme" });
    expect(seenTokens).toEqual(["relay-token-typescript"]);

    process.env[ENV_HOST_SERVICE_TOKEN] = "relay-token-from-env";
    const envTokenAuthorization = Authorization({
      target: `tcp://127.0.0.1:${port}`,
    });
    const envTokenResponse = await envTokenAuthorization.checkAccess({
      subject: {
        type: "user",
        id: "user-456",
        properties: { tenant: "env" },
      },
    });

    expect(envTokenResponse).toEqual({ allowed: true, modelId: "env" });
    expect(seenTokens).toEqual([
      "relay-token-typescript",
      "relay-token-from-env",
    ]);
  } finally {
    if (previousToken === undefined) {
      delete process.env[ENV_HOST_SERVICE_TOKEN];
    } else {
      process.env[ENV_HOST_SERVICE_TOKEN] = previousToken;
    }
    await shutdownGrpcServer(server);
  }
});

function unimplemented<Request, Response>(
  _call: ServerUnaryCall<Request, Response>,
  callback: sendUnaryData<Response>,
): void {
  callback({
    name: "Error",
    message: "unimplemented",
    code: status.UNIMPLEMENTED,
    details: "unimplemented",
    metadata: new Metadata(),
  } satisfies ServiceError);
}

async function bindGrpcServer(server: Server): Promise<number> {
  return await new Promise((resolve, reject) => {
    server.bindAsync(
      "127.0.0.1:0",
      ServerCredentials.createInsecure(),
      (error, port) => {
        if (error) {
          reject(error);
          return;
        }
        resolve(port);
      },
    );
  });
}

async function shutdownGrpcServer(server: Server): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    server.tryShutdown((error) => {
      if (error) {
        reject(error);
        return;
      }
      resolve();
    });
  });
}
