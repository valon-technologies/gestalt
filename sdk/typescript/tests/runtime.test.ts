import { readFileSync } from "node:fs";
import { join } from "node:path";

import { create } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
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
  ConfigureProviderRequestSchema,
  ProviderKind as ProtoProviderKind,
} from "../gen/v1/runtime_pb.ts";
import {
  createCacheService,
  ENV_WRITE_CATALOG,
  createAuthService,
  createProviderService,
  createRuntimeService,
  loadPluginFromTarget,
  loadProviderFromTarget,
  main,
  parseRuntimeArgs,
} from "../src/runtime.ts";
import { defineCacheProvider } from "../src/index.ts";
import { fixturePath, makeTempDir, removeTempDir } from "./helpers.ts";

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

test("integration provider service exposes metadata, configure, execute, and session catalog", async () => {
  const plugin = await loadPluginFromTarget(fixturePath("basic-provider"));
  const service = createProviderService(plugin);

  const metadata = await (service.getMetadata as any)();
  expect(metadata.name).toBe("basic-provider");
  expect(metadata.supportsSessionCatalog).toBe(true);
  expect(
    metadata.staticCatalog?.operations?.some((op: any) => op.id === "hello"),
  ).toBe(true);
  expect(
    metadata.staticCatalog?.operations?.find((op: any) => op.id === "hello")
      ?.allowedRoles,
  ).toEqual(["viewer", "admin"]);

  await (service.startProvider as any)(
    create(StartProviderRequestSchema, {
      name: "configured-provider",
      config: {
        region: "use1",
      },
    }),
  );

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

  await (runtime.configureProvider as any)(
    create(ConfigureProviderRequestSchema, {
      name: "fixture-auth",
      config: {
        issuer: "https://login.example.test",
      },
      protocolVersion: 2,
    }),
  );

  const metadata = await (runtime.getProviderIdentity as any)(
    create(EmptySchema, {}),
  );
  expect(metadata.kind).toBe(2);
  expect(metadata.displayName).toBe("Fixture Auth");

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

  await (runtime.configureProvider as any)(
    create(ConfigureProviderRequestSchema, {
      name: "fixture-cache",
      config: {
        prefix: "runtime",
      },
      protocolVersion: 2,
    }),
  );

  const metadata = await (runtime.getProviderIdentity as any)(
    create(EmptySchema, {}),
  );
  expect(metadata.kind).toBe(ProtoProviderKind.CACHE);
  expect(metadata.displayName).toBe("Fixture Cache");

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
