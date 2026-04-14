import { rmSync } from "node:fs";
import { createServer, type Http2Server } from "node:http2";
import { join } from "node:path";

import { connectNodeAdapter } from "@connectrpc/connect-node";
import { afterAll, beforeAll, describe, expect, test } from "bun:test";

import { FileAPI as FileAPIService } from "../gen/v1/fileapi_pb.ts";
import { ProviderLifecycle } from "../gen/v1/runtime_pb.ts";
import {
  ENV_PROVIDER_SOCKET,
  FileAPI,
  FileAPINotFoundError,
  createFileAPIService,
  createRuntimeService,
  fileAPISocketEnv,
} from "../src/index.ts";
import { createMemoryFileAPIProvider } from "./fileapi_memory.ts";
import { makeTempDir, removeTempDir } from "./helpers.ts";

let tmpDir: string;
let socketPath: string;
let server: Http2Server;
let api: FileAPI;

beforeAll(async () => {
  tmpDir = makeTempDir("fileapi-transport-test-");
  socketPath = join(tmpDir, "fileapi.sock");

  const provider = createMemoryFileAPIProvider();
  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(ProviderLifecycle, createRuntimeService(provider));
      router.service(FileAPIService, createFileAPIService(provider));
    },
  });

  server = createServer(handler);
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(socketPath, () => {
      server.off("error", reject);
      resolve();
    });
  });

  process.env.GESTALT_FILEAPI_SOCKET = socketPath;
  process.env[fileAPISocketEnv("named")] = socketPath;
  process.env[ENV_PROVIDER_SOCKET] = socketPath;
  api = new FileAPI();
}, 30_000);

afterAll(async () => {
  delete process.env.GESTALT_FILEAPI_SOCKET;
  delete process.env[fileAPISocketEnv("named")];
  delete process.env[ENV_PROVIDER_SOCKET];

  await new Promise<void>((resolve) => {
    server?.close(() => resolve());
  });
  if (socketPath) {
    rmSync(socketPath, { force: true });
  }
  if (tmpDir) {
    removeTempDir(tmpDir);
  }
});

describe("FileAPI transport", () => {
  test("createBlob/stat/readBytes round-trip through transport", async () => {
    const blob = await api.createBlob(["hello\nworld"], {
      mimeType: "text/plain",
      endings: "native",
    });

    expect(blob.id).toMatch(/^obj-/);
    expect(blob.type).toStartWith("text/plain");
    expect(await blob.text()).toBe(process.platform === "win32" ? "hello\r\nworld" : "hello\nworld");

    const stat = await api.stat(blob.id);
    expect(stat.kind).toBe("blob");
    expect(stat.size).toBe(blob.size);
  });

  test("createFile preserves file metadata", async () => {
    const file = await api.createFile(["report"], "report.txt", {
      mimeType: "text/plain",
      lastModified: 1_725_391_200_000,
    });

    expect(file.kind).toBe("file");
    expect(file.name).toBe("report.txt");
    expect(file.lastModified).toBe(1_725_391_200_000);
    expect(await file.text()).toBe("report");
  });

  test("slice returns a new blob and dataURL is packaged locally", async () => {
    const blob = await api.createBlob(["hello world"], {
      mimeType: "text/plain",
    });

    const sliced = await blob.slice(6, 11, "text/custom");
    expect(sliced.kind).toBe("blob");
    expect(sliced.type).toBe("text/custom");
    expect(await sliced.text()).toBe("world");
    expect(await sliced.dataURL()).toBe("data:text/custom;base64,d29ybGQ=");
  });

  test("openReadStream yields wire chunks", async () => {
    const blob = await api.createBlob([new Uint8Array([0, 1, 2, 3])]);

    const chunks: Uint8Array[] = [];
    for await (const chunk of api.openReadStream(blob.id)) {
      chunks.push(chunk);
    }

    const bytes = Buffer.concat(chunks.map((chunk) => Buffer.from(chunk)));
    expect([...bytes]).toEqual([0, 1, 2, 3]);
  });

  test("object URL round-trip and revocation work", async () => {
    const blob = await api.createBlob(["url target"]);
    const url = await blob.createObjectURL();

    const resolved = await api.resolveObjectURL(url);
    expect(resolved.id).toBe(blob.id);
    expect(resolved.kind).toBe("blob");

    await api.revokeObjectURL(url);
    try {
      await api.resolveObjectURL(url);
      throw new Error("expected resolveObjectURL() to fail after revocation");
    } catch (error) {
      expect(error).toBeInstanceOf(FileAPINotFoundError);
    }
  });

  test("named socket env selects the requested binding", async () => {
    const named = new FileAPI("named");
    const blob = await named.createBlob(["named"]);
    expect(await blob.text()).toBe("named");
  });
});
