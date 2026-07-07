import { describe, expect, test } from "bun:test";

import type { IndexedDB, Revision } from "../src/index.ts";
import {
  IdempotencyError,
  MemoryIndexedDB,
  verifyAllRevisions,
  verifyRevisionIdempotent,
} from "../src/testing/index.ts";

const schemaRevision: Revision = {
  id: "0001_issues",
  schema: {
    stores: [{ name: "issues", columns: [{ name: "id", primaryKey: true }] }],
  },
};

describe("verifyRevisionIdempotent", () => {
  test("passes for a declarative schema revision", async () => {
    await verifyRevisionIdempotent(schemaRevision);
  });

  test("passes for a convergent in-place backfill", async () => {
    const backfill: Revision = {
      id: "0002_default_status",
      backfill: {
        from: "issues",
        into: "issues",
        value: (row) => ({ ...row, status: row.status ?? "open" }),
      },
    };
    const seed = async (db: IndexedDB) => {
      await db.createObjectStore("issues", {
        columns: [{ name: "id", primaryKey: true }],
      });
      await db.objectStore("issues").put({ id: "a" });
      await db.objectStore("issues").put({ id: "b", status: "closed" });
    };

    await verifyRevisionIdempotent(backfill, seed);
  });

  test("passes for a copy backfill into another store", async () => {
    const backfill: Revision = {
      id: "0002_index",
      backfill: {
        from: "issues",
        into: "issue_index",
        value: (row) => ({ id: row.id, text: `issue-${String(row.id)}` }),
      },
    };
    const seed = async (db: IndexedDB) => {
      await db.createObjectStore("issues", {
        columns: [{ name: "id", primaryKey: true }],
      });
      await db.createObjectStore("issue_index", {
        columns: [{ name: "id", primaryKey: true }],
      });
      await db.objectStore("issues").put({ id: "a" });
    };

    await verifyRevisionIdempotent(backfill, seed);
  });

  test("rejects a non-convergent in-place backfill (append accumulates on replay)", async () => {
    const drift: Revision = {
      id: "0002_append",
      backfill: {
        from: "items",
        into: "items",
        value: (row) => ({
          ...row,
          seen: [...((row.seen as unknown[]) ?? []), "x"],
        }),
      },
    };
    const seed = async (db: IndexedDB) => {
      await db.createObjectStore("items", {
        columns: [{ name: "id", primaryKey: true }],
      });
      await db.objectStore("items").put({ id: "a" });
    };

    await expect(verifyRevisionIdempotent(drift, seed)).rejects.toThrow(
      IdempotencyError,
    );
  });
});

describe("verifyAllRevisions", () => {
  test("passes for a schema + backfill chain", async () => {
    const migrations: Revision[] = [
      schemaRevision,
      {
        id: "0002_default_status",
        backfill: {
          from: "issues",
          into: "issues",
          value: (row) => ({ ...row, status: row.status ?? "open" }),
        },
      },
    ];

    await verifyAllRevisions(migrations, {
      seeds: {
        "0002_default_status": async (db) => {
          await db.objectStore("issues").put({ id: "a" });
        },
      },
    });
  });

  test("accepts the { revisions } option shape", async () => {
    await verifyAllRevisions({ revisions: [schemaRevision] });
  });

  test("rejects a chain containing a non-convergent backfill", async () => {
    const migrations: Revision[] = [
      {
        id: "0001_items",
        schema: {
          stores: [{ name: "items", columns: [{ name: "id", primaryKey: true }] }],
        },
      },
      {
        id: "0002_append",
        backfill: {
          from: "items",
          into: "items",
          value: (row) => ({
            ...row,
            seen: [...((row.seen as unknown[]) ?? []), "x"],
          }),
        },
      },
    ];

    await expect(
      verifyAllRevisions(migrations, {
        seeds: {
          "0002_append": async (db) => {
            await db.objectStore("items").put({ id: "a" });
          },
        },
      }),
    ).rejects.toThrow(IdempotencyError);
  });
});

describe("MemoryIndexedDB", () => {
  test("streams rows through a cursor and dumps state", async () => {
    const db = new MemoryIndexedDB();
    await db.createObjectStore("s", { columns: [{ name: "id", primaryKey: true }] });
    await db.objectStore("s").put({ id: "a", n: 1 });
    await db.objectStore("s").put({ id: "b", n: 2 });

    const seen: string[] = [];
    const cursor = await db.objectStore("s").openCursor();
    while (cursor && (await cursor.continue())) {
      seen.push(cursor.primaryKey);
    }
    expect(seen).toEqual(["a", "b"]);

    const dump = db.dump();
    expect(Object.keys(dump)).toEqual(["s"]);
    expect(dump.s?.rows.length).toBe(2);
  });

  test("clone is an independent copy", async () => {
    const db = new MemoryIndexedDB();
    await db.createObjectStore("s", { columns: [{ name: "id", primaryKey: true }] });
    await db.objectStore("s").put({ id: "a" });

    const copy = db.clone();
    await copy.objectStore("s").put({ id: "b" });

    expect((await db.objectStore("s").getAllKeys()).sort()).toEqual(["a"]);
    expect((await copy.objectStore("s").getAllKeys()).sort()).toEqual(["a", "b"]);
  });
});
