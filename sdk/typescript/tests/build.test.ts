import { spawn, type ChildProcess } from "node:child_process";
import { existsSync } from "node:fs";
import { join } from "node:path";

import { create } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import { expect, test } from "bun:test";

import { AuthProvider as AuthProviderService, BeginLoginRequestSchema } from "../gen/v1/auth_pb.ts";
import { ConfigureProviderRequestSchema, ProviderKind as ProtoProviderKind, ProviderLifecycle } from "../gen/v1/runtime_pb.ts";
import { buildProviderBinary, bunTarget, parseBuildArgs } from "../src/build.ts";
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
