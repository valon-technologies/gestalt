import { spawn, type ChildProcess } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import { create } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { expect, test } from "bun:test";

import {
  BeginLoginRequestSchema,
  CompleteLoginRequestSchema,
  ValidateExternalTokenRequestSchema,
} from "../gen/v1/authentication_pb.ts";
import {
  CacheDeleteManyRequestSchema,
  CacheDeleteRequestSchema,
  CacheGetManyRequestSchema,
  CacheGetRequestSchema,
  CacheSetEntrySchema,
  CacheSetManyRequestSchema,
  CacheSetRequestSchema,
  CacheTouchRequestSchema,
} from "../gen/v1/cache_pb.ts";
import {
  AccessContextSchema,
  CredentialContextSchema,
  ExecuteRequestSchema,
  GetSessionCatalogRequestSchema,
  RequestContextSchema,
  StartProviderRequestSchema,
  SubjectContextSchema,
} from "../gen/v1/plugin_pb.ts";
import {
  GetSecretRequestSchema,
  SecretsProvider as SecretsProviderService,
} from "../gen/v1/secrets_pb.ts";
import {
  ConfigureProviderRequestSchema,
  ProviderKind as ProtoProviderKind,
  ProviderLifecycle,
} from "../gen/v1/runtime_pb.ts";
import {
  PublishWorkflowProviderEventRequestSchema,
  StartWorkflowProviderRunRequestSchema,
  UpsertWorkflowProviderScheduleRequestSchema,
} from "../gen/v1/workflow_pb.ts";
import {
  CURRENT_PROTOCOL_VERSION,
  createCacheService,
  ENV_PROVIDER_SOCKET,
  createAuthenticationService,
  createProviderService,
  createRuntimeService,
  createWorkflowProviderService,
  loadProviderFromTarget,
  main,
  parseRuntimeArgs,
} from "../src/runtime.ts";
import {
  PresignMethod,
  S3,
  WorkflowRunStatus,
  defineCacheProvider,
  definePlugin,
  defineS3Provider,
} from "../src/index.ts";
import { createS3Service } from "../src/s3.ts";
import {
  captureChildStderr,
  createUnixGrpcClient,
  fixturePath,
  makeTempDir,
  removeTempDir,
  stopProcess,
  waitForPath,
} from "./helpers.ts";

const ENV_WRITE_CATALOG = "GESTALT_PLUGIN_WRITE_CATALOG";
const ENV_WRITE_MANIFEST_METADATA = "GESTALT_PLUGIN_WRITE_MANIFEST_METADATA";

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

test("runtime arg parsing requires root and target", () => {
  expect(parseRuntimeArgs(["root", "plugin:./provider.ts#plugin"])).toEqual({
    root: "root",
    target: "plugin:./provider.ts#plugin",
  });
  expect(parseRuntimeArgs(["root"])).toBeUndefined();
});

test("catalog export handshake env vars stay out of the public TypeScript API", async () => {
  const runtimeModule = await import("../src/runtime.ts");
  const packageModule = await import("../src/index.ts");

  expect(Object.hasOwn(runtimeModule, "ENV_WRITE_CATALOG")).toBe(false);
  expect(Object.hasOwn(runtimeModule, "ENV_WRITE_MANIFEST_METADATA")).toBe(
    false,
  );
  expect(Object.hasOwn(packageModule, "ENV_WRITE_CATALOG")).toBe(false);
  expect(Object.hasOwn(packageModule, "ENV_WRITE_MANIFEST_METADATA")).toBe(
    false,
  );
  expect(Object.hasOwn(runtimeModule, "ENV_PROVIDER_SOCKET")).toBe(true);
  expect(Object.hasOwn(packageModule, "ENV_PROVIDER_SOCKET")).toBe(true);
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
            kind: "plugin",
            target: "./provider.ts#plugin",
          },
        },
      }),
      "utf8",
    );
    writeFileSync(
      join(root, "provider.ts"),
      `import { definePlugin, s } from ${JSON.stringify(indexPath)};

export const plugin = definePlugin({
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
    const code = await main([root, "plugin:./provider.ts#plugin"]);
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

test("runtime main writes generated manifest metadata when requested", async () => {
  const root = makeTempDir("gestalt-typescript-runtime-manifest-metadata-");
  const metadataPath = join(root, "manifest-metadata.yaml");
  const previousManifestMetadata = process.env[ENV_WRITE_MANIFEST_METADATA];

  try {
    const indexPath = join(import.meta.dir, "..", "src", "index.ts");
    writeFileSync(
      join(root, "package.json"),
      JSON.stringify({
        name: "@scope/http provider",
        gestalt: {
          provider: {
            kind: "plugin",
            target: "./provider.ts#plugin",
          },
        },
      }),
      "utf8",
    );
    writeFileSync(
      join(root, "provider.ts"),
      `import { definePlugin } from ${JSON.stringify(indexPath)};

export const plugin = definePlugin({
  securitySchemes: {
    slack: {
      type: "slack_signature",
      secret: {
        env: "SLACK_SIGNING_SECRET",
      },
    },
  },
  http: {
    command: {
      path: "/command",
      method: "POST",
      security: "slack",
      target: "handle_command",
      requestBody: {
        required: true,
        content: {
          "application/x-www-form-urlencoded": {},
        },
      },
      ack: {
        status: 200,
        body: {
          response_type: "ephemeral",
          text: "Working on it...",
        },
      },
    },
  },
  operations: [
    {
      id: "handle_command",
      handler() {
        return { ok: true };
      },
    },
  ],
});
`,
      "utf8",
    );

    process.env[ENV_WRITE_MANIFEST_METADATA] = metadataPath;
    const code = await main([root, "plugin:./provider.ts#plugin"]);
    expect(code).toBe(0);
    const metadata = readFileSync(metadataPath, "utf8");
    expect(metadata).toContain("securitySchemes:");
    expect(metadata).toContain("type: slack_signature");
    expect(metadata).toContain("env: SLACK_SIGNING_SECRET");
    expect(metadata).toContain("http:");
    expect(metadata).toContain("path: /command");
    expect(metadata).toContain("target: handle_command");
    expect(metadata).toContain("application/x-www-form-urlencoded");
    expect(metadata).toContain("response_type: ephemeral");
  } finally {
    if (previousManifestMetadata === undefined) {
      delete process.env[ENV_WRITE_MANIFEST_METADATA];
    } else {
      process.env[ENV_WRITE_MANIFEST_METADATA] = previousManifestMetadata;
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

test("loadProviderFromTarget falls through null exports to the next plugin candidate", async () => {
  const plugin = await loadProviderFromTarget(
    fixturePath("basic-provider-null-export"),
  );
  expect(plugin.kind).toBe("integration");
  expect(plugin.name).toBe("basic-provider-null-export");
  expect(plugin.displayName).toBe("Fixture Provider Null Export");
});

test("loadProviderFromTarget ignores whitespace-only explicit targets", async () => {
  const plugin = await loadProviderFromTarget(
    fixturePath("basic-provider"),
    "   ",
  );
  expect(plugin.kind).toBe("integration");
  expect(plugin.name).toBe("basic-provider");
  expect(plugin.displayName).toBe("Fixture Provider");
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
            kind: "authentication",
            target: "./provider.ts#missing",
          },
        },
      }),
      "utf8",
    );
    writeFileSync(
      join(root, "provider.ts"),
      `import { definePlugin } from ${JSON.stringify(indexPath)};

export const plugin = definePlugin({
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
      "authentication:./provider.ts#missing did not resolve to a Gestalt authentication provider",
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
            kind: "plugin",
            target: "./provider.ts#plugin",
          },
        },
      }),
      "utf8",
    );
    writeFileSync(
      join(root, "provider.ts"),
      `import { definePlugin } from ${JSON.stringify(indexPath)};

export const plugin = definePlugin({
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

test("runtime serves a secrets provider over unix gRPC", async () => {
  const runtimeEntry = join(import.meta.dir, "..", "src", "runtime.ts");
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
  const plugin = await loadProviderFromTarget(fixturePath("basic-provider"));
  const service = createProviderService(plugin);

  const metadata = await (service.getMetadata as any)();
  expect(metadata.name).toBe("basic-provider");
  expect(metadata.supportsSessionCatalog).toBe(true);
  expect(metadata.minProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);
  expect(metadata.maxProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);
  expect(
    metadata.staticCatalog?.operations?.some((op: any) => op.id === "hello"),
  ).toBe(true);
  expect(
    metadata.staticCatalog?.operations?.find((op: any) => op.id === "hello")
      ?.allowedRoles,
  ).toEqual(["viewer", "admin"]);

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
  expect(JSON.parse(unconfiguredResult.body)).toMatchObject({
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
          kind: "user",
          authSource: "api_token",
        }),
        credential: create(CredentialContextSchema, {
          mode: "identity",
          subjectId: "identity:__identity__",
        }),
        access: create(AccessContextSchema, {
          policy: "sample_policy",
          role: "admin",
        }),
      }),
      invocationToken: "invocation-token-123",
    }),
  );
  expect(JSON.parse(result.body)).toEqual({
    message: "Hello, Ada.",
    configuredName: "configured-provider",
    region: "iad",
    configuredRegion: "use1",
    subjectId: "user:user-123",
    credentialMode: "identity",
    accessPolicy: "sample_policy",
    accessRole: "admin",
    invocationToken: "invocation-token-123",
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
          kind: "user",
        }),
        credential: create(CredentialContextSchema, {
          mode: "identity",
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
    "Session Hello ops user:user-123 identity viewer",
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
            kind: "plugin",
            target: "./provider.ts#plugin",
          },
        },
      }),
      "utf8",
    );
    writeFileSync(
      join(root, "provider.ts"),
      `import { definePlugin, response, s } from ${JSON.stringify(indexPath)};

export const plugin = definePlugin({
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
        return response(201, {
          id: "new-id",
        });
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

    const plugin = await loadProviderFromTarget(root);
    const service = createProviderService(plugin);

    const echoedBody = await (service.execute as any)(
      create(ExecuteRequestSchema, {
        operation: "echo-body",
      }),
    );
    expect(echoedBody.status).toBe(200);
    expect(JSON.parse(echoedBody.body)).toEqual({
      body: "hello",
    });

    const echoedStatusBody = await (service.execute as any)(
      create(ExecuteRequestSchema, {
        operation: "echo-status-body",
      }),
    );
    expect(echoedStatusBody.status).toBe(200);
    expect(JSON.parse(echoedStatusBody.body)).toEqual({
      status: 42,
      body: "payload",
    });

    const created = await (service.execute as any)(
      create(ExecuteRequestSchema, {
        operation: "created",
      }),
    );
    expect(created.status).toBe(201);
    expect(JSON.parse(created.body)).toEqual({
      id: "new-id",
    });

    const unknown = await (service.execute as any)(
      create(ExecuteRequestSchema, {
        operation: "missing",
      }),
    );
    expect(unknown.status).toBe(404);
    expect(JSON.parse(unknown.body)).toEqual({
      error: "unknown operation",
    });

    const exploded = await (service.execute as any)(
      create(ExecuteRequestSchema, {
        operation: "explode",
      }),
    );
    expect(exploded.status).toBe(500);
    expect(JSON.parse(exploded.body)).toEqual({
      error: "boom",
    });
  } finally {
    removeTempDir(root);
  }
});

test("authentication provider supports runtime metadata, login flows, and token validation", async () => {
  const provider = await loadProviderFromTarget(fixturePath("auth-provider"));
  const runtime = createRuntimeService(provider);
  const auth = createAuthenticationService(provider as any);

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

  const defaultBegin = await (auth.beginLogin as any)(
    create(BeginLoginRequestSchema, {
      callbackUrl: "https://app.example.test/callback",
      hostState: "host-state",
      scopes: ["openid"],
    }),
  );
  expect(defaultBegin.authorizationUrl).toContain(
    "https://issuer.example.test/authorize",
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
  expect(metadata.kind).toBe(ProtoProviderKind.AUTHENTICATION);
  expect(metadata.displayName).toBe("Fixture Auth");
  expect(metadata.minProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);
  expect(metadata.maxProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);

  const begin = await (auth.beginLogin as any)(
    create(BeginLoginRequestSchema, {
      callbackUrl: "https://app.example.test/callback",
      hostState: "host-state",
      scopes: ["openid"],
    }),
  );
  expect(begin.authorizationUrl).toContain(
    "https://login.example.test/authorize",
  );

  const user = await (auth.completeLogin as any)(
    create(CompleteLoginRequestSchema, {
      query: {
        code: "code-123",
      },
      callbackUrl: "https://app.example.test/callback",
      providerState: new Uint8Array([1, 2, 3]),
    }),
  );
  expect(user.subject).toBe("code-123");
  expect(user.claims.issuer).toBe("https://login.example.test");

  const validated = await (auth.validateExternalToken as any)(
    create(ValidateExternalTokenRequestSchema, {
      token: "api-token",
    }),
  );
  expect(validated.email).toBe("api-token@example.com");
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
              bucket: "runtime-bucket",
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
      bucket: "runtime-bucket",
      key: "runtime.txt",
    },
  });
  expect(headed.meta?.size).toBe(7n);

  const listed = await (s3.listObjects as any)({
    bucket: "runtime-bucket",
  });
  expect(listed.objects.map((object: any) => object.ref?.key)).toEqual([
    "runtime.txt",
  ]);

  const copied = await (s3.copyObject as any)({
    source: {
      bucket: "runtime-bucket",
      key: "runtime.txt",
    },
    destination: {
      bucket: "runtime-bucket",
      key: "copy.txt",
    },
  });
  expect(copied.meta?.ref?.key).toBe("copy.txt");

  const presigned = await (s3.presignObject as any)({
    ref: {
      bucket: "runtime-bucket",
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

  const run = await (workflow.startRun as any)(
    create(StartWorkflowProviderRunRequestSchema, {
      idempotencyKey: "req-1",
      createdBy: {
        subjectId: "user:user-123",
        subjectKind: "user",
        displayName: "Ada",
        authSource: "api_token",
      },
      target: {
        pluginName: "roadmap",
        operation: "sync",
        input: {
          project: "alpha",
        },
      },
    }),
  );
  expect(run.target?.pluginName).toBe("roadmap");
  expect(run.status).toBe(WorkflowRunStatus.PENDING);
  expect(run.statusMessage).toBe("idempotency:req-1");
  expect(run.createdBy?.subjectId).toBe("user:user-123");

  const schedule = await (workflow.upsertSchedule as any)(
    create(UpsertWorkflowProviderScheduleRequestSchema, {
      scheduleId: "nightly",
      cron: "*/5 * * * *",
      timezone: "UTC",
      requestedBy: {
        subjectId: "workload:planner",
        subjectKind: "workload",
        displayName: "Planner",
        authSource: "workload_token",
      },
      target: {
        pluginName: "roadmap",
        operation: "sync",
      },
    }),
  );
  expect(schedule.id).toBe("nightly");
  expect(schedule.target?.pluginName).toBe("roadmap");
  expect(schedule.createdBy?.subjectId).toBe("workload:planner");

  const updatedSchedule = await (workflow.upsertSchedule as any)(
    create(UpsertWorkflowProviderScheduleRequestSchema, {
      scheduleId: "nightly",
      cron: "0 * * * *",
      timezone: "UTC",
      requestedBy: {
        subjectId: "user:user-999",
        subjectKind: "user",
        displayName: "Grace",
        authSource: "api_token",
      },
      target: {
        pluginName: "roadmap",
        operation: "sync",
      },
    }),
  );
  expect(updatedSchedule.createdBy?.subjectId).toBe("workload:planner");

  await (workflow.publishEvent as any)(
    create(PublishWorkflowProviderEventRequestSchema, {
      pluginName: "roadmap",
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
  expect(refreshedMetadata.warnings).toEqual(["published-events:1"]);
});

test("integration provider request context includes workflow metadata", async () => {
  const plugin = definePlugin({
    operations: [
      {
        id: "inspect",
        handler(_input, request) {
          return {
            workflow: request.workflow,
          };
        },
      },
    ],
  });
  const service = createProviderService(plugin);

  const result = await (service.execute as any)(
    create(ExecuteRequestSchema, {
      operation: "inspect",
      params: {},
      token: "token-123",
      context: create(RequestContextSchema, {
        workflow: {
          runId: "run-123",
          provider: "temporal",
          executionRef: "exec-ref-123",
          createdBy: {
            subjectId: "user:user-123",
            subjectKind: "user",
            displayName: "Ada",
            authSource: "api_token",
          },
          target: {
            pluginName: "demo",
            operation: "sync",
            connection: "analytics",
            instance: "tenant-a",
          },
          trigger: {
            kind: "event",
            triggerId: "trigger-1",
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
      }),
    }),
  );

  expect(JSON.parse(result.body)).toEqual({
    workflow: {
      runId: "run-123",
      provider: "temporal",
      executionRef: "exec-ref-123",
      createdBy: {
        subjectId: "user:user-123",
        subjectKind: "user",
        displayName: "Ada",
        authSource: "api_token",
      },
      target: {
        pluginName: "demo",
        operation: "sync",
        connection: "analytics",
        instance: "tenant-a",
      },
      trigger: {
        kind: "event",
        triggerId: "trigger-1",
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
        method: PresignMethod.Get,
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
                bucket: "runtime-bucket",
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

test("s3 client writeObject cancels unread readable streams when upload ends early", async () => {
  let canceled = false;
  let pulls = 0;
  const body = new ReadableStream<Uint8Array>({
    pull(controller) {
      pulls += 1;
      if (pulls === 1) {
        controller.enqueue(new TextEncoder().encode("hello"));
        return;
      }
      controller.enqueue(new TextEncoder().encode("goodbye"));
    },
    cancel() {
      canceled = true;
    },
  });

  const s3 = Object.create(S3.prototype) as {
    client: {
      writeObject: (requests: AsyncIterable<unknown>) => Promise<{
        meta: {
          ref: { bucket: string; key: string };
          etag: string;
          size: bigint;
          contentType: string;
          metadata: Record<string, string>;
          storageClass: string;
        };
      }>;
    };
  };
  s3.client = {
    async writeObject(requests: AsyncIterable<unknown>) {
      const iterator = requests[Symbol.asyncIterator]();
      const open = await iterator.next();
      expect(open.done).toBe(false);
      const firstChunk = await iterator.next();
      expect(firstChunk.done).toBe(false);
      await iterator.return?.();
      return {
        meta: {
          ref: { bucket: "runtime-bucket", key: "runtime.txt" },
          etag: "etag",
          size: BigInt(
            (firstChunk.value as { msg: { value: Uint8Array } }).msg.value
              .byteLength,
          ),
          contentType: "text/plain",
          metadata: {},
          storageClass: "STANDARD",
        },
      };
    },
  };

  const meta = await S3.prototype.writeObject.call(
    s3,
    { bucket: "runtime-bucket", key: "runtime.txt" },
    body,
  );

  expect(meta.size).toBe(5n);
  expect(canceled).toBe(true);
});
