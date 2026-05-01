import { mkdtempSync, rmSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawn, type Subprocess } from "bun";

import { afterAll, beforeAll, describe, expect, test } from "bun:test";

import {
  IndexedDB,
  NotFoundError,
  AlreadyExistsError,
  TransactionError,
  ColumnType,
  indexedDBSocketEnv,
  indexedDBSocketTokenEnv,
} from "../src/indexeddb.ts";

const REPO_ROOT = join(import.meta.dir, "..", "..", "..");
const GESTALTD_DIR = join(REPO_ROOT, "gestaltd");

let tmpDir: string;
let harnessBinPath: string;
let socketPath: string;
let proc: Subprocess;
let db: IndexedDB;

beforeAll(async () => {
  tmpDir = mkdtempSync(join(tmpdir(), "indexeddb-transport-test-"));
  harnessBinPath = join(tmpDir, "indexeddbtransportd");
  socketPath = join(tmpDir, "indexeddb.sock");

  const build = spawn(
    ["go", "build", "-o", harnessBinPath, "./internal/testutil/cmd/indexeddbtransportd/"],
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

  process.env.GESTALT_INDEXEDDB_SOCKET = socketPath;
  process.env[indexedDBSocketEnv("named")] = `unix://${socketPath}`;
  db = new IndexedDB();
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
  delete process.env.GESTALT_INDEXEDDB_SOCKET;
  delete process.env[indexedDBSocketEnv("named")];
  if (tmpDir) {
    rmSync(tmpDir, { recursive: true, force: true });
  }
});

describe("IndexedDB transport", () => {
  test("createObjectStore forwards declared columns", async () => {
    const envName = indexedDBSocketEnv("schema");
    process.env[envName] = "/tmp/fake-indexeddb.sock";
    try {
      const local = new IndexedDB("schema");
      const calls: any[] = [];
      (local as any).client = {
        createObjectStore: async (request: any) => {
          calls.push(request);
          return {};
        },
      };

      await local.createObjectStore("typed_store", {
        indexes: [{ name: "by_status", keyPath: ["status"], unique: false }],
        columns: [
          { name: "id", type: ColumnType.String, primaryKey: true, notNull: true },
          { name: "attempts", type: ColumnType.Int },
          { name: "enabled", type: ColumnType.Bool },
          { name: "payload", type: ColumnType.JSON },
        ],
      });

      expect(calls).toHaveLength(1);
      expect(calls[0]).toEqual({
        name: "typed_store",
        schema: {
          indexes: [{ name: "by_status", keyPath: ["status"], unique: false }],
          columns: [
            { name: "id", type: ColumnType.String, primaryKey: true, notNull: true, unique: false },
            { name: "attempts", type: ColumnType.Int, primaryKey: false, notNull: false, unique: false },
            { name: "enabled", type: ColumnType.Bool, primaryKey: false, notNull: false, unique: false },
            { name: "payload", type: ColumnType.JSON, primaryKey: false, notNull: false, unique: false },
          ],
        },
      });
    } finally {
      delete process.env[envName];
    }
  });

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

  test("transaction readwrite commits and reads own writes", async () => {
    const store = "transaction_commit";
    await db.createObjectStore(store);
    const os = db.objectStore(store);

    const tx = await db.transaction([store], "readwrite");
    const txos = tx.objectStore(store);
    await txos.put({ id: "lease-1", owner: "worker-a", attempt: 1 });
    expect((await txos.get("lease-1")).owner).toBe("worker-a");
    await txos.put({ id: "lease-1", owner: "worker-b", attempt: 2 });
    expect(await txos.count()).toBe(1);
    await tx.commit();

    expect(await os.get("lease-1")).toEqual({ id: "lease-1", owner: "worker-b", attempt: 2 });
  });

  test("transaction abort rolls back writes", async () => {
    const store = "transaction_abort";
    await db.createObjectStore(store);
    const os = db.objectStore(store);

    const tx = await db.transaction([store], "readwrite");
    await tx.objectStore(store).put({ id: "row-1", value: "pending" });
    await tx.abort();

    await expect(os.get("row-1")).rejects.toBeInstanceOf(NotFoundError);
  });

  test("readonly transaction rejects writes", async () => {
    const store = "transaction_readonly";
    await db.createObjectStore(store);
    const os = db.objectStore(store);
    await os.put({ id: "row-1", value: "kept" });

    const tx = await db.transaction([store], "readonly");
    expect((await tx.objectStore(store).get("row-1")).value).toBe("kept");
    await expect(tx.objectStore(store).put({ id: "row-2", value: "blocked" })).rejects.toBeInstanceOf(
      TransactionError,
    );
    await expect(os.get("row-2")).rejects.toBeInstanceOf(NotFoundError);
  });

  test("transaction operation error rolls back writes", async () => {
    const store = "transaction_error_rollback";
    await db.createObjectStore(store);
    const os = db.objectStore(store);

    const tx = await db.transaction([store], "readwrite");
    const txos = tx.objectStore(store);
    await txos.add({ id: "row-1", value: "pending" });
    await expect(txos.add({ id: "row-1", value: "duplicate" })).rejects.toBeInstanceOf(AlreadyExistsError);

    await expect(os.get("row-1")).rejects.toBeInstanceOf(NotFoundError);
  });

  test("transaction index operations and bulk deletes roll back", async () => {
    const store = "transaction_index_bulk_rollback";
    await db.createObjectStore(store, {
      indexes: [{ name: "by_status", keyPath: ["status"] }],
    });
    const os = db.objectStore(store);
    await os.add({ id: "a", status: "active" });
    await os.add({ id: "b", status: "active" });
    await os.add({ id: "c", status: "inactive" });
    await os.add({ id: "d", status: "active" });

    const tx = await db.transaction([store], "readwrite");
    const txos = tx.objectStore(store);
    expect(await txos.index("by_status").count(undefined, "active")).toBe(3);
    expect(await txos.index("by_status").getAllKeys(undefined, "active")).toHaveLength(3);
    expect(await txos.deleteRange({ lower: "b", upper: "c" })).toBe(2);
    expect(await txos.index("by_status").delete("active")).toBe(2);
    await txos.clear();
    expect(await txos.count()).toBe(0);
    await tx.abort();

    expect(await os.count()).toBe(4);
    expect(await os.index("by_status").count(undefined, "inactive")).toBe(1);
  });

  test("named socket env selects the requested binding", async () => {
    const namedDb = new IndexedDB("named");
    const store = "named_socket_env";
    await namedDb.createObjectStore(store);
    await namedDb.objectStore(store).put({ id: "row-1", value: "named" });
    const got = await namedDb.objectStore(store).get("row-1");
    expect(got.value).toBe("named");
  });

  test("tcp target env selects the requested binding", async () => {
    const { proc: tcpProc, target } = await startTCPHarness();
    const envName = indexedDBSocketEnv("tcp");
    process.env[envName] = target;
    try {
      const tcpDb = new IndexedDB("tcp");
      const store = "tcp_target_env";
      await tcpDb.createObjectStore(store);
      await tcpDb.objectStore(store).put({ id: "row-1", value: "tcp" });
      const got = await tcpDb.objectStore(store).get("row-1");
      expect(got.value).toBe("tcp");
    } finally {
      tcpProc.kill();
      delete process.env[envName];
    }
  });

  test("tcp target token env selects the requested binding", async () => {
    const token = "relay-token-typescript";
    const { proc: tcpProc, target } = await startTCPHarness(token);
    const envName = indexedDBSocketEnv("tcp-token");
    const tokenEnvName = indexedDBSocketTokenEnv("tcp-token");
    process.env[envName] = target;
    process.env[tokenEnvName] = token;
    try {
      const tcpDb = new IndexedDB("tcp-token");
      const store = "tcp_target_token_env";
      await tcpDb.createObjectStore(store);
      await tcpDb.objectStore(store).put({ id: "row-1", value: "token" });
      const got = await tcpDb.objectStore(store).get("row-1");
      expect(got.value).toBe("token");
    } finally {
      tcpProc.kill();
      delete process.env[envName];
      delete process.env[tokenEnvName];
    }
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

  test("advance rejects non-positive counts", async () => {
    const store = "advance_invalid";
    await db.createObjectStore(store);
    const os = db.objectStore(store);

    await os.put({ id: "x", data: 1 });

    const cursor = await os.openCursor();
    expect(cursor).not.toBeNull();

    await expect(cursor!.advance(0)).rejects.toThrow("advance count must be positive");
    cursor!.close();
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

    await expect(cursor!.delete()).rejects.toThrow(NotFoundError);
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
    cursor!.close();
  });

  test("error mapping: get missing throws NotFoundError", async () => {
    const store = "error_not_found";
    await db.createObjectStore(store);
    const os = db.objectStore(store);

    await expect(os.get("nonexistent")).rejects.toThrow(NotFoundError);
  });

  test("error mapping: duplicate add throws AlreadyExistsError", async () => {
    const store = "error_already_exists";
    await db.createObjectStore(store);
    const os = db.objectStore(store);

    await os.add({ id: "dup-1", data: "first" });
    await expect(os.add({ id: "dup-1", data: "second" })).rejects.toThrow(AlreadyExistsError);
  });
});
