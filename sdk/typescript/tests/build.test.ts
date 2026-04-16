import { spawn, type ChildProcess } from "node:child_process";
import { existsSync } from "node:fs";
import { join } from "node:path";

import { create } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { expect, test } from "bun:test";

import { AuthProvider as AuthProviderService, BeginLoginRequestSchema } from "../gen/v1/auth_pb.ts";
import { Cache as CacheService } from "../gen/v1/cache_pb.ts";
import {
  AccessContextSchema,
  CredentialContextSchema,
  ExecuteRequestSchema,
  GetSessionCatalogRequestSchema,
  IntegrationProvider as IntegrationProviderService,
  RequestContextSchema,
  StartProviderRequestSchema,
  SubjectContextSchema,
} from "../gen/v1/plugin_pb.ts";
import {
  GetSecretRequestSchema,
  SecretsProvider as SecretsProviderService,
} from "../gen/v1/secrets_pb.ts";
import { S3 as S3Service } from "../gen/v1/s3_pb.ts";
import { ConfigureProviderRequestSchema, ProviderKind as ProtoProviderKind, ProviderLifecycle } from "../gen/v1/runtime_pb.ts";
import { buildProviderBinary, bunTarget, parseBuildArgs } from "../src/build.ts";
import { Cache, cacheSocketEnv } from "../src/cache.ts";
import { CURRENT_PROTOCOL_VERSION, ENV_PROVIDER_SOCKET } from "../src/runtime.ts";
import {
  captureChildStderr,
  createUnixGrpcClient,
  fixturePath,
  hostTarget,
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

async function waitForSocket(path: string, stderrText: () => string): Promise<void> {
  try {
    await waitForPath(path, 15_000);
  } catch (error) {
    throw new Error(`${String(error)}${stderrText() ? `\n${stderrText()}` : ""}`);
  }
}

test("build arg parsing validates required arguments", () => {
  expect(parseBuildArgs(["root", "auth:./auth.ts#provider", "out", "name", "darwin", "arm64"])).toEqual(
    {
    root: "root",
      target: "auth:./auth.ts#provider",
    outputPath: "out",
    providerName: "name",
    goos: "darwin",
    goarch: "arm64",
    },
  );
  expect(parseBuildArgs(["root"])).toBeUndefined();
  expect(bunTarget("darwin", "arm64")).toBe("bun-darwin-arm64");
  expect(() => bunTarget("plan9", "amd64")).toThrow("unsupported Bun target for plan9/amd64");
});

test("buildProviderBinary compiles a runnable auth provider executable", async () => {
  const { goos, goarch, executableSuffix } = hostTarget();
  const tempDir = makeTempDir("gestalt-typescript-build-test-");
  const outputPath = join(tempDir, `fixture-provider${executableSuffix}`);
  const socketPath = join(tempDir, "provider.sock");
  let child: ChildProcess | undefined;

  try {
    buildProviderBinary({
      root: fixturePath("auth-provider"),
      target: "auth:./auth.ts#provider",
      outputPath,
      providerName: "fixture-built",
      goos,
      goarch,
    });

    expect(existsSync(outputPath)).toBe(true);

    child = spawn(outputPath, [], {
      env: {
        ...process.env,
        [ENV_PROVIDER_SOCKET]: socketPath,
      },
      stdio: ["ignore", "ignore", "pipe"],
    });
    const stderrText = captureChildStderr(child);

    await waitForSocket(socketPath, stderrText);

    const runtime = createUnixGrpcClient(ProviderLifecycle, socketPath);
    const auth = createUnixGrpcClient(AuthProviderService, socketPath);

    const metadata = await runtime.getProviderIdentity(create(EmptySchema, {}));
    expect(metadata.kind).toBe(ProtoProviderKind.AUTH);
    expect(metadata.name).toBe("fixture-built");

    await runtime.configureProvider(
      create(ConfigureProviderRequestSchema, {
        name: "fixture-built",
        config: {
          issuer: "https://binary.example.test",
        },
        protocolVersion: CURRENT_PROTOCOL_VERSION,
      }),
    );

    const begin = await auth.beginLogin(
      create(BeginLoginRequestSchema, {
        callbackUrl: "https://app.example.test/callback",
        hostState: "binary-state",
        scopes: ["openid"],
      }),
    );
    expect(begin.authorizationUrl).toContain("https://binary.example.test/authorize");

    const settings = await auth.getSessionSettings(create(EmptySchema, {}));
    expect(settings.sessionTtlSeconds).toBe(5400n);
  } finally {
    if (child) {
      await stopProcess(child);
    }
    removeTempDir(tempDir);
  }
}, 15_000);

test("buildProviderBinary compiles a runnable plugin provider executable", async () => {
  const { goos, goarch, executableSuffix } = hostTarget();
  const tempDir = makeTempDir("gts-integration-");

  try {
    const label = "plugin";
    const outputPath = join(tempDir, `fixture-${label}${executableSuffix}`);
    const socketPath = join(tempDir, `${label}.sock`);
    let child: ChildProcess | undefined;

    try {
      buildProviderBinary({
        root: fixturePath("basic-provider"),
        target: "plugin:./provider.ts#plugin",
        outputPath,
        providerName: `fixture-${label}`,
        goos,
        goarch,
      });

      expect(existsSync(outputPath)).toBe(true);

      child = spawn(outputPath, [], {
        env: {
          ...process.env,
          [ENV_PROVIDER_SOCKET]: socketPath,
        },
        stdio: ["ignore", "ignore", "pipe"],
      });
      const stderrText = captureChildStderr(child);

      await waitForSocket(socketPath, stderrText);

      const runtime = createUnixGrpcClient(ProviderLifecycle, socketPath);
      const provider = createUnixGrpcClient(
        IntegrationProviderService,
        socketPath,
      );

      const identity = await runtime.getProviderIdentity(create(EmptySchema, {}));
      expect(identity.kind).toBe(ProtoProviderKind.INTEGRATION);
      expect(identity.name).toBe(`fixture-${label}`);
      expect(identity.minProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);
      expect(identity.maxProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);

      const metadata = await provider.getMetadata(create(EmptySchema, {}));
      expect(metadata.name).toBe(`fixture-${label}`);
      expect(metadata.supportsSessionCatalog).toBe(true);
      expect(metadata.minProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);
      expect(metadata.maxProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);
      expect(
        metadata.staticCatalog?.operations?.some((operation) => operation.id === "hello"),
      ).toBe(true);
      expect(
        metadata.staticCatalog?.operations?.some((operation) => operation.id === "count"),
      ).toBe(true);
      expect(
        metadata.staticCatalog?.operations?.find((operation) => operation.id === "hello"),
      ).toMatchObject({
        method: "POST",
        title: "Hello",
        description: "Return a greeting",
        allowedRoles: ["viewer", "admin"],
      });
      expect(
        metadata.staticCatalog?.operations?.find((operation) => operation.id === "count"),
      ).toMatchObject({
        method: "POST",
        title: "Count",
        description: "Echo an integer count",
      });

      await expectConnectCode(
        provider.startProvider(
          create(StartProviderRequestSchema, {
            name: `fixture-${label}`,
            config: {
              region: "use1",
            },
            protocolVersion: CURRENT_PROTOCOL_VERSION + 1,
          }),
        ),
        Code.FailedPrecondition,
      );

      const started = await provider.startProvider(
        create(StartProviderRequestSchema, {
          name: `fixture-${label}`,
          config: {
            region: "use1",
          },
          protocolVersion: CURRENT_PROTOCOL_VERSION,
        }),
      );
      expect(started.protocolVersion).toBe(CURRENT_PROTOCOL_VERSION);

      const result = await provider.execute(
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
        configuredName: `fixture-${label}`,
        region: "iad",
        configuredRegion: "use1",
        subjectId: "user:user-123",
        credentialMode: "identity",
        accessPolicy: "sample_policy",
        accessRole: "admin",
        requestHandle: "",
      });

      const defaultResult = await provider.execute(
        create(ExecuteRequestSchema, {
          operation: "hello",
          params: {
            excited: "true",
          },
        }),
      );
      expect(defaultResult.status).toBe(200);
      expect(JSON.parse(defaultResult.body)).toMatchObject({
        message: "Hello, World!",
      });

      const countResult = await provider.execute(
        create(ExecuteRequestSchema, {
          operation: "count",
          params: {
            count: "7",
          },
        }),
      );
      expect(countResult.status).toBe(200);
      expect(JSON.parse(countResult.body)).toEqual({
        count: 7,
      });

      const summaryResult = await provider.execute(
        create(ExecuteRequestSchema, {
          operation: "summarize",
          params: {
            count: "7",
            ratio: "12.5",
            values: ["1e3", ".25"],
          },
        }),
      );
      expect(summaryResult.status).toBe(200);
      expect(JSON.parse(summaryResult.body)).toEqual({
        count: 7,
        ratio: 12.5,
        values: [1000, 0.25],
        hasEnabled: false,
      });

      const decodeError = await provider.execute(
        create(ExecuteRequestSchema, {
          operation: "count",
          params: {
            count: "12px",
          },
        }),
      );
      expect(decodeError.status).toBe(400);
      expect(JSON.parse(decodeError.body)).toEqual({
        error: "$.count must be an integer",
      });

      const integerDecimalError = await provider.execute(
        create(ExecuteRequestSchema, {
          operation: "count",
          params: {
            count: "12.5",
          },
        }),
      );
      expect(integerDecimalError.status).toBe(400);
      expect(JSON.parse(integerDecimalError.body)).toEqual({
        error: "$.count must be an integer",
      });

      const missingRequiredError = await provider.execute(
        create(ExecuteRequestSchema, {
          operation: "summarize",
          params: {
            ratio: 1,
            values: [1],
          },
        }),
      );
      expect(missingRequiredError.status).toBe(400);
      expect(JSON.parse(missingRequiredError.body)).toEqual({
        error: "$.count is required",
      });

      const arrayError = await provider.execute(
        create(ExecuteRequestSchema, {
          operation: "summarize",
          params: {
            count: 1,
            ratio: 1,
            values: "bad",
          },
        }),
      );
      expect(arrayError.status).toBe(400);
      expect(JSON.parse(arrayError.body)).toEqual({
        error: "$.values must be an array",
      });

      const numberError = await provider.execute(
        create(ExecuteRequestSchema, {
          operation: "summarize",
          params: {
            count: 1,
            ratio: "Infinity",
            values: [1],
          },
        }),
      );
      expect(numberError.status).toBe(400);
      expect(JSON.parse(numberError.body)).toEqual({
        error: "$.ratio must be a number",
      });

      const garbageNumberError = await provider.execute(
        create(ExecuteRequestSchema, {
          operation: "summarize",
          params: {
            count: 1,
            ratio: 1,
            values: ["123abc"],
          },
        }),
      );
      expect(garbageNumberError.status).toBe(400);
      expect(JSON.parse(garbageNumberError.body)).toEqual({
        error: "$.values[0] must be a number",
      });

      const sessionCatalog = await provider.getSessionCatalog(
        create(GetSessionCatalogRequestSchema, {
          token: "token-123",
          connectionParams: {
            scope: label,
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
      const sessionOperation = sessionCatalog.catalog?.operations?.[0];
      expect(sessionOperation).toBeDefined();
      expect(sessionOperation?.id).toBe("session-hello");
      expect(sessionOperation?.allowedRoles).toEqual([
        "viewer",
        "admin",
      ]);
      expect(sessionOperation?.title).toBe(
        `Session Hello ${label} user:user-123 identity viewer`,
      );
    } finally {
      if (child) {
        await stopProcess(child);
      }
    }
  } finally {
    removeTempDir(tempDir);
  }
}, 30_000);

test("buildProviderBinary compiles a plugin provider executable without an explicit export name", async () => {
  const { goos, goarch, executableSuffix } = hostTarget();
  const tempDir = makeTempDir("gts-integration-fallback-");
  const outputPath = join(tempDir, `fixture-fallback${executableSuffix}`);
  const socketPath = join(tempDir, "provider.sock");
  let child: ChildProcess | undefined;

  try {
    buildProviderBinary({
      root: fixturePath("basic-provider-default-export"),
      target: "plugin:./provider.ts",
      outputPath,
      providerName: "fixture-fallback",
      goos,
      goarch,
    });

    expect(existsSync(outputPath)).toBe(true);

    child = spawn(outputPath, [], {
      env: {
        ...process.env,
        [ENV_PROVIDER_SOCKET]: socketPath,
      },
      stdio: ["ignore", "ignore", "pipe"],
    });
    const stderrText = captureChildStderr(child);

    await waitForSocket(socketPath, stderrText);

    const runtime = createUnixGrpcClient(ProviderLifecycle, socketPath);
    const provider = createUnixGrpcClient(
      IntegrationProviderService,
      socketPath,
    );

    const identity = await runtime.getProviderIdentity(create(EmptySchema, {}));
    expect(identity.kind).toBe(ProtoProviderKind.INTEGRATION);
    expect(identity.name).toBe("fixture-fallback");

    const metadata = await provider.getMetadata(create(EmptySchema, {}));
    expect(metadata.name).toBe("fixture-fallback");
    expect(
      metadata.staticCatalog?.operations?.map((operation) => operation.id),
    ).toEqual(["hello"]);

    const result = await provider.execute(
      create(ExecuteRequestSchema, {
        operation: "hello",
        params: {
          name: "Ada",
        },
      }),
    );
    expect(JSON.parse(result.body)).toEqual({
      message: "Hello, Ada.",
    });
  } finally {
    if (child) {
      await stopProcess(child);
    }
    removeTempDir(tempDir);
  }
}, 15_000);

test("buildProviderBinary falls through null exports to the next plugin candidate", async () => {
  const { goos, goarch, executableSuffix } = hostTarget();
  const tempDir = makeTempDir("gts-integration-null-export-");
  const outputPath = join(tempDir, `fixture-null-export${executableSuffix}`);
  const socketPath = join(tempDir, "provider.sock");
  let child: ChildProcess | undefined;

  try {
    buildProviderBinary({
      root: fixturePath("basic-provider-null-export"),
      target: "plugin:./provider.ts",
      outputPath,
      providerName: "fixture-null-export",
      goos,
      goarch,
    });

    expect(existsSync(outputPath)).toBe(true);

    child = spawn(outputPath, [], {
      env: {
        ...process.env,
        [ENV_PROVIDER_SOCKET]: socketPath,
      },
      stdio: ["ignore", "ignore", "pipe"],
    });
    const stderrText = captureChildStderr(child);

    await waitForSocket(socketPath, stderrText);

    const runtime = createUnixGrpcClient(ProviderLifecycle, socketPath);
    const provider = createUnixGrpcClient(
      IntegrationProviderService,
      socketPath,
    );

    const identity = await runtime.getProviderIdentity(create(EmptySchema, {}));
    expect(identity.kind).toBe(ProtoProviderKind.INTEGRATION);
    expect(identity.name).toBe("fixture-null-export");

    const result = await provider.execute(
      create(ExecuteRequestSchema, {
        operation: "hello",
        params: {
          name: "Ada",
        },
      }),
    );
    expect(JSON.parse(result.body)).toEqual({
      message: "Hello, Ada.",
    });
  } finally {
    if (child) {
      await stopProcess(child);
    }
    removeTempDir(tempDir);
  }
}, 15_000);

test("buildProviderBinary compiles a runnable cache provider executable", async () => {
  const { goos, goarch, executableSuffix } = hostTarget();
  const tempDir = makeTempDir("gts-cache-");
  const outputPath = join(tempDir, `fixture-cache${executableSuffix}`);
  const socketPath = join(tempDir, "p.sock");
  const previousDefaultSocket = process.env.GESTALT_CACHE_SOCKET;
  const previousNamedSocket = process.env[cacheSocketEnv("named")];
  const previousCafeSocket = process.env[cacheSocketEnv("café")];
  const previousEmojiSocket = process.env[cacheSocketEnv("😀")];
  let child: ChildProcess | undefined;

  try {
    buildProviderBinary({
      root: fixturePath("cache-provider"),
      target: "cache:./cache.ts#provider",
      outputPath,
      providerName: "fixture-cache-built",
      goos,
      goarch,
    });

    expect(existsSync(outputPath)).toBe(true);

    child = spawn(outputPath, [], {
      env: {
        ...process.env,
        [ENV_PROVIDER_SOCKET]: socketPath,
      },
      stdio: ["ignore", "ignore", "pipe"],
    });
    const stderrText = captureChildStderr(child);

    await waitForSocket(socketPath, stderrText);

    const runtime = createUnixGrpcClient(ProviderLifecycle, socketPath);
    const rawCache = createUnixGrpcClient(CacheService, socketPath);

    const metadata = await runtime.getProviderIdentity(create(EmptySchema, {}));
    expect(metadata.kind).toBe(ProtoProviderKind.CACHE);
    expect(metadata.name).toBe("fixture-cache-built");

    await runtime.configureProvider(
      create(ConfigureProviderRequestSchema, {
        name: "fixture-cache-built",
        config: {
          prefix: "binary",
        },
        protocolVersion: CURRENT_PROTOCOL_VERSION,
      }),
    );

    process.env.GESTALT_CACHE_SOCKET = socketPath;
    process.env[cacheSocketEnv("named")] = socketPath;
    process.env[cacheSocketEnv("café")] = socketPath;
    process.env[cacheSocketEnv("😀")] = socketPath;

    const encoder = new TextEncoder();
    const decoder = new TextDecoder();
    const cache = new Cache();
    const namedCache = new Cache("named");
    const cafeCache = new Cache("café");
    const emojiCache = new Cache("😀");

    await cache.set("alpha", encoder.encode("one"), {
      ttlMs: 1_500,
    });
    await namedCache.setMany([
      { key: "beta", value: encoder.encode("two") },
      { key: "gamma", value: encoder.encode("three") },
      { key: "toString", value: encoder.encode("reserved") },
      { key: "__proto__", value: encoder.encode("proto") },
    ]);

    expect(decoder.decode((await cache.get("alpha"))!)).toBe("one");
    expect(
      Object.fromEntries(
        Object.entries(await namedCache.getMany(["alpha", "missing", "gamma"])).map(
          ([key, value]) => [key, decoder.decode(value)],
        ),
      ),
    ).toEqual({
      alpha: "one",
      gamma: "three",
    });
    await cafeCache.set("unicode", encoder.encode("accent"));
    expect(decoder.decode((await emojiCache.get("unicode"))!)).toBe("accent");
    const reserved = await namedCache.getMany(["toString", "__proto__", "missing"]);
    expect(Object.keys(reserved).sort()).toEqual(["__proto__", "toString"]);
    expect(decoder.decode(reserved["toString"]!)).toBe("reserved");
    expect(decoder.decode(reserved["__proto__"]!)).toBe("proto");
    expect(await cache.touch("gamma", 2_500)).toBe(true);
    expect(await cache.delete("beta")).toBe(true);
    expect(
      await namedCache.deleteMany(["alpha", "missing", "gamma", "toString", "__proto__"]),
    ).toBe(4);

    const remaining = await rawCache.getMany({
      keys: ["alpha", "beta", "gamma", "toString", "__proto__"],
    });
    expect(remaining.entries.map((entry) => entry.found)).toEqual([
      false,
      false,
      false,
      false,
      false,
    ]);
  } finally {
    if (previousDefaultSocket === undefined) {
      delete process.env.GESTALT_CACHE_SOCKET;
    } else {
      process.env.GESTALT_CACHE_SOCKET = previousDefaultSocket;
    }
    if (previousNamedSocket === undefined) {
      delete process.env[cacheSocketEnv("named")];
    } else {
      process.env[cacheSocketEnv("named")] = previousNamedSocket;
    }
    if (previousCafeSocket === undefined) {
      delete process.env[cacheSocketEnv("café")];
    } else {
      process.env[cacheSocketEnv("café")] = previousCafeSocket;
    }
    if (previousEmojiSocket === undefined) {
      delete process.env[cacheSocketEnv("😀")];
    } else {
      process.env[cacheSocketEnv("😀")] = previousEmojiSocket;
    }
    if (child) {
      await stopProcess(child);
    }
    removeTempDir(tempDir);
  }
}, 15_000);

test("buildProviderBinary compiles a runnable secrets provider executable", async () => {
  const { goos, goarch, executableSuffix } = hostTarget();
  const tempDir = makeTempDir("gts-secrets-");
  const outputPath = join(tempDir, `fixture-secrets${executableSuffix}`);
  const socketPath = join(tempDir, "provider.sock");
  let child: ChildProcess | undefined;

  try {
    buildProviderBinary({
      root: fixturePath("secrets-provider"),
      target: "secrets:./secrets.ts",
      outputPath,
      providerName: "fixture-secrets-built",
      goos,
      goarch,
    });

    expect(existsSync(outputPath)).toBe(true);

    child = spawn(outputPath, [], {
      env: {
        ...process.env,
        [ENV_PROVIDER_SOCKET]: socketPath,
      },
      stdio: ["ignore", "ignore", "pipe"],
    });
    const stderrText = captureChildStderr(child);

    await waitForSocket(socketPath, stderrText);

    const runtime = createUnixGrpcClient(ProviderLifecycle, socketPath);
    const secrets = createUnixGrpcClient(SecretsProviderService, socketPath);

    const metadata = await runtime.getProviderIdentity(create(EmptySchema, {}));
    expect(metadata.kind).toBe(ProtoProviderKind.SECRETS);
    expect(metadata.name).toBe("fixture-secrets-built");
    expect(metadata.displayName).toBe("Fixture Secrets");
    expect(metadata.minProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);
    expect(metadata.maxProtocolVersion).toBe(CURRENT_PROTOCOL_VERSION);

    await expectConnectCode(
      runtime.configureProvider(
        create(ConfigureProviderRequestSchema, {
          name: "fixture-secrets-built",
          config: {
            scope: "binary",
          },
          protocolVersion: CURRENT_PROTOCOL_VERSION + 1,
        }),
      ),
      Code.FailedPrecondition,
    );

    const configured = await runtime.configureProvider(
      create(ConfigureProviderRequestSchema, {
        name: "fixture-secrets-built",
        config: {
          scope: "binary",
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
    expect(secret.value).toBe("fixture-secrets-built:binary:hunter2");

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

test("buildProviderBinary compiles a runnable s3 provider executable", async () => {
  const { goos, goarch, executableSuffix } = hostTarget();
  const tempDir = makeTempDir("gestalt-typescript-s3-build-test-");
  const outputPath = join(tempDir, `fixture-s3${executableSuffix}`);
  const socketPath = join(tempDir, "provider.sock");
  let child: ChildProcess | undefined;

  try {
    buildProviderBinary({
      root: fixturePath("s3-provider"),
      target: "s3:./s3.ts#provider",
      outputPath,
      providerName: "fixture-s3",
      goos,
      goarch,
    });

    expect(existsSync(outputPath)).toBe(true);

    child = spawn(outputPath, [], {
      env: {
        ...process.env,
        [ENV_PROVIDER_SOCKET]: socketPath,
      },
      stdio: ["ignore", "ignore", "pipe"],
    });
    const stderrText = captureChildStderr(child);

    await waitForSocket(socketPath, stderrText);

    const runtime = createUnixGrpcClient(ProviderLifecycle, socketPath);
    const s3 = createUnixGrpcClient(S3Service, socketPath);

    const metadata = await runtime.getProviderIdentity(create(EmptySchema, {}));
    expect(metadata.kind).toBe(ProtoProviderKind.S3);
    expect(metadata.name).toBe("fixture-s3");

    await runtime.configureProvider(
      create(ConfigureProviderRequestSchema, {
        name: "fixture-s3",
        config: {},
        protocolVersion: CURRENT_PROTOCOL_VERSION,
      }),
    );

    const written = await s3.writeObject((async function* () {
      yield {
        msg: {
          case: "open",
          value: {
            ref: {
              bucket: "build-bucket",
              key: "hello.txt",
            },
            contentType: "text/plain",
          },
        },
      };
      yield {
        msg: {
          case: "data",
          value: new TextEncoder().encode("hello"),
        },
      };
    })());
    expect(written.meta?.ref?.key).toBe("hello.txt");

    const headed = await s3.headObject({
      ref: {
        bucket: "build-bucket",
        key: "hello.txt",
      },
    });
    expect(headed.meta?.size).toBe(5n);
  } finally {
    if (child) {
      await stopProcess(child);
    }
    removeTempDir(tempDir);
  }
}, 15_000);
