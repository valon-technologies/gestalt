import { expect, test } from "bun:test";

import {
  AlreadyExistsError,
  ColumnType,
  type IndexedDB,
  NotFoundError,
  type ObjectStore,
  type Record as DbRecord,
} from "../src/providers/indexeddb.ts";
import {
  type MigrationHandle,
  type MigrationSet,
  runMigrations,
} from "../src/migrate.ts";

class FakeStore {
  readonly rows = new Map<string, DbRecord>();

  constructor(private readonly pk: string) {}

  async add(record: DbRecord): Promise<void> {
    const key = String(record[this.pk]);
    if (this.rows.has(key)) {
      throw new AlreadyExistsError();
    }
    this.rows.set(key, record);
  }

  async put(record: DbRecord): Promise<void> {
    this.rows.set(String(record[this.pk]), record);
  }

  async get(id: string): Promise<DbRecord> {
    const row = this.rows.get(id);
    if (row === undefined) {
      throw new NotFoundError();
    }
    return row;
  }

  async delete(id: string): Promise<void> {
    this.rows.delete(id);
  }

  async getAll(): Promise<DbRecord[]> {
    return [...this.rows.values()];
  }

  async getAllKeys(): Promise<string[]> {
    return [...this.rows.keys()];
  }

  async count(): Promise<number> {
    return this.rows.size;
  }

  async clear(): Promise<void> {
    this.rows.clear();
  }

  async deleteRange(): Promise<number> {
    const removed = this.rows.size;
    this.rows.clear();
    return removed;
  }
}

class FakeDB {
  readonly stores = new Map<string, FakeStore>();
  readonly created: string[] = [];
  readonly deleted: string[] = [];

  async createObjectStore(
    name: string,
    schema?: { columns?: Array<{ name: string; primaryKey?: boolean }> },
  ): Promise<ObjectStore> {
    this.created.push(name);
    if (this.stores.has(name)) {
      throw new AlreadyExistsError();
    }
    const pk = schema?.columns?.find((column) => column.primaryKey)?.name ?? "id";
    const store = new FakeStore(pk);
    this.stores.set(name, store);
    return store as unknown as ObjectStore;
  }

  async deleteObjectStore(name: string): Promise<void> {
    this.deleted.push(name);
    if (!this.stores.delete(name)) {
      throw new NotFoundError();
    }
  }

  objectStore(name: string): ObjectStore {
    let store = this.stores.get(name);
    if (!store) {
      store = new FakeStore("id");
      this.stores.set(name, store);
    }
    return store as unknown as ObjectStore;
  }

  close(): void {}
}

function fakeDB(): FakeDB {
  return new FakeDB();
}

function asDB(db: FakeDB): IndexedDB {
  return db as unknown as IndexedDB;
}

const issuesStore: MigrationSet = {
  revisions: [
    {
      id: "0001_issues",
      schema: {
        stores: [
          {
            name: "issues",
            columns: [
              { name: "id", type: ColumnType.String, primaryKey: true, notNull: true },
              { name: "payload", type: ColumnType.JSON, notNull: true },
            ],
          },
        ],
      },
    },
  ],
};

test("fresh install applies a declarative revision and records the ledger", async () => {
  const db = fakeDB();
  const result = await runMigrations(asDB(db), issuesStore);

  expect(result.applied).toEqual(["0001_issues"]);
  expect(result.head).toBe("0001_issues");
  expect(db.stores.has("issues")).toBe(true);
  expect(await db.stores.get("_gestalt_migrations")!.getAllKeys()).toEqual([
    "0001_issues",
  ]);
});

test("re-running is a no-op", async () => {
  const db = fakeDB();
  await runMigrations(asDB(db), issuesStore);
  const second = await runMigrations(asDB(db), issuesStore);

  expect(second.applied).toEqual([]);
  expect(second.head).toBe("0001_issues");
});

test("imperative revision runs against a restricted handle", async () => {
  let seenHandle: MigrationHandle | undefined;
  const plan: MigrationSet = {
    revisions: [
      issuesStore.revisions[0]!,
      {
        id: "0002_seed",
        up: async (handle) => {
          seenHandle = handle;
          await handle.store("issues").put({ id: "seed-1", payload: { status: "open" } });
        },
      },
    ],
  };

  const db = fakeDB();
  const result = await runMigrations(asDB(db), plan);

  expect(result.applied).toEqual(["0001_issues", "0002_seed"]);
  expect(await db.stores.get("issues")!.getAllKeys()).toEqual(["seed-1"]);

  const store = seenHandle!.store("issues") as unknown as Record<string, unknown>;
  expect(typeof store.put).toBe("function");
  expect("add" in store).toBe(false);
});

test("ledger ahead of code fails closed", async () => {
  const db = fakeDB();
  await runMigrations(asDB(db), {
    revisions: [issuesStore.revisions[0]!, { id: "0002_extra", up: async () => {} }],
  });

  await expect(
    runMigrations(asDB(db), issuesStore),
  ).rejects.toThrow(/ledger is ahead of code/);
});

test("declarative drop removes a store", async () => {
  const db = fakeDB();
  await runMigrations(asDB(db), issuesStore);

  await runMigrations(asDB(db), {
    revisions: [
      issuesStore.revisions[0]!,
      { id: "0002_drop", schema: { drop: { stores: ["issues"] } } },
    ],
  });

  expect(db.stores.has("issues")).toBe(false);
});

test("rejects a duplicate revision id", async () => {
  const db = fakeDB();
  await expect(
    runMigrations(asDB(db), {
      revisions: [
        { id: "dup", up: async () => {} },
        { id: "dup", up: async () => {} },
      ],
    }),
  ).rejects.toThrow(/duplicate revision id/);
});

test("rejects a revision that is neither schema nor imperative", async () => {
  const db = fakeDB();
  await expect(
    runMigrations(asDB(db), { revisions: [{ id: "empty" }] }),
  ).rejects.toThrow(/exactly one of/);
});

test("rejects a revision that is both schema and imperative", async () => {
  const db = fakeDB();
  await expect(
    runMigrations(asDB(db), {
      revisions: [
        { id: "both", schema: { stores: [] }, up: async () => {} },
      ],
    }),
  ).rejects.toThrow(/exactly one of/);
});
