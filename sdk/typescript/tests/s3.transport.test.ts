import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawn, type Subprocess } from "bun";

import { afterAll, beforeAll, describe, expect, test } from "bun:test";

import {
  PresignMethod,
  S3,
  S3NotFoundError,
  S3PreconditionFailedError,
  s3SocketEnv,
} from "../src/index.ts";

const REPO_ROOT = join(import.meta.dir, "..", "..", "..");
const GESTALTD_DIR = join(REPO_ROOT, "gestaltd");

let tmpDir: string;
let socketPath: string;
let proc: Subprocess;

beforeAll(async () => {
  tmpDir = mkdtempSync(join(tmpdir(), "s3-transport-test-"));
  const binPath = join(tmpDir, "s3transportd");
  socketPath = join(tmpDir, "s3.sock");

  const build = spawn(
    ["go", "build", "-o", binPath, "./internal/testutil/cmd/s3transportd/"],
    { cwd: GESTALTD_DIR, stdout: "pipe", stderr: "pipe" },
  );
  const buildExit = await build.exited;
  if (buildExit !== 0) {
    const stderr = await new Response(build.stderr).text();
    throw new Error(`go build failed (exit ${buildExit}): ${stderr}`);
  }

  proc = spawn([binPath, "--socket", socketPath], {
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

  process.env.GESTALT_S3_SOCKET = socketPath;
  process.env[s3SocketEnv("named")] = socketPath;
}, 60_000);

afterAll(() => {
  proc?.kill();
  delete process.env.GESTALT_S3_SOCKET;
  delete process.env[s3SocketEnv("named")];
  if (tmpDir) {
    rmSync(tmpDir, { recursive: true, force: true });
  }
});

describe("S3 transport", () => {
  const client = (): S3 => new S3();

  test("named socket env selects the requested binding", async () => {
    const named = new S3("named");
    const object = named.object("named-bucket", "hello.txt");

    await object.writeString("named binding", {
      contentType: "text/plain",
    });

    expect(await object.text()).toBe("named binding");
  });

  test("write, stat, read, and json round-trip", async () => {
    const s3 = client();
    const object = s3.object("docs-bucket", "payload.json");

    const written = await object.writeJSON(
      {
        message: "hello",
        count: 3,
      },
      {
        metadata: {
          env: "test",
        },
      },
    );

    expect(written.size).toBeGreaterThan(0n);
    expect(written.contentType).toBe("application/json");
    expect(written.metadata).toEqual({ env: "test" });
    expect(written.lastModified).toBeInstanceOf(Date);

    const meta = await object.stat();
    expect(meta.etag).toBe(written.etag);
    expect(meta.size).toBe(written.size);
    expect(meta.contentType).toBe("application/json");
    expect(meta.metadata).toEqual({ env: "test" });

    const read = await object.read();
    expect(read.meta.etag).toBe(written.etag);
    expect(await readText(read.stream)).toBe('{"message":"hello","count":3}');
    expect(await object.json<{ message: string; count: number }>()).toEqual({
      message: "hello",
      count: 3,
    });
  });

  test("large in-memory uploads round-trip", async () => {
    const s3 = client();
    const largeText = "x".repeat(5 * 1024 * 1024);
    const textObject = s3.object("docs-bucket", "large.txt");
    const textMeta = await textObject.writeString(largeText);
    expect(textMeta.size).toBe(BigInt(largeText.length));
    expect(await textObject.text()).toBe(largeText);

    const largeBytes = new Uint8Array(5 * 1024 * 1024);
    largeBytes.fill(121);
    const bytesObject = s3.object("docs-bucket", "large.bin");
    const bytesMeta = await bytesObject.writeBytes(largeBytes);
    expect(bytesMeta.size).toBe(BigInt(largeBytes.byteLength));
    expect(await bytesObject.bytes()).toEqual(largeBytes);
  });

  test("writeBytes snapshots mutable buffers before upload", async () => {
    const s3 = client();
    const source = new Uint8Array(5 * 1024 * 1024);
    source.fill(120);
    const expected = source.slice();
    const object = s3.object("docs-bucket", "snapshot.bin");

    const pending = object.writeBytes(source);
    source.fill(121);

    await pending;
    expect(await object.bytes()).toEqual(expected);
  });

  test("zero-byte objects round-trip without extra data frames", async () => {
    const s3 = client();
    const object = s3.object("docs-bucket", "empty.bin");

    const meta = await object.writeBytes(new Uint8Array());
    expect(meta.size).toBe(0n);

    const bytes = await object.bytes();
    expect(bytes.byteLength).toBe(0);
    expect(await object.text()).toBe("");
  });

  test("range reads return the requested subset", async () => {
    const s3 = client();
    const object = s3.object("docs-bucket", "alphabet.txt");
    await object.writeString("abcdef");

    expect(await object.text({ range: { start: 1, end: 3 } })).toBe("bcd");
  });

  test("write preconditions map to typed errors", async () => {
    const s3 = client();
    const object = s3.object("docs-bucket", "create-once.txt");

    await object.writeString("first write", { ifNoneMatch: "*" });
    await expect(
      object.writeString("second write", { ifNoneMatch: "*" }),
    ).rejects.toBeInstanceOf(S3PreconditionFailedError);
  });

  test("listObjects supports pagination and delimiters", async () => {
    const s3 = client();
    const bucket = "listing-bucket";
    await s3.object(bucket, "list/a.txt").writeString("a");
    await s3.object(bucket, "list/b.txt").writeString("b");
    await s3.object(bucket, "list/c.txt").writeString("c");
    await s3.object(bucket, "tree/root.txt").writeString("root");
    await s3.object(bucket, "tree/nested/leaf.txt").writeString("leaf");
    await s3.object(bucket, "tree/nested/branch.txt").writeString("branch");

    const firstPage = await s3.listObjects({
      bucket,
      prefix: "list/",
      maxKeys: 2,
    });
    expect(firstPage.objects.map((object) => object.ref.key)).toEqual([
      "list/a.txt",
      "list/b.txt",
    ]);
    expect(firstPage.hasMore).toBe(true);
    expect(firstPage.nextContinuationToken).toBe("list/b.txt");

    const secondPage = await s3.listObjects({
      bucket,
      prefix: "list/",
      continuationToken: firstPage.nextContinuationToken,
      maxKeys: 2,
    });
    expect(secondPage.objects.map((object) => object.ref.key)).toEqual(["list/c.txt"]);
    expect(secondPage.hasMore).toBe(false);

    const treePage = await s3.listObjects({
      bucket,
      prefix: "tree/",
      delimiter: "/",
    });
    expect(treePage.objects.map((object) => object.ref.key)).toEqual(["tree/root.txt"]);
    expect(treePage.commonPrefixes).toEqual(["tree/nested/"]);
  });

  test("copy, delete, exists, and presign round-trip", async () => {
    const s3 = client();
    const sourceRef = {
      bucket: "copy-bucket",
      key: "source.txt",
    } as const;
    const destinationRef = {
      bucket: "copy-bucket",
      key: "copied.txt",
    } as const;
    const source = s3.object(sourceRef.bucket, sourceRef.key);
    const destination = s3.object(destinationRef.bucket, destinationRef.key);

    await source.writeString("copy me", {
      contentType: "text/plain",
      metadata: {
        copied: "true",
      },
    });

    const copied = await s3.copyObject(sourceRef, destinationRef);
    expect(copied.ref.bucket).toBe("copy-bucket");
    expect(copied.ref.key).toBe("copied.txt");
    expect(await destination.text()).toBe("copy me");

    const presigned = await destination.presign({
      method: PresignMethod.Put,
      expiresSeconds: 60,
      contentType: "text/plain",
      headers: {
        "x-test-header": "present",
      },
    });
    expect(presigned.method).toBe(PresignMethod.Put);
    expect(presigned.url).toContain("https://example.invalid/copy-bucket/copied.txt");
    expect(presigned.url).toContain("method=PUT");
    expect(presigned.headers).toEqual({ "x-test-header": "present" });
    expect(presigned.expiresAt).toBeInstanceOf(Date);

    expect(await destination.exists()).toBe(true);
    await destination.delete();
    expect(await destination.exists()).toBe(false);
    await expect(destination.stat()).rejects.toBeInstanceOf(S3NotFoundError);
  });

  test("partial stream consumption can be cancelled by the caller", async () => {
    const s3 = client();
    const object = s3.object("stream-bucket", "large.txt");
    await object.writeString("x".repeat(128 * 1024));

    const stream = await object.stream();
    const iterator = stream[Symbol.asyncIterator]();
    const first = await iterator.next();
    expect(first.done).toBe(false);
    expect(first.value.byteLength).toBeGreaterThan(0);
    await iterator.return?.();

    expect(await object.text()).toHaveLength(128 * 1024);
  });
});

async function readText(stream: AsyncIterable<Uint8Array>): Promise<string> {
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
  return new TextDecoder().decode(out);
}
