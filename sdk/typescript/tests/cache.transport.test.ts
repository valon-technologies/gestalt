import { mkdtempSync, rmSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawn, type Subprocess } from "bun";

import { afterAll, beforeAll, describe, expect, test } from "bun:test";

import {
  Cache,
  cacheSocketEnv,
  cacheSocketTokenEnv,
} from "../src/cache.ts";

const REPO_ROOT = join(import.meta.dir, "..", "..", "..");
const GESTALTD_DIR = join(REPO_ROOT, "gestaltd");
const encoder = new TextEncoder();
const decoder = new TextDecoder();

let tmpDir: string;
let harnessBinPath: string;
let socketPath: string;
let proc: Subprocess;

beforeAll(async () => {
  tmpDir = mkdtempSync(join(tmpdir(), "cache-transport-test-"));
  harnessBinPath = join(tmpDir, "cachetransportd");
  socketPath = join(tmpDir, "cache.sock");

  const build = spawn(
    ["go", "build", "-o", harnessBinPath, "./internal/testutil/testdata/cmd/cachetransportd/"],
    { cwd: GESTALTD_DIR, stdout: "pipe", stderr: "pipe" },
  );
  const buildExit = await build.exited;
  if (buildExit !== 0) {
    const stderr = await new Response(build.stderr).text();
    throw new Error(`go build failed (exit ${buildExit}): ${stderr}`);
  }

  proc = spawn([harnessBinPath, "--socket", socketPath], {
    stdout: "pipe",
    stderr: "inherit",
  });

  const stdout = proc.stdout;
  if (!stdout || typeof stdout === "number") {
    throw new Error("expected harness stdout to be piped");
  }
  const reader = stdout.getReader();
  const { value } = await reader.read();
  const line = new TextDecoder().decode(value).trim();
  if (!line.includes("READY")) {
    throw new Error(`expected READY, got: ${line}`);
  }
  reader.releaseLock();

  process.env[cacheSocketEnv()] = socketPath;
}, 60_000);

async function reserveTCPAddress(): Promise<string> {
  return await new Promise((resolve, reject) => {
    const server = createServer();
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

async function startTCPHarness(expectToken?: string): Promise<{ proc: Subprocess; target: string }> {
  const address = await reserveTCPAddress();
  const args = [harnessBinPath, "--tcp", address];
  if (expectToken) {
    args.push("--expect-token", expectToken);
  }
  const tcpProc = spawn(args, {
    stdout: "pipe",
    stderr: "inherit",
  });
  const stdout = tcpProc.stdout;
  if (!stdout || typeof stdout === "number") {
    throw new Error("expected tcp harness stdout to be piped");
  }
  const reader = stdout.getReader();
  const { value } = await reader.read();
  const line = new TextDecoder().decode(value).trim();
  if (!line.includes("READY")) {
    throw new Error(`expected READY from tcp harness, got: ${line}`);
  }
  reader.releaseLock();
  return { proc: tcpProc, target: `tcp://${address}` };
}

afterAll(() => {
  proc?.kill();
  delete process.env[cacheSocketEnv()];
  delete process.env[cacheSocketTokenEnv()];
  if (tmpDir) {
    rmSync(tmpDir, { recursive: true, force: true });
  }
});

describe("Cache transport", () => {
  test("unix socket env round-trip", async () => {
    const cache = new Cache();
    await cache.set("unix-key", encoder.encode("unix-value"));
    expect(decoder.decode((await cache.get("unix-key"))!)).toBe("unix-value");
  });

  test("named socket env selects the requested binding", async () => {
    const cache = new Cache("named");
    await cache.set("named-key", encoder.encode("named-value"));
    expect(decoder.decode((await cache.get("named-key"))!)).toBe("named-value");
  });

  test("tcp target env selects the requested binding", async () => {
    const { proc: tcpProc, target } = await startTCPHarness();
    const envName = cacheSocketEnv("tcp");
    const previousTarget = process.env[envName];
    process.env[envName] = target;
    try {
      const cache = new Cache();
      await cache.set("tcp-key", encoder.encode("tcp-value"));
      expect(decoder.decode((await cache.get("tcp-key"))!)).toBe("tcp-value");
    } finally {
      tcpProc.kill();
      if (previousTarget === undefined) {
        delete process.env[envName];
      } else {
        process.env[envName] = previousTarget;
      }
    }
  });

  test("tcp target token env selects the requested binding", async () => {
    const token = "relay-token-typescript";
    const { proc: tcpProc, target } = await startTCPHarness(token);
    const envName = cacheSocketEnv("tcp-token");
    const tokenEnvName = cacheSocketTokenEnv("tcp-token");
    const previousTarget = process.env[envName];
    const previousToken = process.env[tokenEnvName];
    process.env[envName] = target;
    process.env[tokenEnvName] = token;
    try {
      const cache = new Cache();
      await cache.set("tcp-token-key", encoder.encode("tcp-token-value"));
      expect(decoder.decode((await cache.get("tcp-token-key"))!)).toBe(
        "tcp-token-value",
      );
    } finally {
      tcpProc.kill();
      if (previousTarget === undefined) {
        delete process.env[envName];
      } else {
        process.env[envName] = previousTarget;
      }
      if (previousToken === undefined) {
        delete process.env[tokenEnvName];
      } else {
        process.env[tokenEnvName] = previousToken;
      }
    }
  });
});
