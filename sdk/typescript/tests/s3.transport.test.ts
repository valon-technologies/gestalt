import { mkdtempSync, rmSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawn, type Subprocess } from "bun";

import { afterAll, afterEach, beforeAll, describe, expect, test } from "bun:test";

import {
  GestaltError,
  GestaltErrorCode,
  PresignMethod,
  S3,
  S3ObjectAccess,
  type S3ObjectMeta,
  type WriteObjectOpen,
} from "../src/index.ts";
import {
  ENV_HOST_SERVICE_SOCKET,
  ENV_HOST_SERVICE_TOKEN,
  createHostServiceGrpcTransport,
  hostServiceMetadataInterceptors,
  parseHostServiceTarget,
  requireHostServiceTarget,
  type HostServiceGrpcTransport,
} from "../src/host-service.ts";

const REPO_ROOT = join(import.meta.dir, "..", "..", "..");
const GESTALTD_DIR = join(REPO_ROOT, "gestaltd");
const WRITE_CHUNK_SIZE = 64 * 1024;

let tmpDir: string;
let harnessBinPath: string;
let socketPath: string;
let proc: Subprocess;
let defaultTCPProc: Subprocess | undefined;
const activeTransports = new Set<HostServiceGrpcTransport>();

beforeAll(async () => {
  tmpDir = mkdtempSync(join(tmpdir(), "s3-transport-test-"));
  harnessBinPath = join(tmpDir, "s3transportd");
  socketPath = join(tmpDir, "s3.sock");

  const build = spawn(
    ["go", "build", "-o", harnessBinPath, "./internal/testutil/testdata/cmd/s3transportd/"],
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

  const defaultTCP = await startTCPHarness();
  defaultTCPProc = defaultTCP.proc;

  // Keep the heavier default round-trip tests on TCP. Bun's HTTP/2 Unix-socket
  // client can flake after many S3 calls; the named binding test still covers
  // raw Unix socket dialing.
  process.env[ENV_HOST_SERVICE_SOCKET] = defaultTCP.target;
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
  defaultTCPProc?.kill();
  delete process.env[ENV_HOST_SERVICE_SOCKET];
  delete process.env[ENV_HOST_SERVICE_TOKEN];
  if (tmpDir) {
    rmSync(tmpDir, { recursive: true, force: true });
  }
});

function clientTransport(name?: string): HostServiceGrpcTransport {
  const { target, token } = requireHostServiceTarget("s3");
  const transport = createHostServiceGrpcTransport(
    parseHostServiceTarget("s3", target),
    hostServiceMetadataInterceptors(token, name?.trim() ?? ""),
  );
  activeTransports.add(transport);
  return transport;
}

function writeOpen(key: string, overrides: Partial<WriteObjectOpen> = {}): WriteObjectOpen {
  return {
    ref: { key, versionId: "" },
    contentType: "",
    cacheControl: "",
    contentDisposition: "",
    contentEncoding: "",
    contentLanguage: "",
    metadata: {},
    ifMatch: "",
    ifNoneMatch: "",
    ...overrides,
  };
}

async function* chunked(bytes: Uint8Array): AsyncIterable<Uint8Array> {
  for (let offset = 0; offset < bytes.byteLength; offset += WRITE_CHUNK_SIZE) {
    yield bytes.subarray(offset, offset + WRITE_CHUNK_SIZE);
  }
}

async function* noBody(): AsyncIterable<Uint8Array> {}

function bytesOf(text: string): Uint8Array {
  return new TextEncoder().encode(text);
}

async function writeString(
  s3: S3,
  key: string,
  text: string,
  overrides: Partial<WriteObjectOpen> = {},
): Promise<S3ObjectMeta> {
  const response = await s3.writeObject(writeOpen(key, overrides), chunked(bytesOf(text)));
  if (!response.meta) {
    throw new Error("writeObject returned no metadata");
  }
  return response.meta;
}

async function collectBytes(stream: AsyncIterable<Uint8Array>): Promise<Uint8Array> {
  const chunks: Uint8Array[] = [];
  let total = 0;
  for await (const chunk of stream) {
    chunks.push(chunk);
    total += chunk.byteLength;
  }
  const out = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    out.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return out;
}

async function readBytes(s3: S3, key: string): Promise<Uint8Array> {
  const { data } = await s3.readObject({
    ref: { key, versionId: "" },
    ifMatch: "",
    ifNoneMatch: "",
  });
  return await collectBytes(data);
}

async function readText(s3: S3, key: string): Promise<string> {
  return new TextDecoder().decode(await readBytes(s3, key));
}

describe("S3 transport", () => {
  afterEach(() => {
    for (const transport of activeTransports) {
      transport.close();
    }
    activeTransports.clear();
  });

  const client = (name?: string): S3 => new S3(clientTransport(name));

  test("named socket env selects the requested binding", async () => {
    const named = client("named");

    await writeString(named, "hello.txt", "named binding", {
      contentType: "text/plain",
    });

    expect(await readText(named, "hello.txt")).toBe("named binding");
  });

  test("tcp target env selects the requested binding", async () => {
    const { proc: tcpProc, target } = await startTCPHarness();
    const envName = ENV_HOST_SERVICE_SOCKET;
    const previousTarget = process.env[envName];
    process.env[envName] = target;
    try {
      const s3 = client("tcp");
      await writeString(s3, "hello.txt", "tcp binding");
      expect(await readText(s3, "hello.txt")).toBe("tcp binding");
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
    const envName = ENV_HOST_SERVICE_SOCKET;
    const tokenEnvName = ENV_HOST_SERVICE_TOKEN;
    const previousTarget = process.env[envName];
    const previousToken = process.env[tokenEnvName];
    process.env[envName] = target;
    process.env[tokenEnvName] = token;
    try {
      const s3 = client("tcp-token");
      await writeString(s3, "hello.txt", "token binding");
      expect(await readText(s3, "hello.txt")).toBe("token binding");
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

  test("createObjectAccessURL returns a hosted object access URL", async () => {
    const objectAccess = new S3ObjectAccess(clientTransport());
    const access = await objectAccess.createObjectAccessURLRaw({
      ref: { key: "uploads/object.txt", versionId: "" },
      method: PresignMethod.PRESIGN_METHOD_PUT,
      expiresSeconds: 60n,
      contentType: "text/plain",
      contentDisposition: "",
      headers: {
        "Content-Length": "5",
      },
    });
    expect(access.method).toBe(PresignMethod.PRESIGN_METHOD_PUT);
    expect(access.url).toStartWith("https://gestalt.example.test/api/v1/s3/object-access/");
    expect(access.url).not.toContain("uploads/object.txt");
    expect(access.headers).toEqual({ "Content-Length": "5" });
    expect(access.expiresAt).toBeInstanceOf(Date);
  });

  test("presignObject returns a hosted object URL", async () => {
    const presigned = await client().presignObjectRaw({
      ref: { key: "presigned.txt", versionId: "" },
      method: PresignMethod.PRESIGN_METHOD_PUT,
      expiresSeconds: 60n,
      contentType: "text/plain",
      contentDisposition: "",
      headers: {
        "x-test-header": "present",
      },
    });
    expect(presigned.method).toBe(PresignMethod.PRESIGN_METHOD_PUT);
    expect(presigned.url).toContain("https://example.invalid/presigned.txt");
    expect(presigned.url).toContain("method=PUT");
    expect(presigned.headers).toEqual({ "x-test-header": "present" });
    expect(presigned.expiresAt).toBeInstanceOf(Date);
  });

  test("write, head, read, and json round-trip", async () => {
    const s3 = client();

    const written = await writeString(
      s3,
      "payload.json",
      JSON.stringify({ message: "hello", count: 3 }),
      {
        contentType: "application/json",
        metadata: {
          env: "test",
        },
      },
    );

    expect(written.size).toBeGreaterThan(0n);
    expect(written.contentType).toBe("application/json");
    expect(written.metadata).toEqual({ env: "test" });
    expect(written.lastModified).toBeInstanceOf(Date);

    const head = await s3.headObject({ key: "payload.json", versionId: "" });
    expect(head.meta?.etag).toBe(written.etag);
    expect(head.meta?.size).toBe(written.size);
    expect(head.meta?.contentType).toBe("application/json");
    expect(head.meta?.metadata).toEqual({ env: "test" });

    const read = await s3.readObject({
      ref: { key: "payload.json", versionId: "" },
      ifMatch: "",
      ifNoneMatch: "",
    });
    expect(read.meta.etag).toBe(written.etag);
    const text = new TextDecoder().decode(await collectBytes(read.data));
    expect(text).toBe('{"message":"hello","count":3}');
    expect(JSON.parse(text)).toEqual({
      message: "hello",
      count: 3,
    });
  });

  test("copy, delete, and head round-trip", async () => {
    // Use a dedicated TCP harness and run this before the heavier S3 cases:
    // Bun's HTTP/2 client can corrupt session authority after many RPCs.
    const { proc: tcpProc, target } = await startTCPHarness();
    const envName = ENV_HOST_SERVICE_SOCKET;
    const previousTarget = process.env[envName];
    process.env[envName] = target;
    try {
      const s3 = client();
      const sourceRef = { key: "source.txt", versionId: "" };
      const destinationRef = { key: "copied.txt", versionId: "" };

      await writeString(s3, sourceRef.key, "copy me", {
        contentType: "text/plain",
        metadata: {
          copied: "true",
        },
      });

      const copied = await s3.copyObject("", "", sourceRef, destinationRef);
      expect(copied.meta?.ref?.key).toBe("copied.txt");
      expect(await readText(s3, destinationRef.key)).toBe("copy me");

      expect((await s3.headObject(destinationRef)).meta?.ref?.key).toBe("copied.txt");
      await s3.deleteObject(destinationRef);
      const missing = s3.headObject(destinationRef);
      await expect(missing).rejects.toBeInstanceOf(GestaltError);
      await missing.catch((error: GestaltError) => {
        expect(error.code).toBe(GestaltErrorCode.NotFound);
      });
    } finally {
      tcpProc.kill();
      if (previousTarget === undefined) {
        delete process.env[envName];
      } else {
        process.env[envName] = previousTarget;
      }
    }
  });

  test("large in-memory uploads round-trip", async () => {
    const s3 = client();
    const largeText = "x".repeat(5 * 1024 * 1024);
    const textMeta = await writeString(s3, "large.txt", largeText);
    expect(textMeta.size).toBe(BigInt(largeText.length));
    expect(await readText(s3, "large.txt")).toBe(largeText);

    const largeBytes = new Uint8Array(5 * 1024 * 1024);
    largeBytes.fill(121);
    const bytesResponse = await s3.writeObject(writeOpen("large.bin"), chunked(largeBytes));
    expect(bytesResponse.meta?.size).toBe(BigInt(largeBytes.byteLength));
    expect(await readBytes(s3, "large.bin")).toEqual(largeBytes);
  });

  test("zero-byte objects round-trip without extra data frames", async () => {
    const s3 = client();

    const written = await s3.writeObject(writeOpen("empty.bin"), noBody());
    expect(written.meta?.size).toBe(0n);

    const read = await s3.readObject({
      ref: { key: "empty.bin", versionId: "" },
      ifMatch: "",
      ifNoneMatch: "",
    });
    let frames = 0;
    for await (const chunk of read.data) {
      frames += 1;
      expect(chunk.byteLength).toBe(0);
    }
    expect(frames).toBe(0);
  });

  test("range reads return the requested subset", async () => {
    const s3 = client();
    await writeString(s3, "alphabet.txt", "abcdef");

    const read = await s3.readObject({
      ref: { key: "alphabet.txt", versionId: "" },
      range: { start: 1n, end: 3n },
      ifMatch: "",
      ifNoneMatch: "",
    });
    expect(new TextDecoder().decode(await collectBytes(read.data))).toBe("bcd");
  });

  test("write preconditions surface typed error codes", async () => {
    const s3 = client();

    await writeString(s3, "create-once.txt", "first write", { ifNoneMatch: "*" });
    const second = s3.writeObject(
      writeOpen("create-once.txt", { ifNoneMatch: "*" }),
      chunked(bytesOf("second write")),
    );
    await expect(second).rejects.toBeInstanceOf(GestaltError);
    await second.catch((error: GestaltError) => {
      expect(error.code).toBe(GestaltErrorCode.FailedPrecondition);
    });
  });

  test("listObjects supports pagination and delimiters", async () => {
    const s3 = client();
    await writeString(s3, "list/a.txt", "a");
    await writeString(s3, "list/b.txt", "b");
    await writeString(s3, "list/c.txt", "c");
    await writeString(s3, "tree/root.txt", "root");
    await writeString(s3, "tree/nested/leaf.txt", "leaf");
    await writeString(s3, "tree/nested/branch.txt", "branch");

    const firstPage = await s3.listObjects("list/", "", "", "", 2);
    expect(firstPage.objects.map((object) => object.ref?.key)).toEqual([
      "list/a.txt",
      "list/b.txt",
    ]);
    expect(firstPage.hasMore).toBe(true);
    expect(firstPage.nextContinuationToken).toBe("list/b.txt");

    const secondPage = await s3.listObjects(
      "list/",
      "",
      firstPage.nextContinuationToken,
      "",
      2,
    );
    expect(secondPage.objects.map((object) => object.ref?.key)).toEqual(["list/c.txt"]);
    expect(secondPage.hasMore).toBe(false);

    const treePage = await s3.listObjects("tree/", "/", "", "", 0);
    expect(treePage.objects.map((object) => object.ref?.key)).toEqual(["tree/root.txt"]);
    expect(treePage.commonPrefixes).toEqual(["tree/nested/"]);
  });

  test("partial stream consumption can be cancelled by the caller", async () => {
    const s3 = client();
    await writeString(s3, "large.txt", "x".repeat(128 * 1024));

    const read = await s3.readObject({
      ref: { key: "large.txt", versionId: "" },
      ifMatch: "",
      ifNoneMatch: "",
    });
    const iterator = read.data[Symbol.asyncIterator]();
    const first = await iterator.next();
    expect(first.done).toBe(false);
    expect(first.value.byteLength).toBeGreaterThan(0);
    await iterator.return?.();

    expect(await readText(s3, "large.txt")).toHaveLength(128 * 1024);
  });
});
