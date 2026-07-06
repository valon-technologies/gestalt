import { describe, expect, test } from "bun:test";

import {
  AlreadyExistsError,
  type AcquireLockResult,
  type IndexedDB,
  MigrationError,
  NotFoundError,
  type ObjectStoreSchema,
  type Record as DBRecord,
  type Revision,
  runMigrations,
} from "../src/index.ts";

interface FakeStoreState {
  pk: string;
  rows: Map<string, DBRecord>;
}

class FakeIndexedDB {
  readonly stores = new Map<string, FakeStoreState>();
  readonly indexes = new Set<string>();
  readonly calls: string[] = [];
  lock: { holder: string; expiresAt: number } | null = null;
  createObjectStoreError: Error | null = null;

  async createObjectStore(
    name: string,
    schema?: ObjectStoreSchema,
  ): Promise<unknown> {
    this.calls.push(`createObjectStore:${name}`);
    if (this.createObjectStoreError) {
      throw this.createObjectStoreError;
    }
    if (this.stores.has(name)) {
      throw new AlreadyExistsError();
    }
    const pk =
      schema?.columns?.find((column) => column.primaryKey)?.name ?? "id";
    this.stores.set(name, { pk, rows: new Map() });
    for (const index of schema?.indexes ?? []) {
      this.indexes.add(`${name}/${index.name}`);
    }
    return this.objectStore(name);
  }

  async deleteObjectStore(name: string): Promise<void> {
    this.calls.push(`deleteObjectStore:${name}`);
    if (!this.stores.has(name)) {
      throw new NotFoundError();
    }
    this.stores.delete(name);
  }

  async createIndex(
    store: string,
    index: { name: string },
  ): Promise<void> {
    this.calls.push(`createIndex:${store}/${index.name}`);
    const key = `${store}/${index.name}`;
    if (this.indexes.has(key)) {
      throw new AlreadyExistsError();
    }
    this.indexes.add(key);
  }

  async deleteIndex(store: string, name: string): Promise<void> {
    this.calls.push(`deleteIndex:${store}/${name}`);
    const key = `${store}/${name}`;
    if (!this.indexes.has(key)) {
      throw new NotFoundError();
    }
    this.indexes.delete(key);
  }

  objectStore(name: string): unknown {
    const db = this;
    const state = () => {
      const found = db.stores.get(name);
      if (!found) {
        throw new NotFoundError(`store ${name} missing`);
      }
      return found;
    };
    return {
      async getAllKeys(): Promise<string[]> {
        return [...state().rows.keys()];
      },
      async getAll(): Promise<DBRecord[]> {
        return [...state().rows.values()];
      },
      async get(id: string): Promise<DBRecord> {
        const row = state().rows.get(id);
        if (!row) {
          throw new NotFoundError();
        }
        return row;
      },
      async put(record: DBRecord): Promise<void> {
        const s = state();
        s.rows.set(String(record[s.pk]), record);
      },
      async delete(id: string): Promise<void> {
        state().rows.delete(id);
      },
    };
  }

  async acquireLock(
    _key: string,
    holder: string,
    ttlMs: number,
  ): Promise<AcquireLockResult> {
    this.calls.push(`acquireLock:${holder}`);
    const now = Date.now();
    if (this.lock && this.lock.holder !== holder && this.lock.expiresAt > now) {
      return {
        acquired: false,
        holder: this.lock.holder,
        expiresAt: new Date(this.lock.expiresAt),
        fencingToken: 0n,
      };
    }
    this.lock = { holder, expiresAt: now + ttlMs };
    return {
      acquired: true,
      holder,
      expiresAt: new Date(this.lock.expiresAt),
      fencingToken: 1n,
    };
  }

  async releaseLock(_key: string, holder: string): Promise<void> {
    this.calls.push(`releaseLock:${holder}`);
    if (this.lock?.holder === holder) {
      this.lock = null;
    }
  }

  close(): void {}
}

function fakeDb(): { db: IndexedDB; fake: FakeIndexedDB } {
  const fake = new FakeIndexedDB();
  return { db: fake as unknown as IndexedDB, fake };
}

const issuesRevision: Revision = {
  id: "0001_issues",
  schema: {
    stores: [
      {
        name: "issues",
        columns: [
          { name: "id", primaryKey: true, notNull: true },
          { name: "payload", notNull: true },
        ],
      },
    ],
  },
};

async function ledgerIds(fake: FakeIndexedDB): Promise<string[]> {
  return [...(fake.stores.get("_gestalt_migrations")?.rows.keys() ?? [])];
}

describe("runMigrations", () => {
  test("fresh install applies the revision and records the ledger", async () => {
    const { db, fake } = fakeDb();
    await runMigrations(db, { revisions: [issuesRevision] });

    expect(fake.stores.has("issues")).toBe(true);
    expect(await ledgerIds(fake)).toEqual(["0001_issues"]);
    expect(fake.calls.some((c) => c.startsWith("acquireLock:"))).toBe(true);
    expect(fake.calls.some((c) => c.startsWith("releaseLock:"))).toBe(true);
    expect(fake.lock).toBeNull();
  });

  test("returns the applied ids and declared head", async () => {
    const { db } = fakeDb();
    const second: Revision = {
      id: "0002_more",
      schema: { stores: [{ name: "more", columns: [{ name: "id", primaryKey: true }] }] },
    };

    const first = await runMigrations(db, { revisions: [issuesRevision, second] });
    expect(first.applied).toEqual(["0001_issues", "0002_more"]);
    expect(first.head).toBe("0002_more");

    const again = await runMigrations(db, { revisions: [issuesRevision, second] });
    expect(again.applied).toEqual([]);
    expect(again.head).toBe("0002_more");
  });

  test("restart is a no-op and does not re-create stores", async () => {
    const { db, fake } = fakeDb();
    await runMigrations(db, { revisions: [issuesRevision] });
    const createsBefore = fake.calls.filter((c) =>
      c.startsWith("createObjectStore:issues"),
    ).length;

    await runMigrations(db, { revisions: [issuesRevision] });
    const createsAfter = fake.calls.filter((c) =>
      c.startsWith("createObjectStore:issues"),
    ).length;

    expect(createsAfter).toBe(createsBefore);
    expect(await ledgerIds(fake)).toEqual(["0001_issues"]);
  });

  test("adds a second revision on top of an existing ledger", async () => {
    const { db, fake } = fakeDb();
    await runMigrations(db, { revisions: [issuesRevision] });

    const addIndex: Revision = {
      id: "0002_index",
      schema: { addIndexes: [{ store: "issues", index: { name: "by_status", keyPath: ["status"] } }] },
    };
    await runMigrations(db, { revisions: [issuesRevision, addIndex] });

    expect(fake.indexes.has("issues/by_status")).toBe(true);
    expect(await ledgerIds(fake)).toEqual(["0001_issues", "0002_index"]);
  });

  test("imperative revision runs up() with a restricted, idempotent handle", async () => {
    const { db, fake } = fakeDb();
    const seed: Revision = {
      id: "0001_seed",
      schema: { stores: [{ name: "issues", columns: [{ name: "id", primaryKey: true }] }] },
    };
    const backfill: Revision = {
      id: "0002_backfill",
      up: async (m) => {
        const store = m.store("issues");
        expect((store as unknown as { add?: unknown }).add).toBeUndefined();
        for (const row of await store.getAll()) {
          await store.put({ ...row, status: row.status ?? "open" });
        }
      },
    };

    await runMigrations(db, { revisions: [seed] });
    await db.objectStore("issues").put({ id: "a" });
    await runMigrations(db, { revisions: [seed, backfill] });

    const stored = await db.objectStore("issues").get("a");
    expect(stored.status).toBe("open");
    expect(await ledgerIds(fake)).toEqual(["0001_seed", "0002_backfill"]);
  });

  test("a failing revision aborts and is not recorded", async () => {
    const { db, fake } = fakeDb();
    const boom: Revision = {
      id: "0002_boom",
      up: async () => {
        throw new Error("kaboom");
      },
    };

    await expect(
      runMigrations(db, { revisions: [issuesRevision, boom] }),
    ).rejects.toThrow(MigrationError);

    expect(await ledgerIds(fake)).toEqual(["0001_issues"]);
    expect(fake.lock).toBeNull();
  });

  test("failure after an earlier revision reports the applied one as current", async () => {
    const { db } = fakeDb();
    const boom: Revision = {
      id: "0002_boom",
      up: async () => {
        throw new Error("kaboom");
      },
    };

    let error: unknown;
    try {
      await runMigrations(db, { revisions: [issuesRevision, boom] });
    } catch (e) {
      error = e;
    }
    expect(error).toBeInstanceOf(MigrationError);
    expect((error as MigrationError).current).toBe("0001_issues");
    expect((error as MigrationError).attempted).toBe("0002_boom");
  });

  test("lease lost during a revision aborts before recording it", async () => {
    const { db, fake } = fakeDb();
    const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
    const seed: Revision = {
      id: "0001_seed",
      up: async () => {
        fake.lock = { holder: "other", expiresAt: Date.now() + 60_000 };
        await sleep(1_400);
      },
    };
    const next: Revision = {
      id: "0002_next",
      schema: { stores: [{ name: "x", columns: [{ name: "id", primaryKey: true }] }] },
    };

    await expect(
      runMigrations(db, { revisions: [seed, next], lockTtlMs: 2_000 }),
    ).rejects.toThrow(/lost the migration lease/);
    expect(await ledgerIds(fake)).toEqual([]);
    expect(fake.stores.has("x")).toBe(false);
  });

  test("fails closed when the ledger is ahead of the code", async () => {
    const { db, fake } = fakeDb();
    await runMigrations(db, { revisions: [issuesRevision] });

    await db.objectStore("_gestalt_migrations").put({
      revision_id: "0002_future",
      applied_at: new Date().toISOString(),
    });

    await expect(
      runMigrations(db, { revisions: [issuesRevision] }),
    ).rejects.toThrow(/ledger is ahead/);
    expect(fake.lock).toBeNull();
  });

  test("fails closed when a revision is inserted before an applied one", async () => {
    const { db } = fakeDb();
    const first = issuesRevision; // 0001_issues
    const later: Revision = {
      id: "0003_more",
      schema: { stores: [{ name: "more", columns: [{ name: "id", primaryKey: true }] }] },
    };
    await runMigrations(db, { revisions: [first, later] });

    // A new revision is inserted before an already-applied successor.
    const inserted: Revision = {
      id: "0002_between",
      up: async () => {},
    };
    await expect(
      runMigrations(db, { revisions: [first, inserted, later] }),
    ).rejects.toThrow(/ledger has gaps/);
  });

  test("waits for a contended lease then acquires it", async () => {
    const { db, fake } = fakeDb();
    fake.lock = { holder: "other", expiresAt: Date.now() + 40 };

    await runMigrations(db, {
      revisions: [issuesRevision],
      lockTtlMs: 1_000,
      acquireTimeoutMs: 5_000,
    });

    expect(await ledgerIds(fake)).toEqual(["0001_issues"]);
  });

  test("times out if the lease is never free", async () => {
    const { db, fake } = fakeDb();
    fake.lock = { holder: "other", expiresAt: Date.now() + 60_000 };

    await expect(
      runMigrations(db, {
        revisions: [issuesRevision],
        acquireTimeoutMs: 50,
      }),
    ).rejects.toThrow(/waiting for the migration lease/);
  });

  test("rejects duplicate revision ids", async () => {
    const { db } = fakeDb();
    await expect(
      runMigrations(db, { revisions: [issuesRevision, issuesRevision] }),
    ).rejects.toThrow(/duplicate revision id/);
  });

  test("rejects a revision that is neither schema nor imperative", async () => {
    const { db } = fakeDb();
    const bad = { id: "0001_bad" } as unknown as Revision;
    await expect(runMigrations(db, { revisions: [bad] })).rejects.toThrow(
      /exactly one of/,
    );
  });
});
