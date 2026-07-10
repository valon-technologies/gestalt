import { describe, expect, test } from "bun:test";

import {
  AlreadyExistsError,
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
      async openCursor(): Promise<unknown> {
        const s = state();
        const entries = [...s.rows.entries()];
        let i = -1;
        const cursor = {
          key: undefined as unknown,
          primaryKey: "",
          value: undefined as DBRecord | undefined,
          done: false,
          continue: async (): Promise<boolean> => {
            i += 1;
            const entry = entries[i];
            if (entry === undefined) {
              cursor.done = true;
              cursor.value = undefined;
              cursor.primaryKey = "";
              return false;
            }
            cursor.primaryKey = entry[0];
            cursor.value = entry[1];
            return true;
          },
          update: async (record: DBRecord): Promise<void> => {
            s.rows.set(String(record[s.pk]), record);
          },
          close: (): void => {},
        };
        return cursor;
      },
    };
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

  test("rejects a backfill whose from equals into", async () => {
    const { db } = fakeDb();
    const inPlace: Revision = {
      id: "0001_inplace",
      backfill: {
        from: "issues",
        into: "issues",
        value: (row) => ({ ...row, status: row.status ?? "open" }),
      },
    };

    await expect(
      runMigrations(db, { revisions: [inPlace] }),
    ).rejects.toThrow(/"from" and "into" must differ/);
  });

  test("backfill revision copies rows into another store", async () => {
    const { db, fake } = fakeDb();
    const seed: Revision = {
      id: "0001_seed",
      schema: {
        stores: [
          { name: "issues", columns: [{ name: "id", primaryKey: true }] },
          { name: "issue_index", columns: [{ name: "id", primaryKey: true }] },
        ],
      },
    };
    const backfill: Revision = {
      id: "0002_index",
      backfill: {
        from: "issues",
        into: "issue_index",
        value: (row) => ({ id: row.id, text: `issue-${String(row.id)}` }),
      },
    };

    await runMigrations(db, { revisions: [seed] });
    await db.objectStore("issues").put({ id: "a" });
    await runMigrations(db, { revisions: [seed, backfill] });

    expect((await db.objectStore("issue_index").get("a")).text).toBe("issue-a");
    expect((await db.objectStore("issues").get("a")).id).toBe("a");
    expect(await ledgerIds(fake)).toEqual(["0001_seed", "0002_index"]);
  });

  test("a failing revision aborts and is not recorded", async () => {
    const { db, fake } = fakeDb();
    const boom: Revision = {
      id: "0002_boom",
      backfill: {
        from: "missing_src",
        into: "missing_dst",
        value: (row) => row,
      },
    };

    await expect(
      runMigrations(db, { revisions: [issuesRevision, boom] }),
    ).rejects.toThrow(MigrationError);

    expect(await ledgerIds(fake)).toEqual(["0001_issues"]);
  });

  test("failure after an earlier revision reports the applied one as current", async () => {
    const { db } = fakeDb();
    const boom: Revision = {
      id: "0002_boom",
      backfill: {
        from: "missing_src",
        into: "missing_dst",
        value: (row) => row,
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

  test("concurrent runners converge without a lock", async () => {
    const { db, fake } = fakeDb();
    const seed: Revision = {
      id: "0001_seed",
      schema: { stores: [{ name: "issues", columns: [{ name: "id", primaryKey: true }] }] },
    };
    await runMigrations(db, { revisions: [seed] });
    await db.objectStore("issues").put({ id: "a" });
    await db.objectStore("issues").put({ id: "b", status: "closed" });

    const backfill: Revision = {
      id: "0002_index",
      backfill: {
        from: "issues",
        into: "issue_index",
        value: (row) => ({ id: row.id, text: `issue-${String(row.id)}` }),
      },
    };
    const full: Revision[] = [
      seed,
      { id: "0001_5_index", schema: { stores: [{ name: "issue_index", columns: [{ name: "id", primaryKey: true }] }] } },
      backfill,
    ];

    await Promise.all([
      runMigrations(db, { revisions: full }),
      runMigrations(db, { revisions: full }),
    ]);

    expect(await ledgerIds(fake)).toEqual(["0001_seed", "0001_5_index", "0002_index"]);
    expect((await db.objectStore("issue_index").get("a")).text).toBe("issue-a");
    expect((await db.objectStore("issue_index").get("b")).text).toBe("issue-b");
  });

  test("fails closed when the ledger is ahead of the code", async () => {
    const { db } = fakeDb();
    await runMigrations(db, { revisions: [issuesRevision] });

    await db.objectStore("_gestalt_migrations").put({
      revision_id: "0002_future",
      applied_at: new Date().toISOString(),
    });

    await expect(
      runMigrations(db, { revisions: [issuesRevision] }),
    ).rejects.toThrow(/ledger is ahead/);
  });

  test("ignores deeper namespace ledger rows on a shared db", async () => {
    const { db } = fakeDb();
    const nested: Revision = {
      id: "auth/oidc/nested/0001_init",
      schema: {
        stores: [{ name: "nested", columns: [{ name: "id", primaryKey: true }] }],
      },
    };
    await runMigrations(db, { revisions: [nested] });

    const sibling: Revision = {
      id: "auth/oidc/0001_init",
      schema: {
        stores: [{ name: "grants", columns: [{ name: "id", primaryKey: true }] }],
      },
    };
    await expect(runMigrations(db, { revisions: [sibling] })).resolves.toBeDefined();
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
      schema: { stores: [{ name: "between", columns: [{ name: "id", primaryKey: true }] }] },
    };
    await expect(
      runMigrations(db, { revisions: [first, inserted, later] }),
    ).rejects.toThrow(/ledger has gaps/);
  });

  test("rejects duplicate revision ids", async () => {
    const { db } = fakeDb();
    await expect(
      runMigrations(db, { revisions: [issuesRevision, issuesRevision] }),
    ).rejects.toThrow(/duplicate revision id/);
  });

  test("rejects a revision that is neither schema nor backfill", async () => {
    const { db } = fakeDb();
    const bad = { id: "0001_bad" } as unknown as Revision;
    await expect(runMigrations(db, { revisions: [bad] })).rejects.toThrow(
      /exactly one of/,
    );
  });

  test("a null schema alongside a backfill runs the backfill, not applySchema", async () => {
    const { db, fake } = fakeDb();
    const seed: Revision = {
      id: "0001_seed",
      schema: {
        stores: [
          { name: "src", columns: [{ name: "id", primaryKey: true }] },
          { name: "dst", columns: [{ name: "id", primaryKey: true }] },
        ],
      },
    };
    await runMigrations(db, { revisions: [seed] });
    await db.objectStore("src").put({ id: "a" });

    const weird = {
      id: "0002_weird",
      schema: null,
      backfill: { from: "src", into: "dst", value: (row: DBRecord) => row },
    } as unknown as Revision;

    await runMigrations(db, { revisions: [seed, weird] });

    expect((await db.objectStore("dst").get("a")).id).toBe("a");
    expect(await ledgerIds(fake)).toEqual(["0001_seed", "0002_weird"]);
  });

  test("ignores foreign namespace ledger rows for a namespaced provider", async () => {
    const { db } = fakeDb();
    const auth: Revision = {
      id: "auth/oidc/0001_init",
      schema: {
        stores: [{ name: "sessions", columns: [{ name: "id", primaryKey: true }] }],
      },
    };
    await runMigrations(db, { revisions: [auth] });

    const gIssues: Revision = {
      id: "gIssues/0001_init",
      schema: {
        stores: [{ name: "issues", columns: [{ name: "id", primaryKey: true }] }],
      },
    };
    await expect(runMigrations(db, { revisions: [gIssues] })).resolves.toBeDefined();
  });
});
