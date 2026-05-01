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
    ["go", "build", "-o", harnessBinPath, "./services/testutil/testdata/cmd/cachetransportd/"],
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

  process.env.GESTALT_CACHE_SOCKET = socketPath;
  process.env[cacheSocketEnv("named")] = `unix://${socketPath}`;
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
  delete process.env.GESTALT_CACHE_SOCKET;
  delete process.env[cacheSocketEnv("named")];
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
    process.env.GESTALT_CACHE_SOCKET = target;
    try {
      const cache = new Cache();
      await cache.set("tcp-key", encoder.encode("tcp-value"));
      expect(decoder.decode((await cache.get("tcp-key"))!)).toBe("tcp-value");
    } finally {
      tcpProc.kill();
      delete process.env.GESTALT_CACHE_SOCKET;
      process.env.GESTALT_CACHE_SOCKET = socketPath;
    }
  });

  test("tcp target token env selects the requested binding", async () => {
    const token = "relay-token-typescript";
    const { proc: tcpProc, target } = await startTCPHarness(token);
    process.env.GESTALT_CACHE_SOCKET = target;
    process.env[cacheSocketTokenEnv()] = token;
    try {
      const cache = new Cache();
      await cache.set("tcp-token-key", encoder.encode("tcp-token-value"));
      expect(decoder.decode((await cache.get("tcp-token-key"))!)).toBe(
        "tcp-token-value",
      );
    } finally {
      tcpProc.kill();
      delete process.env.GESTALT_CACHE_SOCKET;
      delete process.env[cacheSocketTokenEnv()];
      process.env.GESTALT_CACHE_SOCKET = socketPath;
    }
  });
});
