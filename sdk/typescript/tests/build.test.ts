import { spawn, type ChildProcess } from "node:child_process";
import { existsSync } from "node:fs";
import { join } from "node:path";

import { create } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import { expect, test } from "bun:test";

import { AuthProvider as AuthProviderService, BeginLoginRequestSchema } from "../gen/v1/auth_pb.ts";
import { Cache as CacheService } from "../gen/v1/cache_pb.ts";
import { S3 as S3Service } from "../gen/v1/s3_pb.ts";
import { ConfigureProviderRequestSchema, ProviderKind as ProtoProviderKind, ProviderLifecycle } from "../gen/v1/runtime_pb.ts";
import { buildProviderBinary, bunTarget, parseBuildArgs } from "../src/build.ts";
import { Cache, cacheSocketEnv } from "../src/cache.ts";
import { CURRENT_PROTOCOL_VERSION, ENV_PROVIDER_SOCKET } from "../src/runtime.ts";
import {
  createUnixGrpcClient,
  fixturePath,
  hostTarget,
  makeTempDir,
  removeTempDir,
  waitForPath,
} from "./helpers.ts";

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
  let stderr = "";

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
    child.stderr?.setEncoding("utf8");
    child.stderr?.on("data", (chunk: string) => {
      stderr += chunk;
    });

    await waitForPath(socketPath);

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

test("buildProviderBinary compiles a runnable cache provider executable", async () => {
  const { goos, goarch, executableSuffix } = hostTarget();
  const tempDir = makeTempDir("gts-cache-");
  const outputPath = join(tempDir, `fixture-cache${executableSuffix}`);
  const socketPath = join(tempDir, "p.sock");
  const previousDefaultSocket = process.env.GESTALT_CACHE_SOCKET;
  const previousNamedSocket = process.env[cacheSocketEnv("named")];
  let child: ChildProcess | undefined;
  let stderr = "";

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
    child.stderr?.setEncoding("utf8");
    child.stderr?.on("data", (chunk: string) => {
      stderr += chunk;
    });

    try {
      await waitForPath(socketPath);
    } catch (error) {
      throw new Error(`${String(error)}${stderr ? `\n${stderr}` : ""}`);
    }

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

    const encoder = new TextEncoder();
    const decoder = new TextDecoder();
    const cache = new Cache();
    const namedCache = new Cache("named");

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

    await waitForPath(socketPath);

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
});

async function stopProcess(child: ChildProcess): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) {
    return;
  }
  child.kill("SIGTERM");
  try {
    await waitForExit(child, 2_000);
  } catch {
    child.kill("SIGKILL");
    await waitForExit(child, 2_000);
  }
}

async function waitForExit(
  child: ChildProcess,
  timeoutMs: number,
): Promise<{ code: number | null; signal: NodeJS.Signals | null }> {
  if (child.exitCode !== null || child.signalCode !== null) {
    return {
      code: child.exitCode,
      signal: child.signalCode,
    };
  }
  return await new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      reject(new Error("timed out waiting for child process to exit"));
    }, timeoutMs);
    child.once("close", (code, signal) => {
      clearTimeout(timer);
      resolve({ code, signal });
    });
    child.once("error", (error) => {
      clearTimeout(timer);
      reject(error);
    });
  });
}
