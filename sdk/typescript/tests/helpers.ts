import { type ChildProcess } from "node:child_process";
import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { connect } from "node:net";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

import type { DescService } from "@bufbuild/protobuf";
import { createClient, type Client } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

export function fixturePath(...segments: string[]): string {
  return resolve(import.meta.dir, "fixtures", ...segments);
}

export function makeTempDir(prefix = "gestalt-typescript-test-"): string {
  return mkdtempSync(join(tmpdir(), prefix));
}

export function removeTempDir(path: string): void {
  rmSync(path, {
    recursive: true,
    force: true,
  });
}

export function hostTarget(): { goos: string; goarch: string; executableSuffix: string } {
  const goos = process.platform === "win32" ? "windows" : process.platform;
  const goarch = process.arch === "x64" ? "amd64" : process.arch;
  if (
    (goos !== "darwin" && goos !== "linux" && goos !== "windows") ||
    (goarch !== "amd64" && goarch !== "arm64")
  ) {
    throw new Error(`unsupported host target ${goos}/${goarch}`);
  }
  return {
    goos,
    goarch,
    executableSuffix: goos === "windows" ? ".exe" : "",
  };
}

export async function waitForPath(path: string, timeoutMs = 5_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (existsSync(path) && (await canConnect(path))) {
      return;
    }
    await Bun.sleep(25);
  }
  throw new Error(`timed out waiting for ${path}`);
}

async function canConnect(path: string): Promise<boolean> {
  return await new Promise((resolve) => {
    const socket = connect(path);
    const finish = (ready: boolean) => {
      socket.removeAllListeners();
      socket.destroy();
      resolve(ready);
    };
    socket.once("connect", () => finish(true));
    socket.once("error", () => finish(false));
  });
}

export function createUnixGrpcClient<T extends DescService>(service: T, socketPath: string): Client<T> {
  const transport = createGrpcTransport({
    baseUrl: "http://localhost",
    nodeOptions: {
      createConnection: () => connect(socketPath),
    },
  });
  return createClient(service, transport);
}

export function captureChildStderr(child: ChildProcess): () => string {
  let stderr = "";
  child.stderr?.setEncoding("utf8");
  child.stderr?.on("data", (chunk: string) => {
    stderr += chunk;
  });
  return () => stderr;
}

export async function stopProcess(child: ChildProcess): Promise<void> {
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
