import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawn, type Subprocess } from "bun";

import { afterAll, beforeAll, describe, expect, test } from "bun:test";

import {
  IndexedDB,
  NotFoundError,
  AlreadyExistsError,
} from "../src/indexeddb.ts";

const REPO_ROOT = join(import.meta.dir, "..", "..", "..");
const GESTALTD_DIR = join(REPO_ROOT, "gestaltd");

let tmpDir: string;
let socketPath: string;
let proc: Subprocess;
let db: IndexedDB;

beforeAll(async () => {
  tmpDir = mkdtempSync(join(tmpdir(), "indexeddb-transport-test-"));
  const binPath = join(tmpDir, "indexeddbtransportd");
  socketPath = join(tmpDir, "indexeddb.sock");

  const build = spawn(
    ["go", "build", "-o", binPath, "./internal/testutil/cmd/indexeddbtransportd/"],
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

  process.env.GESTALT_INDEXEDDB_SOCKET = socketPath;
  db = new IndexedDB();
}, 60_000);

afterAll(() => {
  proc?.kill();
  delete process.env.GESTALT_INDEXEDDB_SOCKET;
  if (tmpDir) {
    rmSync(tmpDir, { recursive: true, force: true });
  }
});

describe("IndexedDB transport", () => {
  test("nested JSON round-trip", async () => {
    const store = "nested_json_roundtrip";
    await db.createObjectStore(store);
    const os = db.objectStore(store);

    const record = {
      id: "doc-1",
      title: "test",
      count: 42,
      active: true,
      nothing: null,
      tags: ["alpha", "beta"],
      metadata: {
        created: "2025-01-01",
        scores: [1, 2, 3],
        nested: { deep: true },
      },
    };

    await os.put(record);
    const got = await os.get("doc-1");

    expect(got.id).toBe("doc-1");
    expect(got.title).toBe("test");
    expect(got.count).toBe(42);
    expect(got.active).toBe(true);
    expect(got.nothing).toBeNull();
    expect(Array.isArray(got.tags)).toBe(true);
    expect(got.tags).toEqual(["alpha", "beta"]);
    expect(typeof got.metadata).toBe("object");
    const meta = got.metadata as any;
    expect(meta.created).toBe("2025-01-01");
    expect(meta.scores).toEqual([1, 2, 3]);
    expect(meta.nested).toEqual({ deep: true });
  });

  test("cursor happy path: 4 records in order", async () => {
    const store = "cursor_happy_path";
    await db.createObjectStore(store);
    const os = db.objectStore(store);

    await os.put({ id: "b", val: 2 });
    await os.put({ id: "d", val: 4 });
    await os.put({ id: "a", val: 1 });
    await os.put({ id: "c", val: 3 });

    const cursor = await os.openCursor();
    expect(cursor).not.toBeNull();

    const keys: string[] = [];
    let ok = await cursor!.continue();
    while (ok) {
      keys.push(cursor!.primaryKey);
      ok = await cursor!.continue();
    }
    expect(keys).toEqual(["a", "b", "c", "d"]);
  });

  test("empty cursor: continue returns false", async () => {
    const store = "empty_cursor";
    await db.createObjectStore(store);
    const os = db.objectStore(store);

    const cursor = await os.openCursor();
    expect(cursor).not.toBeNull();

    const hasMore = await cursor!.continue();
    expect(hasMore).toBe(false);
    expect(cursor!.done).toBe(true);
  });

  test("keys-only cursor: value is undefined", async () => {
    const store = "keys_only_cursor";
    await db.createObjectStore(store);
    const os = db.objectStore(store);

    await os.put({ id: "k1", data: "hello" });
    await os.put({ id: "k2", data: "world" });

    const cursor = await os.openKeyCursor();
    expect(cursor).not.toBeNull();

    const moved = await cursor!.continue();
    expect(moved).toBe(true);
    expect(cursor!.primaryKey).toBe("k1");
    expect(cursor!.value).toBeUndefined();

    cursor!.close();
  });

  test("cursor exhaustion: no error after done", async () => {
    const store = "cursor_exhaustion";
    await db.createObjectStore(store);
    const os = db.objectStore(store);

    await os.put({ id: "only", data: "one" });

    const cursor = await os.openCursor();
    expect(cursor).not.toBeNull();

    const first = await cursor!.continue();
    expect(first).toBe(true);
    expect(cursor!.primaryKey).toBe("only");

    const second = await cursor!.continue();
    expect(second).toBe(false);
    expect(cursor!.done).toBe(true);
  });

  test("continueToKey beyond end returns false", async () => {
    const store = "continue_to_key_beyond";
    await db.createObjectStore(store);
    const os = db.objectStore(store);

    await os.put({ id: "a", data: 1 });
    await os.put({ id: "b", data: 2 });

    const cursor = await os.openCursor();
    expect(cursor).not.toBeNull();

    const moved = await cursor!.continueToKey("z");
    expect(moved).toBe(false);
    expect(cursor!.done).toBe(true);
  });

  test("advance past end returns false", async () => {
    const store = "advance_past_end";
    await db.createObjectStore(store);
    const os = db.objectStore(store);

    await os.put({ id: "x", data: 1 });

    const cursor = await os.openCursor();
    expect(cursor).not.toBeNull();

    const moved = await cursor!.advance(100);
    expect(moved).toBe(false);
    expect(cursor!.done).toBe(true);
  });

  test("post-exhaustion: continue false, delete throws NotFoundError", async () => {
    const store = "post_exhaustion";
    await db.createObjectStore(store);
    const os = db.objectStore(store);

    await os.put({ id: "sole", data: "item" });

    const cursor = await os.openCursor();
    expect(cursor).not.toBeNull();

    await cursor!.continue();
    await cursor!.continue();
    expect(cursor!.done).toBe(true);

    const again = await cursor!.continue();
    expect(again).toBe(false);

    expect(cursor!.delete()).rejects.toThrow(NotFoundError);
  });

  test("index cursor: filter by_status=active returns 3 records", async () => {
    const store = "index_cursor";
    await db.createObjectStore(store, {
      indexes: [{ name: "by_status", keyPath: ["status"] }],
    });
    const os = db.objectStore(store);

    await os.put({ id: "r1", status: "active", label: "one" });
    await os.put({ id: "r2", status: "inactive", label: "two" });
    await os.put({ id: "r3", status: "active", label: "three" });
    await os.put({ id: "r4", status: "active", label: "four" });
    await os.put({ id: "r5", status: "inactive", label: "five" });

    const idx = os.index("by_status");
    const cursor = await idx.openCursor(undefined, "active");
    expect(cursor).not.toBeNull();

    const keys: string[] = [];
    let ok = await cursor!.continue();
    while (ok) {
      keys.push(cursor!.primaryKey);
      ok = await cursor!.continue();
    }
    expect(keys).toHaveLength(3);
    expect(keys).toEqual(["r1", "r3", "r4"]);
  });

  test("index cursor: continueToKey round-trips the current key", async () => {
    const store = "index_cursor_seek";
    await db.createObjectStore(store, {
      indexes: [{ name: "by_num", keyPath: ["n"] }],
    });
    const os = db.objectStore(store);

    await os.put({ id: "a", n: 1 });
    await os.put({ id: "b", n: 2 });
    await os.put({ id: "c", n: 3 });

    const cursor = await os.index("by_num").openCursor();
    expect(cursor).not.toBeNull();

    expect(await cursor!.continue()).toBe(true);
    expect(cursor!.key).toEqual([1]);
    expect(await cursor!.continueToKey(cursor!.key)).toBe(true);
    expect(cursor!.primaryKey).toBe("b");
  });

  test("cursor update acknowledges successful mutation", async () => {
    const store = "cursor_update_ack";
    await db.createObjectStore(store);
    const os = db.objectStore(store);

    await os.put({ id: "u1", status: "active" });

    const cursor = await os.openCursor();
    expect(cursor).not.toBeNull();
    expect(await cursor!.continue()).toBe(true);

    await cursor!.update({ id: "u1", status: "inactive" });
    expect(cursor!.value).toEqual({ id: "u1", status: "inactive" });
    const got = await os.get("u1");
    expect(got).toEqual({ id: "u1", status: "inactive" });
  });

  test("error mapping: get missing throws NotFoundError", async () => {
    const store = "error_not_found";
    await db.createObjectStore(store);
    const os = db.objectStore(store);

    expect(os.get("nonexistent")).rejects.toThrow(NotFoundError);
  });

  test("error mapping: duplicate add throws AlreadyExistsError", async () => {
    const store = "error_already_exists";
    await db.createObjectStore(store);
    const os = db.objectStore(store);

    await os.add({ id: "dup-1", data: "first" });
    expect(os.add({ id: "dup-1", data: "second" })).rejects.toThrow(AlreadyExistsError);
  });
});
