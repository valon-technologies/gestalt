import { spawn, type ChildProcess } from "node:child_process";
import { readFileSync } from "node:fs";
import { join } from "node:path";

import { create } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { expect, test } from "bun:test";

import {
  CompleteLoginRequestSchema,
  ValidateExternalTokenRequestSchema,
} from "../gen/v1/auth_pb.ts";
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
  CURRENT_PROTOCOL_VERSION,
  createCacheService,
  ENV_WRITE_CATALOG,
  ENV_PROVIDER_SOCKET,
  createAuthService,
  createProviderService,
  createRuntimeService,
  loadPluginFromTarget,
  loadProviderFromTarget,
  main,
  parseRuntimeArgs,
} from "../src/runtime.ts";
import { PresignMethod, S3, defineCacheProvider, defineS3Provider } from "../src/index.ts";
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

test("runtime main writes a static catalog in catalog mode", async () => {
  const root = fixturePath("basic-provider");
  const tempDir = makeTempDir();
  const catalogPath = join(tempDir, "catalog.yaml");
  const previousCatalog = process.env[ENV_WRITE_CATALOG];

  process.env[ENV_WRITE_CATALOG] = catalogPath;
  try {
    const code = await main([root, "plugin:./provider.ts#plugin"]);
    expect(code).toBe(0);
    const catalog = readFileSync(catalogPath, "utf8");
    expect(catalog).toContain("name: basic-provider");
    expect(catalog).toContain("id: hello");
  } finally {
    if (previousCatalog === undefined) {
      delete process.env[ENV_WRITE_CATALOG];
    } else {
      process.env[ENV_WRITE_CATALOG] = previousCatalog;
    }
    removeTempDir(tempDir);
  }
});

test("loadProviderFromTarget resolves a secrets provider from package metadata", async () => {
  const provider = await loadProviderFromTarget(fixturePath("secrets-provider"));
  expect(provider.kind).toBe("secrets");
  expect(provider.name).toBe("secrets-provider");
  expect(provider.displayName).toBe("Fixture Secrets");
});

test("loadPluginFromTarget falls through null exports to the next plugin candidate", async () => {
  const plugin = await loadPluginFromTarget(
    fixturePath("basic-provider-null-export"),
  );
  expect(plugin.kind).toBe("integration");
  expect(plugin.name).toBe("basic-provider-null-export");
  expect(plugin.displayName).toBe("Fixture Provider Null Export");
});

test("loadPluginFromTarget ignores whitespace-only explicit targets", async () => {
  const plugin = await loadPluginFromTarget(
    fixturePath("basic-provider"),
    "   ",
  );
  expect(plugin.kind).toBe("integration");
  expect(plugin.name).toBe("basic-provider");
  expect(plugin.displayName).toBe("Fixture Provider");
});

test("runtime serves a secrets provider over unix gRPC", async () => {
  const runtimeEntry = join(import.meta.dir, "..", "src", "runtime.ts");
  const root = fixturePath("secrets-provider");
  const tempDir = makeTempDir("gestalt-typescript-runtime-");
  const socketPath = join(tempDir, "provider.sock");
  let child: ChildProcess | undefined;

  try {
    child = spawn(process.execPath, [runtimeEntry, root, "secrets:./secrets.ts"], {
      env: {
        ...process.env,
        [ENV_PROVIDER_SOCKET]: socketPath,
      },
      stdio: ["ignore", "ignore", "pipe"],
    });
    const stderrText = captureChildStderr(child);

    try {
      await waitForPath(socketPath);
    } catch (error) {
      throw new Error(`${String(error)}${stderrText() ? `\n${stderrText()}` : ""}`);
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
  const plugin = await loadPluginFromTarget(fixturePath("basic-provider"));
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

test("auth provider supports runtime metadata, login flows, and token validation", async () => {
  const provider = await loadProviderFromTarget(fixturePath("auth-provider"));
  const runtime = createRuntimeService(provider);
  const auth = createAuthService(provider as any);

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
    create((await import("../gen/v1/auth_pb.ts")).BeginLoginRequestSchema, {
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
  expect(metadata.kind).toBe(2);
  expect(metadata.displayName).toBe("Fixture Auth");
  expect(metadata.minProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);
  expect(metadata.maxProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);

  const begin = await (auth.beginLogin as any)(
    create((await import("../gen/v1/auth_pb.ts")).BeginLoginRequestSchema, {
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

test("s3 provider target resolves and serves runtime metadata", async () => {
  const provider = await loadProviderFromTarget(fixturePath("s3-provider"));
  const runtime = createRuntimeService(provider);
  const s3 = createS3Service(provider as any);

  await (provider as any).writeObject(
    {
      bucket: "runtime-bucket",
      key: "runtime.txt",
    },
    (async function* () {
      yield new TextEncoder().encode("runtime");
    })(),
    {
      contentType: "text/plain",
      metadata: {
        env: "test",
      },
    },
  );

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

  const headed = await (s3.headObject as any)({
    ref: {
      bucket: "runtime-bucket",
      key: "runtime.txt",
    },
  });
  expect(headed.meta?.size).toBe(7n);
  expect(headed.meta?.contentType).toBe("text/plain");
  expect(headed.meta?.metadata).toEqual({ env: "test" });

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
          size: BigInt((firstChunk.value as { msg: { value: Uint8Array } }).msg.value.byteLength),
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
