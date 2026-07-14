import { expect, mock, test } from "bun:test";

import * as gestalt from "../src/index.ts";

type FakeRow = Record<string, unknown>;

const fakeStores = new Map<string, Map<string, FakeRow>>();

class FakeObjectStore {
  constructor(private readonly name: string) {}

  async get(id: string): Promise<FakeRow> {
    const row = fakeStores.get(this.name)?.get(id);
    if (!row) throw new gestalt.NotFoundError(`record ${id} not found`);
    return structuredClone(row);
  }

  async put(row: FakeRow): Promise<void> {
    const store = fakeStores.get(this.name) ?? new Map<string, FakeRow>();
    fakeStores.set(this.name, store);
    store.set(String(row.revision_id ?? row.id), structuredClone(row));
  }

  async delete(): Promise<void> {}

  async clear(): Promise<void> {
    fakeStores.get(this.name)?.clear();
  }

  async getAll(): Promise<FakeRow[]> {
    return [...(fakeStores.get(this.name)?.values() ?? [])].map((row) =>
      structuredClone(row),
    );
  }

  async getAllKeys(): Promise<string[]> {
    return [...(fakeStores.get(this.name)?.keys() ?? [])];
  }

  async openCursor() {
    return {
      value: undefined,
      async continue() {
        return false;
      },
      close() {},
    };
  }
}

class FakeIndexedDB {
  async createObjectStore(name: string): Promise<void> {
    if (!fakeStores.has(name)) {
      fakeStores.set(name, new Map());
    }
  }

  async deleteObjectStore(): Promise<void> {}

  objectStore(name: string): FakeObjectStore {
    if (!fakeStores.has(name)) {
      fakeStores.set(name, new Map());
    }
    return new FakeObjectStore(name);
  }

  close(): void {}
}

mock.module("../src/providers/indexeddb.ts", () => ({
  IndexedDB: FakeIndexedDB,
}));

class MigrationProvider extends gestalt.ProviderBase {
  readonly kind = "integration" as const;

  constructor(migrations: gestalt.Revision[]) {
    super({ migrations });
  }
}

test("configureProvider derives a per-provider migration ledger store", async () => {
  fakeStores.clear();
  const provider = new MigrationProvider([
    {
      id: "gIssues/0001_init",
      schema: {
        stores: [{ name: "widgets", columns: [{ name: "id", primaryKey: true }] }],
      },
    },
  ]);

  await provider.configureProvider("gIssues", { indexeddb: "main-db" });

  expect([...(fakeStores.get("g_issues_migrations")?.keys() ?? [])]).toEqual([
    "gIssues/0001_init",
  ]);
  expect(fakeStores.has("_gestalt_migrations")).toBe(false);
});

test("configureProvider keeps an explicit migration ledger override", async () => {
  fakeStores.clear();
  class CustomLedgerProvider extends gestalt.ProviderBase {
    readonly kind = "integration" as const;

    constructor() {
      super({
        migrations: {
          revisions: [
            {
              id: "gIssues/0001_init",
              schema: {
                stores: [{ name: "widgets", columns: [{ name: "id", primaryKey: true }] }],
              },
            },
          ],
          ledgerStore: "custom_migrations",
        },
      });
    }
  }

  const provider = new CustomLedgerProvider();
  await provider.configureProvider("gIssues", { indexeddb: "main-db" });

  expect([...(fakeStores.get("custom_migrations")?.keys() ?? [])]).toEqual([
    "gIssues/0001_init",
  ]);
  expect(fakeStores.has("g_issues_migrations")).toBe(false);
});
