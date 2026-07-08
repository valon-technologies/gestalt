import { spawn, type ChildProcess } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import { create, toJson } from "@bufbuild/protobuf";
import { EmptySchema, ValueSchema } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { expect, test } from "bun:test";

import {
  AuthorizeRequestSchema,
  IntrospectRequestSchema,
  TokenRequestSchema,
} from "../src/internal/gen/v1/identity_pb.ts";
import {
  CacheDeleteManyRequestSchema,
  CacheDeleteRequestSchema,
  CacheGetManyRequestSchema,
  CacheGetRequestSchema,
  CacheSetEntrySchema,
  CacheSetManyRequestSchema,
  CacheSetRequestSchema,
  CacheTouchRequestSchema,
} from "../src/internal/gen/v1/cache_pb.ts";
import {
  AccessContextSchema,
  AgentToolRefSchema,
  CredentialContextSchema,
  ExecuteRequestSchema,
  GetSessionCatalogRequestSchema,
  HostContextSchema,
  HTTPSubjectRequestSchema,
  RequestContextSchema,
  ResolveHTTPSubjectRequestSchema,
  StartProviderRequestSchema,
  StringListSchema,
  SubjectContextSchema,
} from "../src/internal/gen/v1/app_pb.ts";
import {
  GetRuntimeSessionRequestSchema,
  ListRuntimeSessionsRequestSchema,
  PrepareRuntimeWorkspaceRequestSchema,
  RemoveRuntimeWorkspaceRequestSchema,
  StartHostedAppRequestSchema,
  StartRuntimeSessionRequestSchema,
  StopRuntimeSessionRequestSchema,
} from "../src/internal/gen/v1/runtime_provider_pb.ts";
import {
  GetSecretRequestSchema,
  Secrets as SecretsProviderService,
} from "../src/internal/gen/v1/secrets_pb.ts";
import {
  ConfigureProviderRequestSchema,
  ProviderKind as ProtoProviderKind,
  ProviderLifecycle,
} from "../src/internal/gen/v1/runtime_pb.ts";
import {
  ApplyWorkflowProviderDefinitionRequestSchema,
  DeliverWorkflowProviderEventRequestSchema,
  StartWorkflowProviderRunRequestSchema,
} from "../src/internal/gen/v1/workflow_pb.ts";
import {
  CURRENT_PROTOCOL_VERSION,
  createCacheService,
  ENV_WRITE_CATALOG,
  ENV_PROVIDER_SOCKET,
  createIdentityService,
  createProviderService,
  createRuntimeProviderService,
  createRuntimeService,
  createWorkflowProviderService,
  loadProviderFromTarget,
  main,
  parseRuntimeArgs,
} from "../src/providers/runtime.ts";
import {
  httpSubjectError,
  PresignMethod,
  RuntimeEgressMode,
  WorkflowRunStatus,
  defineCacheProvider,
  defineApp,
  defineRuntimeProvider,
  defineS3Provider,
} from "../src/index.ts";
import { boundWorkflowTargetToProto } from "../src/providers/workflow.ts";
import { createS3Service } from "../src/providers/s3.ts";
import {
  captureChildStderr,
  createUnixGrpcClient,
  fixturePath,
  makeTempDir,
  removeTempDir,
  stopProcess,
  waitForPath,
} from "./helpers.ts";

function jsonBody(body: Uint8Array): unknown {
  return JSON.parse(new TextDecoder("utf-8", { fatal: false }).decode(body)) as unknown;
}

async function expectConnectCode(
  promise: Promise<unknown>,
  code: Code,
): Promise<void> {
  try {
    await promise;
    throw new Error(`expected ConnectError with code ${Code[code]}`);
  } catch (error) {
    expect(error).toBeInstanceOf(ConnectError);
    expect((error as ConnectError).code).toBe(code);
  }
}

function workflowAppStepTarget(
  name: string,
  operation: string,
  extra: Record<string, unknown> = {},
) {
  return boundWorkflowTargetToProto({
    steps: [{ id: operation, app: { name, operation, ...extra } }],
  });
}

test("runtime arg parsing requires root and target", () => {
  expect(parseRuntimeArgs(["root", "app:./provider.ts#app"])).toEqual({
    root: "root",
    target: "app:./provider.ts#app",
  });
  expect(parseRuntimeArgs(["root"])).toBeUndefined();
});

test("runtime main writes a static catalog in catalog mode", async () => {
  const root = makeTempDir("gestalt-typescript-runtime-catalog-");
  const catalogPath = join(root, "catalog.yaml");
  const previousCatalog = process.env[ENV_WRITE_CATALOG];

  try {
    const indexPath = join(import.meta.dir, "..", "src", "index.ts");
    writeFileSync(
      join(root, "package.json"),
      JSON.stringify({
        name: "@scope/catalog provider",
        gestalt: {
          provider: {
            kind: "app",
            target: "./provider.ts#app",
          },
        },
      }),
      "utf8",
    );
    writeFileSync(
      join(root, "provider.ts"),
      `import { defineApp, s } from ${JSON.stringify(indexPath)};

export const app = defineApp({
  displayName: "Catalog Provider",
  operations: [
    {
      id: "ping",
      readOnly: false,
      visible: false,
      input: s.object({
        projectId: s.string(),
      }),
      output: s.object({
        ok: s.boolean(),
      }),
      handler() {
        return { ok: true };
      },
    },
  ],
});
`,
      "utf8",
    );

    process.env[ENV_WRITE_CATALOG] = catalogPath;
    const code = await main([root, "app:./provider.ts#app"]);
    expect(code).toBe(0);
    const catalog = readFileSync(catalogPath, "utf8");
    expect(catalog).toContain("name: catalog-provider");
    expect(catalog).toContain("displayName: Catalog Provider");
    expect(catalog).toContain("id: ping");
    expect(catalog).toContain("readOnly: false");
    expect(catalog).toContain("visible: false");
    expect(catalog).toContain("inputSchema:");
    expect(catalog).toContain("projectId:");
    expect(catalog).toContain("outputSchema:");
    expect(catalog).not.toContain("display_name:");
    expect(catalog).not.toContain("input_schema:");
    expect(catalog).not.toContain("output_schema:");
  } finally {
    if (previousCatalog === undefined) {
      delete process.env[ENV_WRITE_CATALOG];
    } else {
      process.env[ENV_WRITE_CATALOG] = previousCatalog;
    }
    removeTempDir(root);
  }
});

test("loadProviderFromTarget resolves a secrets provider from package metadata", async () => {
  const provider = await loadProviderFromTarget(
    fixturePath("secrets-provider"),
  );
  expect(provider.kind).toBe("secrets");
  expect(provider.name).toBe("secrets-provider");
  expect(provider.displayName).toBe("Fixture Secrets");
});

test("loadProviderFromTarget falls through null exports to the next app candidate", async () => {
  const app = await loadProviderFromTarget(
    fixturePath("basic-provider-null-export"),
  );
  expect(app.kind).toBe("integration");
  expect(app.name).toBe("basic-provider-null-export");
  expect(app.displayName).toBe("Fixture Provider Null Export");
});

test("loadProviderFromTarget ignores whitespace-only explicit targets", async () => {
  const app = await loadProviderFromTarget(
    fixturePath("basic-provider"),
    "   ",
  );
  expect(app.kind).toBe("integration");
  expect(app.name).toBe("basic-provider");
  expect(app.displayName).toBe("Fixture Provider");
});

test("loadProviderFromTarget formats package target in errors when explicit target is whitespace", async () => {
  const root = makeTempDir("gestalt-typescript-runtime-target-");
  try {
    const indexPath = join(import.meta.dir, "..", "src", "index.ts");
    writeFileSync(
      join(root, "package.json"),
      JSON.stringify({
        name: "broken-provider",
        gestalt: {
          provider: {
            kind: "identity",
            target: "./provider.ts#missing",
          },
        },
      }),
      "utf8",
    );
    writeFileSync(
      join(root, "provider.ts"),
      `import { defineApp } from ${JSON.stringify(indexPath)};

export const app = defineApp({
  operations: [
    {
      id: "hello",
      handler() {
        return { ok: true };
      },
    },
  ],
});
`,
      "utf8",
    );

    await expect(loadProviderFromTarget(root, "   ")).rejects.toThrow(
      "identity:./provider.ts#missing did not resolve to a Gestalt identity provider",
    );
  } finally {
    removeTempDir(root);
  }
});

test("loadProviderFromTarget rejects duplicate operation identifiers after trimming", async () => {
  const root = makeTempDir("gestalt-typescript-runtime-duplicate-");

  try {
    const indexPath = join(import.meta.dir, "..", "src", "index.ts");
    writeFileSync(
      join(root, "package.json"),
      JSON.stringify({
        name: "duplicate-provider",
        gestalt: {
          provider: {
            kind: "app",
            target: "./provider.ts#app",
          },
        },
      }),
      "utf8",
    );
    writeFileSync(
      join(root, "provider.ts"),
      `import { defineApp } from ${JSON.stringify(indexPath)};

export const app = defineApp({
  operations: [
    {
      id: "ping",
      handler() {
        return { ok: true };
      },
    },
    {
      id: " ping ",
      handler() {
        return { ok: false };
      },
    },
  ],
});
`,
      "utf8",
    );

    await expect(loadProviderFromTarget(root)).rejects.toThrow(
      'duplicate operation id "ping"',
    );
  } finally {
    removeTempDir(root);
  }
});

test("loadProviderFromTarget rejects structural app objects without the full runtime lifecycle contract", async () => {
  const root = makeTempDir("gestalt-typescript-runtime-structural-runtime-base-");

  try {
    writeFileSync(
      join(root, "package.json"),
      JSON.stringify({
        name: "structural-runtime-base-provider",
        gestalt: {
          provider: {
            kind: "app",
            target: "./provider.ts#app",
          },
        },
      }),
      "utf8",
    );
    writeFileSync(
      join(root, "provider.ts"),
      `export const app = {
  kind: "integration",
  name: "structural-runtime-base",
  displayName: "Structural Runtime Base",
  description: "structural app missing runtime base methods",
  version: "1.0.0",
  connectionMode: "unspecified",
  authTypes: [],
  connectionParams: {},
  resolveName() {},
  async configureProvider() {},
  staticCatalog() {
    return { name: "structural-runtime-base", operations: [] };
  },
  supportsSessionCatalog() {
    return false;
  },
  async catalogForRequest() {
    return undefined;
  },
  async resolveHTTPSubject() {
    return undefined;
  },
  async execute() {
    return { status: 200, body: "{}" };
  },
};`,
      "utf8",
    );

    await expect(loadProviderFromTarget(root)).rejects.toThrow(
      "app:./provider.ts#app did not resolve to a Gestalt app provider",
    );
  } finally {
    removeTempDir(root);
  }
});

test("runtime serves a secrets provider over unix gRPC", async () => {
  const runtimeEntry = join(import.meta.dir, "..", "src", "providers", "runtime.ts");
  const root = fixturePath("secrets-provider");
  const tempDir = makeTempDir("gestalt-typescript-runtime-");
  const socketPath = join(tempDir, "provider.sock");
  let child: ChildProcess | undefined;

  try {
    child = spawn(
      process.execPath,
      [runtimeEntry, root, "secrets:./secrets.ts"],
      {
        env: {
          ...process.env,
          [ENV_PROVIDER_SOCKET]: socketPath,
        },
        stdio: ["ignore", "ignore", "pipe"],
      },
    );
    const stderrText = captureChildStderr(child);

    try {
      await waitForPath(socketPath);
    } catch (error) {
      throw new Error(
        `${String(error)}${stderrText() ? `\n${stderrText()}` : ""}`,
      );
    }

    const runtime = createUnixGrpcClient(ProviderLifecycle, socketPath);
    const secrets = createUnixGrpcClient(SecretsProviderService, socketPath);

    const metadata = await runtime.getProviderIdentity(create(EmptySchema, {}));
    expect(metadata.kind).toBe(ProtoProviderKind.SECRETS);
    expect(metadata.name).toBe("secrets-provider");
    expect(metadata.displayName).toBe("Fixture Secrets");
    expect(metadata.minProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);
    expect(metadata.maxProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);

    await expectConnectCode(
      runtime.configureProvider(
        create(ConfigureProviderRequestSchema, {
          name: "fixture-secrets",
          config: {
            scope: "runtime",
          },
          protocolVersion: CURRENT_PROTOCOL_VERSION + 1,
        }),
      ),
      Code.FailedPrecondition,
    );

    const configured = await runtime.configureProvider(
      create(ConfigureProviderRequestSchema, {
        name: "fixture-secrets",
        config: {
          scope: "runtime",
        },
        protocolVersion: CURRENT_PROTOCOL_VERSION,
      }),
    );
    expect(configured.protocolVersion).toBe(CURRENT_PROTOCOL_VERSION);

    const secret = await secrets.getSecret(
      create(GetSecretRequestSchema, {
        name: "db-password",
      }),
    );
    expect(secret.value).toBe("fixture-secrets:runtime:hunter2");

    await expectConnectCode(
      secrets.getSecret(
        create(GetSecretRequestSchema, {
          name: "missing",
        }),
      ),
      Code.NotFound,
    );
  } finally {
    if (child) {
      await stopProcess(child);
    }
    removeTempDir(tempDir);
  }
}, 15_000);

test("integration provider service exposes metadata, configure, execute, and session catalog", async () => {
  const app = await loadProviderFromTarget(fixturePath("basic-provider"));
  const service = createProviderService(app);

  const metadata = await (service.getMetadata as any)();
  expect(metadata.name).toBe("basic-provider");
  expect(metadata.supportsSessionCatalog).toBe(true);
  expect(metadata.minProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);
  expect(metadata.maxProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);
  expect(
    metadata.staticCatalog?.operations?.some((op: any) => op.id === "hello"),
  ).toBe(true);
  const helloOperation = metadata.staticCatalog?.operations?.find(
    (op: any) => op.id === "hello",
  );
  expect(helloOperation?.allowedRoles).toEqual(["viewer", "admin"]);
  const inputSchema = JSON.parse(helloOperation?.inputSchema ?? "{}");
  expect(inputSchema.properties.name.default).toBe("World");
  expect(inputSchema.properties.name.description).toBe("Name to greet");
  const outputSchema = JSON.parse(helloOperation?.outputSchema ?? "{}");
  expect(outputSchema.properties.message.type).toBe("string");
  const nameParameter = helloOperation?.parameters?.find(
    (parameter: any) => parameter.name === "name",
  );
  expect(toJson(ValueSchema, nameParameter?.default as any)).toBe("World");

  await expectConnectCode(
    (service.startProvider as any)(
      create(StartProviderRequestSchema, {
        name: "configured-provider",
        config: {
          region: "use1",
        },
        protocolVersion: CURRENT_PROTOCOL_VERSION + 1,
      }),
    ),
    Code.FailedPrecondition,
  );

  const unconfiguredResult = await (service.execute as any)(
    create(ExecuteRequestSchema, {
      operation: "hello",
      params: {
        name: "Ada",
      },
      token: "token-123",
      connectionParams: {
        region: "iad",
      },
    }),
  );
  expect(jsonBody(unconfiguredResult.body)).toMatchObject({
    configuredName: "",
    configuredRegion: "",
  });

  const started = await (service.startProvider as any)(
    create(StartProviderRequestSchema, {
      name: "configured-provider",
      config: {
        region: "use1",
      },
      protocolVersion: CURRENT_PROTOCOL_VERSION,
    }),
  );
  expect(started.protocolVersion).toBe(CURRENT_PROTOCOL_VERSION);

  const result = await (service.execute as any)(
    create(ExecuteRequestSchema, {
      operation: "hello",
      params: {
        name: "Ada",
      },
      token: "token-123",
      connectionParams: {
        region: "iad",
      },
      context: create(RequestContextSchema, {
        subject: create(SubjectContextSchema, {
          id: "user:user-123",
          email: "ada@example.com",
        }),
        agentSubject: create(SubjectContextSchema, {
          id: "user:agent-456",
          email: "grace@example.com",
        }),
        credential: create(CredentialContextSchema, {
          mode: "subject",
          subjectId: "user:user-123",
        }),
        access: create(AccessContextSchema, {
          policy: "sample_policy",
          role: "admin",
        }),
      }),
      idempotencyKey: " tool-call-123 ",
    }),
  );
  expect(jsonBody(result.body)).toEqual({
    message: "Hello, Ada.",
    configuredName: "configured-provider",
    region: "iad",
    configuredRegion: "use1",
    subjectId: "user:user-123",
    subjectEmail: "ada@example.com",
    agentSubjectEmail: "grace@example.com",
    credentialMode: "subject",
    accessPolicy: "sample_policy",
    accessRole: "admin",
    idempotencyKey: "tool-call-123",
  });

  const sessionCatalog = await (service.getSessionCatalog as any)(
    create(GetSessionCatalogRequestSchema, {
      token: "token-123",
      connectionParams: {
        scope: "ops",
      },
      context: create(RequestContextSchema, {
        subject: create(SubjectContextSchema, {
          id: "user:user-123",
        }),
        credential: create(CredentialContextSchema, {
          mode: "subject",
        }),
        access: create(AccessContextSchema, {
          policy: "sample_policy",
          role: "viewer",
        }),
      }),
    }),
  );
  expect(sessionCatalog.catalog?.name).toBe("fixture-session");
  expect(sessionCatalog.catalog?.operations).toHaveLength(1);
  expect(sessionCatalog.catalog?.operations[0].id).toBe("session-hello");
  expect(sessionCatalog.catalog?.operations[0].method).toBe("GET");
  expect(sessionCatalog.catalog?.operations[0].allowedRoles).toEqual([
    "viewer",
    "admin",
  ]);
  expect(sessionCatalog.catalog?.operations[0].title).toBe(
    "Session Hello ops user:user-123 subject viewer",
  );

});

test("integration provider service labels metadata failures", async () => {
  const app = defineApp({
    operations: [
      {
        id: "noop",
        handler() {
          return { ok: true };
        },
      },
    ],
  });
  (app as any).supportsSessionCatalog = () => {
    throw new Error("metadata exploded");
  };

  try {
    await (createProviderService(app).getMetadata as any)();
    throw new Error("expected getMetadata to fail");
  } catch (error) {
    expect(error).toBeInstanceOf(ConnectError);
    expect((error as ConnectError).code).toBe(Code.Unknown);
    expect((error as ConnectError).message).toContain(
      "provider metadata: metadata exploded",
    );
  }
});

test("integration provider service resolves hosted HTTP subjects through the app hook", async () => {
  let seenRequest:
    | import("../src/index.ts").HTTPSubjectRequest
    | undefined;
  let seenContext:
    | import("../src/index.ts").HTTPSubjectResolutionContext
    | undefined;

  const app = defineApp({
    resolveHTTPSubject(request, context) {
      seenRequest = request;
      seenContext = context;
      if (request.binding !== "command") {
        return null;
      }
      return {
        id: "user:user-456",
        kind: "user",
        displayName: "Slack User",
        authSource: "slack",
      };
    },
    operations: [
      {
        id: "noop",
        handler() {
          return { ok: true };
        },
      },
    ],
  });
  const service = createProviderService(app);

  const resolved = await (service.resolveHTTPSubject as any)(
    create(ResolveHTTPSubjectRequestSchema, {
      request: create(HTTPSubjectRequestSchema, {
        binding: "command",
        method: "POST",
        path: "/api/v1/agent/command",
        contentType: "application/x-www-form-urlencoded",
        headers: {
          "x-slack-signature": create(StringListSchema, {
            values: ["v0=abc123"],
          }),
        },
        query: {
          trace: create(StringListSchema, {
            values: ["a", "b"],
          }),
        },
        params: {
          team_id: "T123",
          user_id: "U456",
        },
        rawBody: new Uint8Array([112, 97, 121, 108, 111, 97, 100]),
        securityScheme: "slack",
        verifiedSubject: "slack:T123:U456",
        verifiedClaims: {
          team_id: "T123",
        },
      }),
      context: create(RequestContextSchema, {
        subject: create(SubjectContextSchema, {
          id: "system:http_binding:agent:command",
        }),
        credential: create(CredentialContextSchema, {
          mode: "none",
        }),
        access: create(AccessContextSchema, {
          policy: "hosted_http",
          role: "binding",
        }),
        host: create(HostContextSchema, {
          publicBaseUrl: "https://gestalt.example.test",
        }),
        workflow: {
          http: {
            binding: "command",
          },
        },
      }),
    }),
  );
  expect(resolved.subject).toMatchObject({
    id: "user:user-456",
    email: "",
    displayName: "Slack User",
  });
  expect(seenRequest).toEqual({
    binding: "command",
    method: "POST",
    path: "/api/v1/agent/command",
    contentType: "application/x-www-form-urlencoded",
    headers: {
      "x-slack-signature": ["v0=abc123"],
    },
    query: {
      trace: ["a", "b"],
    },
    params: {
      team_id: "T123",
      user_id: "U456",
    },
    rawBody: new Uint8Array([112, 97, 121, 108, 111, 97, 100]),
    securityScheme: "slack",
    verifiedSubject: "slack:T123:U456",
    verifiedClaims: {
      team_id: "T123",
    },
  });
  expect(seenContext).toEqual({
    subject: {
      id: "system:http_binding:agent:command",
      email: "",
      displayName: "",
      kind: "system",
      scopes: [],
      permissions: [],
    },
    credential: {
      mode: "none",
      subjectId: "",
      connection: "",
      instance: "",
    },
    access: {
      policy: "hosted_http",
      role: "binding",
    },
    host: {
      publicBaseUrl: "https://gestalt.example.test",
    },
    workflow: {
      http: {
        binding: "command",
      },
    },
  });

  const fallback = await (service.resolveHTTPSubject as any)(
    create(ResolveHTTPSubjectRequestSchema, {
      request: create(HTTPSubjectRequestSchema, {
        binding: "events",
      }),
    }),
  );
  expect(fallback.subject).toBeUndefined();

  const rejected = await (createProviderService(
    defineApp({
      resolveHTTPSubject() {
        throw httpSubjectError(403, "unmapped slack subject");
      },
      operations: [
        {
          id: "noop",
          handler() {
            return { ok: true };
          },
        },
      ],
    }),
  ).resolveHTTPSubject as any)(
    create(ResolveHTTPSubjectRequestSchema, {
      request: create(HTTPSubjectRequestSchema, {
        binding: "command",
      }),
    }),
  );
  expect(rejected.rejectStatus).toBe(403);
  expect(rejected.rejectMessage).toBe("unmapped slack subject");

  await expectConnectCode(
    (createProviderService(
      defineApp({
        resolveHTTPSubject() {
          throw new Error("boom");
        },
        operations: [
          {
            id: "noop",
            handler() {
              return { ok: true };
            },
          },
        ],
      }),
    ).resolveHTTPSubject as any)(
      create(ResolveHTTPSubjectRequestSchema, {
        request: create(HTTPSubjectRequestSchema, {
          binding: "command",
        }),
      }),
    ),
    Code.Unknown,
  );
});

test("integration provider service preserves body-shaped outputs and explicit responses", async () => {
  const root = makeTempDir("gestalt-typescript-runtime-outputs-");

  try {
    const indexPath = join(import.meta.dir, "..", "src", "index.ts");
    writeFileSync(
      join(root, "package.json"),
      JSON.stringify({
        name: "output-provider",
        gestalt: {
          provider: {
            kind: "app",
            target: "./provider.ts#app",
          },
        },
      }),
      "utf8",
    );
    writeFileSync(
      join(root, "provider.ts"),
      `import { defineApp, response, s } from ${JSON.stringify(indexPath)};

export const app = defineApp({
  operations: [
    {
      id: "echo-body",
      output: s.object({
        body: s.string(),
      }),
      handler() {
        return {
          body: "hello",
        };
      },
    },
    {
      id: "echo-status-body",
      output: s.object({
        status: s.integer(),
        body: s.string(),
      }),
      handler() {
        return {
          status: 42,
          body: "payload",
        };
      },
    },
    {
      id: "created",
      output: s.object({
        id: s.string(),
      }),
      handler() {
        return response(
          201,
          {
            id: "new-id",
          },
          {
            Location: "/items/new-id",
          },
        );
      },
    },
    {
      id: "explode",
      handler() {
        throw new Error("boom");
      },
    },
  ],
});
`,
      "utf8",
    );

    const app = await loadProviderFromTarget(root);
    const service = createProviderService(app);

    const echoedBody = await (service.execute as any)(
      create(ExecuteRequestSchema, {
        operation: "echo-body",
      }),
    );
    expect(echoedBody.status).toBe(200);
    expect(jsonBody(echoedBody.body)).toEqual({
      body: "hello",
    });

    const echoedStatusBody = await (service.execute as any)(
      create(ExecuteRequestSchema, {
        operation: "echo-status-body",
      }),
    );
    expect(echoedStatusBody.status).toBe(200);
    expect(jsonBody(echoedStatusBody.body)).toEqual({
      status: 42,
      body: "payload",
    });

    const created = await (service.execute as any)(
      create(ExecuteRequestSchema, {
        operation: "created",
      }),
    );
    expect(created.status).toBe(201);
    expect(created.headers["Content-Type"]?.values).toEqual([
      "application/json",
    ]);
    expect(created.headers["Location"]?.values).toEqual(["/items/new-id"]);
    expect(jsonBody(created.body)).toEqual({
      id: "new-id",
    });

    const unknown = await (service.execute as any)(
      create(ExecuteRequestSchema, {
        operation: "missing",
      }),
    );
    expect(unknown.status).toBe(404);
    expect(jsonBody(unknown.body)).toEqual({
      error: "unknown operation",
    });

    const exploded = await (service.execute as any)(
      create(ExecuteRequestSchema, {
        operation: "explode",
      }),
    );
    expect(exploded.status).toBe(500);
    expect(jsonBody(exploded.body)).toEqual({
      error: "boom",
    });
  } finally {
    removeTempDir(root);
  }
});

test("identity provider supports runtime metadata, OAuth flows, and introspection", async () => {
  const provider = await loadProviderFromTarget(fixturePath("auth-provider"));
  const runtime = createRuntimeService(provider);
  const auth = createIdentityService(provider as any);

  await expectConnectCode(
    (runtime.configureProvider as any)(
      create(ConfigureProviderRequestSchema, {
        name: "fixture-auth",
        config: {
          issuer: "https://login.example.test",
        },
        protocolVersion: CURRENT_PROTOCOL_VERSION + 1,
      }),
    ),
    Code.FailedPrecondition,
  );

  const configuredAuth = await (runtime.configureProvider as any)(
    create(ConfigureProviderRequestSchema, {
      name: "fixture-auth",
      config: {
        issuer: "https://login.example.test",
      },
      protocolVersion: CURRENT_PROTOCOL_VERSION,
    }),
  );
  expect(configuredAuth.protocolVersion).toBe(CURRENT_PROTOCOL_VERSION);

  const metadata = await (runtime.getProviderIdentity as any)(
    create(EmptySchema, {}),
  );
  expect(metadata.kind).toBe(ProtoProviderKind.IDENTITY);
  expect(metadata.displayName).toBe("Fixture Auth");
  expect(metadata.minProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);
  expect(metadata.maxProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);

  const authorize = await (auth.authorize as any)(
    create(AuthorizeRequestSchema, {
      responseType: "code",
      clientId: "gestaltd",
      redirectUri: "https://app.example.test/callback",
      scope: "openid",
      state: "host-state",
    }),
  );
  expect(authorize.redirectUri).toContain("code=fixture-auth-code");

  const token = await (auth.token as any)(
    create(TokenRequestSchema, {
      grantType: "authorization_code",
      code: "fixture-auth-code",
      redirectUri: "https://app.example.test/callback",
      clientId: "gestaltd",
    }),
  );
  expect(token.accessToken).toBe("fixture-access-token");

  const introspected = await (auth.introspect as any)(
    create(IntrospectRequestSchema, {
      token: token.accessToken,
      tokenTypeHint: "access_token",
    }),
  );
  expect(introspected.active).toBe(true);
  expect(introspected.subject).toBe("user:fixture@example.com");
});

test("identity provider token forwards the request-side expiresIn hint", async () => {
  const provider = await loadProviderFromTarget(fixturePath("auth-provider"));
  const auth = createIdentityService(provider as any);

  const token = await (auth.token as any)(
    create(TokenRequestSchema, {
      grantType: "authorization_code",
      code: "fixture-auth-code",
      redirectUri: "https://app.example.test/callback",
      clientId: "gestaltd",
      expiresIn: 7776000n,
    }),
  );
  // The fixture echoes request.expiresIn back through the runtime adapter;
  // 7776000n seconds (90 days) proves the hint reached the provider handler.
  expect(token.expiresIn).toBe(7776000n);
});

test("runtime lifecycle labels provider identity failures", async () => {
  const app = defineApp({
    operations: [
      {
        id: "noop",
        handler() {
          return { ok: true };
        },
      },
    ],
  });
  (app as any).warnings = async () => {
    throw new Error("identity exploded");
  };

  try {
    await (createRuntimeService(app).getProviderIdentity as any)(
      create(EmptySchema, {}),
    );
    throw new Error("expected getProviderIdentity to fail");
  } catch (error) {
    expect(error).toBeInstanceOf(ConnectError);
    expect((error as ConnectError).code).toBe(Code.Unknown);
    expect((error as ConnectError).message).toContain(
      "provider identity: identity exploded",
    );
  }
});

test("runtime lifecycle start is separate from configure", async () => {
  const calls: string[] = [];
  const provider = defineCacheProvider({
    name: "lifecycle-cache",
    configure(name, config) {
      calls.push(`configure:${name}:${config.prefix ?? ""}`);
    },
    start() {
      calls.push("start");
    },
    async get() {
      return undefined;
    },
    async set() {},
    async delete() {
      return false;
    },
    async touch() {
      return false;
    },
  });
  const runtime = createRuntimeService(provider);

  const configured = await (runtime.configureProvider as any)(
    create(ConfigureProviderRequestSchema, {
      name: "fixture-cache",
      config: {
        prefix: "runtime",
      },
      protocolVersion: CURRENT_PROTOCOL_VERSION,
    }),
  );
  expect(configured.protocolVersion).toBe(CURRENT_PROTOCOL_VERSION);
  expect(calls).toEqual(["configure:fixture-cache:runtime"]);

  const started = await (runtime.startProvider as any)(create(EmptySchema, {}));
  expect(started.protocolVersion).toBe(CURRENT_PROTOCOL_VERSION);
  expect(calls).toEqual(["configure:fixture-cache:runtime", "start"]);
});

test("runtime lifecycle start is no-op when provider has no start hook", async () => {
  const provider = defineCacheProvider({
    name: "no-start-cache",
    async get() {
      return undefined;
    },
    async set() {},
    async delete() {
      return false;
    },
    async touch() {
      return false;
    },
  });
  const runtime = createRuntimeService(provider);

  const started = await (runtime.startProvider as any)(create(EmptySchema, {}));
  expect(started.protocolVersion).toBe(CURRENT_PROTOCOL_VERSION);
});

test("cache provider supports runtime metadata and cache operations", async () => {
  const provider = await loadProviderFromTarget(fixturePath("cache-provider"));
  const runtime = createRuntimeService(provider);
  const cache = createCacheService(provider as any);

  const configuredCache = await (runtime.configureProvider as any)(
    create(ConfigureProviderRequestSchema, {
      name: "fixture-cache",
      config: {
        prefix: "runtime",
      },
      protocolVersion: CURRENT_PROTOCOL_VERSION,
    }),
  );
  expect(configuredCache.protocolVersion).toBe(CURRENT_PROTOCOL_VERSION);

  const metadata = await (runtime.getProviderIdentity as any)(
    create(EmptySchema, {}),
  );
  expect(metadata.kind).toBe(ProtoProviderKind.CACHE);
  expect(metadata.displayName).toBe("Fixture Cache");
  expect(metadata.minProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);
  expect(metadata.maxProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);

  const encoder = new TextEncoder();
  const decoder = new TextDecoder();

  await (cache.set as any)(
    create(CacheSetRequestSchema, {
      key: "alpha",
      value: encoder.encode("one"),
      ttl: {
        seconds: 1n,
        nanos: 500_000_000,
      },
    }),
  );

  await (cache.setMany as any)(
    create(CacheSetManyRequestSchema, {
      entries: [
        create(CacheSetEntrySchema, {
          key: "beta",
          value: encoder.encode("two"),
        }),
        create(CacheSetEntrySchema, {
          key: "gamma",
          value: encoder.encode("three"),
        }),
        create(CacheSetEntrySchema, {
          key: "toString",
          value: encoder.encode("reserved"),
        }),
        create(CacheSetEntrySchema, {
          key: "__proto__",
          value: encoder.encode("proto"),
        }),
      ],
    }),
  );

  const getAlpha = await (cache.get as any)(
    create(CacheGetRequestSchema, {
      key: "alpha",
    }),
  );
  expect(getAlpha.found).toBe(true);
  expect(decoder.decode(getAlpha.value)).toBe("one");

  const getMany = await (cache.getMany as any)(
    create(CacheGetManyRequestSchema, {
      keys: ["alpha", "missing", "gamma"],
    }),
  );
  expect(getMany.entries).toHaveLength(3);
  expect(getMany.entries[0]).toMatchObject({
    key: "alpha",
    found: true,
  });
  expect(decoder.decode(getMany.entries[0].value)).toBe("one");
  expect(getMany.entries[1]).toMatchObject({
    key: "missing",
    found: false,
  });
  expect(getMany.entries[2]).toMatchObject({
    key: "gamma",
    found: true,
  });
  const reservedMany = await (cache.getMany as any)(
    create(CacheGetManyRequestSchema, {
      keys: ["toString", "__proto__", "missing"],
    }),
  );
  expect(reservedMany.entries).toHaveLength(3);
  expect(reservedMany.entries[0]).toMatchObject({
    key: "toString",
    found: true,
  });
  expect(decoder.decode(reservedMany.entries[0].value)).toBe("reserved");
  expect(reservedMany.entries[1]).toMatchObject({
    key: "__proto__",
    found: true,
  });
  expect(decoder.decode(reservedMany.entries[1].value)).toBe("proto");
  expect(reservedMany.entries[2]).toMatchObject({
    key: "missing",
    found: false,
  });

  const touched = await (cache.touch as any)(
    create(CacheTouchRequestSchema, {
      key: "gamma",
      ttl: {
        seconds: 2n,
        nanos: 0,
      },
    }),
  );
  expect(touched.touched).toBe(true);

  const deleted = await (cache.delete as any)(
    create(CacheDeleteRequestSchema, {
      key: "beta",
    }),
  );
  expect(deleted.deleted).toBe(true);

  const deleteMany = await (cache.deleteMany as any)(
    create(CacheDeleteManyRequestSchema, {
      keys: ["alpha", "missing", "gamma", "toString", "__proto__"],
    }),
  );
  expect(deleteMany.deleted).toBe(4n);
});

test("cache provider deleteMany fallback deletes each unique key once", async () => {
  const calls: string[] = [];
  const provider = defineCacheProvider({
    async get() {
      return undefined;
    },
    async set() {},
    async delete(key) {
      calls.push(key);
      return key !== "missing";
    },
    async touch() {
      return false;
    },
  });

  expect(
    await provider.deleteMany([
      "alpha",
      "alpha",
      "missing",
      "beta",
      "beta",
      "missing",
    ]),
  ).toBe(2);
  expect(calls).toEqual(["alpha", "missing", "beta"]);
});

test("s3 provider target resolves and serves runtime metadata plus object operations", async () => {
  const provider = await loadProviderFromTarget(fixturePath("s3-provider"));
  const runtime = createRuntimeService(provider);
  const s3 = createS3Service(provider as any);

  const configuredS3 = await (runtime.configureProvider as any)(
    create(ConfigureProviderRequestSchema, {
      name: "fixture-s3",
      config: {},
      protocolVersion: CURRENT_PROTOCOL_VERSION,
    }),
  );
  expect(configuredS3.protocolVersion).toBe(CURRENT_PROTOCOL_VERSION);

  const metadata = await (runtime.getProviderIdentity as any)(
    create(EmptySchema, {}),
  );
  expect(metadata.kind).toBe(ProtoProviderKind.S3);
  expect(metadata.displayName).toBe("Fixture S3");
  expect(metadata.minProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);
  expect(metadata.maxProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);

  const written = await (s3.writeObject as any)(
    (async function* () {
      yield {
        msg: {
          case: "open",
          value: {
            ref: {
              key: "runtime.txt",
            },
            contentType: "text/plain",
            metadata: {
              env: "test",
            },
          },
        },
      };
      yield {
        msg: {
          case: "data",
          value: new TextEncoder().encode("runtime"),
        },
      };
    })(),
  );
  expect(written.meta?.ref?.key).toBe("runtime.txt");

  const headed = await (s3.headObject as any)({
    ref: {
      key: "runtime.txt",
    },
  });
  expect(headed.meta?.size).toBe(7n);

  const listed = await (s3.listObjects as any)({});
  expect(listed.objects.map((object: any) => object.ref?.key)).toEqual([
    "runtime.txt",
  ]);

  const copied = await (s3.copyObject as any)({
    source: {
      key: "runtime.txt",
    },
    destination: {
      key: "copy.txt",
    },
  });
  expect(copied.meta?.ref?.key).toBe("copy.txt");

  const presigned = await (s3.presignObject as any)({
    ref: {
      key: "copy.txt",
    },
    method: 2,
    headers: {
      "x-test": "1",
    },
  });
  expect(presigned.url).toContain("method=PUT");
  expect(presigned.headers).toEqual({ "x-test": "1" });
});

test("runtime provider serves runtime metadata plus sessions", async () => {
  let startAppWorkdir: string | undefined;
  const provider = defineRuntimeProvider({
    name: "runtime-provider",
    displayName: "Fixture Runtime",
    warnings: ["set RUNTIME_ENDPOINT"],
    getSupport() {
      return {
        canHostApps: true,
        egressMode: RuntimeEgressMode.HOSTNAME,
        supportsPrepareWorkspace: true,
      };
    },
    startSession(request) {
      return {
        id: `${request.appName || "app"}-session`,
        state: "ready",
      };
    },
    getSession(request) {
      return {
        id: request.sessionId,
        state: "ready",
      };
    },
    listSessions(request) {
      expect(request.pageSize).toBe(100);
      expect(request.pageToken).toBe("");
      return {
        sessions: [{ id: "app-session", state: "ready" }],
        nextPageToken: "next-page",
      };
    },
    stopSession() {},
    prepareWorkspace(request) {
      return {
        workspace: {
          root: `/runtime/${request.agentSessionId}`,
          cwd: `/runtime/${request.agentSessionId}/app`,
        },
      };
    },
    removeWorkspace() {},
    startApp(request) {
      startAppWorkdir = request.workdir;
      return {
        id: "hosted-app-1",
        sessionId: request.sessionId,
        appName: request.appName,
        dialTarget: "unix:///tmp/app.sock",
      };
    },
  });

  const runtime = createRuntimeService(provider as any);
  const service = createRuntimeProviderService(provider);
  const identity = await (runtime.getProviderIdentity as any)(
    create(EmptySchema),
  );
  expect(identity.kind).toBe(ProtoProviderKind.RUNTIME);
  expect(identity.name).toBe("runtime-provider");
  expect(identity.displayName).toBe("Fixture Runtime");
  expect(identity.warnings).toEqual(["set RUNTIME_ENDPOINT"]);

  const support = await (service.getSupport as any)(create(EmptySchema));
  expect(support.canHostApps).toBe(true);
  expect(support.egressMode).toBe(RuntimeEgressMode.HOSTNAME);
  expect(support.supportsPrepareWorkspace).toBe(true);

  const session = await (service.startSession as any)(
    create(StartRuntimeSessionRequestSchema, {
      appName: "app",
      image: "example/app:latest",
    }),
  );
  expect(session.id).toBe("app-session");
  expect(session.state).toBe("ready");

  const fetched = await (service.getSession as any)(
    create(GetRuntimeSessionRequestSchema, {
      sessionId: "app-session",
    }),
  );
  expect(fetched.id).toBe("app-session");

  const sessions = await (service.listSessions as any)(
    create(ListRuntimeSessionsRequestSchema),
  );
  expect(sessions.sessions).toHaveLength(1);
  expect(sessions.sessions[0].id).toBe("app-session");
  expect(sessions.nextPageToken).toBe("next-page");

  const prepared = await (service.prepareWorkspace as any)(
    create(PrepareRuntimeWorkspaceRequestSchema, {
      sessionId: "app-session",
      agentSessionId: "agent-session-1",
      workspace: {
        cwd: "app",
        checkouts: [{
          url: "git@github.com:valon-technologies/app.git",
          ref: "refs/heads/main",
          path: "app",
        }],
      },
    }),
  );
  expect(prepared.workspace?.cwd).toBe("/runtime/agent-session-1/app");

  const removed = await (service.removeWorkspace as any)(
    create(RemoveRuntimeWorkspaceRequestSchema, {
      sessionId: "app-session",
      agentSessionId: "agent-session-1",
    }),
  );
  expect(removed.$typeName).toBe("google.protobuf.Empty");

  const hosted = await (service.startApp as any)(
    create(StartHostedAppRequestSchema, {
      sessionId: "app-session",
      appName: "app",
      workdir: "/runtime/providers/app",
    }),
  );
  expect(hosted).toMatchObject({
    id: "hosted-app-1",
    sessionId: "app-session",
    appName: "app",
    dialTarget: "unix:///tmp/app.sock",
  });
  expect(startAppWorkdir).toBe("/runtime/providers/app");

  const stopped = await (service.stopSession as any)(
    create(StopRuntimeSessionRequestSchema, {
      sessionId: "app-session",
    }),
  );
  expect(stopped.$typeName).toBe("google.protobuf.Empty");
});

test("workflow provider target resolves and serves runtime metadata plus workflow operations", async () => {
  const provider = await loadProviderFromTarget(
    fixturePath("workflow-provider"),
  );
  const runtime = createRuntimeService(provider);
  const workflow = createWorkflowProviderService(provider as any);

  await (runtime.configureProvider as any)(
    create(ConfigureProviderRequestSchema, {
      name: "fixture-workflow",
      config: {},
      protocolVersion: CURRENT_PROTOCOL_VERSION,
    }),
  );

  const metadata = await (runtime.getProviderIdentity as any)(
    create(EmptySchema, {}),
  );
  expect(metadata.kind).toBe(ProtoProviderKind.WORKFLOW);
  expect(metadata.displayName).toBe("Fixture Workflow");

  const definition = await (workflow.applyDefinition as any)(
    create(ApplyWorkflowProviderDefinitionRequestSchema, {
      idempotencyKey: "def-1",
      requestedBySubjectId: "service_account:planner",
      spec: {
        id: "roadmap_sync",
        target: workflowAppStepTarget("roadmap", "sync", {
          input: { object: { project: { literal: "alpha" } } },
        }),
        activations: [{
          id: "nightly",
          trigger: {
            case: "schedule",
            value: { cron: "*/5 * * * *", timezone: "UTC" },
          },
        }],
      },
    }),
  );
  expect(definition.id).toBe("roadmap_sync");
  expect(definition.createdBySubjectId).toBe("service_account:planner");

  const run = await (workflow.startRun as any)(
    create(StartWorkflowProviderRunRequestSchema, {
      idempotencyKey: "req-1",
      definitionId: "roadmap_sync",
      createdBySubjectId: "user:user-123",
    }),
  );
  const runApp = run.target?.steps[0]?.action.case === "app"
    ? run.target.steps[0].action.value
    : undefined;
  if (runApp === undefined) {
    throw new Error("workflow run target does not have an app step");
  }
  expect(runApp.name).toBe("roadmap");
  expect(run.status).toBe(WorkflowRunStatus.PENDING);
  expect(run.statusMessage).toBe("idempotency:req-1");
  expect(run.createdBySubjectId).toBe("user:user-123");

  const pausedDefinition = await (workflow.setActivationPaused as any)({
    definitionId: "roadmap_sync",
    activationId: "nightly",
    paused: true,
  });
  expect(pausedDefinition.activations[0]?.paused).toBe(true);

  await (workflow.deliverEvent as any)(
    create(DeliverWorkflowProviderEventRequestSchema, {
      appName: "roadmap",
      event: {
        id: "evt-1",
        source: "tests",
        specVersion: "1.0",
        type: "roadmap.changed",
      },
    }),
  );

  const refreshedMetadata = await (runtime.getProviderIdentity as any)(
    create(EmptySchema, {}),
  );
  expect(refreshedMetadata.warnings).toEqual(["delivered-events:1"]);
});


test("integration provider request context includes workflow metadata", async () => {
  const app = defineApp({
    operations: [
      {
        id: "inspect",
        handler(_input, request) {
          return {
            host: request.host,
            workflow: request.workflow,
            toolRefsSet: request.toolRefsSet,
            toolRefs: request.toolRefs,
          };
        },
      },
    ],
  });
  const service = createProviderService(app);

  const result = await (service.execute as any)(
    create(ExecuteRequestSchema, {
      operation: "inspect",
      params: {},
      token: "token-123",
      context: create(RequestContextSchema, {
        host: create(HostContextSchema, {
          publicBaseUrl: "https://gestalt.example.test",
        }),
        workflow: {
          runId: "run-123",
          provider: "temporal",
          executionRef: "exec-ref-123",
          createdBySubjectId: "user:user-123",
          target: {
            kind: "steps",
            steps: [
              {
                id: "sync",
                kind: "app",
                app: "demo",
                operation: "sync",
              },
            ],
          },
          trigger: {
            kind: "event",
            activationId: "activation-1",
            event: {
              id: "evt-1",
              source: "urn:test",
              specVersion: "1.0",
              type: "demo.refresh",
              dataContentType: "application/json",
            },
          },
          input: {
            customerId: "cust_123",
          },
          metadata: {
            attempt: 2,
          },
        },
        toolRefs: [
          create(AgentToolRefSchema, {
            app: "github",
            operation: "bot.getPullRequest",
            runAs: create(SubjectContextSchema, {
              id: "service_account:github-review",
            }),
          }),
        ],
        toolRefsSet: true,
      }),
    }),
  );

  expect(jsonBody(result.body)).toEqual({
    host: {
      publicBaseUrl: "https://gestalt.example.test",
    },
    workflow: {
      runId: "run-123",
      provider: "temporal",
      executionRef: "exec-ref-123",
      createdBySubjectId: "user:user-123",
      target: {
        kind: "steps",
        steps: [
          {
            id: "sync",
            kind: "app",
            app: "demo",
            operation: "sync",
          },
        ],
      },
      trigger: {
        kind: "event",
        activationId: "activation-1",
        event: {
          id: "evt-1",
          source: "urn:test",
          specVersion: "1.0",
          type: "demo.refresh",
          dataContentType: "application/json",
        },
      },
      input: {
        customerId: "cust_123",
      },
      metadata: {
        attempt: 2,
      },
    },
    toolRefsSet: true,
    toolRefs: [
      {
        app: "github",
        operation: "bot.getPullRequest",
        connection: "",
        instance: "",
        title: "",
        description: "",
        credentialMode: "",
        system: "",
        runAs: {
          id: "service_account:github-review",
          email: "",
          displayName: "",
          scopes: [],
          permissions: [],
        },
      },
    ],
  });

  const omittedToolRefs = await (service.execute as any)(
    create(ExecuteRequestSchema, {
      operation: "inspect",
      params: {},
      token: "token-123",
      context: create(RequestContextSchema, {
        host: create(HostContextSchema, {
          publicBaseUrl: "https://gestalt.example.test",
        }),
      }),
    }),
  );

  expect(jsonBody(omittedToolRefs.body)).toMatchObject({
    host: {
      publicBaseUrl: "https://gestalt.example.test",
    },
    toolRefsSet: false,
    toolRefs: [],
  });
});

test("s3 writeObject closes unread request frames when provider returns early", async () => {
  let requestClosed = false;
  const provider = defineS3Provider({
    async headObject(ref) {
      return {
        ref,
        etag: "",
        size: 0n,
        contentType: "",
        metadata: {},
        storageClass: "",
      };
    },
    async readObject(ref) {
      return {
        meta: {
          ref,
          etag: "",
          size: 0n,
          contentType: "",
          metadata: {},
          storageClass: "",
        },
      };
    },
    async writeObject(ref, body) {
      const iterator = body[Symbol.asyncIterator]();
      const first = await iterator.next();
      expect(first.done).toBe(false);
      return {
        ref,
        etag: "etag",
        size: BigInt(first.value?.byteLength ?? 0),
        contentType: "text/plain",
        metadata: {},
        storageClass: "STANDARD",
      };
    },
    async deleteObject() {},
    async listObjects() {
      return {
        objects: [],
        commonPrefixes: [],
        nextContinuationToken: "",
        hasMore: false,
      };
    },
    async copyObject(_source, destination) {
      return {
        ref: destination,
        etag: "",
        size: 0n,
        contentType: "",
        metadata: {},
        storageClass: "",
      };
    },
    async presignObject() {
      return {
        url: "https://example.invalid",
        method: PresignMethod.GET,
        headers: {},
      };
    },
  });
  const s3 = createS3Service(provider);

  const response = await (s3.writeObject as any)(
    (async function* () {
      try {
        yield {
          msg: {
            case: "open",
            value: {
              ref: {
                key: "runtime.txt",
              },
            },
          },
        };
        yield {
          msg: {
            case: "data",
            value: new TextEncoder().encode("hello"),
          },
        };
        yield {
          msg: {
            case: "data",
            value: new TextEncoder().encode("goodbye"),
          },
        };
      } finally {
        requestClosed = true;
      }
    })(),
  );

  expect(response.meta?.size).toBe(5n);
  expect(requestClosed).toBe(true);
});
